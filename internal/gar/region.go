package gar

import (
	"fmt"
	"strconv"
	"strings"
)

// Обёртки полного и дельта-архивов ГАР: первым сегментом пути
// может стоять имя самого архива без расширения.
var knownWrappers = map[string]bool{
	"gar_xml":       true,
	"gar_delta_xml": true,
}

// RegionCode извлекает код региона из пути внутри архива выгрузки:
// полная выгрузка разложена по папкам регионов ("77/AS_HOUSES_...xml"),
// общероссийские справочники лежат в корне ("AS_ADDR_OBJ_TYPES_...xml" → 0).
// Обёртка gar_xml/ или gar_delta_xml/ игнорируется; неожиданная папка — ошибка,
// а не общероссийский файл.
func RegionCode(archivePath string) (int, error) {
	segments := strings.Split(archivePath, "/")
	if len(segments) > 0 && knownWrappers[segments[0]] {
		segments = segments[1:]
	}
	if len(segments) <= 1 {
		return 0, nil // файл в корне архива — общероссийский справочник
	}
	first := segments[0]
	n, err := strconv.Atoi(first)
	if err != nil || n < 1 || n > 99 {
		return 0, fmt.Errorf("gar: %q: папка не является кодом региона", archivePath)
	}
	return n, nil
}
