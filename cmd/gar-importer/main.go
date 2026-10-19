// Команда gar-importer — хранение выгрузки ГАР через Ent и подготовка к поиску.
// Вся логика команд находится в internal/cli: main только подключает сигналы
// и задаёт коды завершения.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"gar-importer/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, cli.ErrUsage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
