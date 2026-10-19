package importjob_test

import (
	"reflect"
	"strings"
	"testing"

	"gar-importer/ent"
	"gar-importer/internal/gar"
	"gar-importer/internal/gar/record"
)

// coverageModels связывает формат с Ent-моделью хранения.
var coverageModels = map[gar.Kind]any{
	gar.KindAddrObj:           ent.AddrObj{},
	gar.KindAddrObjDivision:   ent.AddrObjDivision{},
	gar.KindAddrObjTypes:      ent.AddrObjType{},
	gar.KindAdmHierarchy:      ent.AdmHierarchyItem{},
	gar.KindApartments:        ent.Apartment{},
	gar.KindApartmentTypes:    ent.ApartmentType{},
	gar.KindCarplaces:         ent.Carplace{},
	gar.KindChangeHistory:     ent.ChangeHistory{},
	gar.KindHouses:            ent.House{},
	gar.KindHouseTypes:        ent.HouseType{},
	gar.KindAddHouseTypes:     ent.AddHouseType{},
	gar.KindMunHierarchy:      ent.MunHierarchyItem{},
	gar.KindNormativeDocs:     ent.NormativeDoc{},
	gar.KindNormativeDocKinds: ent.NormativeDocKind{},
	gar.KindNormativeDocTypes: ent.NormativeDocType{},
	gar.KindObjectLevels:      ent.ObjectLevel{},
	gar.KindOperationTypes:    ent.OperationType{},
	gar.KindParam:             ent.Param{},
	gar.KindParamTypes:        ent.ParamType{},
	gar.KindReestrObjects:     ent.ReestrObject{},
	gar.KindRooms:             ent.Room{},
	gar.KindRoomTypes:         ent.RoomType{},
	gar.KindSteads:            ent.Stead{},
}

// Спецификации из XSD должны покрывать ровно те XML-атрибуты, которые декодирует
// Ent-модель: обязательность — непустой (не-указательный) тип поля.
func TestSpecCoversEntModels(t *testing.T) {
	for kind, prototype := range coverageModels {
		t.Run(string(kind), func(t *testing.T) {
			spec, ok := gar.Spec(kind)
			if !ok {
				t.Fatalf("no spec for %s", kind)
			}
			model := reflect.TypeOf(prototype)
			xmlNames := map[string]string{} // xml attr -> go field
			for i := 0; i < model.NumField(); i++ {
				field := model.Field(i)
				tag, ok := field.Tag.Lookup("xml")
				if !ok {
					continue
				}
				parts := strings.Split(tag, ",")
				if len(parts) < 2 || parts[1] != "attr" || parts[0] == "-" {
					continue // json:"-" или не-атрибут
				}
				if _, duplicate := xmlNames[parts[0]]; duplicate {
					t.Fatalf("duplicate xml attr %s", parts[0])
				}
				xmlNames[parts[0]] = field.Name
			}
			specNames := map[string]bool{}
			for _, fieldSpec := range spec.Fields {
				field, ok := xmlNames[fieldSpec.Name]
				if !ok {
					t.Fatalf("spec attr %s is not decoded by model", fieldSpec.Name)
				}
				structField, _ := model.FieldByName(field)
				isPointer := structField.Type.Kind() == reflect.Pointer
				if fieldSpec.Required == isPointer {
					t.Fatalf("attr %s: required=%v but model optional=%v", fieldSpec.Name, fieldSpec.Required, isPointer)
				}
				specNames[fieldSpec.Name] = true
			}
			for name := range xmlNames {
				if !specNames[name] {
					t.Fatalf("model decodes %s but spec has no such attr", name)
				}
			}
		})
	}
	if len(coverageModels) != 23 {
		t.Fatalf("coverage covers %d formats, want 23", len(coverageModels))
	}
}

// coverageRecords связывает формат с независимой записью.
var coverageRecords = map[gar.Kind]any{
	gar.KindAddrObj:           record.AddrObj{},
	gar.KindAddrObjDivision:   record.AddrObjDivision{},
	gar.KindAddrObjTypes:      record.AddrObjType{},
	gar.KindAdmHierarchy:      record.AdmHierarchyItem{},
	gar.KindApartments:        record.Apartment{},
	gar.KindApartmentTypes:    record.ApartmentType{},
	gar.KindCarplaces:         record.Carplace{},
	gar.KindChangeHistory:     record.ChangeHistory{},
	gar.KindHouses:            record.House{},
	gar.KindHouseTypes:        record.HouseType{},
	gar.KindAddHouseTypes:     record.AddHouseType{},
	gar.KindMunHierarchy:      record.MunHierarchyItem{},
	gar.KindNormativeDocs:     record.NormativeDoc{},
	gar.KindNormativeDocKinds: record.NormativeDocKind{},
	gar.KindNormativeDocTypes: record.NormativeDocType{},
	gar.KindObjectLevels:      record.ObjectLevel{},
	gar.KindOperationTypes:    record.OperationType{},
	gar.KindParam:             record.Param{},
	gar.KindParamTypes:        record.ParamType{},
	gar.KindReestrObjects:     record.ReestrObject{},
	gar.KindRooms:             record.Room{},
	gar.KindRoomTypes:         record.RoomType{},
	gar.KindSteads:            record.Stead{},
}

// Сгенерированные записи должны повторять Ent-модели поле в поле
// (имя, тип, теги), пока Ent остаётся описанием хранения.
func TestRecordsMatchEntModels(t *testing.T) {
	for kind, prototype := range coverageModels {
		recordPrototype, ok := coverageRecords[kind]
		if !ok {
			t.Fatalf("%s: no record", kind)
		}
		entModel := reflect.TypeOf(prototype)
		recordModel := reflect.TypeOf(recordPrototype)
		var entFields []reflect.StructField
		for i := 0; i < entModel.NumField(); i++ {
			if field := entModel.Field(i); field.PkgPath == "" {
				entFields = append(entFields, field) // неэкспортируемые config/selectValues Ent не декодируются
			}
		}
		if len(entFields) != recordModel.NumField() {
			t.Fatalf("%s: fields %d != %d", kind, recordModel.NumField(), len(entFields))
		}
		for i, want := range entFields {
			got := recordModel.Field(i)
			if want.Name != got.Name || want.Type != got.Type || want.Tag != got.Tag {
				t.Fatalf("%s.%s: record field mismatch (%s %s %q)", kind, want.Name, got.Name, got.Type, got.Tag)
			}
		}
	}
	if len(coverageRecords) != len(coverageModels) {
		t.Fatalf("records cover %d formats, want %d", len(coverageRecords), len(coverageModels))
	}
}
