// Package storage подключает генерируемый Ent-клиент к PostgreSQL
// и реализует порт пакетной записи importjob.Writer на Ent.
package storage

import (
	"context"
	"database/sql"
	"fmt"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"gar-importer/ent"
	"gar-importer/ent/migrate"
	"gar-importer/internal/config"
	"gar-importer/internal/control"
	"gar-importer/internal/gar"
	"gar-importer/internal/importjob"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const SchemaName = "gar"

type Store struct {
	Client *ent.Client
	db     *sql.DB
}

// Open подключается к PostgreSQL с настройками пула из конфигурации.
func Open(ctx context.Context, cfg config.Config) (*Store, error) {
	parsed, err := pgx.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("разбор строки подключения PostgreSQL (%s): %w", config.RedactedDSN(cfg.DatabaseURL), err)
	}
	// Схемы Ent используют один namespace; public не участвует в запросах.
	parsed.RuntimeParams["search_path"] = SchemaName
	db := stdlib.OpenDB(*parsed)
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("подключение к PostgreSQL (%s): %w", config.RedactedDSN(cfg.DatabaseURL), err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	return &Store{Client: client, db: db}, nil
}

func (s *Store) Close() error { return s.Client.Close() }

// SQL отдаёт пул соединений для служебных операций (миграции).
func (s *Store) SQL() *sql.DB { return s.db }

// Exec исполняет сырой SQL (служебные операции, сброс схемы в тестах).
func (s *Store) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, query, args...)
}

// WriterFactory реализует порт importjob.WriterFactory поверх Ent:
// каждый коммит пакета — транзакция, в которой вместе с данными
// фиксируется checkpoint файла журнала.
type WriterFactory struct {
	client  *ent.Client
	journal *control.Journal
}

// NewWriterFactory создаёт фабрику; journal может быть nil (без журнала).
func NewWriterFactory(client *ent.Client, journal *control.Journal) *WriterFactory {
	return &WriterFactory{client: client, journal: journal}
}

// BeginFile создаёт пакетный writer формата и подключает checkpoint.
func (f *WriterFactory) BeginFile(ctx context.Context, entry gar.Entry, batchSize int, baseCommitted, fileID int64) (importjob.Writer, error) {
	batch, err := ent.NewGARBatch(f.client, entry.Kind, entry.Source, batchSize)
	if err != nil {
		return nil, err
	}
	if f.journal != nil && fileID > 0 {
		if setter, ok := batch.(interface {
			SetCheckpoint(func(ctx context.Context, drv dialect.Driver, received int64) error)
		}); ok {
			journal, base := f.journal, baseCommitted
			setter.SetCheckpoint(func(ctx context.Context, drv dialect.Driver, received int64) error {
				return journal.CommitFile(ctx, dialectExec{drv}, fileID, base+received, base+received)
			})
		}
	}
	return batch, nil
}

// dialectExec адаптирует драйвер Ent (внутри транзакции пакета)
// к интерфейсу checkpoint-SQL журнала.
type dialectExec struct{ drv dialect.Driver }

func (e dialectExec) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	var result sql.Result
	if err := e.drv.Exec(ctx, query, args, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ExpectedSchema возвращает ожидаемые таблицы и столбцы из метаданных Ent
// (для проверки состояния legacy-базы перед baseline).
func ExpectedSchema() map[string][]string {
	expected := make(map[string][]string, len(migrate.Tables))
	for _, table := range migrate.Tables {
		columns := make([]string, 0, len(table.Columns))
		for _, column := range table.Columns {
			columns = append(columns, column.Name)
		}
		expected[table.Name] = columns
	}
	return expected
}
