package app

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sync/singleflight"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// cacheKeyPrefix — префикс ключей карточек адресных объектов (версия схемы
// кэша: при изменении формата значения префикс увеличивается).
const cacheKeyPrefix = "addr:v1:"

// cache — cache-aside кэш карточек в Redis. Недоступность Redis не считается
// ошибкой запроса: читаем из PostgreSQL напрямую (деградация без 5xx).
type cache struct {
	client  *redis.Client
	ttl     time.Duration
	log     *slog.Logger
	metrics *metrics
	version func(context.Context) (string, error)
	flights singleflight.Group
}

// newCache создаёт кэш поверх клиента Redis.
func newCache(client *redis.Client, ttl time.Duration, log *slog.Logger, m *metrics) *cache {
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	if ttl > 15*time.Minute {
		ttl = 15 * time.Minute
	}
	return &cache{client: client, ttl: ttl, log: log, metrics: m}
}

// get читает карточку из кэша; data=nil при промахе или ошибке Redis.
func (c *cache) get(ctx context.Context, objectID int64, version ...string) (data []byte, hit bool) {
	key := versionedKey(objectID, version)
	value, err := c.client.Get(ctx, key).Bytes()
	switch {
	case err == nil:
		c.metrics.cache.WithLabelValues("hit").Inc()
		return value, true
	case errors.Is(err, redis.Nil):
		c.metrics.cache.WithLabelValues("miss").Inc()
		return nil, false
	default:
		c.metrics.cache.WithLabelValues("error").Inc()
		c.log.WarnContext(ctx, "кэш: чтение не удалось, работаем напрямую из БД", "key", key, "error", err)
		return nil, false
	}
}

// set записывает карточку в кэш; ошибка Redis только журналируется.
func (c *cache) set(ctx context.Context, objectID int64, data []byte, version ...string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 200*time.Millisecond)
	defer cancel()
	key := versionedKey(objectID, version)
	ttl := c.ttl
	if string(data) == "null" {
		ttl = 30 * time.Second
	}
	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		c.metrics.cache.WithLabelValues("error").Inc()
		c.log.WarnContext(ctx, "кэш: запись не удалась", "key", key, "error", err)
	}
}

// ping проверяет доступность Redis (для /readyz).
func (c *cache) ping(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	return nil
}

// cacheKey — ключ карточки объекта.
func cacheKey(objectID int64) string {
	return fmt.Sprintf("%s%d", cacheKeyPrefix, objectID)
}

func versionedKey(id int64, version []string) string {
	if len(version) == 0 {
		return cacheKey(id)
	}
	return fmt.Sprintf("addr:v2:%s:%d", version[0], id)
}
