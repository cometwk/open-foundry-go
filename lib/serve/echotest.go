package serve

import (
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/openfoundry/lib/util"
)

func EchoTestSetup() *echo.Echo {
	engine := echo.New()

	// 基础中间件
	// engine.Use(middleware.Recover())
	engine.Use(middleware.RequestIDWithConfig(middleware.RequestIDConfig{
		Generator: func() string {
			return util.NextId("W") // W0000 = WEB跟踪号, Y0000 = 业务流水号
		},
	}))
	// engine.Use(middleware.BodyLimit("10M")) // 限制请求报文大小

	// 自定义 middleware
	engine.Use(sessionMiddleware())
	// engine.Use(httpLogMiddleware()) // 设置 HTTP 日志
	// engine.Use(dumpMiddleware) // 开发日志

	// JSON 校验
	engine.Validator = NewCustomValidator()
	return engine
}
