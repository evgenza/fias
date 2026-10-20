// Package address — сборка поисковых документов адресов из таблиц ГАР
// в PostgreSQL: иерархия (adm_hierarchy), собственные записи объектов
// и справочники типов преобразуются в документы search.Document.
//
// Правила формирования адресной строки — по официальному руководству ФНС
// «Формирование адресной строки» (docs/01 Руководства): полное наименование
// элемента — «тип наименование», для домов — основной тип и номер плюс
// дополнительные типы и номера (корпус, строение); элементы иерархии
// соединяются разделителем «, ». В индекс пишутся сокращённые типы
// («г Москва, ул Тверская, д 7»).
package address

import (
	"strings"
)

// Сокращённые типы по умолчанию (применяются, когда справочник не задан).
const (
	defaultHouseType     = "д"
	defaultApartmentType = "кв"
	defaultRoomType      = "ком"
	defaultSteadType     = "уч"
	defaultCarplaceType  = "м/м"
)

// Component — один элемент адресной строки: сокращённый тип и наименование
// («ул» + «Тверская»).
type Component struct {
	Type string
	Name string
}

// String собирает элемент: «ул Тверская»; пустые части опускаются.
func (c Component) String() string {
	switch {
	case c.Type == "":
		return c.Name
	case c.Name == "":
		return c.Type
	default:
		return c.Type + " " + c.Name
	}
}

// JoinComponents соединяет элементы иерархии разделителем «, »,
// пропуская пустые.
func JoinComponents(components []Component) string {
	parts := make([]string, 0, len(components))
	for _, component := range components {
		text := component.String()
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, ", ")
}

// NormalizeShortType приводит сокращение типа из справочника к виду
// без завершающей точки и пробелов: «кв.» → «кв».
func NormalizeShortType(short string) string {
	return strings.TrimSuffix(strings.TrimSpace(short), ".")
}

// AddrObjComponent — элемент адресообразующего объекта: «г Москва».
func AddrObjComponent(typeName, name string) Component {
	return Component{Type: NormalizeShortType(typeName), Name: strings.TrimSpace(name)}
}

// NumberedPart — дополнительный номер дома: номер и короткий тип
// («1» + «к» → «к 1»).
type NumberedPart struct {
	Num  string
	Type string
}

// String собирает дополнительный номер: «к 1»; без типа — просто номер.
func (p NumberedPart) String() string {
	num := strings.TrimSpace(p.Num)
	if num == "" {
		return ""
	}
	if short := NormalizeShortType(p.Type); short != "" {
		return short + " " + num
	}
	return num
}

// HouseComponent — элемент дома по правилу руководства: основной тип и номер,
// затем дополнительные типы и номера при наличии: «д 7 к 1 стр 2».
// Пустой основной тип заменяется на «д».
func HouseComponent(num, houseType string, add1, add2 NumberedPart) Component {
	short := NormalizeShortType(houseType)
	if short == "" {
		short = defaultHouseType
	}
	name := strings.TrimSpace(num)
	for _, add := range []NumberedPart{add1, add2} {
		if text := add.String(); text != "" {
			if name != "" {
				name += " "
			}
			name += text
		}
	}
	return Component{Type: short, Name: name}
}

// NumberComponent — элемент объекта с простым номером (квартира, комната,
// участок, машино-место): «кв 10»; пустой тип заменяется на fallback.
func NumberComponent(typeShort, fallback, number string) Component {
	short := NormalizeShortType(typeShort)
	if short == "" {
		short = fallback
	}
	return Component{Type: short, Name: strings.TrimSpace(number)}
}
