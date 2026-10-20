package address

import "testing"

// Кейсы взяты из руководства «Формирование адресной строки»: тип и
// наименование через пробел, элементы иерархии — через «, », для домов —
// основной тип и номер плюс дополнительные (корпус, строение).
func TestComponents(t *testing.T) {
	tests := []struct {
		name      string
		component Component
		want      string
	}{
		{"город", AddrObjComponent("г", "Москва"), "г Москва"},
		{"улица", AddrObjComponent("ул", "Тверская"), "ул Тверская"},
		{"городской округ", AddrObjComponent("г.о.", "Павлово-Посадский"), "г.о Павлово-Посадский"},
		{"дом", HouseComponent("7", "д", NumberedPart{}, NumberedPart{}), "д 7"},
		{"дом с точкой в типе", HouseComponent("93", "д.", NumberedPart{}, NumberedPart{}), "д 93"},
		{"дом без типа", HouseComponent("7", "", NumberedPart{}, NumberedPart{}), "д 7"},
		{"дом с корпусом и строением", HouseComponent("7", "д",
			NumberedPart{Num: "6", Type: "корпус"}, NumberedPart{Num: "2", Type: "стр"}),
			"д 7 корпус 6 стр 2"},
		{"дом только с корпусом", HouseComponent("", "влд",
			NumberedPart{Num: "1", Type: "к"}, NumberedPart{}), "влд к 1"},
		{"квартира", NumberComponent("кв.", defaultApartmentType, "10"), "кв 10"},
		{"квартира без типа", NumberComponent("", defaultApartmentType, "17"), "кв 17"},
		{"комната", NumberComponent("ком", defaultRoomType, "2"), "ком 2"},
		{"участок", NumberComponent("", defaultSteadType, "12"), "уч 12"},
		{"машино-место", NumberComponent("", defaultCarplaceType, "34"), "м/м 34"},
		{"без типа", Component{Name: "Москва"}, "Москва"},
		{"пустой", Component{}, ""},
	}
	for _, tt := range tests {
		if got := tt.component.String(); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestJoinComponents(t *testing.T) {
	chain := []Component{
		AddrObjComponent("г", "Москва"),
		AddrObjComponent("ул", "Тверская"),
		HouseComponent("7", "д", NumberedPart{}, NumberedPart{}),
		NumberComponent("кв", defaultApartmentType, "10"),
	}
	if got := JoinComponents(chain); got != "г Москва, ул Тверская, д 7, кв 10" {
		t.Fatalf("got %q", got)
	}
	// Пустые элементы опускаются, лишних разделителей нет.
	withEmpty := []Component{{}, chain[0], {Type: ""}, chain[2]}
	if got := JoinComponents(withEmpty); got != "г Москва, д 7" {
		t.Fatalf("got %q", got)
	}
	if got := JoinComponents(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeShortType(t *testing.T) {
	tests := map[string]string{
		"кв.":   "кв",
		" д ":   "д",
		"ул":    "ул",
		"г.о.":  "г.о",
		"":      "",
		" м/м ": "м/м",
	}
	for input, want := range tests {
		if got := NormalizeShortType(input); got != want {
			t.Errorf("NormalizeShortType(%q): got %q, want %q", input, got, want)
		}
	}
}

func TestParsePath(t *testing.T) {
	ids := parsePath("807356.162142236.815937")
	if len(ids) != 3 || ids[0] != 807356 || ids[2] != 815937 {
		t.Fatalf("got %v", ids)
	}
	// Мусорные токены пропускаются.
	if ids := parsePath("1..x.3"); len(ids) != 2 || ids[0] != 1 || ids[1] != 3 {
		t.Fatalf("got %v", ids)
	}
}
