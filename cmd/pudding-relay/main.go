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
	"strings"
	"syscall"
	"time"

	"github.com/teatak/pudding-relay/internal/httpserver"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9623", "HTTP listen address / HTTP 监听地址")
	trustedProxies := flag.String("trusted-proxies", os.Getenv("PUDDING_RELAY_TRUSTED_PROXIES"), "Trusted reverse proxy CIDRs, comma-separated / 可信反向代理 CIDR，逗号分隔")
	dataFile := flag.String("data-file", "data/registrations.json", "Registration digest file / 登记摘要文件")
	assetsDir := flag.String("assets-dir", os.Getenv("PUDDING_RELAY_ASSETS_DIR"), "Mobile Web build directory / 手机 Web 构建目录")
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
	secretFile := os.Getenv("PUDDING_RELAY_ADMIN_SECRET_FILE")
	if secretFile == "" {
		slog.Error("PUDDING_RELAY_ADMIN_SECRET_FILE is required")
		os.Exit(1)
	}
	secret, err := os.ReadFile(secretFile)
	if err != nil {
		slog.Error("cannot read admin secret file")
		os.Exit(1)
	}
	store, err := httpserver.OpenStore(*dataFile)
	if err != nil {
		slog.Error("cannot open registration store")
		os.Exit(1)
	}
	cfg := httpserver.Config{TrustedProxies: strings.Split(*trustedProxies, ","), AdminSecret: strings.TrimSpace(string(secret)), Store: store, AssetsDir: *assetsDir}
	if err := serve(ctx, *listen, cfg); err != nil {
		slog.Error("relay stopped", "error", err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, address string, cfg httpserver.Config) error {
	relay, handler, err := httpserver.NewRelay(httpserver.BuildInfo{Version: version, Commit: commit}, cfg)
	if err != nil {
		return err
	}
	defer relay.Close()
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
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
		relay.Close()
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
