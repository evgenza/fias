// Package app — микросервис indexer платформы ГАР: потребляет события
// ObjectsChanged из NATS JetStream и перестраивает поисковый индекс
// адресов в Elasticsearch по данным PostgreSQL.
package app

import (
	"fmt"
	"os"
	"strings"
)

// Параметры по умолчанию.
const (
	defaultNATSURL      = "nats://127.0.0.1:4222"
	defaultESAddress    = "http://127.0.0.1:9200"
	defaultHTTPAddr     = ":9212"
	defaultReconnectSec = 10
)

// Config — настройки indexer.
type Config struct {
	// DatabaseURL — строка подключения к PostgreSQL (обязательна).
	DatabaseURL string
	// NATSURL — адрес NATS (nats://host:4222).
	NATSURL string
	// ESAddresses — адреса Elasticsearch.
	ESAddresses []string
	// ESIndex — имя поискового индекса.
	ESIndex string
	// HTTPAddr — адрес HTTP-сервера (/metrics, /healthz, /readyz).
	HTTPAddr string
}

// ConfigFromEnv читает настройки из окружения: DATABASE_URL (обязательна),
// NATS_URL, ES_ADDRESSES (через запятую), ES_INDEX, INDEXER_HTTP_ADDR.
// Уровень журнала и трейсинг настраиваются платформой (LOG_LEVEL,
// OTEL_EXPORTER_OTLP_ENDPOINT).
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		NATSURL:     envOr("NATS_URL", defaultNATSURL),
		ESIndex:     envOr("ES_INDEX", ""),
		HTTPAddr:    envOr("INDEXER_HTTP_ADDR", defaultHTTPAddr),
	}
	addresses := strings.Split(envOr("ES_ADDRESSES", defaultESAddress), ",")
	for _, address := range addresses {
		if trimmed := strings.TrimSpace(address); trimmed != "" {
			cfg.ESAddresses = append(cfg.ESAddresses, trimmed)
		}
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
