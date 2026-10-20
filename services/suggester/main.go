// suggester — микросервис подсказок адресов ГАР: gRPC-сервис
// suggest.v1.Suggester поверх поискового индекса Elasticsearch.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"gar-importer/internal/platform"
	"gar-importer/services/suggester/internal/app"
)

func main() {
	logger := platform.Logger("gar-suggester")
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
