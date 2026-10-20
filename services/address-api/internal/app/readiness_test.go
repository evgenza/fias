package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	"gar-importer/internal/platform"
)

type readinessStub struct{ err error }

func (s readinessStub) PingContext(context.Context) error { return s.err }

func TestReadinessWithUnavailableOptionalDependencies(t *testing.T) {
	registry := platform.NewRegistry("test")
	met := newMetrics(registry)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	cache := newCache(client, time.Minute, log, met)
	conn, err := grpc.NewClient("passthrough:///127.0.0.1:1", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if conn.GetState() != connectivity.Idle {
		t.Fatal("new channel must be IDLE")
	}
	check := readyz(readinessStub{}, cache, conn, met, log)
	if err := check(context.Background()); err != nil {
		t.Fatalf("optional dependencies affect readiness: %v", err)
	}
	if conn.GetState() == connectivity.Idle {
		t.Fatal("readiness did not wake IDLE channel")
	}
	pgErr := errors.New("postgres unavailable")
	if err := readyz(readinessStub{err: pgErr}, cache, conn, met, log)(context.Background()); !errors.Is(err, pgErr) {
		t.Fatalf("want postgres failure, got %v", err)
	}
}
