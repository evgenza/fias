package platform

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Тест проверяет /healthz и /readyz: живость всегда 200, готовность зависит
// от проверки.
func TestHealthMux(t *testing.T) {
	fail := errors.New("база недоступна")
	ready := true
	mux := HealthMux(func(context.Context) error {
		if !ready {
			return fail
		}
		return nil
	})
	for _, path := range []string{"/healthz", "/readyz"} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s: got %d, want 200", path, recorder.Code)
		}
	}
	ready = false
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz при ошибке: got %d, want 503", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("/healthz при ошибке готовности: got %d, want 200", recorder.Code)
	}
}

// Тест проверяет /readyz без проверки (nil — всегда готов).
func TestHealthMuxNilReady(t *testing.T) {
	recorder := httptest.NewRecorder()
	HealthMux(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("/readyz без проверки: got %d, want 200", recorder.Code)
	}
}

// Тест проверяет, что тело /readyz при ошибке не раскрывает детали
// (адреса, пользователь БД): клиенту — только «not ready», детали в логе.
func TestHealthMuxBodyHidesDetails(t *testing.T) {
	fail := errors.New("dial tcp 10.0.0.5:5432: user=gar: connection refused")
	mux := HealthMux(func(context.Context) error { return fail })
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz при ошибке: got %d, want 503", recorder.Code)
	}
	body := recorder.Body.String()
	if body != "not ready\n" {
		t.Fatalf("тело /readyz: got %q, want %q", body, "not ready\n")
	}
	if strings.Contains(body, "10.0.0.5") || strings.Contains(body, "user=gar") {
		t.Fatalf("тело /readyz раскрывает детали ошибки: %q", body)
	}
}

// Тест проверяет, что сервер обслуживает запросы и завершается по отмене
// контекста без ошибки.
func TestRunHTTPServerGracefulShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunHTTPServer(ctx, addr, HealthMux(nil))
	}()
	// Ждём готовности сервера.
	var response *http.Response
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response, err = http.Get(fmt.Sprintf("http://%s/healthz", addr))
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("сервер не поднялся: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("healthz: got %d", response.StatusCode)
	}
	cancel()
	response, err = http.Get(fmt.Sprintf("http://%s/readyz", addr))
	if err != nil {
		t.Fatalf("readiness drain request: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("draining readiness: %d", response.StatusCode)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("остановка сервера: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("сервер не завершился после отмены контекста")
	}
}
