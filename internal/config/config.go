// Package config — настройки запуска: окружение, .env, пул соединений
// и маскирование секрета в DSN.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// Config — проверенные настройки запуска.
type Config struct {
	DatabaseURL string

	// Пул соединений PostgreSQL: значения по умолчанию соответствуют
	// прежнему поведению импортёра.
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxIdleTime time.Duration
	ConnectTimeout  time.Duration
}

// Load читает .env (если есть) и окружение, проверяет обязательные значения.
func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("чтение .env: %w", err)
	}
	cfg := Config{
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		MaxOpenConns:    envInt("GAR_MAX_OPEN_CONNS", 8),
		MaxIdleConns:    envInt("GAR_MAX_IDLE_CONNS", 8),
		ConnMaxIdleTime: 5 * time.Minute,
		ConnectTimeout:  10 * time.Second,
	}
	if cfg.DatabaseURL == "" {
		return cfg, fmt.Errorf("задайте DATABASE_URL в окружении или .env")
	}
	return cfg, nil
}

// RedactedDSN скрывает пароль соединения для вывода в ошибки и логи.
func RedactedDSN(databaseURL string) string {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "redacted"
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			parsed.User = url.UserPassword(parsed.User.Username(), "***")
		}
	}
	return parsed.String()
}

func envInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}
