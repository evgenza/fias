// Package objectstorage — доступ к архивам ГАР в S3/MinIO: скачивание
// во временный файл и открытие как importjob.Archive для сценария импорта.
package objectstorage

import (
	"os"
	"strings"
)

// Config — настройки подключения к S3/MinIO.
type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

// ConfigFromEnv читает настройки из окружения с заданным префиксом:
// <prefix>_ENDPOINT, <prefix>_ACCESS_KEY, <prefix>_SECRET_KEY, <prefix>_USE_SSL.
// Пустой prefix трактуется как S3.
func ConfigFromEnv(prefix string) Config {
	if prefix == "" {
		prefix = "S3"
	}
	return Config{
		Endpoint:  os.Getenv(prefix + "_ENDPOINT"),
		AccessKey: os.Getenv(prefix + "_ACCESS_KEY"),
		SecretKey: os.Getenv(prefix + "_SECRET_KEY"),
		UseSSL:    envBool(prefix + "_USE_SSL"),
	}
}

// envBool разбирает логическое значение окружения (1/true/yes).
func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
