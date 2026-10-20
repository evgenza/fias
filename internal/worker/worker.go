// Package worker — режим фонового исполнителя gar-importer: задачи импорта
// приходят из NATS JetStream (gar.import.requested), архив скачивается из
// S3/MinIO, импорт выполняет сценарий importjob с журналом control, о каждом
// завершённом файле публикуется событие gar.objects.changed. Метрики
// Prometheus, проверки /healthz и /readyz и трасировка OpenTelemetry
// подключаются из platform.
package worker

import (
	"archive/zip"
	"compress/flate"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/minio/minio-go/v7"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"gar-importer/internal/config"
	"gar-importer/internal/control"
	"gar-importer/internal/events"
	"gar-importer/internal/gar"
	"gar-importer/internal/importjob"
	"gar-importer/internal/migrations"
	"gar-importer/internal/objectstorage"
	"gar-importer/internal/platform"
	"gar-importer/internal/storage"
)

const (
	// defaultMetricsAddr — адрес HTTP-сервера метрик и проверок по умолчанию.
	defaultMetricsAddr = ":9211"
	// defaultServiceName — имя сервиса в логах, метриках и трасировке.
	defaultServiceName = "importer"
	// defaultMigrationsDir — каталог миграций Atlas относительно рабочего каталога.
	defaultMigrationsDir = "migrations/atlas"
	// consumerDurable — имя durable-консьюмера запросов импорта.
	consumerDurable = "importer"
	// tracerName — имя трейсера спанов воркера.
	tracerName = "gar-importer/worker"
	// nakDelay — задержка повторной доставки при временной ошибке.
	nakDelay = 5 * time.Second
	// readyTimeout — предел проверки зависимостей в /readyz.
	readyTimeout = 2 * time.Second
	// migrateTimeout — предел применения миграций при старте.
	migrateTimeout = 2 * time.Minute
	// tracingShutdownTimeout — предел выгрузки спанов при остановке.
	tracingShutdownTimeout = 5 * time.Second
)

// errBadRequest помечает постоянные ошибки параметров задачи.
var errBadRequest = errors.New("worker: некорректные параметры задачи")

// Config — настройки воркера.
type Config struct {
	config.Config // DatabaseURL и пул соединений PostgreSQL
	// NATSUrl — URL NATS JetStream (nats://host:4222).
	NATSUrl string
	// S3 — настройки подключения к S3/MinIO.
	S3 objectstorage.Config
	// S3BucketDefault — бакет архивов, если задача не задала свой.
	S3BucketDefault string
	// MetricsAddr — адрес HTTP-сервера (/metrics, /healthz, /readyz); пусто — ":9211".
	MetricsAddr string
	// ServiceName — имя сервиса для логов, метрик и трасировки; пусто — "importer".
	ServiceName string
	// MigrationsDir — каталог миграций Atlas; пусто — "migrations/atlas".
	MigrationsDir string
}

// Run запускает воркер и работает до отмены контекста: применяет миграции,
// подключается к PostgreSQL, NATS JetStream и S3, поднимает HTTP-сервер
// метрик и последовательно выполняет задачи импорта из очереди. Задача
// в обработке при остановке не подтверждается: AckWait консьюмера обеспечит
// её повторную доставку.
func Run(ctx context.Context, cfg Config) error {
	if cfg.MetricsAddr == "" {
		cfg.MetricsAddr = defaultMetricsAddr
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultServiceName
	}
	if cfg.MigrationsDir == "" {
		cfg.MigrationsDir = defaultMigrationsDir
	}
	logger := platform.Logger(cfg.ServiceName)
	slog.SetDefault(logger)
	shutdownTracing, err := platform.InitTracing(ctx, cfg.ServiceName)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tracingShutdownTimeout)
		defer cancel()
		_ = shutdownTracing(shutdownCtx)
	}()

	database, err := storage.Open(ctx, cfg.Config)
	if err != nil {
		return err
	}
	defer database.Close()

	// Схема данных приводится миграциями до приёма задач; отсутствующая схема
	// gar создаётся внутри Runner.Apply.
	migrateCtx, cancel := context.WithTimeout(ctx, migrateTimeout)
	runner, err := migrations.NewRunner(database.SQL(), cfg.MigrationsDir)
	if err == nil {
		var applied []migrations.Version
		applied, err = runner.Apply(migrateCtx)
		if err == nil {
			logger.Info("миграции актуальны", "applied", len(applied))
		}
	}
	cancel()
	if err != nil {
		return err
	}

	journal, err := control.Open(ctx, database.SQL())
	if err != nil {
		return err
	}
	objects, err := objectstorage.NewClient(cfg.S3)
	if err != nil {
		return err
	}
	conn, err := events.Connect(cfg.NATSUrl)
	if err != nil {
		return err
	}
	defer conn.Close()
	js, err := conn.JetStream()
	if err != nil {
		return fmt.Errorf("worker: контекст JetStream: %w", err)
	}
	if _, err := events.EnsureStream(ctx, js); err != nil {
		return err
	}
	registry := platform.NewRegistry(cfg.ServiceName)
	requests, err := events.NewSubscriber(js).WithMetrics(registry).SubscribeImportRequests(ctx, consumerDurable)
	if err != nil {
		return err
	}

	w := &worker{
		cfg:             cfg,
		logger:          logger,
		tracer:          otel.Tracer(tracerName),
		database:        database,
		journal:         journal,
		objects:         objects,
		natsConn:        conn,
		publisher:       events.NewPublisher(js),
		metrics:         newTaskMetrics(registry),
		metricsRegistry: registry,
	}

	mux := platform.HealthMux(w.ready)
	mux.Handle("/metrics", w.metricsRegistry.Handler())
	httpDone := make(chan error, 1)
	go func() { httpDone <- platform.RunHTTPServer(ctx, cfg.MetricsAddr, mux) }()

	logger.Info("воркер запущен",
		"nats", cfg.NATSUrl, "s3_endpoint", cfg.S3.Endpoint,
		"bucket", cfg.S3BucketDefault, "metrics", cfg.MetricsAddr)
	for {
		select {
		case <-ctx.Done():
			logger.Info("воркер остановлен")
			return nil
		case err := <-httpDone:
			return err
		case msg, ok := <-requests:
			if !ok {
				// Канал подписки закрывается по отмене контекста.
				return nil
			}
			w.handle(ctx, msg)
		}
	}
}

// worker — исполнитель задач импорта с зависимостями одного запуска.
type worker struct {
	cfg             Config
	logger          *slog.Logger
	tracer          trace.Tracer
	database        *storage.Store
	journal         *control.Journal
	objects         *objectstorage.Client
	natsConn        *nats.Conn
	publisher       *events.Publisher
	metrics         *taskMetrics
	metricsRegistry *platform.Metrics
}

// ready проверяет готовность воркера: доступность PostgreSQL и соединение NATS.
func (w *worker) ready(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	if err := w.database.SQL().PingContext(pingCtx); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	if !w.natsConn.IsConnected() {
		return errors.New("nats: соединение не установлено")
	}
	return nil
}

// handle выполняет одну задачу импорта и подтверждает сообщение по результату:
// успех — Ack; временная ошибка (сеть, БД, NATS, timeout) — Nak с задержкой;
// постоянная (валидация XML, битый архив, нет объекта в S3, bad request) — Term.
func (w *worker) handle(ctx context.Context, msg events.ImportRequestMsg) {
	request := msg.Payload
	ctx, span := w.tracer.Start(ctx, "import task", trace.WithAttributes(
		attribute.String("gar.request_id", request.RequestID),
		attribute.String("gar.bucket", request.Bucket),
		attribute.String("gar.key", request.Key),
	))
	defer span.End()
	logger := w.logger.With("request_id", request.RequestID, "bucket", request.Bucket, "key", request.Key)

	started := time.Now()
	report, err := w.runImport(ctx, request)
	w.metrics.runDuration.Observe(time.Since(started).Seconds())
	if err != nil {
		for _, file := range report.Files {
			if !file.Done && !file.SkippedComplete {
				w.metrics.filesTotal.WithLabelValues(string(file.Kind), "failed").Inc()
			}
		}
	}
	switch {
	case err == nil:
		logger.Info("задача импорта выполнена",
			"run_id", report.RunID, "files", len(report.Files), "written", report.Written,
			"duration", report.Duration.Round(time.Millisecond).String())
		if aerr := msg.Ack(); aerr != nil {
			logger.Error("подтверждение сообщения", "error", aerr)
		}
		w.metrics.tasksTotal.WithLabelValues("ack").Inc()
	case ctx.Err() != nil:
		// Остановка воркера: сообщение не подтверждается, AckWait консьюмера
		// обеспечит повторную доставку следующему запуску.
		logger.Warn("задача прервана остановкой воркера", "error", err)
	case isPermanent(err):
		span.RecordError(err)
		logger.Error("постоянная ошибка задачи, сообщение терминировано", "error", err)
		if terr := msg.Term(); terr != nil {
			logger.Error("терминирование сообщения", "error", terr)
		}
		w.metrics.tasksTotal.WithLabelValues("term").Inc()
	default:
		span.RecordError(err)
		logger.Warn("временная ошибка задачи, повторная доставка", "error", err)
		if nerr := msg.Retry(err, nakDelay); nerr != nil {
			logger.Error("запрос повторной доставки", "error", nerr)
		}
		w.metrics.tasksTotal.WithLabelValues("nak").Inc()
	}
}

// runImport выполняет сценарий импорта архива задачи: скачивание из S3,
// потоковая запись через importjob с журналом и публикацией события по каждому
// завершённому файлу.
func (w *worker) runImport(ctx context.Context, request events.ImportRequested) (importjob.Report, error) {
	var report importjob.Report
	if err := validateRequest(request); err != nil {
		return report, err
	}
	bucket := request.Bucket
	if bucket == "" {
		bucket = w.cfg.S3BucketDefault
	}
	if bucket == "" {
		return report, fmt.Errorf("%w: не задан bucket архива (ни в задаче, ни по умолчанию)", errBadRequest)
	}
	archive, cleanup, err := w.objects.OpenArchive(ctx, bucket, request.Key)
	if err != nil {
		return report, err
	}
	defer func() { _ = cleanup() }()
	return importjob.RunWithJournal(ctx, archive,
		storage.NewWriterFactory(w.database.Client, w.journal), w.journal,
		importjob.Options{
			BatchSize:       request.BatchSize,
			LimitPerFile:    request.LimitPerFile,
			Regions:         request.Regions,
			OnFileCompleted: w.publishFileCompleted,
		})
}

// validateRequest проверяет параметры задачи до обращения к хранилищам:
// повторная доставка не исправит невалидный запрос.
func validateRequest(request events.ImportRequested) error {
	if request.Key == "" {
		return fmt.Errorf("%w: пустой key объекта", errBadRequest)
	}
	for _, region := range request.Regions {
		if region < 1 || region > 99 {
			return fmt.Errorf("%w: регион должен быть от 1 до 99: %d", errBadRequest, region)
		}
	}
	if request.BatchSize < 0 || request.BatchSize > importjob.MaxBatchSize {
		return fmt.Errorf("%w: размер пакета должен быть от 0 (по умолчанию) до %d: %d",
			errBadRequest, importjob.MaxBatchSize, request.BatchSize)
	}
	if request.LimitPerFile < 0 {
		return fmt.Errorf("%w: лимит на файл не может быть отрицательным: %d", errBadRequest, request.LimitPerFile)
	}
	return nil
}

// publishFileCompleted — хук завершения файла сценария импорта: метрики файла
// и публикация gar.objects.changed (спан publish внутри спана задачи). Ошибка
// публикации оставляет файл незавершённым в журнале: задача будет повторена.
func (w *worker) publishFileCompleted(ctx context.Context, event importjob.FileCompletedEvent) error {
	w.metrics.recordsTotal.WithLabelValues(string(event.Kind), strconv.Itoa(event.Region)).Add(float64(event.Written))
	w.metrics.filesTotal.WithLabelValues(string(event.Kind), "done").Inc()
	ctx, span := w.tracer.Start(ctx, "publish gar.objects.changed", trace.WithAttributes(
		attribute.Int64("gar.run_id", event.RunID),
		attribute.String("gar.kind", string(event.Kind)),
		attribute.Int("gar.region", event.Region),
		attribute.String("gar.path", event.Path),
	))
	defer span.End()
	if err := w.publisher.PublishObjectsChanged(ctx, events.ObjectsChanged{
		RunID:       event.RunID,
		Kind:        string(event.Kind),
		Region:      event.Region,
		Path:        event.Path,
		Written:     event.Written,
		CompletedAt: event.CompletedAt,
	}); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

// isPermanent классифицирует ошибку задачи: постоянные (валидация записей по
// XSD, битый XML или ZIP, отсутствующий объект/бакет S3, некорректные
// параметры запроса) повторная доставка не исправит — сообщение терминируется.
func isPermanent(err error) bool {
	if errors.Is(err, errBadRequest) || errors.Is(err, importjob.ErrNoSelectedFiles) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var recordErr *gar.RecordError
	if errors.As(err, &recordErr) {
		return true
	}
	var syntaxErr *xml.SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}
	if errors.Is(err, zip.ErrFormat) || errors.Is(err, zip.ErrChecksum) {
		return true
	}
	var corrupt flate.CorruptInputError
	if errors.As(err, &corrupt) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (strings.HasPrefix(pgErr.Code, "22") || strings.HasPrefix(pgErr.Code, "23")) {
		return true
	}
	var s3Err minio.ErrorResponse
	if errors.As(err, &s3Err) {
		switch s3Err.Code {
		case "NoSuchKey", "NoSuchBucket":
			return true
		}
	}
	return false
}

// taskMetrics — метрики воркера в реестре платформы.
type taskMetrics struct {
	recordsTotal *prometheus.CounterVec // gar_import_records_total{kind,region}
	filesTotal   *prometheus.CounterVec // gar_import_files_total{kind,status}
	runDuration  prometheus.Histogram   // gar_import_run_duration_seconds
	tasksTotal   *prometheus.CounterVec // gar_worker_tasks_total{result}
}

// newTaskMetrics создаёт и регистрирует метрики воркера в реестре сервиса.
func newTaskMetrics(registry *platform.Metrics) *taskMetrics {
	metrics := &taskMetrics{
		recordsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gar_import_records_total",
			Help: "Количество записанных объектов импорта по формату и региону.",
		}, []string{"kind", "region"}),
		filesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gar_import_files_total",
			Help: "Количество обработанных файлов архива по формату и статусу (done|failed).",
		}, []string{"kind", "status"}),
		runDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "gar_import_run_duration_seconds",
			Help: "Длительность выполнения одной задачи импорта в секундах.",
			// Полный снимок ГАР импортируется часами: шкала от секунды до суток.
			Buckets: prometheus.ExponentialBuckets(1, 4, 10),
		}),
		tasksTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gar_worker_tasks_total",
			Help: "Количество завершённых задач воркера по результату (ack|nak|term).",
		}, []string{"result"}),
	}
	registry.MustRegister(metrics.recordsTotal, metrics.filesTotal, metrics.runDuration, metrics.tasksTotal)
	return metrics
}
