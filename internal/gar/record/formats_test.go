package record

import (
	"encoding/json"
	"encoding/xml"
	"os"
	"reflect"
	"testing"
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
