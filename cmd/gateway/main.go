// Package main is the entry point for the CloudGate API Gateway.
// It wires together all internal components and starts the HTTP server.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aryaMehta26/cloudgate/internal/audit"
	"github.com/aryaMehta26/cloudgate/internal/config"
	"github.com/aryaMehta26/cloudgate/internal/metrics"
	"github.com/aryaMehta26/cloudgate/internal/middleware"
	"github.com/aryaMehta26/cloudgate/internal/router"
	"github.com/aryaMehta26/cloudgate/pkg/cassandra"
	"github.com/aryaMehta26/cloudgate/pkg/redis"
	"go.uber.org/zap"
)

func main() {
	// ── Logger ────────────────────────────────────────────────────────────────
	log, _ := zap.NewProduction()
	defer log.Sync() //nolint:errcheck

	// ── Config ────────────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("failed to load config", zap.Error(err))
	}

	// ── Prometheus metrics ────────────────────────────────────────────────────
	reg := metrics.NewRegistry()

	// ── Redis ─────────────────────────────────────────────────────────────────
	rdb, err := redis.New(cfg.Redis)
	if err != nil {
		log.Fatal("failed to connect to redis", zap.Error(err))
	}
	defer rdb.Close()

	// ── Cassandra ─────────────────────────────────────────────────────────────
	cass, err := cassandra.New(cfg.Cassandra)
	if err != nil {
		log.Fatal("failed to connect to cassandra", zap.Error(err))
	}
	defer cass.Close()

	// ── Audit writer ──────────────────────────────────────────────────────────
	auditor := audit.New(cass, log)
	auditor.Start()
	defer auditor.Stop()

	// ── Router / reverse proxy ────────────────────────────────────────────────
	mux := router.New(cfg, rdb, reg, auditor, log)

	// ── Middleware chain ──────────────────────────────────────────────────────
	handler := middleware.Chain(
		mux,
		middleware.RequestID(),
		middleware.Logger(log),
		middleware.Recovery(log),
		middleware.Metrics(reg),
	)

	// ── HTTP server ───────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           handler,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ── Metrics server (separate port) ────────────────────────────────────────
	metricsSrv := &http.Server{
		Addr:    cfg.Server.MetricsAddr,
		Handler: reg.Handler(),
	}

	go func() {
		log.Info("metrics server listening", zap.String("addr", cfg.Server.MetricsAddr))
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("metrics server error", zap.Error(err))
		}
	}()

	go func() {
		log.Info("gateway listening", zap.String("addr", cfg.Server.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("gateway server error", zap.Error(err))
			os.Exit(1)
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error("gateway shutdown error", zap.Error(err))
	}
	if err := metricsSrv.Shutdown(ctx); err != nil {
		log.Error("metrics server shutdown error", zap.Error(err))
	}

	log.Info("shutdown complete")
}
