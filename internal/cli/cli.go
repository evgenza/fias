// Package cli — разбор флагов, вывод и коды завершения gar-importer.
// Точка входа main только подключает сигналы и вызывает Run.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"gar-importer/ent/migrate"
	"gar-importer/internal/config"
	"gar-importer/internal/control"
	"gar-importer/internal/gar"
	"gar-importer/internal/importjob"
	"gar-importer/internal/input/zipgar"
	"gar-importer/internal/migrations"
	"gar-importer/internal/storage"
)

// ErrUsage означает ошибку в аргументах командной строки (код выхода 2).
var ErrUsage = errors.New("cli: usage error")

const usage = `gar-importer — подготовка выгрузки ГАР (ФИАС) для поискового индекса

Использование:
  gar-importer <команда> [аргументы]

Команды:
  version  — версия сборки
  migrate  — применить схемы Ent к PostgreSQL (DATABASE_URL из окружения или .env)
  import   — потоковый импорт локального ZIP в таблицы PostgreSQL через Ent

Миграция:
  gar-importer migrate [--write-sql migrations/0001_ent_init.sql]

Импорт:
  gar-importer import --zip /path/gar_xml.zip [--regions 77,50]
      [--formats AS_HOUSES,AS_ADDR_OBJ] [--batch-size 1000] [--limit-per-file 1000]
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
		if err := runMigrate(ctx, args[1:], stdout); err != nil {
			fmt.Fprintln(stderr, "migrate:", err)
			return err
		}
		return nil
	case "import":
		if err := runImport(ctx, args[1:], stdout, stderr); err != nil {
			fmt.Fprintln(stderr, "import:", err)
			return err
		}
		return nil
	default:
		fmt.Fprint(stderr, usage)
		return ErrUsage
	}
}

func runMigrate(ctx context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	plan := flags.Bool("plan", false, "показать неприменённые миграции, не меняя базу")
	baseline := flags.Bool("baseline", false, "пометить существующую legacy-схему как текущую (после проверки)")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", ErrUsage, err)
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
		return fmt.Errorf("%w: %v", ErrUsage, err)
	}
	if *archivePath == "" || flags.NArg() != 0 {
		return fmt.Errorf("%w: используйте import --zip /путь/gar_xml.zip", ErrUsage)
	}
	options := importjob.Options{BatchSize: *batchSize, LimitPerFile: *limit}
	if *resume {
		options.ResumeRunID = -1
	}
	if *regionList != "" {
		for _, token := range strings.Split(*regionList, ",") {
			region, err := strconv.Atoi(strings.TrimSpace(token))
			if err != nil || region < 1 || region > 99 {
				return fmt.Errorf("неверный регион %q", token)
			}
			options.Regions = append(options.Regions, region)
		}
	}
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
