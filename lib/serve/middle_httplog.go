package serve

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/mssola/user_agent"
	"github.com/openfoundry/lib/env"
	"github.com/openfoundry/lib/xlog"
)

func readRequestBody(c *echo.Context) (string, error) {
	req := c.Request()
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return "", err
	}
	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	return string(bodyBytes), nil
}

// route 应用的开始！
func httpLogMiddleware() echo.MiddlewareFunc {
	return simpleRequestLogger()
}

func simpleRequestLogger() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			// 请求开始前记录时间
			start := time.Now()
			req := c.Request()
			resWriter := c.Response()
			res, err := echo.UnwrapResponse(resWriter)
			if err != nil {
				return err
			}

			// 读取 context 中的 reqid
			reqid := c.Response().Header().Get(echo.HeaderXRequestID)
			ua := user_agent.New(req.UserAgent())

			osinfo := ua.OSInfo()
			os := fmt.Sprintf("%s %s", osinfo.Name, osinfo.Version)

			name, version := ua.Browser()
			browser := fmt.Sprintf("%s %s", name, version)

			// 设置context属性
			ctx := xlog.WithRequestID(req.Context(), reqid)
			// ctx = log.WithFeature(ctx, c.Path())
			req = req.WithContext(ctx)
			c.SetRequest(req)

			var requestBody string

			mimetype := req.Header.Get("content-type")
			if !strings.HasPrefix(mimetype, echo.MIMEMultipartForm) {
				if env.IsDebug() {
					// 如果请求体过大，会导致日志输出不全
					requestBody, _ = readRequestBody(c)
				}
			}

			attrs := []slog.Attr{
				slog.String("module", "httplog"),
				slog.String("user_agent", req.UserAgent()),
				slog.String("browser", browser),
				slog.String("os", os),
				slog.String("referer", req.Referer()),
				slog.String("host", req.Host),
				slog.String("ip", c.RealIP()),
				slog.String("bytes_in", req.Header.Get(echo.HeaderContentLength)),
				slog.String("request_body", requestBody),
			}

			// dump request header with startWith x- or X-
			for k := range req.Header {
				if strings.HasPrefix(k, "x-") || strings.HasPrefix(k, "X-") {
					attrs = append(attrs, slog.String(k, req.Header.Get(k)))
				}
			}

			// logger := log.FromCtx(ctx)
			// logger.LogAttrs(ctx, slog.LevelInfo,
			// 	fmt.Sprintf("%s %s %s", req.Method, req.Proto, req.RequestURI),
			// 	attrs...)
			slog.LogAttrs(ctx, slog.LevelInfo,
				fmt.Sprintf("%s %s %s", req.Method, req.Proto, req.RequestURI),
				attrs...)

			// 执行下一个处理函数
			err = next(c)

			latency := time.Since(start).Milliseconds()
			latencyHuman := fmt.Sprintf("%dms", latency)
			fields := []slog.Attr{
				slog.String("module", "httplog"),
				slog.Int64("latency", latency),
				slog.String("latency_human", latencyHuman),
				slog.String("protocol", req.Proto),
				slog.String("ip", c.RealIP()),
				slog.String("host", req.Host),
				slog.String("method", req.Method),
				slog.String("uri", req.RequestURI),
				slog.String("path", req.URL.Path),
				slog.String("route", c.Path()),
				slog.String("reqid", reqid),
				slog.String("referer", req.Referer()),
				slog.String("browser", browser),
				slog.String("os", os),
				slog.Int("status", res.Status),
				slog.String("bytes_in", req.Header.Get(echo.HeaderContentLength)),
				slog.Int64("bytes_out", res.Size),
			}

			// 如果发生错误，记录错误信息
			if err != nil {
				fields = append(fields, slog.String("error", err.Error()))
				if he, ok := err.(*echo.HTTPError); ok {
					fields = append(fields, slog.Int("status", he.Code))
				}
			}

			slog.LogAttrs(ctx, slog.LevelInfo,
				fmt.Sprintf("%d %s", res.Status, req.RequestURI),
				fields...)

			return err

		}
	}
}
