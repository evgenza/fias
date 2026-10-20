// Package platform — общая инфраструктура сервисов платформы ГАР: журнал
// с трасировкой, метрики Prometheus, трасировка OpenTelemetry и HTTP-сервер
// с graceful shutdown.
package platform

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// Logger возвращает журнал сервиса: JSON в stdout, уровень из LOG_LEVEL
// (debug/info/warn/error, по умолчанию info), атрибут service, а при валидном
// спане OpenTelemetry в контексте записи — атрибуты trace_id и span_id.
func Logger(service string) *slog.Logger {
	return logger(os.Stdout, service)
}

// logger собирает журнал с выводом в writer (выделено для тестов).
func logger(writer io.Writer, service string) *slog.Logger {
	handler := slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: parseLogLevel(os.Getenv("LOG_LEVEL"))})
	return slog.New(&ContextHandler{next: handler}).With(slog.String("service", service))
}

// parseLogLevel разбирает LOG_LEVEL; неизвестные значения трактуются как info.
func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ContextHandler обогащает записи журнала идентификаторами трасировки
// OpenTelemetry, если в контексте записи есть валидный SpanContext.
type ContextHandler struct {
	next slog.Handler
}

var _ slog.Handler = (*ContextHandler)(nil)

// Enabled делегирует проверку уровня вложенному обработчику.
func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle добавляет trace_id и span_id активного спана и пишет запись.
func (h *ContextHandler) Handle(ctx context.Context, record slog.Record) error {
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", span.TraceID().String()),
			slog.String("span_id", span.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, record)
}

// WithAttrs сохраняет обогащение для производных журналов.
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup сохраняет обогащение для сгруппированных журналов.
func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{next: h.next.WithGroup(name)}
}
