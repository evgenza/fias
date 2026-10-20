// Package app — микросервис address-api платформы ГАР: публичный REST API
// адресного поиска (подсказки через gRPC suggester, карточки объектов из
// PostgreSQL с кэшем Redis) плюс веб-клиент и Swagger UI на одном h2c-порту.
package app

import (
	"fmt"
	"os"
	"time"
)

// Параметры по умолчанию.
const (
	defaultSuggesterGRPCAddr = "127.0.0.1:50051"
	defaultRedisAddr         = "127.0.0.1:6379"
	defaultCacheTTL          = 5 * time.Minute
	defaultHTTPAddr          = ":8080"
)

// Config — настройки address-api.
type Config struct {
	// DatabaseURL — строка подключения к PostgreSQL (обязательна).
	DatabaseURL string
	// SuggesterGRPCAddr — адрес gRPC-сервиса suggester.
	SuggesterGRPCAddr string
	// RedisAddr — адрес Redis кэша карточек.
	RedisAddr string
	// RedisPassword — пароль Redis (пусто — без аутентификации).
	RedisPassword string
	// CacheTTL — время жизни карточки объекта в кэше.
	CacheTTL time.Duration
	// HTTPAddr — адрес HTTP-сервера (h2c: HTTP/1.1 и HTTP/2 prior-knowledge).
	HTTPAddr string
}

// ConfigFromEnv читает настройки из окружения: DATABASE_URL (обязательна),
// SUGGESTER_GRPC_ADDR, REDIS_ADDR, REDIS_PASSWORD, CACHE_TTL (длительность Go,
// например 5m), HTTP_ADDR. Уровень журнала и трейсинг настраиваются платформой
// (LOG_LEVEL, OTEL_EXPORTER_OTLP_ENDPOINT).
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		SuggesterGRPCAddr: envOr("SUGGESTER_GRPC_ADDR", defaultSuggesterGRPCAddr),
		RedisAddr:         envOr("REDIS_ADDR", defaultRedisAddr),
		RedisPassword:     os.Getenv("REDIS_PASSWORD"),
		HTTPAddr:          envOr("HTTP_ADDR", defaultHTTPAddr),
		CacheTTL:          defaultCacheTTL,
	}
	if value := os.Getenv("CACHE_TTL"); value != "" {
		ttl, err := time.ParseDuration(value)
		if err != nil || ttl <= 0 || ttl > 15*time.Minute {
			return cfg, fmt.Errorf("CACHE_TTL %q: ожидается длительность от 0 до 15m (например 5m)", value)
		}
		cfg.CacheTTL = ttl
	}
	if cfg.DatabaseURL == "" {
		return cfg, fmt.Errorf("задайте DATABASE_URL в окружении")
	}
	return cfg, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
