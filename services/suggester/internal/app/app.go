package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"gar-importer/internal/platform"
	"gar-importer/internal/search"
	suggestv1 "gar-importer/proto/suggest/v1"
	"gar-importer/services/suggester/internal/server"
)

// Параметры сервиса.
const (
	serviceName = "gar-suggester"
	// healthServiceName — имя сервиса в grpc_health_v1.
	healthServiceName = "suggest.v1.Suggester"
	// gracefulStopTimeout — предел graceful stop gRPC-сервера, после которого
	// соединения закрываются принудительно.
	gracefulStopTimeout = 10 * time.Second
)

// Run запускает suggester и работает до отмены контекста: gRPC-сервер
// подсказок (GRPCAddr) и HTTP-сервер проверок и метрик (HTTPAddr).
func Run(ctx context.Context, cfg Config) error {
	log := slog.Default()
	shutdown, err := platform.InitTracing(ctx, serviceName)
	if err != nil {
		return err
	}
	defer func() {
		// Родитель уже отменён: завершаем провайдер безотменным контекстом.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = shutdown(shutdownCtx)
	}()
	registry := platform.NewRegistry("suggester")
	grpcMetrics := server.NewMetrics(registry)

	searchClient, err := search.New(search.Config{Addresses: cfg.ESAddresses, Index: cfg.ESIndex})
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(grpcMetrics.UnaryServerInterceptor()),
		grpc.ChainStreamInterceptor(grpcMetrics.StreamServerInterceptor()),
	)
	suggestv1.RegisterSuggesterServer(grpcServer, server.New(searchClient, log))
	reflection.Register(grpcServer)
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus(healthServiceName, healthpb.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	listener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("suggester: gRPC-прослушиватель %s: %w", cfg.GRPCAddr, err)
	}
	grpcErr := make(chan error, 1)
	go func() {
		log.Info("gRPC-сервер запущен", "addr", listener.Addr().String())
		grpcErr <- grpcServer.Serve(listener)
	}()

	// Готовность — доступность поискового индекса.
	mux := platform.HealthMux(searchClient.Ping)
	mux.Handle("/metrics", registry.Handler())
	httpErr := make(chan error, 1)
	go func() { httpErr <- platform.RunHTTPServer(ctx, cfg.HTTPAddr, mux) }()

	log.Info("suggester запущен", "es_index", searchClient.IndexName())
	select {
	case err := <-grpcErr:
		if err != nil {
			return fmt.Errorf("suggester: gRPC-сервер: %w", err)
		}
		return nil
	case err := <-httpErr:
		return err
	case <-ctx.Done():
	}

	// Graceful stop: сначала завершаем gRPC, затем дожидаемся HTTP.
	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(gracefulStopTimeout):
		grpcServer.Stop()
		<-stopped
	}
	log.Info("gRPC-сервер остановлен")
	return <-httpErr
}
