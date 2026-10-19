//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"gar-importer/internal/migrations"
	"gar-importer/internal/storage"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func sqlOpen(databaseURL string) (*sql.DB, error) { return sql.Open("pgx", databaseURL) }

func testDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("GAR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set GAR_TEST_DATABASE_URL to an empty test database")
	}
	db, err := sqlOpen(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func resetAll(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, schemaName := range []string{migrations.DataSchema, migrations.ControlSchema} {
		if _, err := db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
}

const migrationsDir = "../../migrations/atlas"

func newRunner(t *testing.T, db *sql.DB) *migrations.Runner {
	t.Helper()
	runner, err := migrations.NewRunner(db, migrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestMigrateEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	db := testDB(t, ctx)
	resetAll(t, ctx, db)
	runner := newRunner(t, db)

	// Plan на пустой базе не создаёт ничего.
	if _, err := runner.Plan(ctx); err != nil {
		t.Fatal(err)
	}
	var controlExists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = $1)`,
		migrations.ControlSchema).Scan(&controlExists); err != nil {
		t.Fatal(err)
	}
	if controlExists {
		t.Fatal("Plan created control schema (must be read-only)")
	}

	applied, err := runner.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) == 0 {
		t.Fatal("no migrations applied on empty database")
	}
	var tables int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = $1`,
		migrations.DataSchema).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 23 {
		t.Fatalf("tables after apply: %d, want 23", tables)
	}

	// Повторный apply — no-op.
	second, err := runner.Apply(ctx)
	if err != nil || len(second) != 0 {
		t.Fatalf("re-apply: %d migrations, %v", len(second), err)
	}
}

func TestMigrateChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	db := testDB(t, ctx)
	resetAll(t, ctx, db)
	runner := newRunner(t, db)
	if _, err := runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	// Подделка реестра: сумма применённой версии заменяется.
	if _, err := db.ExecContext(ctx,
		`UPDATE gar_control.atlas_schema_revisions SET hash = 'tampered'`); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Plan(ctx); err == nil {
		t.Fatal("checksum mismatch not detected")
	}
}

func TestMigrateConcurrentApply(t *testing.T) {
	ctx := context.Background()
	db := testDB(t, ctx)
	resetAll(t, ctx, db)
	// Два независимых пула: параллельный apply сериализуется блокировкой.
	cfgURL := os.Getenv("GAR_TEST_DATABASE_URL")
	other, err := sqlOpen(cfgURL)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	first, second := make(chan error, 1), make(chan error, 1)
	go func() {
		runner, err := migrations.NewRunner(db, migrationsDir)
		if err != nil {
			first <- err
			return
		}
		_, err = runner.Apply(ctx)
		first <- err
	}()
	go func() {
		runner, err := migrations.NewRunner(other, migrationsDir)
		if err != nil {
			second <- err
			return
		}
		_, err = runner.Apply(ctx)
		second <- err
	}()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	var revisions int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM gar_control.atlas_schema_revisions`).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions == 0 {
		t.Fatal("no revisions recorded")
	}
}

func TestMigrateBaselineRefusesLegacy(t *testing.T) {
	ctx := context.Background()
	db := testDB(t, ctx)
	resetAll(t, ctx, db)
	// Legacy-схема: только часть таблиц, без gar_control.
	if _, err := db.ExecContext(ctx,
		`CREATE SCHEMA gar; CREATE TABLE gar.param (id bigint NOT NULL PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	runner := newRunner(t, db)
	if _, err := runner.Baseline(ctx, storage.ExpectedSchema()); err == nil {
		t.Fatal("baseline accepted incomplete legacy schema")
	}
	var revisions int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = $1 AND table_name = $2`,
		migrations.ControlSchema, "atlas_schema_revisions").Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if revisions != 0 {
		t.Fatal("baseline recorded revisions on refusal")
	}
}

func TestMigrateBaselineAfterRepair(t *testing.T) {
	ctx := context.Background()
	db := testDB(t, ctx)
	resetAll(t, ctx, db)
	runner := newRunner(t, db)
	// Полная схема создаётся apply, затем история удаляется — имитация
	// legacy-базы, приведённой к ожидаемой схеме вручную.
	if _, err := runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "DROP SCHEMA gar_control CASCADE"); err != nil {
		t.Fatal(err)
	}
	recorded, err := runner.Baseline(ctx, storage.ExpectedSchema())
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded) == 0 {
		t.Fatal("baseline recorded nothing")
	}
	// После baseline apply ничего не делает.
	pending, err := runner.Plan(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after baseline: %d, %v", len(pending), err)
	}
}
