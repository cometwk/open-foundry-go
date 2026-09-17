package serve

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func ServeHTTP2(ctx context.Context, route func(*http.ServeMux)) {
	mux := http.NewServeMux()

	// 1. 注册路由 Handler (支持 Go 1.22+ 的 HTTP 方法前缀语法)
	route(mux)

	// TODO: 打印全部路由

	// 2. 将系统退出信号转换为 Context 的 Done 事件
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 3. 配置 http.Server 参数（生产环境切忌使用 http.ListenAndServe，避免连接泄露与超时隐患）
	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,               // 传入我们的 ServeMux
		ReadTimeout:  5 * time.Second,   // 读取请求头/请求体的超时时间
		WriteTimeout: 10 * time.Second,  // 写入响应的超时时间
		IdleTimeout:  120 * time.Second, // 长连接空闲超时
	}

	// 4. 后台监听 Context，收到停机信号后触发 Shutdown
	go func() {
		<-ctx.Done()
		slog.Info("shutting down server...")

		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shCtx); err != nil {
			slog.Error("error shutting down server", "err", err)
		}
	}()

	// 5. 阻塞启动 HTTP 服务
	slog.Info("listening", "addr", server.Addr)
	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		slog.Info("server exited gracefully")
		return
	}
	if err != nil {
		slog.Error("error starting server", "err", err)
	}
}
