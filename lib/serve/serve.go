package serve

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"

	"net/http"
	"os"
	"os/signal"
	"path"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/lib/orm"
	"github.com/openfoundry/lib/util"
	"github.com/openfoundry/lib/xlog"
)

type EchoServer struct {
	engine        *echo.Echo
	web_directory string
	webFS         fs.FS
	initFunc      func(e *echo.Echo) error
	slogHandlers  []slog.Handler
}

type Option func(*EchoServer)

func WithInit(init func(e *echo.Echo) error) Option {
	return func(s *EchoServer) {
		s.initFunc = init
	}
}

func WithWebFS(webFS fs.FS) Option {
	return func(s *EchoServer) {
		s.webFS = webFS
	}
}

func WithSlogHandlers(handlers ...slog.Handler) Option {
	return func(s *EchoServer) {
		s.slogHandlers = handlers
	}
}

func NewEchoServer(opts ...Option) *EchoServer {
	s := &EchoServer{
		engine: echo.New(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (e *EchoServer) Start() {
	debug := env.IsDebug()
	dev := env.IsDev()
	fmt.Printf("DEBUG: %v\n", debug)
	fmt.Printf("DEV: %v\n", dev)

	// reboot:
	// 集群主机ID设置
	// utils.SetHostId(config.CLUSTER)

	// 确保日志文件存在
	logdir := env.DirPath("LOG_DIR", "./log")
	if err := os.MkdirAll(logdir, 0o755); err != nil {
		slog.Error(fmt.Sprintf("创建目录 '%s' 错: %v", logdir, err))
		return
	}

	// 临时文件目录
	tmpdir := env.DirPath("TMP_DIR", "./tmp")
	if err := os.MkdirAll(tmpdir, 0o755); err != nil {
		slog.Error(fmt.Sprintf("创建临时目录 '%s' 错: %v", tmpdir, err))
		return
	}

	// 设置 slog
	if env.IsDebug() {
		// 输出日志到终端,方便调试
		xlog.InitDebug()
	}

	// 输出日志到文件
	logfile := env.String("LOG_FILE", "main.log")
	rotateHanlder := xlog.NewRotateFileHandler(path.Join(logdir, logfile))
	multiHandler := slog.NewMultiHandler(append(e.slogHandlers, rotateHanlder)...)
	logger := slog.New(multiHandler)
	slog.SetDefault(logger)

	level := env.String("LOG_LEVEL", "debug")
	switch level {
	case "debug":
		xlog.SetLevel(slog.LevelDebug)
	case "info":
		xlog.SetLevel(slog.LevelInfo)
	case "warn":
		xlog.SetLevel(slog.LevelWarn)
	case "error":
		xlog.SetLevel(slog.LevelError)
	default:
		xlog.SetLevel(slog.LevelInfo)
	}

	// orm 日志
	orm.SetLogger(logger.With(
		slog.String("reqid", "abc"),
		slog.String("module", "orm"),
	))

	// HTTP 服务器
	engine := e.engine
	engine.HTTPErrorHandler = e.createhttpErrorHandler()
	// engine.Logger = slog.New(log.NewDebugHandler())
	engine.Logger = logger.With(slog.String("module", "echo"))

	// 基础中间件
	engine.Use(middleware.Recover())
	engine.Use(middleware.RequestIDWithConfig(middleware.RequestIDConfig{
		Generator: func() string {
			return util.NextId("W") // W0000 = WEB跟踪号, 0000 = 业务流水号
		},
	}))
	engine.Use(middleware.BodyLimit(10 * 1024 * 1024)) // 限制请求报文大小

	// 自定义 middleware
	// engine.Use(sessionMiddleware())
	engine.Use(httpLogMiddleware()) // 设置 HTTP 日志
	if dev {
		engine.Use(dumpMiddleware) // 开发日志
	}

	// JSON 校验
	engine.Validator = NewCustomValidator()
	engine.Binder = &customBinder{}

	// Route => handler
	// engine.GET("/", HelloWorld)

	// // 使用 Static 中间件 (HTML5: true 是关键)
	// // 它的逻辑是：
	// // - 如果请求的文件存在（例如 /assets/app.js），就返回文件。
	// // - 如果文件不存在（例如 /user/123），就自动返回 index.html。
	// engine.Use(middleware.StaticWithConfig(middleware.StaticConfig{
	// 	Root:       "web/dist",       // 根目录是 web/dist
	// 	HTML5:      true,             // ✅ 开启 SPA 模式 (自动 fallback 到 index.html)
	// 	Filesystem: http.FS(e.webFS), // 适配 embed

	// 	// 🔥 关键点：跳过 /api 开头的请求
	// 	// 这样 /api/xxx 就会透传给下面的路由，而不是被当做 SPA 返回 index.html
	// 	Skipper: func(c *echo.Context) bool {
	// 		path := c.Path()
	// 		return strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/admin")
	// 	},
	// }))

	// 速率限制
	rlconfig := middleware.DefaultRateLimiterConfig
	rlconfig.Store = middleware.NewRateLimiterMemoryStore(20)
	rlconfig.Skipper = func(c *echo.Context) bool {
		// GET 方法不限制
		return c.Request().Method == http.MethodGet
	}
	engine.Use(middleware.RateLimiterWithConfig(rlconfig))
	// engine.Use(auth.Authentication)
	// engine.Use(ops.Recorder)

	if e.initFunc != nil {
		if err := e.initFunc(engine); err != nil {
			slog.Error("初始化失败", "err", err)
			return
		}
	}

	// 打印所有的bean
	{
		// bean.PrintBeans()
	}

	// 打印所有路由
	if env.IsDev() {
		routes := engine.Router().Routes()

		sort.SliceStable(routes, func(i, j int) bool {
			return routes[i].Path < routes[j].Path
		})
		sb := strings.Builder{}

		for i, v := range routes {
			if v.Method == "echo_route_not_found" {
				// 打印这个时，太乱
				continue
			}

			arr := strings.Split(v.Name, "/")
			fn := arr[len(arr)-1]
			sb.WriteString(
				fmt.Sprintf("\n%4d %-6s %-42s %s", i, v.Method, v.Path, fn),
			)
		}
		fmt.Printf("%s\n", sb.String())
	}

	// 在 goroutine 中启动服务器，这样主 goroutine 不会阻塞
	startup(engine)
}

// 在单独的 goroutine 中启动 http 服务
func startup(engine *echo.Echo) {
	bind := env.String("HOST", "") + ":" + env.String("PORT", "4444")
	secure := false

	if len(bind) == 0 {
		if secure {
			bind = ":https"
		} else {
			bind = ":http"
		}
	}

	// XXX
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP,
	)
	defer stop()

	sc := echo.StartConfig{
		HideBanner:      true,
		Address:         bind,
		GracefulTimeout: 10 * time.Second,
		BeforeServeFunc: func(s *http.Server) error {
			slog.Info(fmt.Sprintf("HTTP 服务 %d 准备就绪, 监听地址 %s", os.Getpid(), bind))
			return nil
		},
		OnShutdownError: func(err error) {
			slog.Error("server exited", "err", err)
		},
	}

	if err := sc.Start(ctx, engine); err != nil {
		slog.Error("server exited", "err", err)
	}
}

func (e *EchoServer) createhttpErrorHandler() echo.HTTPErrorHandler {
	// web_directory := e.web_directory
	// webfs := e.webFS

	// HTTP 错误处理
	httpErrorHandler := func(c *echo.Context, err error) {
		// log := logrus.WithField("reqid", "abc").WithField("id", "123").WithField("app", "demo")

		url := c.Request().URL.String()
		method := c.Request().Method

		// // 前端是使用客户端路由的 React 应用，为了支持用户从任意路径访问，例如 /some/place
		// // (/some/place 是客户端路由)，需要响应 index.html 而不是 404
		// if e, ok := err.(*echo.HTTPError); ok {
		// 	if (e.Code == 404 || e.Code == 405) && method == http.MethodGet {
		// 		accept := c.Request().Header["Accept"]
		// 		if len(accept) > 0 && strings.Contains(accept[0], "text/html") {
		// 			log.WithField("url", url).Infof("%s 未找到, 返回 index.html", url)
		// 			if webfs != nil {
		// 				content, err := fs.ReadFile(webfs, "web/index.html")
		// 				if err != nil {
		// 					logrus.Errorf("读 web/index.html 错: %v", err)
		// 					c.NoContent(http.StatusInternalServerError)
		// 					return
		// 				}
		// 				c.HTML(http.StatusOK, string(content))
		// 			} else {
		// 				c.Response().Status = http.StatusOK
		// 				c.File(path.Join(web_directory, "index.html"))
		// 			}
		// 			return
		// 		}
		// 	}
		// }

		reqid := c.Response().Header().Get(echo.HeaderXRequestID)
		// logger := log.FromCtx(c.Request().Context())
		// logger.Info(fmt.Sprintf("HTTP服务错误: url: %s, %v", url, err), "reqid", reqid, "url", url, "method", method, "error", err)
		slog.InfoContext(c.Request().Context(), fmt.Sprintf("HTTP服务错误: url: %s, %v", url, err), slog.String("reqid", reqid), slog.String("url", url), slog.String("method", method), slog.String("error", err.Error()))

		// 默认错误处理
		def := echo.DefaultHTTPErrorHandler(true)
		def(c, err)
	}
	return httpErrorHandler
}
