package platform

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace/noop"
)

// InitTracing настраивает глобальный TracerProvider сервиса: OTLP gRPC
// экспортёр (адрес из OTEL_EXPORTER_OTLP_ENDPOINT; пусто — noop-провайдер),
// ресурс с service.name, sampler ParentBased(TraceIDRatio) с долей из
// OTEL_TRACES_RATIO (0..1, по умолчанию 1.0). Возвращает функцию завершения,
// которую нужно вызвать при остановке сервиса.
func InitTracing(ctx context.Context, service string) (func(context.Context) error, error) {
	res, err := resource.Merge(resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(service)))
	if err != nil {
		return nil, fmt.Errorf("tracing: ресурс: %w", err)
	}
	sampler := newSampler(tracesRatio())
	// Пропагация контекста W3C работает и без экспортёра. Baggage не
	// пропагаем: на публичной границе входящий baggage ничем не фильтруется.
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		// Коллектор не задан: спаны создаются, но отбрасываются.
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("tracing: OTLP gRPC экспортёр: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

// newSampler — ParentBased-сэмплер, не доверяющий входящему sampled-флагу:
// удалённый родитель с публичной границы может принудительно включать 100%
// выборку, поэтому и для sampled, и для not sampled удалённых родителей
// применяем собственную долю.
func newSampler(ratio float64) sdktrace.Sampler {
	local := sdktrace.TraceIDRatioBased(ratio)
	return sdktrace.ParentBased(local,
		sdktrace.WithRemoteParentSampled(local),
		sdktrace.WithRemoteParentNotSampled(local),
	)
}

// tracesRatio разбирает OTEL_TRACES_RATIO; некорректные значения — как 1.0.
func tracesRatio() float64 {
	value := os.Getenv("OTEL_TRACES_RATIO")
	if value == "" {
		return 1.0
	}
	ratio, err := strconv.ParseFloat(value, 64)
	if err != nil || ratio < 0 || ratio > 1 {
		return 1.0
	}
	return ratio
}
