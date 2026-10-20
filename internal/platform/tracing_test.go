package platform

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
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
