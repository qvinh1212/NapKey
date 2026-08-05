// Package main provides the entry point for Kiro API Proxy.
//
// Kiro API Proxy is a reverse proxy service that translates Kiro API requests
// into OpenAI and Anthropic (Claude) compatible formats. Key features include:
//   - Multi-account pool with round-robin load balancing
//   - Automatic OAuth token refresh
//   - Streaming response support for real-time AI interactions
//   - Admin panel for account and configuration management
//
// The service exposes the following endpoints:
//   - /v1/messages - Claude API compatible endpoint
//   - /v1/chat/completions - OpenAI API compatible endpoint
//   - /admin - Web-based administration panel
package main

import (
	"context"
	"errors"
	"fmt"
	"kiro-go/config"
	"kiro-go/logger"
	"kiro-go/pool"
	"kiro-go/proxy"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	// 配置文件路径，支持环境变量覆盖
	configPath := "data/config.json"
	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		configPath = envPath
	}

	// 确保数据目录存在
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// 加载配置
	if err := config.Init(configPath); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize log level: LOG_LEVEL env var takes priority over config, defaulting to "info".
	logger.Init(config.GetLogLevel())

	// 环境变量覆盖密码
	if envPassword := os.Getenv("ADMIN_PASSWORD"); envPassword != "" {
		config.SetPassword(envPassword)
	}

	// A password generated on first run (or minted to replace the old hardcoded
	// default) exists only inside data/config.json, so print it once here: it is
	// the operator's only chance to read it without opening the file. Set
	// ADMIN_PASSWORD to keep it out of the logs entirely.
	if pw := config.TakeGeneratedPassword(); pw != "" {
		logger.Warnf("Admin password generated: %s (stored in %s; set ADMIN_PASSWORD to override)", pw, configPath)
	}

	// 初始化账号池
	pool.GetPool()

	// Resolve the upstream before binding a port. A missing 9Router endpoint or key,
	// or an empty account pool with 9Router switched off, means no request can be
	// served: failing the deploy here surfaces it in the deploy log instead of as a
	// customer-facing outage discovered one refused request at a time.
	upstream, err := proxy.DescribeUpstream()
	if err != nil {
		logger.Fatalf("No usable upstream: %v", err)
	}
	logger.Infof("Upstream: %s", upstream)

	// 创建 HTTP 处理器（包含后台刷新任务）
	handler := proxy.NewHandler()

	// Start the usage reporter here rather than lazily on the first billable
	// request, so the operator sees at boot whether usage is being forwarded to the
	// control plane instead of inferring it later from an absence of data.
	usageReporter := proxy.UsageReporter()
	if usageReporter == nil {
		logger.Infof("Usage reporting is off (set NAPKEY_CORE_URL and NAPKEY_INTERNAL_TOKEN to enable)")
	}

	// 启动服务器
	addr := fmt.Sprintf("%s:%d", config.GetHost(), config.GetPort())
	logger.Infof("Kiro-Go starting on http://%s (log level: %s)", addr, logger.LevelName(logger.GetLevel()))
	logger.Infof("Admin panel: http://%s/admin", addr)
	logger.Infof("Claude API: http://%s/v1/messages", addr)
	logger.Infof("OpenAI API: http://%s/v1/chat/completions", addr)

	// WriteTimeout intentionally 0: SSE streams can run for minutes while the
	// upstream model produces tokens. ReadHeaderTimeout + ReadTimeout still
	// guard against slowloris-style header/body stalls.
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Shut down on a signal instead of being killed mid-flight. Beyond tidiness:
	// queued usage reports describe traffic that was already served, so dropping
	// them on every deploy means billing less than was delivered.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			logger.Fatalf("Server failed: %v", err)
		}
		return
	case sig := <-shutdown:
		logger.Infof("Received %s, draining connections", sig)
	}

	// Streams can be long-lived, so give in-flight requests room to finish.
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelDrain()
	if err := srv.Shutdown(drainCtx); err != nil {
		logger.Warnf("Graceful shutdown timed out: %v", err)
	}

	// Flush usage last: a report is only queued once its request has finished, so
	// the queue is not final until the server has drained.
	flushCtx, cancelFlush := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelFlush()
	usageReporter.Shutdown(flushCtx)

	logger.Infof("Kiro-Go stopped")
}
