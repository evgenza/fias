package server

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"gar-importer/internal/platform"
)

// Metrics — метрики gRPC-сервера suggester: счётчик запросов и гистограмма
// длительности по методу и gRPC-коду ответа.
type Metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewMetrics создаёт и регистрирует метрики gRPC-сервера в реестре платформы.
func NewMetrics(registry *platform.Metrics) *Metrics {
	m := &Metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "grpc_requests_total",
			Help: "Количество обработанных gRPC-запросов по методу и коду ответа.",
		}, []string{"method", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "grpc_request_duration_seconds",
			Help:    "Длительность обработки gRPC-запросов в секундах.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "code"}),
	}
	registry.MustRegister(m.requests, m.duration)
	return m
}

// UnaryServerInterceptor измеряет unary-вызовы.
func (m *Metrics) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		started := time.Now()
		resp, err := handler(ctx, req)
		err = normalizeContextError(err)
		m.observe(info.FullMethod, status.Code(err), started)
		return resp, err
	}
}

// StreamServerInterceptor измеряет streaming-вызовы (по завершении потока).
func (m *Metrics) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		started := time.Now()
		err := handler(srv, stream)
		err = normalizeContextError(err)
		m.observe(info.FullMethod, status.Code(err), started)
		return err
	}
}

// observe фиксирует результат одного вызова.
func (m *Metrics) observe(method string, code interface{ String() string }, started time.Time) {
	m.requests.WithLabelValues(method, code.String()).Inc()
	m.duration.WithLabelValues(method, code.String()).Observe(time.Since(started).Seconds())
}

func normalizeContextError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	return err
}
