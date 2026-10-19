// Package gar — каталог форматов выгрузки ГАР: Kind, разбор имён файлов,
// регионы, спецификации атрибутов записей (генерируются из закреплённых XSD)
// и план обработки архива.
//
// Полная выгрузка gar_xml.zip разложена по папкам регионов (01..99),
// внутри — файлы вида R_T_ГГГГММДД_N.xml; общероссийские справочники лежат
// в корне архива. Дельта gar_delta_xml.zip имеет ту же структуру, но содержит
// только изменённые записи. Допустима обёртка gar_xml/ или gar_delta_xml/
// первым сегментом пути.
package gar

import "fmt"

// Kind — формат файла выгрузки (префикс R_T имени файла, например AS_HOUSES).
type Kind string

const (
	KindAddrObj           Kind = "AS_ADDR_OBJ"
	KindAddrObjDivision   Kind = "AS_ADDR_OBJ_DIVISION"
	KindAddrObjTypes      Kind = "AS_ADDR_OBJ_TYPES"
	KindAdmHierarchy      Kind = "AS_ADM_HIERARCHY"
	KindApartments        Kind = "AS_APARTMENTS"
	KindApartmentTypes    Kind = "AS_APARTMENT_TYPES"
	KindCarplaces         Kind = "AS_CARPLACES"
	KindChangeHistory     Kind = "AS_CHANGE_HISTORY"
	KindHouses            Kind = "AS_HOUSES"
	KindHouseTypes        Kind = "AS_HOUSE_TYPES"
	KindAddHouseTypes     Kind = "AS_ADDHOUSE_TYPES"
	KindMunHierarchy      Kind = "AS_MUN_HIERARCHY"
	KindNormativeDocs     Kind = "AS_NORMATIVE_DOCS"
	KindNormativeDocKinds Kind = "AS_NORMATIVE_DOCS_KINDS"
	KindNormativeDocTypes Kind = "AS_NORMATIVE_DOCS_TYPES"
	KindObjectLevels      Kind = "AS_OBJECT_LEVELS"
	KindOperationTypes    Kind = "AS_OPERATION_TYPES"
	KindParam             Kind = "AS_PARAM"
	KindParamTypes        Kind = "AS_PARAM_TYPES"
	KindReestrObjects     Kind = "AS_REESTR_OBJECTS"
	KindRooms             Kind = "AS_ROOMS"
	KindRoomTypes         Kind = "AS_ROOM_TYPES"
	KindSteads            Kind = "AS_STEADS"
)

// layout — имена корневого элемента и элемента записи в XML-файле.
type layout struct {
	root   string
	record string
}

var layouts = map[Kind]layout{
	KindAddrObj:           {"ADDRESSOBJECTS", "OBJECT"},
	KindAddrObjDivision:   {"ITEMS", "ITEM"},
	KindAddrObjTypes:      {"ADDRESSOBJECTTYPES", "ADDRESSOBJECTTYPE"},
	KindAdmHierarchy:      {"ITEMS", "ITEM"},
	KindApartments:        {"APARTMENTS", "APARTMENT"},
	KindApartmentTypes:    {"APARTMENTTYPES", "APARTMENTTYPE"},
	KindCarplaces:         {"CARPLACES", "CARPLACE"},
	KindChangeHistory:     {"ITEMS", "ITEM"},
	KindHouses:            {"HOUSES", "HOUSE"},
	KindHouseTypes:        {"HOUSETYPES", "HOUSETYPE"},
	KindAddHouseTypes:     {"HOUSETYPES", "HOUSETYPE"},
	KindMunHierarchy:      {"ITEMS", "ITEM"},
	KindNormativeDocs:     {"NORMDOCS", "NORMDOC"},
	KindNormativeDocKinds: {"NDOCKINDS", "NDOCKIND"},
	KindNormativeDocTypes: {"NDOCTYPES", "NDOCTYPE"},
	KindObjectLevels:      {"OBJECTLEVELS", "OBJECTLEVEL"},
	KindOperationTypes:    {"OPERATIONTYPES", "OPERATIONTYPE"},
	KindParam:             {"PARAMS", "PARAM"},
	KindParamTypes:        {"PARAMTYPES", "PARAMTYPE"},
	KindReestrObjects:     {"REESTR_OBJECTS", "OBJECT"},
	KindRooms:             {"ROOMS", "ROOM"},
	KindRoomTypes:         {"ROOMTYPES", "ROOMTYPE"},
	KindSteads:            {"STEADS", "STEAD"},
}

// parameterSources нормализует PARAM/PARAMS конкретных семейств объектов
// для бизнес-ключа параметра (source, source_id).
var parameterSources = map[string]string{
	"AS_ADDR_OBJ_PARAM": "AS_ADDR_OBJ_PARAMS", "AS_ADDR_OBJ_PARAMS": "AS_ADDR_OBJ_PARAMS",
	"AS_HOUSES_PARAM": "AS_HOUSES_PARAMS", "AS_HOUSES_PARAMS": "AS_HOUSES_PARAMS",
	"AS_APARTMENTS_PARAM": "AS_APARTMENTS_PARAMS", "AS_APARTMENTS_PARAMS": "AS_APARTMENTS_PARAMS",
	"AS_ROOMS_PARAM": "AS_ROOMS_PARAMS", "AS_ROOMS_PARAMS": "AS_ROOMS_PARAMS",
	"AS_STEADS_PARAM": "AS_STEADS_PARAMS", "AS_STEADS_PARAMS": "AS_STEADS_PARAMS",
	"AS_CARPLACES_PARAM": "AS_CARPLACES_PARAMS", "AS_CARPLACES_PARAMS": "AS_CARPLACES_PARAMS",
}

// ParseKind распознаёт формат: префикс файла параметров конкретного семейства
// даёт KindParam, иначе — собственный формат. Префикс AS_PARAM без семейства
// объектов — валидный Kind, но неоднозначный источник (см. ParseFile/Plan).
func ParseKind(prefix string) (Kind, error) {
	if _, ok := parameterSources[prefix]; ok {
		return KindParam, nil
	}
	kind := Kind(prefix)
	if !kind.Known() {
		return "", fmt.Errorf("gar: неизвестный формат %q", prefix)
	}
	return kind, nil
}

// Known сообщает, что формат поддерживается импортёром.
func (k Kind) Known() bool {
	_, ok := layouts[k]
	return ok
}

// RootElement — имя корневого элемента XML-файла данного формата.
func (k Kind) RootElement() string { return layouts[k].root }

// RecordElement — имя элемента записи внутри корневого элемента.
func (k Kind) RecordElement() string { return layouts[k].record }

// ParameterPrefixes возвращает множество префиксов файлов параметров
// (включая нормализованные имена вида AS_HOUSES_PARAMS).
func ParameterPrefixes() []string {
	prefixes := make([]string, 0, len(parameterSources))
	for prefix := range parameterSources {
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}

// IsParameterPrefix сообщает, что префикс — файл параметров конкретного семейства.
func IsParameterPrefix(prefix string) bool {
	_, ok := parameterSources[prefix]
	return ok
}

// Source нормализует префикс файла параметров для бизнес-ключа записи.
// Для префикса без известного семейства (голый AS_PARAM) возвращает сам префикс.
func Source(prefix string) string {
	if source, ok := parameterSources[prefix]; ok {
		return source
	}
	return prefix
}
