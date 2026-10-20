package gar

import (
	"encoding/xml"
	"fmt"
	"strings"
	"unicode/utf8"

	"gar-importer/internal/gartype"
)

// Спецификация атрибутов формата генерируется из закреплённых XSD (xsd/ в корне
// репозитория) командой cmd/gar-gen-spec и служит контрактом приёма записей:
// обязательность и ограничения проверяются до дедупликации и записи в БД.

// AttrKind — тип значения атрибута по XSD.
type AttrKind uint8

const (
	// AttrString — xs:string (включая GUID: ограничение длины из xs:length).
	AttrString AttrKind = iota
	// AttrDate — xs:date, календарный день.
	AttrDate
	// AttrInteger — xs:integer без гарантии диапазона int64 (gartype.Integer).
	AttrInteger
	// AttrLong — xs:long, значение должно помещаться в int64.
	AttrLong
	// AttrBoolean — xs:boolean; в файлах данных записывается как "0"/"1".
	AttrBoolean
)

// FieldSpec описывает один XML-атрибут записи.
type FieldSpec struct {
	Name     string
	Kind     AttrKind
	Required bool
	MinLen   int      // xs:minLength для строк (обязательные строки имеют minLength=1)
	MaxLen   int      // xs:maxLength для строк
	Length   int      // xs:length для строк (GUID — 36)
	Int64    bool     // xs:integer, чей Go-тип поля int64: значение должно помещаться в int64
	UUID     bool     // строка — UUID (в БД колонка uuid)
	Enum     []string // допустимые значения xs:enumeration из XSD
}

// FormatSpec — состав записи формата: имена элементов и атрибуты.
type FormatSpec struct {
	Root   string
	Record string
	Fields []FieldSpec
}

// RecordError — структурированная ошибка приёма записи: формат, атрибут и причина.
// Контекст (файл, номер записи, смещение XML) добавляют декодер и сценарий импорта.
type RecordError struct {
	Kind      Kind
	Attribute string
	Cause     string
}

func (e *RecordError) Error() string {
	if e.Attribute == "" {
		return fmt.Sprintf("gar: %s: %s", e.Kind, e.Cause)
	}
	return fmt.Sprintf("gar: %s: атрибут %s: %s", e.Kind, e.Attribute, e.Cause)
}

// Spec возвращает спецификацию формата, сгенерированную из XSD.
func Spec(kind Kind) (FormatSpec, bool) {
	spec, ok := generatedSpecs[kind]
	return spec, ok
}

// ValidateRecord проверяет набор и значения атрибутов записи по спецификации формата:
// присутствие обязательных атрибутов, допустимость значений и запрет неизвестных
// атрибутов (строгий режим не теряет данные новых выпусков молча).
func ValidateRecord(kind Kind, spec FormatSpec, element xml.StartElement) error {
	byName := make(map[string]*FieldSpec, len(spec.Fields))
	for i := range spec.Fields {
		byName[spec.Fields[i].Name] = &spec.Fields[i]
	}
	present := make(map[string]bool, len(element.Attr))
	for _, attr := range element.Attr {
		if attr.Name.Space == "xmlns" || (attr.Name.Space == "" && attr.Name.Local == "xmlns") {
			continue // объявление пространства имён, не атрибут данных
		}
		if attr.Name.Space != "" {
			return &RecordError{Kind: kind, Attribute: attr.Name.Space + ":" + attr.Name.Local,
				Cause: "неизвестный атрибут: attributeFormDefault=\"unqualified\""}
		}
		field, ok := byName[attr.Name.Local]
		if !ok {
			return &RecordError{Kind: kind, Attribute: attr.Name.Local,
				Cause: fmt.Sprintf("неизвестный атрибут, отсутствует в XSD формата %s", kind)}
		}
		present[attr.Name.Local] = true
		if err := checkValue(kind, field, attr.Value); err != nil {
			return err
		}
	}
	for i := range spec.Fields {
		if field := &spec.Fields[i]; field.Required && !present[field.Name] {
			return &RecordError{Kind: kind, Attribute: field.Name, Cause: "отсутствует обязательный атрибут"}
		}
	}
	return nil
}

func checkValue(kind Kind, field *FieldSpec, value string) error {
	attribute := func(cause string) error {
		return &RecordError{Kind: kind, Attribute: field.Name, Cause: cause}
	}
	switch field.Kind {
	case AttrString:
		chars := utf8.RuneCountInString(value)
		if field.Length > 0 && chars != field.Length {
			return attribute(fmt.Sprintf("длина %d, ожидается %d", chars, field.Length))
		}
		if field.MinLen > 0 && chars < field.MinLen {
			return attribute(fmt.Sprintf("длина %d, ожидается не менее %d", chars, field.MinLen))
		}
		if field.MaxLen > 0 && chars > field.MaxLen {
			return attribute(fmt.Sprintf("длина %d, ожидается не более %d", chars, field.MaxLen))
		}
		if field.UUID && !isUUID(value) {
			return attribute(fmt.Sprintf("значение %q не UUID", value))
		}
	case AttrDate:
		if trimmed := strings.Trim(value, " \t\r\n"); trimmed == "" {
			return attribute(fmt.Sprintf("неверная дата %q", value))
		}
		var date gartype.Date
		if err := date.UnmarshalText([]byte(value)); err != nil {
			return attribute(fmt.Sprintf("неверная дата %q", value))
		}
	case AttrInteger, AttrLong:
		var integer gartype.Integer
		if err := integer.UnmarshalText([]byte(value)); err != nil {
			return attribute(err.Error())
		}
		if field.Kind == AttrLong || field.Int64 {
			if _, err := integer.Int64(); err != nil {
				return attribute(fmt.Sprintf("значение %s не помещается в int64", integer))
			}
		}
	case AttrBoolean:
		var flag gartype.Flag
		if err := flag.UnmarshalText([]byte(value)); err != nil || value == "" {
			return attribute(fmt.Sprintf("неверное логическое значение %q", value))
		}
	}
	if len(field.Enum) > 0 {
		candidate := value
		if field.Kind == AttrInteger || field.Kind == AttrLong {
			// Перечисление целых сравнивается в канонической форме: "+1" == "1".
			var integer gartype.Integer
			if err := integer.UnmarshalText([]byte(value)); err == nil {
				candidate = integer.String()
			}
		}
		allowed := false
		for _, item := range field.Enum {
			if candidate == item {
				allowed = true
				break
			}
		}
		if !allowed {
			return attribute(fmt.Sprintf("значение %q не из перечисления %s", value, strings.Join(field.Enum, ", ")))
		}
	}
	return nil
}

// isUUID проверяет формат UUID: 32 шестнадцатеричные цифры с дефисами 8-4-4-4-12.
func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			validHex := '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
			if !validHex {
				return false
			}
		}
	}
	return true
}
