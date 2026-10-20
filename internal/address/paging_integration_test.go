//go:build integration

package address

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"gar-importer/internal/search"
)

func TestBuildPagesReleaseConnection(t *testing.T) {
	store := dictionaryStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := store.Exec(ctx, `INSERT INTO gar.houses (id,object_id,object_guid,change_id,house_num,opertype_id,update_date,start_date,end_date,is_actual,is_active)
 SELECT n,n,'10000000-0000-0000-0000-000000000001',1,n::text,1,'2026-10-19','2026-10-19','2079-06-06',true,true FROM generate_series(2,2001) n;
 INSERT INTO gar.adm_hierarchy (id,object_id,change_id,region_code,update_date,start_date,end_date,is_active,path)
 SELECT n,n,1,'077','2026-10-19','2026-10-19','2079-06-06',true,n::text FROM generate_series(1,2001) n;`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Exec(ctx, `INSERT INTO gar.adm_hierarchy (id,object_id,change_id,region_code,update_date,start_date,end_date,is_active,path)
 SELECT n,n,1,'78','2026-10-19','2026-10-19','2079-06-06',true,n::text FROM generate_series(3000,53000) n;
 ANALYZE gar.adm_hierarchy; ANALYZE gar.houses;`)
	if err != nil {
		t.Fatal(err)
	}
	var planJSON []byte
	if err := store.SQL().QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+housesQuery, 77, int64(0), BatchSize).Scan(&planJSON); err != nil {
		t.Fatal(err)
	}
	var plans []map[string]any
	if err := json.Unmarshal(planJSON, &plans); err != nil {
		t.Fatal(err)
	}
	var inspect func(any)
	inspect = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if node["Node Type"] == "Seq Scan" && (node["Relation Name"] == "houses" || node["Relation Name"] == "adm_hierarchy") {
				t.Errorf("full scan: %s", planJSON)
			}
			for _, child := range node {
				inspect(child)
			}
		case []any:
			for _, child := range node {
				inspect(child)
			}
		}
	}
	inspect(plans[0])
	store.SQL().SetMaxOpenConns(1)
	count, pages := 0, 0
	err = NewBuilder(store).BuildHouses(ctx, 77, func(docs []search.Document) error {
		if err := store.SQL().PingContext(ctx); err != nil {
			return fmt.Errorf("cursor held during sink: %w", err)
		}
		pages++
		for _, doc := range docs {
			count++
			want := fmt.Sprint(count)
			if doc.ObjectID != want || doc.Name != map[bool]string{true: "7", false: want}[count == 1] || doc.FullAddress != "д "+doc.Name {
				return fmt.Errorf("unexpected document %d: %+v", count, doc)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2001 || pages != 2 {
		t.Fatalf("count=%d pages=%d", count, pages)
	}
}
