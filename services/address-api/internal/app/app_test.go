package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Тест проверяет, что имя спана строится по шаблону маршрута
// («GET /api/v1/addresses/{id}»), а не по сырому пути: каждый object_id
// не должен порождать отдельное имя спана.
func TestSpanRouteName(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := provider.Tracer("test")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/addresses/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := spanRouteName(mux)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/addresses/123456", nil)
	ctx, span := tracer.Start(request.Context(), "GET")
	handler.ServeHTTP(httptest.NewRecorder(), request.WithContext(ctx))
	span.End()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("спанов: got %d, want 1", len(spans))
	}
	want := "GET /api/v1/addresses/{id}"
	if spans[0].Name() != want {
		t.Fatalf("имя спана: got %q, want %q", spans[0].Name(), want)
	}
}
