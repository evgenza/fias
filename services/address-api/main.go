// address-api — публичный REST API адресного поиска ГАР: подсказки через
// gRPC suggester, карточки объектов из PostgreSQL с кэшем Redis, веб-клиент
// и Swagger UI на одном h2c-порту.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"gar-importer/internal/platform"
	"gar-importer/services/address-api/internal/app"
)

func main() {
	logger := platform.Logger("gar-address-api")
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := app.ConfigFromEnv()
	if err != nil {
		logger.Error("конфигурация", "error", err)
		os.Exit(1)
	}
	if err := app.Run(ctx, cfg); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("завершение с ошибкой", "error", err)
		os.Exit(1)
	}
}
