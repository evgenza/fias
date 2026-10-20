// Package migrations — раннер версионированных SQL-миграций (формат каталога
// Atlas: файлы NNN_name.sql + atlas.sum, генерируются Ent/Atlas).
//
// Гарантии: применение только вперёд, по одной версии в одной транзакции
// вместе с записью в реестр ревизий; блокировка параллельного применения
// через advisory lock на закреплённом соединении; проверка контрольных сумм
// каталога (atlas.sum) и уже применённых версий; baseline для существующих
// БД — только после проверки фактического состояния схемы.
package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"ariga.io/atlas/sql/migrate"
)

// migrationLockKey — фиксированный ключ advisory-блокировки применения.
const migrationLockKey = int64(0x6761725F6D6967) // "gar_mig"

// revisionsTable — реестр применённых версий в формате Atlas
// (atlas_schema_revisions: будущий atlas CLI увидит историю).
const revisionsTable = "atlas_schema_revisions"

// ControlSchema — служебная схема управления (расширяется на этапах 6–7).
const ControlSchema = "gar_control"

// DataSchema — схема рабочих данных GAR.
const DataSchema = "gar"

// Version — одна миграция/ревизия.
type Version struct {
	Name      string
	Checksum  string
	AppliedAt time.Time
}

// dbtx — минимум SQL-операций, нужных раннеру (соединение или база).
type dbtx interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// Runner применяет версионированные миграции к целевой базе.
type Runner struct {
	dirPath string
	db      dbtx
}

// NewRunner принимает соединение с целевой базой и каталог миграций.
func NewRunner(db *sql.DB, dirPath string) (*Runner, error) {
	if _, err := migrate.NewLocalDir(dirPath); err != nil {
		return nil, fmt.Errorf("migrations: открытие каталога: %w", err)
	}
	return &Runner{dirPath: dirPath, db: db}, nil
}

// checksumFile считает sha256 содержимого файла миграции.
func checksumFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// files возвращает SQL-файлы каталога в порядке версий с контрольными суммами.
func (r *Runner) files() ([]Version, error) {
	entries, err := os.ReadDir(r.dirPath)
	if err != nil {
		return nil, err
	}
	var files []Version
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue // down-файлы каталога не применяются раннером
		}
		checksum, err := checksumFile(r.dirPath + "/" + name)
		if err != nil {
			return nil, err
		}
		files = append(files, Version{Name: name, Checksum: checksum})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

// Validate сверяет каталог с atlas.sum (обнаруживает правки задним числом).
func (r *Runner) Validate() error {
	dir, err := migrate.NewLocalDir(r.dirPath)
	if err != nil {
		return fmt.Errorf("migrations: открытие каталога: %w", err)
	}
	if err := migrate.Validate(dir); err != nil {
		return fmt.Errorf("migrations: проверка каталога не пройдена: %w", err)
	}
	return nil
}

// Plan возвращает неприменённые версии; читает только целевую базу
// (таблица ревизий не создаётся).
func (r *Runner) Plan(ctx context.Context) ([]Version, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	files, err := r.files()
	if err != nil {
		return nil, err
	}
	applied, err := r.applied(ctx)
	if err != nil {
		return nil, err
	}
	var pending []Version
	for _, file := range files {
		version, exists := applied[file.Name]
		if !exists {
			pending = append(pending, file)
			continue
		}
		if version.Checksum != file.Checksum {
			return nil, fmt.Errorf("migrations: %s: несовпадение контрольной суммы: в реестре %q, в каталоге %q",
				file.Name, version.Checksum, file.Checksum)
		}
	}
	return pending, nil
}

// applied читает реестр ревизий, если он существует.
func (r *Runner) applied(ctx context.Context) (map[string]Version, error) {
	result := make(map[string]Version)
	exists, err := r.tableExists(ctx, ControlSchema, revisionsTable)
	if err != nil {
		return nil, err
	}
	if !exists {
		return result, nil
	}
	rows, err := r.db.QueryContext(ctx,
		fmt.Sprintf("SELECT version, hash, applied FROM %s.%s", ControlSchema, revisionsTable))
	if err != nil {
		return nil, fmt.Errorf("migrations: чтение реестра версий: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			version    string
			hash       string
			appliedMts int64
		)
		if err := rows.Scan(&version, &hash, &appliedMts); err != nil {
			return nil, err
		}
		result[version] = Version{Name: version, Checksum: hash,
			AppliedAt: time.UnixMilli(appliedMts)}
	}
	return result, rows.Err()
}

func (r *Runner) tableExists(ctx context.Context, schemaName, table string) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2)`,
		schemaName, table).Scan(&exists)
	return exists, err
}

// Apply применяет ожидающие миграции. Всё выполняется на одном закреплённом
// соединении под advisory-блокировкой: вторая параллельная команда ждёт,
// затем видит применённые версии и ничего не делает. Потеря соединения
// снимает блокировку и прекращает владение.
func (r *Runner) Apply(ctx context.Context) ([]Version, error) {
	pool, ok := r.db.(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("migrations: для применения требуется *sql.DB")
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrations: получение соединения: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SELECT pg_advisory_lock(%d)", migrationLockKey)); err != nil {
		return nil, fmt.Errorf("migrations: получение блокировки: %w", err)
	}
	defer func() {
		// Служебный контекст: основной может быть уже отменён.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(releaseCtx, fmt.Sprintf("SELECT pg_advisory_unlock(%d)", migrationLockKey))
	}()
	// DDL каталога использует неквалифицированные имена: сессия работает
	// в схеме данных; отсутствие схемы создаётся до применения.
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", DataSchema)); err != nil {
		return nil, fmt.Errorf("migrations: создание схемы: %w", err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", DataSchema)); err != nil {
		return nil, fmt.Errorf("migrations: установка search_path: %w", err)
	}
	lockedRunner := &Runner{dirPath: r.dirPath, db: conn}
	pending, err := lockedRunner.Plan(ctx)
	if err != nil {
		return nil, err
	}
	if len(pending) == 0 {
		return nil, nil
	}
	if err := lockedRunner.ensureControlSchema(ctx); err != nil {
		return nil, err
	}
	applied := make([]Version, 0, len(pending))
	for _, version := range pending {
		started := time.Now()
		if err := lockedRunner.applyVersion(ctx, version, started); err != nil {
			return applied, fmt.Errorf("migrations: %s: %w", version.Name, err)
		}
		applied = append(applied, version)
	}
	return applied, nil
}

// applyVersion исполняет один файл миграции и записывает ревизию
// в одной транзакции.
func (r *Runner) applyVersion(ctx context.Context, version Version, started time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Откат после Commit — no-op (ErrTxDone), ошибка намеренно игнорируется.
	defer func() { _ = tx.Rollback() }()
	content, err := os.ReadFile(r.dirPath + "/" + version.Name)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s.%s (version, description, type, applied, total_time, hash, operator)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ControlSchema, revisionsTable),
		version.Name, strings.TrimSuffix(version.Name, ".sql"), 2,
		time.Now().UnixMilli(), time.Since(started).Milliseconds(), version.Checksum, "gar-importer")
	if err != nil {
		return fmt.Errorf("запись ревизии: %w", err)
	}
	return tx.Commit()
}

func (r *Runner) ensureControlSchema(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", ControlSchema)); err != nil {
		return fmt.Errorf("migrations: создание %s: %w", ControlSchema, err)
	}
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.%s (
		version character varying(255) NOT NULL,
		description text NULL,
		type int NULL,
		applied bigint NULL,
		total_time bigint NULL,
		hash character varying(255) NULL,
		operator character varying(255) NULL,
		PRIMARY KEY (version)
	)`, ControlSchema, revisionsTable))
	if err != nil {
		return fmt.Errorf("migrations: создание %s.%s: %w", ControlSchema, revisionsTable, err)
	}
	return nil
}

// Baseline регистрирует существующие версии как применённые, проверив фактическое
// состояние схемы против ожидаемой (ent-метаданные): слепая пометка недопустима.
// expected описывает требуемые таблицы и столбцы; отсутствующие — ошибка с
// указанием пути восстановления.
func (r *Runner) Baseline(ctx context.Context, expected map[string][]string) ([]Version, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	files, err := r.files()
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("migrations: в каталоге нет файлов миграций для baseline")
	}
	applied, err := r.applied(ctx)
	if err != nil {
		return nil, err
	}
	if len(applied) > 0 {
		return nil, fmt.Errorf("migrations: история уже записана; baseline предназначен только для legacy-баз")
	}
	for table, columns := range expected {
		exists, err := r.tableExists(ctx, DataSchema, table)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("migrations: baseline отклонён: таблица %s.%s не существует; "+
				"примените миграции на пустой базе или исправьте legacy-схему", DataSchema, table)
		}
		for _, column := range columns {
			var exists bool
			if err := r.db.QueryRowContext(ctx,
				`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 AND column_name = $3)`,
				DataSchema, table, column).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				return nil, fmt.Errorf("migrations: baseline отклонён: в таблице %s.%s нет столбца %s; "+
					"сначала исправьте legacy-схему (см. migrations/legacy/README.md)", DataSchema, table, column)
			}
		}
	}
	if err := r.ensureControlSchema(ctx); err != nil {
		return nil, err
	}
	recorded := make([]Version, 0, len(files))
	for _, version := range files {
		if _, exists := applied[version.Name]; exists {
			continue
		}
		// Baseline только регистрирует историю: исполнение DDL на legacy-базе
		// недопустимо, схема уже приведена вручную.
		if err := r.recordVersion(ctx, version, time.Now()); err != nil {
			return recorded, fmt.Errorf("migrations: baseline %s: %w", version.Name, err)
		}
		recorded = append(recorded, version)
	}
	return recorded, nil
}

// recordVersion пишет ревизию без исполнения файла (baseline).
func (r *Runner) recordVersion(ctx context.Context, version Version, started time.Time) error {
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO %s.%s (version, description, type, applied, total_time, hash, operator)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ControlSchema, revisionsTable),
		version.Name, strings.TrimSuffix(version.Name, ".sql"), 2,
		time.Now().UnixMilli(), time.Since(started).Milliseconds(), version.Checksum, "gar-importer (baseline)")
	return err
}
