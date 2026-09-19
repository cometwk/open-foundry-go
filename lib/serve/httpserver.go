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
)

func NewEcho() *echo.Echo {
	e := echo.New()
	e.Use(middleware.RequestLogger())
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

	e.GET("/", func(c *echo.Context) error {
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
