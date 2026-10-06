// Package garxml — сведения о структуре файловой выгрузки ГАР:
// типы файлов по префиксу имени, разбор имени файла и пути внутри архива.
//
// Полная выгрузка gar_xml.zip разложена по папкам регионов (01..99),
// внутри — файлы вида R_T_GGGGMMDD_N.xml; общероссийские справочники
// лежат в корне архива. Дельта gar_delta_xml.zip имеет ту же структуру,
// но содержит только изменённые записи.
package garxml

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

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

// Known сообщает, что Kind — один из 22 форматов выгрузки.
func (k Kind) Known() bool {
	_, ok := layouts[k]
	return ok
}

// RootElement — имя корневого элемента XML-файла данного формата.
func (k Kind) RootElement() string { return layouts[k].root }

// RecordElement — имя элемента записи внутри корневого элемента.
func (k Kind) RecordElement() string { return layouts[k].record }

// File разбирает базовое имя файла выгрузки вида R_T_GGGGMMDD_N.xml,
// где R_T — префикс формата, GGGGMMDD — дата формирования выгрузки,
// N — идентификационный номер файла (1–36 знаков, обеспечивает уникальность).
func ParseFileName(base string) (kind Kind, date string, seq string, err error) {
	name := base
	if ext := path.Ext(name); strings.EqualFold(ext, ".xml") {
		name = name[:len(name)-len(ext)]
	} else if ext != "" {
		return "", "", "", fmt.Errorf("garxml: %q: unexpected extension %q", base, ext)
	}

	parts := strings.Split(name, "_")
	if len(parts) < 4 {
		return "", "", "", fmt.Errorf("garxml: %q: want R_T_GGGGMMDD_N.xml", base)
	}
	kind = Kind(strings.Join(parts[:len(parts)-2], "_"))
	date, seq = parts[len(parts)-2], parts[len(parts)-1]
	if !kind.Known() {
		return "", "", "", fmt.Errorf("garxml: %q: unknown format %q", base, kind)
	}
	if len(date) != 8 || !allDigits(date) {
		return "", "", "", fmt.Errorf("garxml: %q: want 8-digit date, got %q", base, date)
	}
	if seq == "" || len(seq) > 36 {
		return "", "", "", fmt.Errorf("garxml: %q: bad file sequence %q", base, seq)
	}
	return kind, date, seq, nil
}

// RegionCode извлекает код региона из пути внутри архива выгрузки:
// полная выгрузка разложена по папкам регионов ("77/AS_HOUSES_...xml"),
// общероссийские справочники лежат в корне ("AS_ADDR_OBJ_TYPES_...xml" → 0).
func RegionCode(archivePath string) (int, error) {
	first := archivePath
	if i := strings.IndexByte(archivePath, '/'); i >= 0 {
		first = archivePath[:i]
	}
	if strings.Contains(first, ".") {
		return 0, nil // файл в корне архива — общероссийский справочник
	}
	n, err := strconv.Atoi(first)
	if err != nil || n < 1 || n > 99 {
		return 0, fmt.Errorf("garxml: %q: not a region folder", archivePath)
	}
	return n, nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
