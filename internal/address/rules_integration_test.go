//go:build integration

package address

import (
	"context"
	"testing"

	"gar-importer/internal/search"
)

func TestAddressRulesGolden(t *testing.T) {
	store := dictionaryStore(t)
	ctx := context.Background()
	_, err := store.Exec(ctx, `
 INSERT INTO gar.addr_obj (id,object_id,object_guid,change_id,name,type_name,level,opertype_id,update_date,start_date,end_date,is_actual,is_active) VALUES
 (807356,807356,'10000000-0000-0000-0000-000000000001',1,'Московская','обл','1',1,'2020-01-01','2020-01-01','2079-06-06',true,true),
 (162142236,162142236,'10000000-0000-0000-0000-000000000002',1,'Павлово-Посадский','г.о.','3',1,'2020-01-01','2020-01-01','2079-06-06',true,true),
 (815937,815937,'10000000-0000-0000-0000-000000000003',1,'Павловский Посад','г','5',1,'2020-01-01','2020-01-01','2079-06-06',true,true),
 (828325,828325,'10000000-0000-0000-0000-000000000004',1,'Тихонова','ул','8',1,'2020-01-01','2020-01-01','2079-06-06',true,true);
 INSERT INTO gar.param (source_id,source,object_id,change_id_end,type_id,value,update_date,start_date,end_date) VALUES
 (1,'AS_ADDR_OBJ_PARAMS',807356,0,16,'Московская область','2020-01-01','2020-01-01','2079-06-06'),
 (2,'AS_ADDR_OBJ_PARAMS',162142236,0,16,'Историческое имя','2010-01-01','2010-01-01','2011-01-01'),
 (3,'AS_ADDR_OBJ_PARAMS',815937,0,16,'Не для уровня 5','2020-01-01','2020-01-01','2079-06-06'),
 (4,'AS_HOUSES_PARAMS',807356,0,16,'Чужой источник','2025-01-01','2025-01-01','2079-06-06');
 INSERT INTO gar.adm_hierarchy (id,object_id,change_id,region_code,update_date,start_date,end_date,is_active,path) VALUES
 (807356,807356,1,'050','2020-01-01','2020-01-01','2079-06-06',true,'807356'),
 (1,1,1,NULL,'2020-01-01','2020-01-01','2079-06-06',true,'807356.162142236.815937.828325.1');
 UPDATE gar.houses SET house_num=NULL,add_num1='1',add_type1=1;
 INSERT INTO gar.add_house_types (id,name,shortname,update_date,start_date,end_date,is_active) VALUES
 (1,'корпус','к','2020-01-01','2020-01-01','2079-06-06',true);
 INSERT INTO gar.addr_obj_types (id,level,name,shortname,update_date,start_date,end_date,is_active) VALUES
 (9,9,'земельный участок','з/у','2020-01-01','2020-01-01','2079-06-06',true);
 INSERT INTO gar.steads (id,object_id,object_guid,change_id,number,opertype_id,update_date,start_date,end_date,is_actual,is_active) VALUES
 (2,2,'10000000-0000-0000-0000-000000000005',1,'12',1,'2020-01-01','2020-01-01','2079-06-06',true,true);
 INSERT INTO gar.mun_hierarchy (id,object_id,parent_obj_id,change_id,update_date,start_date,end_date,is_active,path) VALUES
 (2,2,828325,1,'2020-01-01','2020-01-01','2079-06-06',true,'807356.162142236.815937.828325.2');`)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(store)
	prefix := "Московская область, г.о Павлово-Посадский, г Павловский Посад, ул Тихонова, "
	for _, tc := range []struct {
		id                 int64
		build              func(context.Context, int, func([]search.Document) error) error
		name, typ, address string
	}{
		{1, b.BuildHouses, "1", "к", prefix + "к 1"},
		{2, b.BuildSteads, "12", "з/у", prefix + "з/у 12"},
	} {
		count := 0
		if err := tc.build(ctx, 50, func(docs []search.Document) error {
			for _, d := range docs {
				count++
				if d.Name != tc.name || d.TypeName != tc.typ || d.FullAddress != tc.address || d.Region != "50" {
					t.Errorf("document: %+v; want %q", d, tc.address)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("object %d: built %d", tc.id, count)
		}
		d, err := b.DescribeObject(ctx, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if d.Name != tc.name || d.TypeName != tc.typ || d.FullAddress != tc.address || d.Region != "50" {
			t.Errorf("description: %+v; want %q", d, tc.address)
		}
	}

	// If both hierarchies exist, the administrative path wins without duplicates.
	if _, err := store.Exec(ctx, `INSERT INTO gar.mun_hierarchy
 (id,object_id,change_id,update_date,start_date,end_date,is_active,path)
 VALUES (1,1,1,'2020-01-01','2020-01-01','2079-06-06',true,'807356.1')`); err != nil {
		t.Fatal(err)
	}
	countHouses := 0
	if err := b.BuildHouses(ctx, 50, func(docs []search.Document) error {
		for _, d := range docs {
			countHouses++
			if d.FullAddress != prefix+"к 1" {
				t.Errorf("hierarchy priority: %+v", d)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if countHouses != 1 {
		t.Fatalf("duplicate hierarchies: %d", countHouses)
	}

	// Official names apply to the object itself as well as its descendants.
	d, err := b.DescribeObject(ctx, 807356)
	if err != nil || d.FullAddress != "Московская область" {
		t.Fatalf("root: %+v %v", d, err)
	}
	_, err = store.Exec(ctx, `UPDATE gar.houses SET house_num='93',add_num1=NULL;
 INSERT INTO gar.apartments (id,object_id,object_guid,change_id,number,apart_type,opertype_id,update_date,start_date,end_date,is_actual,is_active) VALUES
 (3,3,'10000000-0000-0000-0000-000000000003',1,'17',1,1,'2020-01-01','2020-01-01','2079-06-06',true,true);
 INSERT INTO gar.rooms (id,object_id,object_guid,change_id,number,room_type,opertype_id,update_date,start_date,end_date,is_actual,is_active) VALUES
 (4,4,'10000000-0000-0000-0000-000000000004',1,'2',1,1,'2020-01-01','2020-01-01','2079-06-06',true,true);
 INSERT INTO gar.mun_hierarchy (id,object_id,parent_obj_id,change_id,update_date,start_date,end_date,is_active,path) VALUES
 (4,4,3,1,'2020-01-01','2020-01-01','2079-06-06',true,'807356.162142236.815937.828325.1.3.4');`)
	if err != nil {
		t.Fatal(err)
	}
	want := prefix + "д 93, кв 17, ком 2"
	d, err = b.DescribeObject(ctx, 4)
	if err != nil || d.FullAddress != want {
		t.Fatalf("guide room: %+v %v; want %q", d, err, want)
	}
	count := 0
	if err := b.BuildRooms(ctx, 50, func(docs []search.Document) error {
		for _, d := range docs {
			count++
			if d.FullAddress != want {
				t.Errorf("guide room: %+v; want %q", d, want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("guide room documents: %d", count)
	}

}

func TestOfficialNamesValidity(t *testing.T) {
	store := dictionaryStore(t)
	ctx := context.Background()
	_, err := store.Exec(ctx, `INSERT INTO gar.addr_obj
 (id,object_id,object_guid,change_id,name,type_name,level,opertype_id,update_date,start_date,end_date,is_actual,is_active)
 VALUES (100,100,'10000000-0000-0000-0000-000000000100',1,'Название','тип','4',1,'2020-01-01','2020-01-01','2079-06-06',true,true);
 INSERT INTO gar.param (source_id,source,object_id,change_id_end,type_id,value,update_date,start_date,end_date) VALUES
 (1,'AS_ADDR_OBJ_PARAMS',100,0,16,'Официальное название','2020-01-01','2020-01-01','2079-06-06'),
 (2,'AS_ADDR_OBJ_PARAMS',100,0,16,'Будущее название','2070-01-01','2070-01-01','2079-06-06'),
 (3,'AS_ADDR_OBJ_PARAMS',100,0,16,'Историческое название','2010-01-01','2010-01-01','2011-01-01');`)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(store)
	for _, level := range []string{"1", "3", "4", "5"} {
		if _, err := store.Exec(ctx, `UPDATE gar.addr_obj SET level=$1`, level); err != nil {
			t.Fatal(err)
		}
		d, err := b.DescribeObject(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		want := "Официальное название"
		if level == "5" {
			want = "тип Название"
		}
		if d.FullAddress != want {
			t.Errorf("level %s: %q want %q", level, d.FullAddress, want)
		}
	}
	if _, err := store.Exec(ctx, `UPDATE gar.addr_obj SET level='4'; UPDATE gar.param SET end_date='2011-01-01' WHERE source_id=1`); err != nil {
		t.Fatal(err)
	}
	d, err := b.DescribeObject(ctx, 100)
	if err != nil || d.FullAddress != "тип Название" {
		t.Fatalf("expired: %+v %v", d, err)
	}
}
