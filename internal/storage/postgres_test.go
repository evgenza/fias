//go:build integration

package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"reflect"
	"testing"
	"time"

	"gar-importer/ent"
	"gar-importer/ent/addrobj"
	"gar-importer/internal/config"
	"gar-importer/internal/gartype"
	"gar-importer/internal/migrations"
	"gar-importer/internal/storage"
)

// Тест пишет реальные строки в отдельную тестовую БД (схема сбрасывается
// перед запуском): durability-проверки не должны прятаться за откатом.
func TestPostgresRoundTrip(t *testing.T) {
	databaseURL := os.Getenv("GAR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set GAR_TEST_DATABASE_URL to an empty test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := storage.Open(ctx, config.Config{
		DatabaseURL:    databaseURL,
		MaxOpenConns:   4,
		MaxIdleConns:   4,
		ConnectTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(ctx, "DROP SCHEMA IF EXISTS gar CASCADE"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, "DROP SCHEMA IF EXISTS "+migrations.ControlSchema+" CASCADE"); err != nil {
		t.Fatal(err)
	}
	runner, err := migrations.NewRunner(db.SQL(), "../../migrations/atlas")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Apply(ctx); err != nil {
		t.Fatal(err)
	}
	client := db.Client
	data, err := os.ReadFile("../gar/record/testdata/records.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct{ Model, XML string }
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	prototypes := []any{ent.AddrObj{}, ent.House{}, ent.Stead{}, ent.Apartment{},
		ent.Room{}, ent.Carplace{}, ent.ReestrObject{}, ent.AdmHierarchyItem{},
		ent.MunHierarchyItem{}, ent.AddrObjDivision{}, ent.ChangeHistory{}, ent.Param{},
		ent.AddrObjType{}, ent.ApartmentType{}, ent.HouseType{}, ent.RoomType{},
		ent.OperationType{}, ent.ParamType{}, ent.ObjectLevel{}, ent.NormativeDoc{},
		ent.NormativeDocKind{}, ent.NormativeDocType{}, ent.AddHouseType{}}
	types := make(map[string]reflect.Type)
	for _, prototype := range prototypes {
		typ := reflect.TypeOf(prototype)
		types[typ.Name()] = typ
	}
	if len(fixtures) != len(prototypes) {
		t.Fatalf("fixtures: %d, models: %d", len(fixtures), len(prototypes))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Model, func(t *testing.T) {
			value := reflect.New(types[fixture.Model])
			if err := xml.Unmarshal([]byte(fixture.XML), value.Interface()); err != nil {
				t.Fatal(err)
			}
			// Значения выявляют переполнение bigint, потерю точности и ведущих нулей.
			switch record := value.Interface().(type) {
			case *ent.Stead:
				record.ID, record.ObjectID = "9999999999999999999", "9999999999999999998"
				record.ChangeID, record.OpertypeID = "9999999999999999997", "01"
				prev := gartype.Integer("-9999999999999999999")
				record.PrevID = &prev
			case *ent.OperationType:
				record.ID = "123456789012345678901234567890"
			case *ent.ApartmentType:
				record.ID = 9999999999
			case *ent.AddrObj:
				record.ID, record.ObjectID, record.Level = 9223372036854775807, 9007199254740993, "03"
			case *ent.House:
				housetype := gartype.Integer("123456789012345678901234567890")
				record.HouseType = &housetype
			case *ent.NormativeDoc:
				record.DocDate = "-0001-01-01"
				regdate := gartype.Date("10000-01-01")
				record.RegDate = &regdate
			}
			roundTrip(t, ctx, client, value)
			if record, ok := value.Interface().(*ent.AddrObj); ok {
				if err := client.AddrObj.Create().SetID(record.ID).
					SetObjectID(record.ObjectID).SetObjectGUID(record.ObjectGUID).
					SetChangeID(record.ChangeID).SetName("Изменённое название").
					SetTypeName(record.TypeName).SetLevel(record.Level).SetOpertypeID(record.OpertypeID).
					SetUpdateDate(record.UpdateDate).SetStartDate(record.StartDate).SetEndDate(record.EndDate).
					SetIsActual(record.IsActual).SetIsActive(record.IsActive).
					OnConflictColumns(addrobj.FieldID).UpdateNewValues().Exec(ctx); err != nil {
					t.Fatal(err)
				}
				got, err := client.AddrObj.Get(ctx, record.ID)
				if err != nil || got.Name != "Изменённое название" {
					t.Fatalf("upsert: %v, %v", got, err)
				}
			}
			// Отсутствующие поля должны сохраняться как SQL NULL, включая Date/Integer.
			if fixture.Model == "Stead" || fixture.Model == "House" || fixture.Model == "NormativeDoc" {
				for i := range value.Elem().NumField() {
					field := value.Elem().Field(i)
					if field.CanSet() && field.Kind() == reflect.Pointer {
						field.SetZero()
					}
				}
				id := value.Elem().FieldByName("ID")
				if id.Kind() == reflect.String {
					id.SetString("2")
				} else {
					id.SetInt(2)
				}
				roundTrip(t, ctx, client, value)
			}
		})
	}
}

func roundTrip(t *testing.T, ctx context.Context, client *ent.Client, value reflect.Value) {
	t.Helper()
	entity := reflect.ValueOf(client).Elem().FieldByName(value.Elem().Type().Name())
	builder := entity.MethodByName("Create").Call(nil)[0]
	for i := range value.Elem().NumField() {
		field := value.Elem().Type().Field(i)
		if tag := field.Tag.Get("xml"); tag == "" || tag == "-" {
			continue
		}
		method := "Set" + field.Name
		if field.Type.Kind() == reflect.Pointer {
			method = "SetNillable" + field.Name
		}
		setter := builder.MethodByName(method)
		if !setter.IsValid() {
			t.Fatalf("missing Ent builder method %s", method)
		}
		setter.Call([]reflect.Value{value.Elem().Field(i)})
	}
	result := builder.MethodByName("Save").Call([]reflect.Value{reflect.ValueOf(ctx)})
	if !result[1].IsNil() {
		t.Fatal(result[1].Interface())
	}
	result = entity.MethodByName("Get").Call([]reflect.Value{reflect.ValueOf(ctx), result[0].Elem().FieldByName("ID")})
	if !result[1].IsNil() {
		t.Fatal(result[1].Interface())
	}
	want, err := json.Marshal(value.Interface())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(result[0].Interface())
	if err != nil || !bytes.Equal(want, got) {
		t.Fatalf("PostgreSQL changed record:\nwant %s\ngot  %s\nerror %v", want, got, err)
	}
}
