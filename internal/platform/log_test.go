package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// Тест проверяет, что журнал пишет валидный JSON с атрибутом service.
func TestLoggerJSON(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	var buffer bytes.Buffer
	log := logger(&buffer, "test-service")
	log.Debug("проверка", "key", "value")
	var entry map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &entry); err != nil {
		t.Fatalf("запись журнала не JSON: %v\n%s", err, buffer.String())
	}
	if entry["service"] != "test-service" {
		t.Fatalf("service: got %v", entry["service"])
	}
	if entry["msg"] != "проверка" || entry["level"] != "DEBUG" || entry["key"] != "value" {
		t.Fatalf("неожиданная запись: %v", entry)
	}
}

// Тест проверяет уровень по умолчанию и фильтрацию debug при пустом LOG_LEVEL.
func TestLoggerDefaultLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	var buffer bytes.Buffer
	log := logger(&buffer, "test-service")
	log.Debug("не должно попасть в журнал")
	if buffer.Len() != 0 {
		t.Fatalf("debug-запись прошла при уровне info: %s", buffer.String())
	}
}

// Тест проверяет обогащение записи trace_id/span_id из контекста спана.
func TestContextHandlerTraceAttrs(t *testing.T) {
	var buffer bytes.Buffer
	log := logger(&buffer, "test-service")
	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatal(err)
	}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)
	log.InfoContext(ctx, "со спаном")
	var entry map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["trace_id"] != traceID.String() || entry["span_id"] != spanID.String() {
		t.Fatalf("трасировка не попала в журнал: %v", entry)
	}

	// Без спана в контексте атрибутов трасировки быть не должно.
	buffer.Reset()
	log.InfoContext(context.Background(), "без спана")
	entry = map[string]any{}
	if err := json.Unmarshal(buffer.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if _, ok := entry["trace_id"]; ok {
		t.Fatalf("trace_id без спана: %v", entry)
	}
}

// Тест проверяет, что WithAttrs сохраняет обогащение контекстом.
func TestContextHandlerWithAttrs(t *testing.T) {
	var buffer bytes.Buffer
	var handler slog.Handler = &ContextHandler{next: slog.NewJSONHandler(&buffer, nil)}
	log := slog.New(handler).With("component", "api")
	traceID, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	spanID, _ := trace.SpanIDFromHex("0102030405060708")
	ctx := trace.ContextWithSpanContext(context.Background(),
		trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID}))
	log.InfoContext(ctx, "производный журнал")
	var entry map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["component"] != "api" || entry["trace_id"] != traceID.String() {
		t.Fatalf("атрибуты потеряны: %v", entry)
	}
}
