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

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewChiRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Logger)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("welcome"))
	})

	return r
}

func ServeHTTP(ctx context.Context, srv *http.Server) error {
	r, ok := srv.Handler.(*chi.Mux)
	if !ok {
		return fmt.Errorf("http server handler is not a chi.Mux")
	}

	if srv.Addr == "" {
		srv.Addr = ":4000"
	}

	// srv := &http.Server{
	// 	Addr:    addr,
	// 	Handler: r,
	// }

	// --------------------------------------------------
	// 核心：使用 chi.Walk 遍历并打印所有路由
	// --------------------------------------------------
	walkFunc := func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		slog.Info(fmt.Sprintf("[%s] %s", method, route))
		return nil
	}

	slog.Info("=== Chi Registered Routes ===")
	if err := chi.Walk(r, walkFunc); err != nil {
		return fmt.Errorf("failed to walk routes: %w", err)
	}

	// 启动 HTTP Server
	go func() {
		slog.Info("HTTP server listening on", "addr", srv.Addr)

		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("failed to listen and serve", "error", err)
			return
		}
	}()

	// 等待 SIGINT / SIGTERM
	sigCtx, stop := signal.NotifyContext(
		ctx,
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	<-sigCtx.Done()

	slog.Info("shutting down HTTP server...")

	// 最多等待 10 秒，让正在处理的请求完成
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
