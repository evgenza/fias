//go:build integration

package address

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"gar-importer/internal/config"
	"gar-importer/internal/gartype"
	"gar-importer/internal/migrations"
	"gar-importer/internal/storage"
)

func dictionaryStore(t *testing.T) *storage.Store {
	t.Helper()
	dsn := os.Getenv("GAR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set GAR_TEST_DATABASE_URL to an empty test database")
	}
	ctx := context.Background()
	store, err := storage.Open(ctx, config.Config{DatabaseURL: dsn, MaxOpenConns: 8, MaxIdleConns: 8, ConnectTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.Exec(ctx, `DROP SCHEMA IF EXISTS gar CASCADE; DROP SCHEMA IF EXISTS gar_control CASCADE`); err != nil {
		t.Fatal(err)
	}
	runner, err := migrations.NewRunner(store.SQL(), "../../migrations/atlas")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	date := gartype.Date("2026-10-19")
	if _, err := store.Client.House.Create().SetID(1).SetObjectID(1).
		SetObjectGUID("10000000-0000-0000-0000-000000000001").SetChangeID(1).
		SetHouseNum("7").SetHouseType(gartype.Integer("2")).SetOpertypeID(gartype.Integer("1")).
		SetUpdateDate(date).SetStartDate(date).SetEndDate(date).SetIsActual(true).SetIsActive(true).Save(ctx); err != nil {
		t.Fatal(err)
	}
	return store
}

func addDictionary(t *testing.T, store *storage.Store) {
	t.Helper()
	date := gartype.Date("2026-10-19")
	if _, err := store.Client.HouseType.Create().SetID(2).SetName("владение").SetShortName("влд").
		SetUpdateDate(date).SetStartDate(date).SetEndDate(date).SetIsActive(true).Save(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyDictionariesRefresh(t *testing.T) {
	store := dictionaryStore(t)
	builder := NewBuilder(store)
	before, err := builder.DescribeObject(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if before.TypeName != "д" {
		t.Fatalf("empty fallback: %+v", before)
	}
	addDictionary(t, store)
	after, err := builder.DescribeObject(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if after.TypeName != "влд" {
		t.Fatalf("dictionary stayed empty: %+v", after)
	}
	if _, err := store.Client.HouseType.UpdateOneID(2).SetShortName("влад").Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	builder.InvalidateDictionaries()
	updated, err := builder.DescribeObject(context.Background(), 1)
	if err != nil || updated.TypeName != "влад" {
		t.Fatalf("invalidation failed: %+v %v", updated, err)
	}
	if _, err := store.Client.HouseType.UpdateOneID(2).SetShortName("вл").Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	builder.dictsMu.Lock()
	builder.dictsLoadedAt = time.Now().Add(-2 * DictionaryTTL)
	builder.dictsMu.Unlock()
	expired, err := builder.DescribeObject(context.Background(), 1)
	if err != nil || expired.TypeName != "вл" {
		t.Fatalf("TTL refresh failed: %+v %v", expired, err)
	}

}

func TestConcurrentDescriptions(t *testing.T) {
	store := dictionaryStore(t)
	addDictionary(t, store)
	builder := NewBuilder(store)
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 24 {
			builder.InvalidateDictionaries()
		}
	})
	for range 24 {
		wg.Go(func() {
			for range 3 {
				desc, err := builder.DescribeObject(context.Background(), 1)
				if err != nil {
					t.Error(err)
					return
				}
				if desc.TypeName != "влд" {
					t.Errorf("wrong type: %q", desc.TypeName)
				}
			}
		})
	}
	wg.Wait()
}
