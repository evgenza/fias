package gar

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixtureRecord struct {
	Kind Kind
	XML  string
}

func loadFixtures(t *testing.T) []fixtureRecord {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "gar", "record", "testdata", "records.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Kind string
		XML  string
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	records := make([]fixtureRecord, 0, len(fixtures))
	for _, fixture := range fixtures {
		records = append(records, fixtureRecord{Kind: Kind(fixture.Kind), XML: fixture.XML})
	}
	if len(records) != 23 {
		t.Fatalf("got %d fixtures, want 23", len(records))
	}
	return records
}

// recordElement разбирает одиночную запись из fixture.
func recordElement(t *testing.T, xmlText string) xml.StartElement {
	t.Helper()
	decoder := xml.NewDecoder(strings.NewReader(xmlText))
	for {
		token, err := decoder.Token()
		if err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		if element, ok := token.(xml.StartElement); ok {
			return element
		}
	}
}

func withoutAttr(start xml.StartElement, name string) xml.StartElement {
	attrs := make([]xml.Attr, 0, len(start.Attr))
	for _, attr := range start.Attr {
		if attr.Name.Local != name {
			attrs = append(attrs, attr)
		}
	}
	return xml.StartElement{Name: start.Name, Attr: attrs}
}

func withAttrValue(start xml.StartElement, name, value string) xml.StartElement {
	attrs := make([]xml.Attr, 0, len(start.Attr)+1)
	replaced := false
	for _, attr := range start.Attr {
		if attr.Name.Local == name {
			attr.Value = value
			replaced = true
		}
		attrs = append(attrs, attr)
	}
	if !replaced {
		attrs = append(attrs, xml.Attr{Name: xml.Name{Local: name}, Value: value})
	}
	return xml.StartElement{Name: start.Name, Attr: attrs}
}

// Спецификации согласованы с layout файлов: единый источник имён элементов до этапа 2.
func TestSpecMatchesLayouts(t *testing.T) {
	for kind, layout := range layouts {
		spec, ok := Spec(kind)
		if !ok {
			t.Fatalf("%s: no generated spec", kind)
		}
		if spec.Root != layout.root || spec.Record != layout.record {
			t.Fatalf("%s: spec (%s,%s) != layout (%s,%s)", kind, spec.Root, spec.Record, layout.root, layout.record)
		}
	}
	if len(generatedSpecs) != len(layouts) {
		t.Fatalf("specs=%d, layouts=%d", len(generatedSpecs), len(layouts))
	}
}

// Валидные записи всех форматов проходят проверку.
func TestValidateFixtures(t *testing.T) {
	for _, fixture := range loadFixtures(t) {
		t.Run(string(fixture.Kind), func(t *testing.T) {
			spec, ok := Spec(fixture.Kind)
			if !ok {
				t.Fatalf("no spec for %s", fixture.Kind)
			}
			start := recordElement(t, fixture.XML)
			if err := ValidateRecord(fixture.Kind, spec, start); err != nil {
				t.Fatalf("valid fixture rejected: %v", err)
			}
		})
	}
}

// Удаление каждого обязательного атрибута из валидной записи даёт ошибку до writer.
func TestMissingRequiredAttribute(t *testing.T) {
	for _, fixture := range loadFixtures(t) {
		t.Run(string(fixture.Kind), func(t *testing.T) {
			spec, ok := Spec(fixture.Kind)
			if !ok {
				t.Fatal("no spec")
			}
			start := recordElement(t, fixture.XML)
			for _, field := range spec.Fields {
				if !field.Required {
					continue
				}
				broken := withoutAttr(start, field.Name)
				err := ValidateRecord(fixture.Kind, spec, broken)
				if err == nil {
					t.Fatalf("removing required %s was accepted", field.Name)
				}
				recordErr, ok := err.(*RecordError)
				if !ok || recordErr.Attribute != field.Name {
					t.Fatalf("removing %s: wrong error %v", field.Name, err)
				}
				if !strings.Contains(err.Error(), "отсутствует обязательный атрибут") {
					t.Fatalf("removing %s: unexpected cause: %v", field.Name, err)
				}
			}
		})
	}
}

// Удаление необязательного атрибута допустимо.
func TestOptionalAttributeCanBeOmitted(t *testing.T) {
	for _, fixture := range loadFixtures(t) {
		spec, _ := Spec(fixture.Kind)
		start := recordElement(t, fixture.XML)
		for _, field := range spec.Fields {
			if field.Required {
				continue
			}
			if err := ValidateRecord(fixture.Kind, spec, withoutAttr(start, field.Name)); err != nil {
				t.Fatalf("%s: omitting optional %s: %v", fixture.Kind, field.Name, err)
			}
		}
	}
}

func TestValueRules(t *testing.T) {
	house := recordElement(t, `<HOUSE ID="1" OBJECTID="1" OBJECTGUID="ab08d8da-ca9c-4a36-953a-6f1ce0d5b28e" CHANGEID="1" OPERTYPEID="1" UPDATEDATE="2024-11-23" STARTDATE="2024-11-23" ENDDATE="2024-11-23" ISACTUAL="1" ISACTIVE="0"/>`)
	spec, _ := Spec(KindHouses)
	cases := []struct {
		name      string
		start     xml.StartElement
		attribute string
		cause     string
	}{
		{"guid too short", withAttrValue(house, "OBJECTGUID", "ab08d8da"), "OBJECTGUID", "длина 8, ожидается 36"},
		{"empty guid", withAttrValue(house, "OBJECTGUID", ""), "OBJECTGUID", "длина 0, ожидается 36"},
		{"empty date", withAttrValue(house, "UPDATEDATE", ""), "UPDATEDATE", "неверная дата"},
		{"impossible date", withAttrValue(house, "STARTDATE", "20269999"), "STARTDATE", "неверная дата"},
		{"impossible february", withAttrValue(house, "ENDDATE", "2023-02-29"), "ENDDATE", "неверная дата"},
		{"bad oper type", withAttrValue(house, "OPERTYPEID", "12x"), "OPERTYPEID", "неверное значение xs:integer"},
		{"empty oper type", withAttrValue(house, "OPERTYPEID", "  "), "OPERTYPEID", "пустое значение xs:integer"},
		{"long overflow", withAttrValue(house, "CHANGEID", "99999999999999999999"), "CHANGEID", "не помещается в int64"},
		{"boolean via integer ok", withAttrValue(house, "ISACTIVE", "1"), "", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateRecord(KindHouses, spec, test.start)
			if test.cause == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), test.attribute) || !strings.Contains(err.Error(), test.cause) {
				t.Fatalf("wrong error: %v", err)
			}
		})
	}
	// Integer без ограничения диапазона int64 допустим (HOUSETYPE хранится как numeric).
	if err := ValidateRecord(KindHouses, spec, withAttrValue(house, "HOUSETYPE", "99999999999999999999")); err != nil {
		t.Fatalf("unbounded integer rejected: %v", err)
	}
	// Буквальный false в справочниках и "0" в файлах данных сохраняются.
	dict := recordElement(t, `<HOUSETYPE ID="1" NAME="дом" UPDATEDATE="2024-11-23" STARTDATE="2024-11-23" ENDDATE="2024-11-23" ISACTIVE="false"/>`)
	dictSpec, _ := Spec(KindHouseTypes)
	if err := ValidateRecord(KindHouseTypes, dictSpec, dict); err != nil {
		t.Fatalf("literal false rejected: %v", err)
	}
	if err := ValidateRecord(KindHouseTypes, dictSpec, withAttrValue(dict, "ISACTIVE", "")); err == nil {
		t.Fatal("empty boolean accepted")
	}
	// Строки с пробелом удовлетворяют minLength=1 по XSD (проверка без обрезки).
	if err := ValidateRecord(KindHouses, spec, withAttrValue(house, "HOUSENUM", " ")); err != nil {
		t.Fatalf("whitespace string rejected: %v", err)
	}
	// maxLength уровня адресных объектов — 10 символов.
	addrObj := recordElement(t, `<OBJECT ID="1" OBJECTID="1" OBJECTGUID="ab08d8da-ca9c-4a36-953a-6f1ce0d5b28e" CHANGEID="1" NAME="тест" TYPENAME="город" LEVEL="12345678901" OPERTYPEID="1" UPDATEDATE="2024-11-23" STARTDATE="2024-11-23" ENDDATE="2024-11-23" ISACTUAL="1" ISACTIVE="1"/>`)
	addrSpec, _ := Spec(KindAddrObj)
	err := ValidateRecord(KindAddrObj, addrSpec, addrObj)
	if err == nil || !strings.Contains(err.Error(), "LEVEL") || !strings.Contains(err.Error(), "не более 10") {
		t.Fatalf("long LEVEL accepted: %v", err)
	}
}

func TestUnknownAttributeAndNamespace(t *testing.T) {
	house := recordElement(t, `<HOUSE ID="1" OBJECTID="1" OBJECTGUID="ab08d8da-ca9c-4a36-953a-6f1ce0d5b28e" CHANGEID="1" OPERTYPEID="1" UPDATEDATE="2024-11-23" STARTDATE="2024-11-23" ENDDATE="2024-11-23" ISACTUAL="1" ISACTIVE="1"/>`)
	spec, _ := Spec(KindHouses)
	err := ValidateRecord(KindHouses, spec, withAttrValue(house, "NEWATTR", "x"))
	if err == nil || !strings.Contains(err.Error(), "NEWATTR") || !strings.Contains(err.Error(), "неизвестный атрибут") {
		t.Fatalf("unknown attribute accepted: %v", err)
	}
	nsHouse := xml.StartElement{
		Name: house.Name,
		Attr: append(append([]xml.Attr(nil), house.Attr...), xml.Attr{
			Name:  xml.Name{Space: "xmlns", Local: "ext"},
			Value: "urn:example",
		}),
	}
	if err := ValidateRecord(KindHouses, spec, nsHouse); err != nil {
		t.Fatalf("xmlns declaration rejected: %v", err)
	}
}

// Записи без ключа отклоняются до дедупликации: каждая отдельно.
func TestTwoRecordsWithoutIDRejected(t *testing.T) {
	house := `<HOUSE OBJECTID="1" OBJECTGUID="ab08d8da-ca9c-4a36-953a-6f1ce0d5b28e" CHANGEID="1" OPERTYPEID="1" UPDATEDATE="2024-11-23" STARTDATE="2024-11-23" ENDDATE="2024-11-23" ISACTUAL="1" ISACTIVE="1"/>`
	document := "<HOUSES>" + house + house + "</HOUSES>"
	decoder := xml.NewDecoder(strings.NewReader(document))
	spec, _ := Spec(KindHouses)
	validated := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "HOUSE" {
			continue
		}
		if err := ValidateRecord(KindHouses, spec, start); err == nil {
			t.Fatal("record without ID accepted")
		}
		validated++
	}
	if validated != 2 {
		t.Fatalf("validated=%d, want 2: каждая запись без ключа отклоняется отдельно", validated)
	}
}
