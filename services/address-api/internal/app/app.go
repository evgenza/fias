package app

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	"gar-importer/internal/address"
	"gar-importer/internal/config"
	"gar-importer/internal/platform"
	"gar-importer/internal/storage"
	suggestv1 "gar-importer/proto/suggest/v1"
	"gar-importer/services/address-api/static"
)

// Параметры сервиса.
const (
	serviceName = "gar-address-api"
	// readyzTimeout — верхняя граница проверки зависимостей в /readyz.
	readyzTimeout = 2 * time.Second
	// staticCacheControl — кэширование статики (Swagger UI, веб-клиент).
	staticCacheControl = "public, max-age=3600"
)

// Run запускает address-api и работает до отмены контекста: один порт
// (HTTPAddr) обслуживает REST API, служебные эндпоинты, Swagger UI
// и веб-клиент по HTTP/1.1 и HTTP/2 prior-knowledge (h2c включается
// в platform.RunHTTPServer).
func Run(ctx context.Context, cfg Config) error {
	log := slog.Default()
	shutdown, err := platform.InitTracing(ctx, serviceName)
	if err != nil {
		return err
	}
	defer func() {
		// Родитель уже отменён: завершаем провайдер безотменным контекстом.
		_ = shutdown(context.WithoutCancel(ctx))
	}()
	registry := platform.NewRegistry("address-api")
	met := newMetrics(registry)
	httpMetrics := registry.HTTPServerMetrics()

	store, err := storage.Open(ctx, config.Config{
		DatabaseURL:     cfg.DatabaseURL,
		MaxOpenConns:    8,
		MaxIdleConns:    8,
		ConnMaxIdleTime: 5 * time.Minute,
		ConnectTimeout:  10 * time.Second,
	})
	if err != nil {
		return err
	}
	defer store.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	})
	defer redisClient.Close()
	cardCache := newCache(redisClient, cfg.CacheTTL, log, met)

	// gRPC-клиент suggester: ленивое соединение, Connect запускает его сразу,
	// чтобы начать подключение до первого запроса.
	grpcConn, err := grpc.NewClient(cfg.SuggesterGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return fmt.Errorf("address-api: gRPC-клиент suggester: %w", err)
	}
	defer grpcConn.Close()
	grpcConn.Connect()

	h := &handler{
		store:     store,
		builder:   address.NewBuilder(store),
		cache:     cardCache,
		suggester: suggestv1.NewSuggesterClient(grpcConn),
		metrics:   met,
		log:       log,
	}

	mux := newMux(h, registry, readyz(store.SQL(), cardCache, grpcConn, met, log))
	// Цепочка middleware: трейсинг → метрики (шаблон маршрута как метка) → лог.
	var root http.Handler = mux
	root = logRequests(log, root)
	root = httpMetrics.HTTPMiddleware(routeLabel)(root)
	root = spanRouteName(root)
	root = otelhttp.NewHandler(root, "address-api",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return "address-api"
		}))
	// h2c (HTTP/1.1 и HTTP/2 prior-knowledge на одном порту) включается
	// в platform.RunHTTPServer через Server.Protocols.

	log.Info("address-api запущен",
		"addr", cfg.HTTPAddr, "suggester", cfg.SuggesterGRPCAddr,
		"redis", cfg.RedisAddr, "cache_ttl", cfg.CacheTTL)
	return platform.RunHTTPServer(ctx, cfg.HTTPAddr, root)
}

// newMux собирает маршруты сервиса (паттерны Go 1.22+).
func newMux(h *handler, registry *platform.Metrics, ready func(context.Context) error) *http.ServeMux {
	mux := platform.HealthMux(ready)
	mux.Handle("/metrics", registry.Handler())

	mux.HandleFunc("GET /api/v1/suggest", h.handleSuggest)
	mux.HandleFunc("GET /api/v1/addresses/{id}", h.handleAddress)
	mux.HandleFunc("GET /api/v1/addresses/{id}/children", h.handleChildren)

	mux.HandleFunc("GET /openapi.yaml", serveOpenAPI)

	// Swagger UI и веб-клиент — встроенная статика (embed).
	swaggerFS, err := fs.Sub(static.FS, "swagger-ui")
	if err != nil {
		panic(fmt.Sprintf("address-api: embed swagger-ui: %v", err))
	}
	webFS, err := fs.Sub(static.FS, "web")
	if err != nil {
		panic(fmt.Sprintf("address-api: embed web: %v", err))
	}
	mux.Handle("GET /swagger/", cacheStatic(http.StripPrefix("/swagger/", http.FileServerFS(swaggerFS))))
	mux.HandleFunc("GET /swagger", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/", http.StatusMovedPermanently)
	})
	// Веб-клиент: точные паттерны (subtree-паттерн «GET /» конфликтовал бы
	// с безметодными /healthz, /readyz, /metrics из platform.HealthMux).
	mux.Handle("GET /{$}", cacheStatic(http.FileServerFS(webFS)))
	mux.Handle("GET /index.html", cacheStatic(http.FileServerFS(webFS)))
	mux.Handle("GET /app.js", cacheStatic(http.FileServerFS(webFS)))
	mux.Handle("GET /style.css", cacheStatic(http.FileServerFS(webFS)))
	return mux
}

// serveOpenAPI отдаёт спецификацию OpenAPI (встроенная копия
// api/openapi/address-api.yaml).
func serveOpenAPI(w http.ResponseWriter, _ *http.Request) {
	data, err := static.FS.ReadFile("openapi.yaml")
	if err != nil {
		writeError(w, http.StatusInternalServerError, codeInternal, "внутренняя ошибка")
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// cacheStatic добавляет заголовки кэширования статическим файлам.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", staticCacheControl)
		next.ServeHTTP(w, r)
	})
}

// routeLabel — метка path для метрик: шаблон маршрута ServeMux
// («/api/v1/addresses/{id}») вместо сырого пути (кардинальность object_id).
func routeLabel(r *http.Request) string {
	pattern := r.Pattern
	if pattern == "" {
		return "unknown"
	}
	// Паттерн с методом: «GET /api/v1/suggest» → «/api/v1/suggest».
	if _, path, ok := strings.Cut(pattern, " "); ok {
		if path == "/{$}" {
			return "/"
		}
		return path
	}
	return pattern
}

// spanRouteName переименовывает активный спан по шаблону маршрута после
// отработки mux (r.Pattern заполняется ServeMux до вызова обработчика):
// каждый object_id не порождает отдельное имя спана.
func spanRouteName(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.Pattern == "" {
			return
		}
		span := trace.SpanFromContext(r.Context())
		if span.IsRecording() {
			span.SetName(r.Pattern)
		}
	})
}

// logRequests — JSON-лог каждого запроса: метод, путь, код, длительность.
func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		started := time.Now()
		next.ServeHTTP(recorder, r)
		log.InfoContext(r.Context(), "HTTP-запрос",
			"method", r.Method,
			"path", r.URL.Path,
			"code", recorder.code,
			"duration", time.Since(started).Round(time.Microsecond),
		)
	})
}

// statusRecorder перехватывает код ответа обработчика.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

// WriteHeader фиксирует код ответа и делегирует запись.
func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap отдаёт исходный ResponseWriter для http.ResponseController.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

type readinessDB interface {
	PingContext(context.Context) error
}

// readyz — проверка готовности: доступность PostgreSQL. Redis и suggester
// необязательны (карточки работают из PostgreSQL), их недоступность не валит
// реплики из балансировки: состояние отдаётся в метрике
// address_api_dependency_up и логе.
func readyz(db readinessDB, cardCache *cache, grpcConn *grpc.ClientConn, met *metrics, log *slog.Logger) func(context.Context) error {
	return func(ctx context.Context) error {
		pingCtx, cancel := context.WithTimeout(ctx, readyzTimeout)
		defer cancel()
		pgErr := db.PingContext(pingCtx)
		if pgErr != nil {
			log.WarnContext(ctx, "readiness: PostgreSQL недоступна", "error", pgErr)
		}
		// Канал через 30 минут простоя уходит в IDLE и сам не просыпается:
		// GetState() его не будит, поэтому будим явно.
		if grpcConn.GetState() == connectivity.Idle {
			grpcConn.Connect()
		}
		suggesterUp := grpcConn.GetState() == connectivity.Ready
		met.dependency.WithLabelValues("suggester").Set(boolToFloat(suggesterUp))
		if !suggesterUp {
			log.WarnContext(ctx, "readiness: suggester недоступен", "state", grpcConn.GetState())
		}
		redisErr := cardCache.ping(pingCtx)
		met.dependency.WithLabelValues("redis").Set(boolToFloat(redisErr == nil))
		if redisErr != nil {
			log.WarnContext(ctx, "readiness: Redis недоступен", "error", redisErr)
		}
		return pgErr
	}
}

// boolToFloat переводит состояние зависимости в значение gauge.
func boolToFloat(up bool) float64 {
	if up {
		return 1
	}
	return 0
}
