//go:build integration

package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"gar-importer/internal/platform"
)

func TestCacheDetachedWritesAndNamespaces(t *testing.T) {
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{Image: "redis:7-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForLog("Ready to accept connections")}, Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	addr, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(redisOptions(Config{RedisAddr: addr}))
	defer client.Close()
	c := newCache(client, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)), newMetrics(platform.NewRegistry("cache-test")))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	c.set(canceled, 1, []byte(`{"object_id":1}`), "release-1")
	data, hit := c.get(ctx, 1, "release-1")
	if !hit || string(data) != `{"object_id":1}` {
		t.Fatalf("canceled request lost cache write: %s %v", data, hit)
	}
	if _, hit := c.get(ctx, 1, "release-2"); hit {
		t.Fatal("old release leaked into new namespace")
	}
	c.set(ctx, 2, []byte("null"), "release-1")
	ttl, err := client.TTL(ctx, versionedKey(2, []string{"release-1"})).Result()
	if err != nil || ttl <= 0 || ttl > 30*time.Second {
		t.Fatalf("negative TTL %v: %v", ttl, err)
	}
}
