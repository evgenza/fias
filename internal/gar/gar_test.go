package gar

import (
	"errors"
	"strings"
	"testing"
)

func TestParseFile(t *testing.T) {
	cases := []struct {
		in      string
		kind    Kind
		date    string
		wantErr bool
	}{
		{in: "AS_ADDR_OBJ_20260925_4f8b6f5a7f0b4f7d9e3c1a2b.xml", kind: KindAddrObj, date: "20260925"},
		{in: "AS_HOUSES_20260925_01.XML", kind: KindHouses, date: "20260925"},
		{in: "AS_MUN_HIERARCHY_20260925_x.xml", kind: KindMunHierarchy, date: "20260925"},
		{in: "AS_NORMATIVE_DOCS_TYPES_20260925_x.xml", kind: KindNormativeDocTypes, date: "20260925"},
		{in: "AS_UNKNOWN_20260925_x.xml", wantErr: true},
		{in: "AS_HOUSES_2026-09-25_x.xml", wantErr: true},
		{in: "AS_HOUSES_20260925.csv", wantErr: true},
		{in: "AS_HOUSES.xml", wantErr: true},
		// Календарная дата имени файла: невозможные дни и месяцы отклоняются.
		{in: "AS_HOUSES_20269999_x.xml", wantErr: true},
		{in: "AS_HOUSES_20260230_x.xml", wantErr: true},
		{in: "AS_HOUSES_20261301_x.xml", wantErr: true},
		{in: "AS_HOUSES_20240229_x.xml", kind: KindHouses, date: "20240229"},
	}
	for _, c := range cases {
		file, err := ParseFile(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseFile(%q): want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseFile(%q): %v", c.in, err)
			continue
		}
		if file.Kind != c.kind || file.Date != c.date || file.Sequence == "" {
			t.Errorf("ParseFile(%q) = %+v", c.in, file)
		}
	}
	// 2026 — не високосный год: 29 февраля невозможен.
	if _, err := ParseFile("AS_HOUSES_20260229_x.xml"); err == nil {
		t.Error("2026-02-29 must be rejected")
	}
}

func TestParseFileErrorClassification(t *testing.T) {
	// Неизвестный формат против повреждённого имени известного.
	_, err := ParseFile("AS_UNKNOWN_20260925_x.xml")
	var nameErr *NameError
	if !errors.As(err, &nameErr) || nameErr.Known {
		t.Fatalf("unknown prefix: %v", err)
	}
	_, err = ParseFile("AS_HOUSES_2026092_x.xml")
	if !errors.As(err, &nameErr) || !nameErr.Known {
		t.Fatalf("damaged known name: %v", err)
	}
}

func TestRegionCode(t *testing.T) {
	cases := []struct {
		path   string
		region int
		err    bool
	}{
		{"77/AS_HOUSES_20260925_x.xml", 77, false},
		{"1/AS_HOUSES_20260925_x.xml", 1, false},
		{"AS_ADDR_OBJ_TYPES_20260925_x.xml", 0, false},
		// Обёртки полного и дельта-архивов поддержаны явно.
		{"gar_xml/77/AS_HOUSES_20260925_x.xml", 77, false},
		{"gar_delta_xml/5/AS_HOUSES_20260925_x.xml", 5, false},
		{"gar_xml/AS_ADDR_OBJ_TYPES_20260925_x.xml", 0, false},
		// Неизвестная папка — ошибка, а не общий справочник.
		{"unexpected/AS_HOUSES_20260925_x.xml", 0, true},
		{"data/gar_xml/77/AS_HOUSES_20260925_x.xml", 0, true},
		{"gar_xml/gar_xml/77/AS_HOUSES_20260925_x.xml", 0, true},
		{"100/AS_HOUSES_20260925_x.xml", 0, true},
		{"00/AS_HOUSES_20260925_x.xml", 0, true},
	}
	for _, c := range cases {
		region, err := RegionCode(c.path)
		if c.err {
			if err == nil {
				t.Errorf("RegionCode(%q): want error, got region %d", c.path, region)
			}
			continue
		}
		if err != nil || region != c.region {
			t.Errorf("RegionCode(%q) = %d, %v", c.path, region, err)
		}
	}
}

func TestParameterFileSources(t *testing.T) {
	for _, prefix := range []string{"AS_ADDR_OBJ", "AS_HOUSES", "AS_APARTMENTS", "AS_ROOMS", "AS_STEADS", "AS_CARPLACES"} {
		for _, suffix := range []string{"_PARAM", "_PARAMS"} {
			file, err := ParseFile(prefix + suffix + "_20260925_x.xml")
			if err != nil || file.Kind != KindParam || file.Source() != prefix+"_PARAMS" {
				t.Errorf("%s%s: %+v, source=%q, %v", prefix, suffix, file, file.Source(), err)
			}
			if !IsParameterPrefix(prefix + suffix) {
				t.Errorf("%s%s: not a parameter prefix", prefix, suffix)
			}
		}
	}
	// Голый AS_PARAM — валидный Kind, но неоднозначный источник.
	file, err := ParseFile("AS_PARAM_20260925_x.xml")
	if err != nil || file.Kind != KindParam || file.Source() != "AS_PARAM" {
		t.Fatalf("bare AS_PARAM: %+v, %v", file, err)
	}
}

func TestPlanAndSelect(t *testing.T) {
	paths := []string{
		"gar_xml/77/AS_HOUSES_20260925_1.xml",
		"gar_xml/78/AS_HOUSES_20260925_1.xml",
		"gar_xml/AS_ADDR_OBJ_TYPES_20260925_1.xml",
		"gar_xml/77/AS_HOUSES_PARAMS_20260925_1.xml",
		"gar_xml/77/AS_ADDR_OBJ_PARAMS_20260925_1.xml",
		"gar_xml/77/AS_PARAM_20260925_1.xml",   // голый AS_PARAM
		"unexpected/AS_HOUSES_20260925_1.xml",  // неизвестная папка
		"gar_xml/77/AS_UNKNOWN_20260925_1.xml", // неизвестный формат
		"gar_xml/77/AS_HOUSES_20269999_1.xml",  // повреждённое имя известного формата
		"gar_xml/77/AS_HOUSES_20260925_1.xml",  // дубликат первого пути
		"gar_xml/77/AS_ROOMS_20260925_1.xml",
	}
	entries := Plan(paths)
	if len(entries) != len(paths) {
		t.Fatalf("plan lost entries: %d != %d", len(entries), len(paths))
	}
	status := func(i int) (Entry, EntryStatus) { return entries[i], entries[i].Status }
	if entry := entries[0]; entry.Kind != KindHouses || entry.Region != 77 || entry.Source != "AS_HOUSES" || entry.Date != "20260925" || entry.Sequence != "1" {
		t.Fatalf("entry 0: %+v", entry)
	}
	if _, s := status(0); s != StatusSelected {
		t.Fatalf("entry 0 status: %v", s)
	}
	if _, s := status(1); s != StatusSelected {
		t.Fatalf("entry 1 status: %v", s)
	}
	if _, s := status(2); s != StatusSelected {
		t.Fatalf("entry 2 (root dictionary) status: %v", s)
	}
	if entry := entries[3]; entry.Source != "AS_HOUSES_PARAMS" || entry.Kind != KindParam {
		t.Fatalf("entry 3: %+v", entry)
	}
	if _, s := status(5); s != StatusUnknown || !strings.Contains(entries[5].Reason, "голый AS_PARAM") {
		t.Fatalf("entry 5 (bare param): %v, %q", s, entries[5].Reason)
	}
	if _, s := status(6); s != StatusUnknown || !strings.Contains(entries[6].Reason, "не является кодом региона") {
		t.Fatalf("entry 6 (unexpected folder): %v, %q", s, entries[6].Reason)
	}
	if _, s := status(7); s != StatusUnknown || !strings.Contains(entries[7].Reason, "неподдерживаемый формат") {
		t.Fatalf("entry 7 (unknown format): %v, %q", s, entries[7].Reason)
	}
	if _, s := status(8); s != StatusUnknown || !strings.Contains(entries[8].Reason, "повреждённое имя") {
		t.Fatalf("entry 8 (повреждённое имя): %v, %q", s, entries[8].Reason)
	}
	if _, s := status(9); s != StatusDuplicate {
		t.Fatalf("entry 9 (duplicate): %v", s)
	}

	// Точный фильтр семейства параметров выбирает только своё семейство.
	filters := Filters{
		Regions: map[int]bool{77: true},
		Sources: map[string]bool{"AS_HOUSES_PARAMS": true},
	}
	plan := Plan(paths)
	selected := Select(plan, filters)
	selectedPaths := SelectedPaths(plan)
	want := []string{paths[3]}
	if selected != len(want) || !equalStrings(selectedPaths, want) {
		t.Fatalf("sources filter: selected=%d, paths=%v", selected, selectedPaths)
	}

	// AS_PARAM выбирает все семейства параметров, но не другие форматы.
	paramFilters := Filters{Regions: map[int]bool{77: true}, Kinds: map[Kind]bool{KindParam: true}}
	plan = Plan(paths)
	Select(plan, paramFilters)
	got := SelectedPaths(plan)
	want = []string{paths[3], paths[4]}
	if !equalStrings(got, want) {
		t.Fatalf("param kind filter: %v", got)
	}

	// Форматный фильтр AS_HOUSES не выбирает параметры домов.
	houseFilters := Filters{Regions: map[int]bool{77: true}, Kinds: map[Kind]bool{KindHouses: true}}
	plan = Plan(paths)
	Select(plan, houseFilters)
	got = SelectedPaths(plan)
	want = []string{paths[0]}
	if !equalStrings(got, want) {
		t.Fatalf("houses filter: %v", got)
	}

	// Без фильтров выбирается всё корректное, кроме unknown/duplicate.
	plan = Plan(paths)
	Select(plan, Filters{})
	if got := SelectedPaths(plan); len(got) != 6 {
		t.Fatalf("no filters: %v", got)
	}
	if unknown := Unknown(plan); len(unknown) != 5 {
		t.Fatalf("unknown entries: %v", unknown)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
