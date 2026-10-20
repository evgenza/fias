package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// shutdownTimeout — предел graceful shutdown HTTP-сервера.
const shutdownTimeout = 10 * time.Second

// RunHTTPServer запускает HTTP-сервер и работает до отмены контекста, затем
// завершает соединения с таймаутом 10 секунд. Сервер принимает HTTP/1.1
// и HTTP/2 prior-knowledge без TLS (h2c) — это нужно address-api для
// клиентов на HTTP/2, остальным сервисам просто не мешает.
// Старт и останов пишутся в slog.Default(). Ошибка запуска возвращается сразу.
func RunHTTPServer(ctx context.Context, addr string, handler http.Handler) error {
	// h2c через Server.Protocols: пакет golang.org/x/net/http2/h2c объявлен
	// deprecated, стандартная библиотека (Go 1.24+) умеет это сама.
	// Ненулевой Protocols — точный список, поэтому HTTP/1.1 включаем явно.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		Protocols:         protocols,
	}
	served := make(chan error, 1)
	go func() {
		slog.Info("HTTP-сервер запущен", "addr", addr)
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		served <- err
	}()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	// Родитель уже отменён: таймаут отсчитывается от безотменной копии.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("остановка HTTP-сервера %s: %w", addr, err)
	}
	if err := <-served; err != nil {
		return err
	}
	slog.Info("HTTP-сервер остановлен", "addr", addr)
	return nil
}

// HealthMux возвращает mux проверок состояния: /healthz — живость (всегда 200),
// /readyz — готовность (503, пока ready возвращает ошибку; nil — всегда готов).
func HealthMux(ready func(context.Context) error) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if ready != nil {
			if err := ready(r.Context()); err != nil {
				// Клиенту — только факт неготовности, детали (адреса,
				// пользователь БД) остаются в логе.
				slog.WarnContext(r.Context(), "проверка готовности не пройдена", "error", err)
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("not ready\n"))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}
