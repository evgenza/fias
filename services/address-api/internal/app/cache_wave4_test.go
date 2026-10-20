package app

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestCacheTTLBound(t *testing.T) {
	c := newCache(nil, 24*time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if c.ttl > 15*time.Minute {
		t.Fatalf("unbounded TTL: %v", c.ttl)
	}
}
