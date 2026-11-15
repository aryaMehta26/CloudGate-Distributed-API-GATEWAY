// Package main is the entry point for the CloudGate gRPC Admin Service.
// It exposes APIs for hot-reloading tenant policies without a gateway restart.
package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/aryaMehta26/cloudgate/internal/config"
	"github.com/aryaMehta26/cloudgate/internal/tenant"
	"github.com/aryaMehta26/cloudgate/pkg/redis"
	adminpb "github.com/aryaMehta26/cloudgate/proto/admin"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	log, _ := zap.NewProduction()
	defer log.Sync() //nolint:errcheck

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("failed to load config", zap.Error(err))
	}

	rdb, err := redis.New(cfg.Redis)
	if err != nil {
		log.Fatal("failed to connect to redis", zap.Error(err))
	}
	defer rdb.Close()

	store := tenant.NewRedisStore(rdb, log)

	lis, err := net.Listen("tcp", cfg.Admin.GRPCAddr)
	if err != nil {
		log.Fatal("failed to listen", zap.Error(err), zap.String("addr", cfg.Admin.GRPCAddr))
	}

	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(loggingInterceptor(log)),
	)
	adminpb.RegisterAdminServiceServer(grpcSrv, tenant.NewAdminServer(store, log))
	reflection.Register(grpcSrv)

	go func() {
		log.Info("admin gRPC server listening", zap.String("addr", cfg.Admin.GRPCAddr))
		if err := grpcSrv.Serve(lis); err != nil {
			log.Error("grpc server error", zap.Error(err))
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("stopping admin gRPC server...")
	grpcSrv.GracefulStop()
	log.Info("admin server stopped")
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
