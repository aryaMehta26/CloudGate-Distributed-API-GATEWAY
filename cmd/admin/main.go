// Package main is the entry point for the CloudGate gRPC Admin Service.
// It exposes APIs for hot-reloading tenant policies without a gateway restart.
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/aryaMehta26/cloudgate/internal/config"
	"github.com/aryaMehta26/cloudgate/internal/tenant"
	"github.com/aryaMehta26/cloudgate/pkg/redis"
	adminpb "github.com/aryaMehta26/cloudgate/proto/admin"
)

func main() {
	log, _ := zap.NewProduction()
	// run returns instead of calling log.Fatal, so deferred cleanup runs.
	if err := run(log); err != nil {
		log.Error("admin server exited with error", zap.Error(err))
		_ = log.Sync()
		os.Exit(1)
	}
	_ = log.Sync()
}

func run(log *zap.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	rdb, err := redis.New(cfg.Redis)
	if err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	defer rdb.Close()

	store := tenant.NewRedisStore(rdb, log)

	lis, err := net.Listen("tcp", cfg.Admin.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Admin.GRPCAddr, err)
	}

	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(loggingInterceptor(log)),
	)
	adminpb.RegisterAdminServiceServer(grpcSrv, tenant.NewAdminServer(store, log))
	reflection.Register(grpcSrv)

	serveErr := make(chan error, 1)
	go func() {
		log.Info("admin gRPC server listening", zap.String("addr", cfg.Admin.GRPCAddr))
		serveErr <- grpcSrv.Serve(lis)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
	case err = <-serveErr:
		return fmt.Errorf("grpc server: %w", err)
	}

	log.Info("stopping admin gRPC server...")
	grpcSrv.GracefulStop()
	log.Info("admin server stopped")
	return nil
}

func loggingInterceptor(log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		log.Info("grpc call", zap.String("method", info.FullMethod))
		resp, err := handler(ctx, req)
		if err != nil {
			log.Error("grpc error", zap.String("method", info.FullMethod), zap.Error(err))
		}
		return resp, err
	}
}
