package gar

import (
	"fmt"
	"path"
	"strings"

	"gar-importer/internal/gartype"
)

// File — сведения о файле выгрузки, разобранные из имени.
type File struct {
	Kind                   Kind
	Date, Sequence, Prefix string
}

// Source — нормализованное семейство параметров для бизнес-ключа записи
// (для файлов PARAM); для остальных форматов совпадает с префиксом.
func (f File) Source() string { return Source(f.Prefix) }

// NameError — ошибка разбора имени файла. Known различает повреждённое имя
// известного формата и неизвестный новый формат.
type NameError struct {
	Base  string
	Known bool
	Cause error
}

func (e *NameError) Error() string {
	return fmt.Sprintf("gar: %q: %v", e.Base, e.Cause)
}

func (e *NameError) Unwrap() error { return e.Cause }

// ParseFile разбирает базовое имя файла выгрузки вида R_T_ГГГГММДД_N.xml,
// где R_T — префикс формата, ГГГГММДД — дата формирования выгрузки
// (календарная), N — идентификационный номер файла (1–36 знаков).
func ParseFile(base string) (File, error) {
	name := base
	if ext := path.Ext(name); strings.EqualFold(ext, ".xml") {
		name = name[:len(name)-len(ext)]
	} else if ext != "" {
		return File{}, &NameError{Base: base, Cause: fmt.Errorf("неожиданное расширение %q", ext)}
	}

	parts := strings.Split(name, "_")
	if len(parts) < 4 {
		return File{}, &NameError{Base: base, Cause: fmt.Errorf("ожидается имя вида R_T_ГГГГММДД_N.xml")}
	}
	prefix := strings.Join(parts[:len(parts)-2], "_")
	kind, err := ParseKind(prefix)
	if err != nil {
		return File{}, &NameError{Base: base, Cause: fmt.Errorf("неизвестный формат %q", prefix)}
	}
	date, seq := parts[len(parts)-2], parts[len(parts)-1]
	if len(date) != 8 || !allDigits(date) {
		return File{}, &NameError{Base: base, Known: true, Cause: fmt.Errorf("ожидается дата из 8 цифр, получено %q", date)}
	}
	// Дата имени файла — календарный день выпуска (версияid DownloadFileInfo).
	compact := date[:4] + "-" + date[4:6] + "-" + date[6:]
	var calendar gartype.Date
	if err := calendar.UnmarshalText([]byte(compact)); err != nil {
		return File{}, &NameError{Base: base, Known: true, Cause: fmt.Errorf("неверная дата файла %q", date)}
	}
	if seq == "" || len(seq) > 36 {
		return File{}, &NameError{Base: base, Known: true, Cause: fmt.Errorf("неверный номер файла %q", seq)}
	}
	return File{Kind: kind, Date: date, Sequence: seq, Prefix: prefix}, nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
