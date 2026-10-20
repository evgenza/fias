// Command gar-search-migrate upgrades the search index while indexer writers are stopped.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"gar-importer/internal/search"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	addresses := os.Getenv("ES_ADDRESSES")
	if addresses == "" {
		addresses = "http://127.0.0.1:9200"
	}
	client, err := search.New(search.Config{Addresses: strings.Split(addresses, ","), Index: os.Getenv("ES_INDEX")})
	if err == nil {
		err = client.MigrateIndex(ctx)
	}
	if err != nil {
		slog.Error("search migration failed; retain source and retry after resolving the error", "error", err)
		os.Exit(1)
	}
	slog.Info("search index migrated")
}
