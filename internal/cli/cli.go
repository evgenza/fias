// Package cli — разбор флагов, вывод и коды завершения gar-importer.
// Точка входа main только подключает сигналы и вызывает Run.
package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"gar-importer/ent/migrate"
	"gar-importer/internal/config"
	"gar-importer/internal/control"
	"gar-importer/internal/events"
	"gar-importer/internal/gar"
	"gar-importer/internal/importjob"
	"gar-importer/internal/input/zipgar"
	"gar-importer/internal/migrations"
	"gar-importer/internal/objectstorage"
	"gar-importer/internal/storage"
	"gar-importer/internal/worker"
)

// ErrUsage означает ошибку в аргументах командной строки (код выхода 2).
var ErrUsage = errors.New("cli: usage error")

const usage = `gar-importer — подготовка выгрузки ГАР (ФИАС) для поискового индекса

Использование:
  gar-importer <команда> [аргументы]

Команды:
  version         — версия сборки
  migrate         — применить схемы Ent к PostgreSQL (DATABASE_URL из окружения или .env)
  import          — потоковый импорт локального ZIP в таблицы PostgreSQL через Ent
  worker          — фоновый исполнитель: задачи импорта из NATS, архивы из S3/MinIO
  request-import  — разовая публикация задачи импорта в NATS JetStream

Миграция:
  gar-importer migrate [--write-sql migrations/0001_ent_init.sql]

Импорт:
  gar-importer import --zip /path/gar_xml.zip [--regions 77,50]
      [--formats AS_HOUSES,AS_ADDR_OBJ] [--batch-size 1000] [--limit-per-file 1000]

Воркер:
  gar-importer worker [--nats nats://127.0.0.1:4222] [--s3-endpoint 127.0.0.1:9000]
      [--s3-bucket gar-archives] [--s3-access-key ...] [--s3-secret-key ...]
      [--metrics-addr :9211] [--migrations migrations/atlas]
  DATABASE_URL читается из окружения или .env; параметры S3 — из env S3_*.

Постановка задачи:
  gar-importer request-import --s3-uri s3://gar-archives/2026-10/gar_xml.zip
      [--regions 77,50] [--limit-per-file 1000] [--batch-size 1000]
      [--nats nats://127.0.0.1:4222]
`

// Run выполняет команду; возвращает ErrUsage для некорректных аргументов.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) < 1 {
		fmt.Fprint(stderr, usage)
		return ErrUsage
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, "gar-importer dev")
		return nil
	case "migrate":
		return finishCommand("migrate", runMigrate(ctx, args[1:], stdout), stdout, stderr)
	case "import":
		return finishCommand("import", runImport(ctx, args[1:], stdout, stderr), stdout, stderr)
	case "worker":
		return finishCommand("worker", runWorker(ctx, args[1:]), stdout, stderr)
	case "request-import":
		return finishCommand("request-import", runRequestImport(ctx, args[1:], stdout), stdout, stderr)
	default:
		fmt.Fprint(stderr, usage)
		return ErrUsage
	}
}

// finishCommand обрабатывает результат подкоманды: запрос помощи (--help)
// выводит общий usage без ошибки, прочие ошибки печатаются с именем команды.
func finishCommand(name string, err error, stdout, stderr io.Writer) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, usage)
		return nil
	}
	fmt.Fprintln(stderr, name+":", err)
	return err
}

func runMigrate(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	plan := flags.Bool("plan", false, "показать неприменённые миграции, не меняя базу")
	baseline := flags.Bool("baseline", false, "пометить существующую legacy-схему как текущую (после проверки)")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%w: неизвестные аргументы: %v", ErrUsage, flags.Args())
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	migrateCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	database, err := storage.Open(migrateCtx, cfg)
	if err != nil {
		return err
	}
	defer database.Close()
	runner, err := migrations.NewRunner(database.SQL(), "migrations/atlas")
	if err != nil {
		return err
	}
	switch {
	case *plan:
		pending, err := runner.Plan(migrateCtx)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			fmt.Fprintln(stdout, "Планируемых миграций нет")
			return nil
		}
		for _, version := range pending {
			fmt.Fprintf(stdout, "ожидается: %s\n", version.Name)
		}
		return nil
	case *baseline:
		recorded, err := runner.Baseline(migrateCtx, storage.ExpectedSchema())
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Baseline: помечено версий: %d\n", len(recorded))
		return nil
	default:
		applied, err := runner.Apply(migrateCtx)
		if err != nil {
			return err
		}
		if len(applied) == 0 {
			fmt.Fprintln(stdout, "Миграции актуальны")
			return nil
		}
		for _, version := range applied {
			fmt.Fprintf(stdout, "применено: %s\n", version.Name)
		}
		fmt.Fprintf(stdout, "Схема %s, таблиц: %d\n", storage.SchemaName, len(migrate.Tables))
		return nil
	}
}

func runImport(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("import", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	archivePath := flags.String("zip", "", "путь к локальному ZIP-архиву ГАР")
	batchSize := flags.Int("batch-size", importjob.DefaultBatchSize, "записей в пакете (1..2000)")
	regionList := flags.String("regions", "", "коды регионов через запятую; справочники включаются всегда")
	formatList := flags.String("formats", "", "форматы через запятую, например AS_HOUSES,AS_ADDR_OBJ,AS_HOUSES_PARAMS")
	limit := flags.Int64("limit-per-file", 0, "первые N записей каждого XML для теста; 0 — весь файл")
	resume := flags.Bool("resume", false, "продолжить последний незавершённый запуск этого архива")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}
	if *archivePath == "" || flags.NArg() != 0 {
		return fmt.Errorf("%w: используйте import --zip /путь/gar_xml.zip", ErrUsage)
	}
	options := importjob.Options{BatchSize: *batchSize, LimitPerFile: *limit}
	if *resume {
		options.ResumeRunID = -1
	}
	regions, err := parseRegions(*regionList)
	if err != nil {
		return err
	}
	options.Regions = regions
	if *formatList != "" {
		for _, token := range strings.Split(*formatList, ",") {
			name := strings.TrimSpace(token)
			if gar.IsParameterPrefix(name) {
				// Точное семейство параметров (например AS_HOUSES_PARAMS)
				// выбирает только свой источник, а не все шесть.
				options.Sources = append(options.Sources, name)
				continue
			}
			kind, err := gar.ParseKind(name)
			if err != nil {
				return err
			}
			options.Kinds = append(options.Kinds, kind)
		}
	}
	if err := importjob.ValidateOptions(options); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := storage.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer database.Close()
	journal, err := control.Open(ctx, database.SQL())
	if err != nil {
		return err
	}
	archive, err := zipgar.Open(*archivePath)
	if err != nil {
		return err
	}
	lastProgress := time.Time{}
	options.Progress = func(stats importjob.FileStats) {
		if stats.Done || stats.Decoded+stats.Written == 0 || time.Since(lastProgress) >= 2*time.Second {
			status := ""
			if stats.Limited {
				status = " (лимит теста)"
			}
			fmt.Fprintf(stderr, "%s: прочитано=%d, записано=%d%s\n", stats.Name, stats.Decoded, stats.Written, status)
			lastProgress = time.Now()
		}
	}
	report, err := importjob.RunWithJournal(ctx, archive, storage.NewWriterFactory(database.Client, journal), journal, options)
	printReport(report, stdout)
	for _, skipped := range report.Skipped {
		fmt.Fprintf(stderr, "Пропущен XML %s: %s\n", skipped.Name, skipped.Reason)
	}
	return err
}

// runWorker запускает фонового исполнителя: задачи из NATS JetStream,
// архивы из S3/MinIO, метрики и проверки на --metrics-addr.
func runWorker(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("worker", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	natsURL := flags.String("nats", envOr("NATS_URL", "nats://127.0.0.1:4222"), "URL NATS JetStream (env NATS_URL)")
	s3env := objectstorage.ConfigFromEnv("")
	s3endpoint := flags.String("s3-endpoint", envOr("S3_ENDPOINT", "127.0.0.1:9000"), "endpoint S3/MinIO (env S3_ENDPOINT)")
	s3bucket := flags.String("s3-bucket", envOr("S3_BUCKET", "gar-archives"), "бакет архивов по умолчанию (env S3_BUCKET)")
	s3accessKey := flags.String("s3-access-key", s3env.AccessKey, "access key S3 (env S3_ACCESS_KEY)")
	s3secretKey := flags.String("s3-secret-key", s3env.SecretKey, "secret key S3 (env S3_SECRET_KEY)")
	s3useSSL := flags.Bool("s3-use-ssl", s3env.UseSSL, "HTTPS для S3 (env S3_USE_SSL)")
	metricsAddr := flags.String("metrics-addr", envOr("WORKER_HTTP_ADDR", ":9211"), "адрес HTTP /metrics,/healthz,/readyz (env WORKER_HTTP_ADDR)")
	migrationsDir := flags.String("migrations", envOr("GAR_MIGRATIONS_DIR", "migrations/atlas"), "каталог миграций Atlas (env GAR_MIGRATIONS_DIR)")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%w: неизвестные аргументы: %v", ErrUsage, flags.Args())
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return worker.Run(ctx, worker.Config{
		Config:  cfg,
		NATSUrl: *natsURL,
		S3: objectstorage.Config{
			Endpoint:  *s3endpoint,
			AccessKey: *s3accessKey,
			SecretKey: *s3secretKey,
			UseSSL:    *s3useSSL,
		},
		S3BucketDefault: *s3bucket,
		MetricsAddr:     *metricsAddr,
		MigrationsDir:   *migrationsDir,
	})
}

// runRequestImport публикует разовую задачу импорта архива из S3 в NATS
// JetStream и печатает RequestID опубликованного запроса.
func runRequestImport(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("request-import", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	s3uri := flags.String("s3-uri", "", "URI архива в хранилище: s3://bucket/key (обязателен)")
	regionList := flags.String("regions", "", "коды регионов через запятую; справочники включаются всегда")
	limit := flags.Int64("limit-per-file", 0, "первые N записей каждого XML для пробы; 0 — весь файл")
	batchSize := flags.Int("batch-size", 0, "записей в пакете (0 — по умолчанию)")
	natsURL := flags.String("nats", envOr("NATS_URL", "nats://127.0.0.1:4222"), "URL NATS JetStream (env NATS_URL)")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}
	if *s3uri == "" || flags.NArg() != 0 {
		return fmt.Errorf("%w: используйте request-import --s3-uri s3://bucket/key", ErrUsage)
	}
	bucket, key, err := parseS3URI(*s3uri)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUsage, err)
	}
	regions, err := parseRegions(*regionList)
	if err != nil {
		return err
	}
	if *limit < 0 || *batchSize < 0 || *batchSize > importjob.MaxBatchSize {
		return fmt.Errorf("%w: неверные --limit-per-file или --batch-size (0..%d)", ErrUsage, importjob.MaxBatchSize)
	}
	requestID, err := newRequestID()
	if err != nil {
		return err
	}
	conn, err := events.Connect(*natsURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	js, err := conn.JetStream()
	if err != nil {
		return fmt.Errorf("контекст JetStream: %w", err)
	}
	if _, err := events.EnsureStream(ctx, js); err != nil {
		return err
	}
	event := events.ImportRequested{
		RequestID:    requestID,
		Bucket:       bucket,
		Key:          key,
		Regions:      regions,
		LimitPerFile: *limit,
		BatchSize:    *batchSize,
		RequestedAt:  time.Now().UTC(),
	}
	if err := events.NewPublisher(js).PublishImportRequested(ctx, event); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Задача опубликована: RequestID=%s\n", requestID)
	return nil
}

// parseS3URI разбирает URI вида s3://bucket/key.
func parseS3URI(uri string) (bucket, key string, err error) {
	rest, ok := strings.CutPrefix(uri, "s3://")
	if !ok {
		return "", "", fmt.Errorf("ожидается URI вида s3://bucket/key, получено %q", uri)
	}
	bucket, key, ok = strings.Cut(rest, "/")
	if !ok || bucket == "" || key == "" {
		return "", "", fmt.Errorf("ожидается URI вида s3://bucket/key, получено %q", uri)
	}
	return bucket, key, nil
}

// parseRegions разбирает коды регионов через запятую; пустая строка — все регионы.
func parseRegions(list string) ([]int, error) {
	if list == "" {
		return nil, nil
	}
	var regions []int
	for _, token := range strings.Split(list, ",") {
		region, err := strconv.Atoi(strings.TrimSpace(token))
		if err != nil || region < 1 || region > 99 {
			return nil, fmt.Errorf("неверный регион %q", token)
		}
		regions = append(regions, region)
	}
	return regions, nil
}

// newRequestID генерирует идентификатор задачи: 16 байт crypto/rand в hex.
func newRequestID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("генерация RequestID: %w", err)
	}
	return hex.EncodeToString(random[:]), nil
}

// envOr возвращает значение переменной окружения или значение по умолчанию.
func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func printReport(report importjob.Report, stdout io.Writer) {
	if report.RunID != 0 {
		fmt.Fprintf(stdout, "Запуск: id=%d\n", report.RunID)
	}
	fmt.Fprintf(stdout, "ZIP: файлов=%d, прочитано=%d, записано=%d, отфильтровано=%d, неизвестных XML=%d, время=%s\n",
		len(report.Files), report.Decoded, report.Written, report.Filtered, len(report.Skipped), report.Duration.Round(time.Millisecond))
	totals := make(map[string]int64)
	for _, file := range report.Files {
		totals[string(file.Kind)] += file.Written
	}
	kinds := make([]string, 0, len(totals))
	for kind := range totals {
		kinds = append(kinds, kind)
	}
	sortStrings(kinds)
	for _, kind := range kinds {
		fmt.Fprintf(stdout, "  %s: %d\n", kind, totals[kind])
	}
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
