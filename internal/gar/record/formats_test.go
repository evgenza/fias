package record

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"gar-importer/internal/gar"
)

// Примеры форматов проверены исходными XSD ФНС с помощью xmllint.
// XSD-валидатор не является частью чтения выгрузки или запуска этих тестов.
func TestAllFormatsXMLAndJSON(t *testing.T) {
	data, err := os.ReadFile("testdata/records.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Kind, XML, Model string }
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	models := map[string]any{
		"AddrObj": AddrObj{}, "House": House{}, "Stead": Stead{},
		"Apartment": Apartment{}, "Room": Room{}, "Carplace": Carplace{},
		"ReestrObject": ReestrObject{}, "AdmHierarchyItem": AdmHierarchyItem{},
		"MunHierarchyItem": MunHierarchyItem{}, "AddrObjDivision": AddrObjDivision{},
		"ChangeHistory": ChangeHistory{}, "Param": Param{}, "AddrObjType": AddrObjType{},
		"ApartmentType": ApartmentType{}, "HouseType": HouseType{}, "RoomType": RoomType{},
		"AddHouseType":  AddHouseType{},
		"OperationType": OperationType{}, "ParamType": ParamType{}, "ObjectLevel": ObjectLevel{},
		"NormativeDoc": NormativeDoc{}, "NormativeDocKind": NormativeDocKind{}, "NormativeDocType": NormativeDocType{},
	}
	if len(cases) != len(models) {
		t.Fatalf("%d examples for %d models", len(cases), len(models))
	}
	seen := map[string]bool{}
	for _, c := range cases {
		t.Run(c.Kind, func(t *testing.T) {
			prototype, ok := models[c.Model]
			if !ok || seen[c.Model] {
				t.Fatalf("unknown or duplicate model %q", c.Model)
			}
			seen[c.Model] = true
			value := reflect.New(reflect.TypeOf(prototype)).Interface()
			if err := xml.Unmarshal([]byte(c.XML), value); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			restored := reflect.New(reflect.TypeOf(prototype)).Interface()
			if err := json.Unmarshal(data, restored); err != nil || !reflect.DeepEqual(value, restored) {
				t.Fatalf("JSON round trip changed %s: %v", c.Model, err)
			}
		})
	}
}

func TestDecodeFailureIsRecordError(t *testing.T) {
	data, err := os.ReadFile("testdata/records.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Kind, XML string }
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Kind, func(t *testing.T) {
			kind := gar.Kind(c.Kind)
			spec, ok := gar.Spec(kind)
			if !ok {
				t.Fatalf("unknown kind %s", kind)
			}
			src := "<" + spec.Root + ">" + strings.Replace(c.XML, "/>", ">", 1)
			_, err := DecodeStream(context.Background(), strings.NewReader(src), kind, 0, nil)
			var recordErr *gar.RecordError
			if !errors.As(err, &recordErr) {
				t.Fatalf("want RecordError, got %T: %v", err, err)
			}
		})
	}
}
