// Package main is the entry point for the CloudGate API Gateway.
// It wires together all internal components and starts the HTTP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/aryaMehta26/cloudgate/internal/audit"
	"github.com/aryaMehta26/cloudgate/internal/config"
	"github.com/aryaMehta26/cloudgate/internal/metrics"
	"github.com/aryaMehta26/cloudgate/internal/middleware"
	"github.com/aryaMehta26/cloudgate/internal/router"
	"github.com/aryaMehta26/cloudgate/pkg/cassandra"
	"github.com/aryaMehta26/cloudgate/pkg/redis"
)

// Server timeouts. ReadHeaderTimeout bounds slow-header (Slowloris) clients.
const (
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 120 * time.Second
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 30 * time.Second
)

func main() {
	log, _ := zap.NewProduction()
	// run returns instead of calling log.Fatal, so its deferred cleanup (audit
	// drain, Redis/Cassandra close) always runs before the process exits.
	if err := run(log); err != nil {
		log.Error("gateway exited with error", zap.Error(err))
		_ = log.Sync()
		os.Exit(1)
	}
	_ = log.Sync()
}

func run(log *zap.Logger) error {
	// ── Config ────────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// ── Prometheus metrics ────────────────────────────────────────────────────
	reg := metrics.NewRegistry()

	// ── Redis ─────────────────────────────────────────────────────────────────
	rdb, err := redis.New(cfg.Redis)
	if err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	defer rdb.Close()

	// ── Cassandra ─────────────────────────────────────────────────────────────
	cass, err := cassandra.New(cfg.Cassandra)
	if err != nil {
		return fmt.Errorf("connect to cassandra: %w", err)
	}
	defer cass.Close()

	// ── Audit writer ──────────────────────────────────────────────────────────
	auditor := audit.New(cass, log)
	auditor.Start()
	defer auditor.Stop()

	// ── Router / reverse proxy ────────────────────────────────────────────────
	mux, err := router.New(cfg, rdb, reg, auditor, log)
	if err != nil {
		return fmt.Errorf("invalid tenant configuration: %w", err)
	}

	// Apply policy changes from the admin API on every replica.
	watchCtx, stopWatch := context.WithCancel(context.Background())
	defer stopWatch()
	go func() {
		if werr := mux.WatchPolicyUpdates(watchCtx, nil); werr != nil {
			log.Error("policy update watcher stopped", zap.Error(werr))
		}
	}()

	// ── Middleware chain ──────────────────────────────────────────────────────
	handler := middleware.Chain(
		mux,
		middleware.RequestID(),
		middleware.Logger(log),
		middleware.Recovery(log),
		middleware.Metrics(reg),
	)

	// ── HTTP servers ──────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           handler,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	metricsSrv := &http.Server{
		Addr:              cfg.Server.MetricsAddr,
		Handler:           reg.Handler(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	serveErr := make(chan error, 2) //nolint:mnd // one slot per server
	serve := func(name string, s *http.Server) {
		log.Info(name+" listening", zap.String("addr", s.Addr))
		if serr := s.ListenAndServe(); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("%s: %w", name, serr)
		}
	}
	go serve("metrics server", metricsSrv)
	go serve("gateway", srv)

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	var runErr error
	select {
	case <-quit:
		log.Info("shutting down gracefully...")
	case runErr = <-serveErr:
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("gateway shutdown error", zap.Error(err))
	}
	if err := metricsSrv.Shutdown(ctx); err != nil {
		log.Error("metrics server shutdown error", zap.Error(err))
	}
	log.Info("shutdown complete")
	return runErr
}
