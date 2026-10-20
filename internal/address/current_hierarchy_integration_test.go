//go:build integration

package address

import (
	"context"
	"testing"

	"gar-importer/internal/search"
)

func TestMovedAndInactiveObjectsAreNotBuilt(t *testing.T) {
	store := dictionaryStore(t)
	ctx := context.Background()
	_, err := store.Exec(ctx, `INSERT INTO gar.adm_hierarchy (id,object_id,change_id,region_code,update_date,start_date,end_date,is_active,path) VALUES
 (1,1,1,'77','2026-10-19','2026-10-19','2026-10-19',false,'1'),
 (2,1,2,'78','2026-10-20','2026-10-20','2079-06-06',true,'1')`)
	if err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder(store)
	count := func(region int) int {
		t.Helper()
		total := 0
		if err := builder.BuildHouses(ctx, region, func(docs []search.Document) error { total += len(docs); return nil }); err != nil {
			t.Fatal(err)
		}
		return total
	}
	if got := count(77); got != 0 {
		t.Fatalf("historical region still builds %d documents", got)
	}
	if got := count(78); got != 1 {
		t.Fatalf("current region builds %d", got)
	}
	if _, err := store.Exec(ctx, `UPDATE gar.houses SET is_active=false`); err != nil {
		t.Fatal(err)
	}
	if got := count(78); got != 0 {
		t.Fatalf("inactive object still built: %d", got)
	}
}
