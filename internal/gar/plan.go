package gar

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// EntryStatus — результат включения файла в план архива.
type EntryStatus uint8

const (
	// StatusSelected — файл разобран и может быть выбран фильтрами.
	StatusSelected EntryStatus = iota
	// StatusFiltered — файл корректен, но не выбран фильтрами регионов/форматов.
	StatusFiltered
	// StatusUnknown — неизвестный формат, повреждённое имя или неожиданный путь.
	StatusUnknown
	// StatusDuplicate — повторное вхождение того же пути; побеждает первое.
	StatusDuplicate
)

func (s EntryStatus) String() string {
	switch s {
	case StatusSelected:
		return "selected"
	case StatusFiltered:
		return "filtered"
	case StatusUnknown:
		return "unknown"
	case StatusDuplicate:
		return "duplicate"
	}
	return fmt.Sprintf("EntryStatus(%d)", uint8(s))
}

// Entry — план обработки одного XML-файла архива.
type Entry struct {
	Index    int // позиция в исходном списке путей
	Path     string
	Kind     Kind
	Prefix   string // префикс формата из имени файла
	Source   string // нормализованное семейство параметров (для PARAM)
	Region   int    // 0 — общероссийский справочник
	Date     string // календарная дата имени файла, ГГГГММДД
	Sequence string // идентификатор файла в выпуске, не порядок
	Status   EntryStatus
	Reason   string // пояснение для filtered/unknown/duplicate
}

// Plan разбирает пути XML-файлов архива в детерминированный план: неизвестная
// папка или голый AS_PARAM — unknown, а не общероссийский файл; повторный
// путь — duplicate. Порядок входа сохраняется.
func Plan(paths []string) []Entry {
	entries := make([]Entry, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for index, archivePath := range paths {
		entry := Entry{Index: index, Path: archivePath, Status: StatusSelected}
		if seen[archivePath] {
			entry.Status, entry.Reason = StatusDuplicate, "повторный путь в архиве"
			entries = append(entries, entry)
			continue
		}
		seen[archivePath] = true
		region, err := RegionCode(archivePath)
		if err != nil {
			entry.Status, entry.Reason = StatusUnknown, err.Error()
			entries = append(entries, entry)
			continue
		}
		entry.Region = region
		file, err := ParseFile(path.Base(archivePath))
		if err != nil {
			var nameErr *NameError
			reason := err.Error()
			if asNameError(err, &nameErr) {
				reason = fmt.Sprintf("%s: %v", nameErrorKind(nameErr), nameErr.Cause)
			}
			entry.Status, entry.Reason = StatusUnknown, reason
			entries = append(entries, entry)
			continue
		}
		entry.Kind, entry.Prefix, entry.Date, entry.Sequence = file.Kind, file.Prefix, file.Date, file.Sequence
		entry.Source = file.Source()
		if file.Kind == KindParam && entry.Source == string(KindParam) {
			// Голый AS_PARAM не позволяет определить семейство объектов:
			// источник в БД был бы неоднозначным.
			entry.Status, entry.Reason = StatusUnknown,
				fmt.Sprintf("голый %s: не определено семейство объектов для источника параметров", KindParam)
		}
		entries = append(entries, entry)
	}
	return entries
}

// nameErrorKind описывает класс ошибки имени для отчёта.
func nameErrorKind(err *NameError) string {
	if err.Known {
		return "повреждённое имя известного формата"
	}
	return "неподдерживаемый формат"
}

func asNameError(err error, target **NameError) bool {
	for err != nil {
		if nameErr, ok := err.(*NameError); ok {
			*target = nameErr
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// Filters — выбор файлов архива: регионы (пустой список — все, справочники
// всегда), форматы и точные семейства параметров.
type Filters struct {
	Regions map[int]bool
	Kinds   map[Kind]bool
	Sources map[string]bool // например AS_HOUSES_PARAMS: только параметры домов
}

// Select применяет фильтры к плану, помечая непрошедшие как filtered,
// и возвращает число выбранных файлов.
func Select(entries []Entry, filters Filters) int {
	selected := 0
	for i := range entries {
		entry := &entries[i]
		if entry.Status != StatusSelected {
			continue
		}
		if !selectEntry(*entry, filters) {
			entry.Status = StatusFiltered
			entry.Reason = filterReason(*entry, filters)
			continue
		}
		selected++
	}
	return selected
}

func selectEntry(entry Entry, filters Filters) bool {
	if len(filters.Regions) > 0 && entry.Region != 0 && !filters.Regions[entry.Region] {
		return false
	}
	if len(filters.Kinds) == 0 && len(filters.Sources) == 0 {
		return true
	}
	if entry.Kind == KindParam {
		// AS_PARAM выбирает все шесть семейств; точное имя выбирает только своё.
		return filters.Kinds[KindParam] || filters.Sources[entry.Source]
	}
	return filters.Kinds[entry.Kind]
}

func filterReason(entry Entry, filters Filters) string {
	if len(filters.Regions) > 0 && entry.Region != 0 && !filters.Regions[entry.Region] {
		return fmt.Sprintf("регион %d не выбран", entry.Region)
	}
	if entry.Kind == KindParam {
		return fmt.Sprintf("семейство параметров %s не выбрано", entry.Source)
	}
	return fmt.Sprintf("формат %s не выбран", entry.Kind)
}

// Unknown возвращает файлы, не вошедшие в обработку из-за имени или пути,
// в детерминированном порядке.
func Unknown(entries []Entry) []Entry {
	var unknown []Entry
	for _, entry := range entries {
		if entry.Status == StatusUnknown || entry.Status == StatusDuplicate {
			unknown = append(unknown, entry)
		}
	}
	sort.Slice(unknown, func(i, j int) bool { return unknown[i].Index < unknown[j].Index })
	return unknown
}

// SelectedPaths возвращает пути выбранных файлов в порядке архива.
func SelectedPaths(entries []Entry) []string {
	var paths []string
	for _, entry := range entries {
		if entry.Status == StatusSelected {
			paths = append(paths, entry.Path)
		}
	}
	return paths
}

// TrimWrappers убирает обёртки каталогов для отображения имени файла.
func TrimWrappers(archivePath string) string {
	segments := strings.Split(archivePath, "/")
	if len(segments) > 1 && knownWrappers[segments[0]] {
		return strings.Join(segments[1:], "/")
	}
	return archivePath
}
