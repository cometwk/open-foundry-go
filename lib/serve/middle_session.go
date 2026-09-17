package serve

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/lib/serve/session"
	pkglog "github.com/openfoundry/lib/xlog"
)

func sessionMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {

			// 设置 request 中的 context 中的 reqid
			reqid := c.Response().Header().Get(echo.HeaderXRequestID)
			req := c.Request()
			ctx := pkglog.WithRequestID(req.Context(), reqid) // 保存 reqid 到 context
			ctx = session.WithSession(ctx, nil)               // TODO: 保存 session 到 context
			req = req.WithContext(ctx)
			c.SetRequest(req)

			now := time.Now()

			// c.Response().Before(func()
			{
				urlpath := req.URL.Path
				method := req.Method

				elapsed := time.Since(now).Seconds()

				// 如果处理请求超出 3 秒，记录一条警告
				if elapsed > 3 {
					slog.Warn(fmt.Sprintf("%s %s 耗时 %f 秒", method, urlpath, elapsed))
				} else if elapsed > 1 {
					// 如果处理请求超出 1 秒，记录一条信息
					if env.IsDev() {
						s := fmt.Sprintf("%s %s 耗时 %f 秒", method, urlpath, elapsed)
						m := fmt.Sprintf("IP: `%s`, ReqID: `%s`", c.RealIP(), reqid)
						// event.Add(event.LevelTodo, s, m)
						slog.Warn(fmt.Sprintf("%s %s", s, m))
					}
				}
				// 对于下列资源启用客户端缓存
				if c.Request().Method == http.MethodGet {
					if strings.HasPrefix(urlpath, "/static/js/") {
						c.Response().Header().Set("cache-control", "max-age=31536000")
					}
					if strings.HasPrefix(urlpath, "/static/media/") {
						c.Response().Header().Set("cache-control", "max-age=31536000")
					}
					if strings.HasPrefix(urlpath, "/static/css/") {
						c.Response().Header().Set("cache-control", "max-age=31536000")
					}
				}
			}

			return next(c)
		}
	}
}
