package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/teatak/pudding-relay/internal/httpserver"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address / HTTP 监听地址")
	showVersion := flag.Bool("version", false, "Print build version / 显示构建版本")
	flag.Parse()
	if flag.NArg() != 0 {
		slog.Error("unexpected positional arguments")
		os.Exit(2)
	}
	if *showVersion {
		fmt.Printf("pudding-relay %s (%s)\n", version, commit)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := serve(ctx, *listen); err != nil {
		slog.Error("relay stopped", "error", err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, address string) error {
	server := &http.Server{
		Addr:              address,
		Handler:           httpserver.NewHandler(httpserver.BuildInfo{Version: version, Commit: commit}),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	result := make(chan error, 1)
	go func() {
		result <- server.ListenAndServe()
	}()

	select {
	case err := <-result:
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown: %w", err)
		}
		if err := <-result; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	}
}
