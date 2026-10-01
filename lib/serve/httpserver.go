package serve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/lib/util"
)

func NewEcho() *echo.Echo {
	e := echo.New()

	// 错误处理
	e.HTTPErrorHandler = createhttpErrorHandler(e)

	// JSON 校验
	e.Validator = NewCustomValidator()
	e.Binder = &customBinder{}

	// 基础中间件
	e.Use(middleware.Recover())
	e.Use(middleware.RequestIDWithConfig(middleware.RequestIDConfig{
		Generator: func() string {
			return util.NextId("W") // W0000 = WEB跟踪号, 0000 = 业务流水号
		},
	}))
	e.Use(middleware.BodyLimit(10 * 1024 * 1024)) // 限制请求报文大小
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		// 允许任意来源跨域访问
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{
			echo.HeaderOrigin,
			echo.HeaderContentType,
			echo.HeaderAccept,
			"X-OpenFoundry-Tenant", // 如果包含自定义 Header，记得在这里加上
		},
	}))

	// HTTP 日志
	// e.Use(middleware.RequestLogger())
	e.Use(httpLogMiddleware())
	if env.IsDev() {
		e.Use(dumpMiddleware) // 开发日志
	}

	e.GET("/", func(c *echo.Context) error {
		slog.InfoContext(c.Request().Context(), "welcome")
		return c.String(http.StatusOK, "welcome")
	})

	return e
}

func ServeHTTP(ctx context.Context, srv *http.Server) error {
	e, ok := srv.Handler.(*echo.Echo)
	if !ok {
		return fmt.Errorf("http server handler is not an echo.Echo")
	}

	if srv.Addr == "" {
		srv.Addr = ":4000"
	}

	slog.Info("=== Echo Registered Routes ===")
	for _, r := range e.Router().Routes() {
		slog.Info(fmt.Sprintf("[%s] %s", r.Method, r.Path))
	}

	go func() {
		slog.Info("HTTP server listening on", "addr", srv.Addr)

		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("failed to listen and serve", "error", err)
			return
		}
	}()

	sigCtx, stop := signal.NotifyContext(
		ctx,
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	<-sigCtx.Done()

	slog.Info("shutting down HTTP server...")

	shutdownCtx, cancel := context.WithTimeout(
		ctx,
		10*time.Second,
	)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP server shutdown", "error", err)
	}

	slog.Info("HTTP server stopped")

	return nil
}

func createhttpErrorHandler(e *echo.Echo) echo.HTTPErrorHandler {
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
