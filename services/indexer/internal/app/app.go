package app

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"gar-importer/internal/address"
	"gar-importer/internal/config"
	"gar-importer/internal/events"
	"gar-importer/internal/gar"
	"gar-importer/internal/platform"
	"gar-importer/internal/search"
	"gar-importer/internal/storage"
)

// Параметры сервиса.
const (
	serviceName = "gar-indexer"
	// durableName — имя durable-консьюмера ObjectsChanged.
	durableName = "indexer"
	// nakDelay — задержка повторной доставки при временной ошибке.
	nakDelay = 10 * time.Second
	// ensureIndexMaxWait — верхняя граница backoff ожидания Elasticsearch
	// (ES может стартовать дольше indexer'а).
	ensureIndexMaxWait = 60 * time.Second
)

// metrics — метрики сервиса.
type metrics struct {
	documents *prometheus.CounterVec
	events    *prometheus.CounterVec
	duration  *prometheus.HistogramVec
}

// newMetrics создаёт и регистрирует метрики indexer.
func newMetrics(registry *platform.Metrics) *metrics {
	m := &metrics{
		documents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "indexer_documents_indexed_total",
			Help: "Количество проиндексированных документов по виду документа.",
		}, []string{"kind"}),
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "indexer_events_total",
			Help: "Количество обработанных событий ObjectsChanged по виду и результату.",
		}, []string{"kind", "result"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "indexer_build_duration_seconds",
			Help:    "Длительность пересборки индекса по событию в секундах.",
			Buckets: prometheus.DefBuckets,
		}, []string{"kind"}),
	}
	registry.MustRegister(m.documents, m.events, m.duration)
	return m
}

// buildTask — пересборка одного вида документов.
type buildTask struct {
	// docKind — вид документа индекса (метка метрик): house, apartment…
	docKind string
	build   func(ctx context.Context, region int, sink func([]search.Document) error) error
}

// service — собранные зависимости indexer.
type service struct {
	log     *slog.Logger
	tracer  trace.Tracer
	builder *address.Builder
	search  *search.Client
	metrics *metrics
}

// Run запускает indexer и работает до отмены контекста.
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
	registry := platform.NewRegistry("indexer")
	met := newMetrics(registry)

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

	searchClient, err := search.New(search.Config{Addresses: cfg.ESAddresses, Index: cfg.ESIndex})
	if err != nil {
		return err
	}
	if err := ensureIndex(ctx, searchClient, log); err != nil {
		return err
	}

	conn, err := events.Connect(cfg.NATSURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	js, err := conn.JetStream()
	if err != nil {
		return fmt.Errorf("indexer: JetStream: %w", err)
	}
	if _, err := events.EnsureStream(ctx, js); err != nil {
		return err
	}
	subscriber := events.NewSubscriber(js).WithMetrics(registry)
	messages, err := subscriber.SubscribeObjectsChanged(ctx, durableName)
	if err != nil {
		return err
	}

	svc := &service{
		log:     log,
		tracer:  otel.Tracer("gar-importer/services/indexer"),
		builder: address.NewBuilder(store),
		search:  searchClient,
		metrics: met,
	}

	mux := platform.HealthMux(readyz(searchClient, conn))
	mux.Handle("/metrics", registry.Handler())
	httpErr := make(chan error, 1)
	go func() { httpErr <- platform.RunHTTPServer(ctx, cfg.HTTPAddr, mux) }()

	log.Info("indexer запущен", "es_index", searchClient.IndexName(), "durable", durableName)
	for {
		select {
		case <-ctx.Done():
			// Дожидаемся остановки HTTP-сервера; необработанные сообщения
			// будут доставлены повторно (AckWait).
			return <-httpErr
		case err := <-httpErr:
			return err
		case msg, ok := <-messages:
			if !ok {
				return <-httpErr
			}
			svc.handle(ctx, msg)
		}
	}
}

// handle обрабатывает одно событие ObjectsChanged.
func (s *service) handle(ctx context.Context, msg events.ObjectsChangedMsg) {
	event := msg.Payload
	ctx, span := s.tracer.Start(ctx, "objects.changed "+event.Kind,
		trace.WithAttributes(
			attribute.String("gar.kind", event.Kind),
			attribute.Int("gar.region", event.Region),
			attribute.Int64("gar.run_id", event.RunID),
		))
	defer span.End()
	log := s.log.With("kind", event.Kind, "region", event.Region, "run_id", event.RunID)

	// Корневые справочники (регион 0): типы учитываются при чтении данных,
	// пересборка не требуется (задел на будущее).
	if event.Region == 0 {
		switch gar.Kind(event.Kind) {
		case gar.KindHouseTypes, gar.KindAddHouseTypes, gar.KindApartmentTypes, gar.KindRoomTypes:
			s.builder.InvalidateDictionaries()
		}
		log.Debug("корневой справочник, индексация не требуется")
		s.metrics.events.WithLabelValues(event.Kind, "skipped").Inc()
		if err := msg.Ack(); err != nil {
			log.Error("ack", "error", err)
		}
		return
	}

	tasks, known := s.routeTasks(gar.Kind(event.Kind))
	switch {
	case tasks == nil && known:
		// Известный формат без влияния на индекс (params, change_history…).
		log.Debug("формат не индексируется")
		s.metrics.events.WithLabelValues(event.Kind, "ignored").Inc()
		if err := msg.Ack(); err != nil {
			log.Error("ack", "error", err)
		}
		return
	case tasks == nil:
		log.Warn("неизвестный вид объектов, сообщение завершается")
		s.metrics.events.WithLabelValues(event.Kind, "unknown").Inc()
		if err := msg.Term(); err != nil {
			log.Error("term", "error", err)
		}
		return
	}

	started := time.Now()
	err := s.buildAll(ctx, event.Region, tasks)
	s.metrics.duration.WithLabelValues(event.Kind).Observe(time.Since(started).Seconds())
	if err != nil {
		span.RecordError(err)
		log.Error("пересборка индекса не удалась, повторная доставка", "error", err, "delay", nakDelay)
		s.metrics.events.WithLabelValues(event.Kind, "error").Inc()
		if nackErr := msg.Retry(err, nakDelay); nackErr != nil {
			log.Error("nak", "error", nackErr)
		}
		return
	}
	log.Info("индекс пересобран", "duration", time.Since(started).Round(time.Millisecond))
	s.metrics.events.WithLabelValues(event.Kind, "ok").Inc()
	if err := msg.Ack(); err != nil {
		log.Error("ack", "error", err)
	}
}

// routeTasks выбирает виды документов для пересборки по виду события;
// known — известен ли формат ГАР вообще.
func (s *service) routeTasks(kind gar.Kind) ([]buildTask, bool) {
	byDoc := func(docKind string, build func(context.Context, int, func([]search.Document) error) error) buildTask {
		return buildTask{docKind: docKind, build: build}
	}
	all := []buildTask{
		byDoc(search.KindAddrObj, s.builder.BuildAddrObjects),
		byDoc(search.KindHouse, s.builder.BuildHouses),
		byDoc(search.KindApartment, s.builder.BuildApartments),
		byDoc(search.KindRoom, s.builder.BuildRooms),
		byDoc(search.KindStead, s.builder.BuildSteads),
		byDoc(search.KindCarplace, s.builder.BuildCarplaces),
	}
	switch kind {
	case gar.KindAddrObj, gar.KindReestrObjects, gar.KindParam:
		return all, true
	case gar.KindHouses:
		return append(append([]buildTask{}, all[1:4]...), all[5]), true
	case gar.KindApartments:
		return all[2:4], true
	case gar.KindRooms:
		return all[3:4], true
	case gar.KindSteads:
		return all[4:5], true
	case gar.KindCarplaces:
		return all[5:6], true
	case gar.KindMunHierarchy:
		return nil, true
	case gar.KindAdmHierarchy:
		// Иерархия меняет адресные строки всех видов объектов региона.
		return all, true
	default:
		return nil, kind.Known()
	}
}

// buildAll пересобирает перечисленные виды документов региона; пачки
// документов индексируются в Elasticsearch по мере готовности.
func (s *service) buildAll(ctx context.Context, region int, tasks []buildTask) error {
	for _, task := range tasks {
		started := time.Now().UTC().Truncate(time.Millisecond)
		sink := func(docs []search.Document) error {
			indexed, err := s.search.BulkIndex(ctx, docs)
			s.metrics.documents.WithLabelValues(task.docKind).Add(float64(indexed))
			return err
		}
		if err := task.build(ctx, region, sink); err != nil {
			return err
		}
		if err := s.search.DeleteStale(ctx, strconv.Itoa(region), task.docKind, started); err != nil {
			return err
		}
	}
	return nil
}

// ensureIndex создаёт индекс, дожидаясь Elasticsearch: backoff от секунды
// до ensureIndexMaxWait, пока контекст не отменён.
func ensureIndex(ctx context.Context, client *search.Client, log *slog.Logger) error {
	wait := time.Second
	for {
		err := client.EnsureIndex(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return fmt.Errorf("indexer: ожидание Elasticsearch: %w", ctx.Err())
		}
		log.Warn("Elasticsearch недоступен, повтор", "wait", wait, "error", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("indexer: ожидание Elasticsearch: %w", ctx.Err())
		case <-time.After(wait):
		}
		wait = min(wait*2, ensureIndexMaxWait)
	}
}

// readyz — проверка готовности: доступен Elasticsearch и подключён NATS.
func readyz(client *search.Client, conn *nats.Conn) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := client.Ping(ctx); err != nil {
			return err
		}
		if !conn.IsConnected() {
			return fmt.Errorf("NATS не подключён")
		}
		return nil
	}
}
