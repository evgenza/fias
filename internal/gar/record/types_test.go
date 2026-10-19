package record

import (
	"encoding/json"
	"encoding/xml"
	"reflect"
	"testing"
	"time"

	"gar-importer/internal/gartype"
)

func TestChangeHistoryGUID(t *testing.T) {
	const guid = "ab08d8da-ca9c-4a36-953a-6f1ce0d5b28e"
	var record ChangeHistory
	if err := xml.Unmarshal([]byte(`<ITEM CHANGEID="1" OBJECTID="2" ADROBJECTID="`+guid+`" OPERTYPEID="10" CHANGEDATE="2024-11-23"/>`), &record); err != nil {
		t.Fatal(err)
	}
	if record.AdrObjectID != guid {
		t.Fatalf("GUID = %q", record.AdrObjectID)
	}
}

func TestWideIntegerXML(t *testing.T) {
	var stead Stead
	if err := xml.Unmarshal([]byte(`<STEAD ID="9999999999999999999" OBJECTID="9999999999999999998" CHANGEID="1" PREVID="-9999999999999999999" OPERTYPEID="01"/>`), &stead); err != nil {
		t.Fatal(err)
	}
	if stead.ID != "9999999999999999999" || stead.ObjectID != "9999999999999999998" || stead.PrevID == nil || *stead.PrevID != "-9999999999999999999" || stead.OpertypeID != "01" {
		t.Fatalf("integer/code changed: %+v", stead)
	}
	if _, err := stead.ID.Int64(); err == nil {
		t.Fatal("out-of-range integer converted to int64")
	}
	var bounded ApartmentType
	if err := xml.Unmarshal([]byte(`<APARTMENTTYPE ID="9999999999"/>`), &bounded); err != nil || bounded.ID != 9999999999 {
		t.Fatalf("10-digit ID = %d, %v", bounded.ID, err)
	}
	var unbounded OperationType
	if err := xml.Unmarshal([]byte(`<OPERATIONTYPE ID="123456789012345678901234567890"/>`), &unbounded); err != nil || unbounded.ID != "123456789012345678901234567890" {
		t.Fatalf("unbounded ID = %s, %v", unbounded.ID, err)
	}
}

func TestDateZones(t *testing.T) {
	for _, src := range []string{"2024-11-23Z", "2024-11-23+03:00", "2024-11-23-14:00", " 2024-11-23+14:00\t"} {
		var got gartype.Date
		if err := got.UnmarshalText([]byte(src)); err != nil || got != "2024-11-23" {
			t.Errorf("%q: date=%s, error=%v", src, got, err)
		}
	}
	for _, src := range []string{"2024-11-23+14:01", "2024-11-23+15:00", "2024-11-23+03:60", "1900-02-29", "0000-01-01", "02024-01-01"} {
		var got gartype.Date
		if err := got.UnmarshalText([]byte(src)); err == nil {
			t.Errorf("accepted invalid date %q", src)
		}
	}
	for _, src := range []string{"2000-02-29", "10000-01-01", "-0001-01-01", "12345678901234567890000-02-29"} {
		var got gartype.Date
		if err := got.UnmarshalText([]byte(src)); err != nil || string(got) != src {
			t.Errorf("%q: date=%s, error=%v", src, got, err)
		}
	}
}

func TestDateScanKeepsCalendarDay(t *testing.T) {
	for _, offset := range []int{-12 * 3600, 0, 3 * 3600, 14 * 3600} {
		var got gartype.Date
		src := time.Date(2024, 11, 23, 0, 0, 0, 0, time.FixedZone("test", offset))
		if err := got.Scan(src); err != nil || got != "2024-11-23" {
			t.Errorf("offset %d: date=%s, error=%v", offset, got, err)
		}
	}
	var got gartype.Date
	if err := got.Scan(nil); err != nil || !got.IsZero() {
		t.Fatalf("nil: date=%s, error=%v", got, err)
	}
	if value, err := gartype.Date("2024-11-23").Value(); err != nil || value != "2024-11-23" {
		t.Fatalf("SQL value = %v, %v", value, err)
	}
}

func TestSearchJSONTypes(t *testing.T) {
	want := AddrObj{ID: 9223372036854775807, ObjectID: 9007199254740993,
		ChangeID: 1, OpertypeID: "10", Level: "03", IsActive: true,
		UpdateDate: "2024-11-23", StartDate: "1900-01-01", EndDate: "2079-06-06"}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"id": `"9223372036854775807"`, "object_id": `"9007199254740993"`, "is_active": `true`, "is_actual": `false`, "update_date": `"2024-11-23"`, "level": `"03"`} {
		if string(fields[key]) != value {
			t.Errorf("%s = %s, want %s", key, fields[key], value)
		}
	}
	if _, ok := fields["prev_id"]; ok {
		t.Error("absent optional identifier should be omitted")
	}
	var got AddrObj
	if err := json.Unmarshal(data, &got); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON round trip = %+v, %v", got, err)
	}
	for _, value := range []gartype.Integer{"9999999999999999999", "-9999999999999999999", "123456789012345678901234567890"} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var got gartype.Integer
		if err := json.Unmarshal(data, &got); err != nil || got != value {
			t.Fatalf("integer JSON round trip: %s, %v", data, err)
		}
	}
}

func TestIntegerNormalization(t *testing.T) {
	for src, want := range map[string]gartype.Integer{" +00042 ": "42", "-00042": "-42", "-0": "0", "0000": "0"} {
		var got gartype.Integer
		if err := got.UnmarshalText([]byte(src)); err != nil || got != want {
			t.Errorf("%q: got=%s, error=%v", src, got, err)
		}
	}
}
