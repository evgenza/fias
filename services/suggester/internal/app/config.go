// Package app — микросервис suggester платформы ГАР: gRPC-сервис подсказок
// адресов поверх поискового индекса Elasticsearch (gar-addresses).
package app

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Параметры по умолчанию.
const (
	defaultESAddress = "http://127.0.0.1:9200"
	defaultGRPCAddr  = ":50051"
	defaultHTTPAddr  = ":9213"
)

// Config — настройки suggester.
type Config struct {
	// ESAddresses — адреса Elasticsearch.
	ESAddresses []string
	// ESIndex — имя поискового индекса.
	ESIndex string
	// GRPCAddr — адрес gRPC-сервера.
	GRPCAddr string
	// HTTPAddr — адрес HTTP-сервера (/metrics, /healthz, /readyz).
	HTTPAddr         string
	EnableReflection bool
}

// ConfigFromEnv читает настройки из окружения: ES_ADDRESSES (через запятую),
// ES_INDEX, GRPC_ADDR, SUGGESTER_HTTP_ADDR. Уровень журнала и трейсинг
// настраиваются платформой (LOG_LEVEL, OTEL_EXPORTER_OTLP_ENDPOINT).
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		ESIndex:  os.Getenv("ES_INDEX"),
		GRPCAddr: envOr("GRPC_ADDR", defaultGRPCAddr),
		HTTPAddr: envOr("SUGGESTER_HTTP_ADDR", defaultHTTPAddr),
	}
	if value := os.Getenv("GRPC_REFLECTION"); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return cfg, fmt.Errorf("GRPC_REFLECTION: %w", err)
		}
		cfg.EnableReflection = enabled
	}
	addresses := strings.Split(envOr("ES_ADDRESSES", defaultESAddress), ",")
	for _, address := range addresses {
		if trimmed := strings.TrimSpace(address); trimmed != "" {
			cfg.ESAddresses = append(cfg.ESAddresses, trimmed)
		}
	}
	return cfg, nil
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
