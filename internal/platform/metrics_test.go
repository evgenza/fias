package platform

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Тест проверяет, что middleware пишет счётчик и гистограмму с метками
// метода, нормализованного пути, кода ответа и константной меткой service.
func TestHTTPMiddlewareWritesMetrics(t *testing.T) {
	metrics := NewRegistry("test-service")
	httpMetrics := metrics.HTTPServerMetrics()
	labeler := func(r *http.Request) string { return "/objects/{id}" }
	handler := httpMetrics.HTTPMiddleware(labeler)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	request := httptest.NewRequest(http.MethodPut, "/objects/123", nil)
	handler.ServeHTTP(httptest.NewRecorder(), request)

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body, err := io.ReadAll(response.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	output := string(body)
	wantLabels := `code="201",method="PUT",path="/objects/{id}",service="test-service"`
	if !strings.Contains(output, "platform_http_requests_total{"+wantLabels+"} 1") {
		t.Fatalf("счётчик не найден, ожидались метки %s:\n%s", wantLabels, output)
	}
	if !strings.Contains(output, "platform_http_request_duration_seconds_count{"+wantLabels+"} 1") {
		t.Fatalf("гистограмма не найдена:\n%s", output)
	}
	if !strings.Contains(output, "go_goroutines") {
		t.Fatalf("стандартные коллекторы Go не зарегистрированы:\n%s", output)
	}
}

// Тест проверяет статус по умолчанию (200 без явного WriteHeader)
// и labeler по умолчанию (сырой путь).
func TestHTTPMiddlewareDefaults(t *testing.T) {
	metrics := NewRegistry("test-service")
	httpMetrics := metrics.HTTPServerMetrics()
	handler := httpMetrics.HTTPMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/raw/path", nil))

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body, _ := io.ReadAll(response.Result().Body)
	if !strings.Contains(string(body), `platform_http_requests_total{code="200",method="GET",path="/raw/path",service="test-service"} 1`) {
		t.Fatalf("счётчик по умолчанию не найден:\n%s", body)
	}
}
