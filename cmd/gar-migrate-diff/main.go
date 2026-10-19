// Команда gar-migrate-diff генерирует следующую версионированную миграцию:
// сравнивает фактическое состояние схемы на dev-базе с описанием Ent
// и дописывает файл NNN_name.sql (+ atlas.sum) в каталог migrations/atlas.
//
//	go run ./cmd/gar-migrate-diff -name add_x -dev-url postgres://.../gar_dev
//
// dev-url должен указывать на одноразовую базу, применяющую текущий каталог
// миграций; целевая база не используется и не изменяется.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	"gar-importer/ent/migrate"
	"gar-importer/internal/config"

	atlasmigrate "ariga.io/atlas/sql/migrate"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/schema"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	name := flag.String("name", "", "имя новой миграции")
	devURL := flag.String("dev-url", "", "URL одноразовой dev-базы (целевая база не используется)")
	dirPath := flag.String("dir", "migrations/atlas", "каталог миграций")
	flag.Parse()
	if *name == "" || *devURL == "" {
		fmt.Fprintln(os.Stderr, "gar-migrate-diff: укажите -name и -dev-url")
		os.Exit(2)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gar-migrate-diff:", err)
		os.Exit(1)
	}
	_ = cfg // DATABASE_URL не нужен: целевая база в diff не участвует

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dev, err := sql.Open("pgx", *devURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gar-migrate-diff:", err)
		os.Exit(1)
	}
	defer dev.Close()
	if err := dev.PingContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "gar-migrate-diff: connect dev:", err)
		os.Exit(1)
	}
	dir, err := atlasmigrate.NewLocalDir(*dirPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gar-migrate-diff:", err)
		os.Exit(1)
	}
	driver := entsql.OpenDB("postgres", dev)
	defer driver.Close()
	m, err := schema.NewMigrate(driver,
		schema.WithDir(dir),
		schema.WithSchemaName("gar"),
		migrate.WithForeignKeys(false),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gar-migrate-diff:", err)
		os.Exit(1)
	}
	if err := m.NamedDiff(ctx, *name, migrate.Tables...); err != nil {
		fmt.Fprintln(os.Stderr, "gar-migrate-diff:", err)
		os.Exit(1)
	}
	fmt.Printf("migration written to %s\n", *dirPath)
}
