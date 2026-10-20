package platform

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Тест проверяет noop-режим: без OTEL_EXPORTER_OTLP_ENDPOINT провайдер
// создаётся, спаны не падают, завершение не требует коллектора.
func TestInitTracingNoop(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := InitTracing(context.Background(), "test-service")
	if err != nil {
		t.Fatal(err)
	}
	_, span := otel.Tracer("test").Start(context.Background(), "проверка")
	span.End()
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Тест проверяет, что внешний sampled-флаг из входящего traceparent не
// принуждает к записи: при доле выборки 0 спан с удалённым sampled-родителем
// отбрасывается. На публичной границе входящему traceparent не доверяют.
func TestSamplerIgnoresRemoteSampledFlag(t *testing.T) {
	sampler := newSampler(0)
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x01},
		SpanID:     trace.SpanID{0x02},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), parent)
	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: ctx,
		TraceID:       parent.TraceID(),
		Name:          "GET /api/v1/addresses/1",
	})
	if result.Decision != sdktrace.Drop {
		t.Fatalf("удалённый sampled-родитель при ratio=0: got %v, want Drop", result.Decision)
	}
}

// Тест проверяет разбор OTEL_TRACES_RATIO.
func TestTracesRatio(t *testing.T) {
	t.Setenv("OTEL_TRACES_RATIO", "")
	if got := tracesRatio(); got != 1.0 {
		t.Fatalf("по умолчанию: got %v, want 1.0", got)
	}
	t.Setenv("OTEL_TRACES_RATIO", "0.25")
	if got := tracesRatio(); got != 0.25 {
		t.Fatalf("got %v, want 0.25", got)
	}
	t.Setenv("OTEL_TRACES_RATIO", "мусор")
	if got := tracesRatio(); got != 1.0 {
		t.Fatalf("некорректное значение: got %v, want 1.0", got)
	}
	t.Setenv("OTEL_TRACES_RATIO", "5")
	if got := tracesRatio(); got != 1.0 {
		t.Fatalf("вне диапазона: got %v, want 1.0", got)
	}
}
