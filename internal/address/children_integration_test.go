//go:build integration

package address

import (
	"context"
	"strings"
	"testing"
)

func TestBatchDescriptions(t *testing.T) {
	store := dictionaryStore(t)
	b := NewBuilder(store)
	got, err := b.DescribeObjects(context.Background(), []int64{1, 999999})
	if err != nil || len(got) != 1 {
		t.Fatalf("batch: %+v %v", got, err)
	}
	single, err := b.DescribeObject(context.Background(), 1)
	if err != nil || *single != *got[0] {
		t.Fatalf("single %+v batch %+v error %v", single, got[0], err)
	}
	exists, err := ObjectExists(context.Background(), store, 999999)
	if err != nil || exists {
		t.Fatalf("missing parent: %v %v", exists, err)
	}
}

func TestBatchDescriptionsQueryBudget(t *testing.T) {
	store := dictionaryStore(t)
	ctx := context.Background()
	var preload string
	if err := store.SQL().QueryRowContext(ctx, `SHOW shared_preload_libraries`).Scan(&preload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preload, "pg_stat_statements") {
		t.Skip("requires PostgreSQL shared_preload_libraries=pg_stat_statements")
	}
	if _, err := store.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements WITH SCHEMA public;
 INSERT INTO gar.houses (id,object_id,object_guid,change_id,house_num,opertype_id,update_date,start_date,end_date,is_actual,is_active)
 SELECT n,n,'10000000-0000-0000-0000-000000000001',1,n::text,1,'2020-01-01','2020-01-01','2079-06-06',true,true FROM generate_series(2,201) n;
 INSERT INTO gar.reestr_objects (object_id,object_guid,change_id,level_id,is_active,create_date,update_date) SELECT n,('10000000-0000-0000-0000-'||lpad(n::text,12,'0'))::uuid,1,10,true,'2020-01-01','2020-01-01' FROM generate_series(2,201) n;
 INSERT INTO gar.adm_hierarchy (id,object_id,parent_obj_id,change_id,update_date,start_date,end_date,is_active,path)
 SELECT n,n,999999,1,'2020-01-01','2020-01-01','2079-06-06',true,'999999.'||n FROM generate_series(2,201) n;
 INSERT INTO gar.adm_hierarchy (id,object_id,change_id,update_date,start_date,end_date,is_active,path)
 VALUES (999999,999999,1,'2020-01-01','2020-01-01','2079-06-06',true,'999999');
 SELECT public.pg_stat_statements_reset();`); err != nil {
		t.Fatal(err)
	}
	exists, err := ObjectExists(ctx, store, 999999)
	if err != nil || !exists {
		t.Fatalf("hierarchy parent: %v %v", exists, err)
	}
	ids, err := ListChildren(ctx, store, 999999, 200)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewBuilder(store).DescribeObjects(ctx, ids)
	if err != nil || len(got) != 200 {
		t.Fatalf("batch size %d: %v", len(got), err)
	}
	var count int
	if err := store.SQL().QueryRowContext(ctx, `SELECT COALESCE(sum(calls),0)::int FROM public.pg_stat_statements WHERE query LIKE 'SELECT%' AND query NOT LIKE '%pg_stat_statements%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count > 10 {
		t.Fatalf("query count=%d > 10", count)
	}
	t.Logf("200 children, cold dictionary cache: %d SQL queries", count)
}
