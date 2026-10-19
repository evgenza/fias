package gartype

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Integer — целое xs:integer, для которого XSD не гарантирует диапазон int64.
// Каноническая десятичная строка сохраняет все цифры. JSON использует строку
// для keyword в поисковом индексе; PostgreSQL хранит значение как numeric.
type Integer string

func trimXMLSpace(s string) string { return strings.Trim(s, " \t\r\n") }

func parseInteger(s string) (Integer, error) {
	s = trimXMLSpace(s)
	negative := false
	if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		negative = s[0] == '-'
		s = s[1:]
	}
	if s == "" {
		return "", fmt.Errorf("пустое значение xs:integer")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return "", fmt.Errorf("неверное значение xs:integer %q", s)
		}
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return "0", nil
	}
	if negative {
		s = "-" + s
	}
	return Integer(s), nil
}

func (v *Integer) UnmarshalText(text []byte) error {
	parsed, err := parseInteger(string(text))
	if err == nil {
		*v = parsed
	}
	return err
}

func (v Integer) MarshalText() ([]byte, error) {
	parsed, err := parseInteger(string(v))
	return []byte(parsed), err
}

func (v Integer) String() string { return string(v) }

// Int64 возвращает ошибку, если значение не помещается в 64 бита.
func (v Integer) Int64() (int64, error) {
	text, err := v.MarshalText()
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(string(text), 10, 64)
}

func (v Integer) MarshalJSON() ([]byte, error) {
	text, err := v.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(text))
}

func (v *Integer) UnmarshalJSON(text []byte) error {
	var value string
	if err := json.Unmarshal(text, &value); err != nil {
		return err
	}
	return v.UnmarshalText([]byte(value))
}

func (v Integer) Value() (driver.Value, error) {
	text, err := v.MarshalText()
	return string(text), err
}

func (v *Integer) Scan(src any) error {
	switch value := src.(type) {
	case nil:
		*v = ""
		return nil
	case string:
		return v.UnmarshalText([]byte(value))
	case []byte:
		return v.UnmarshalText(value)
	case int64:
		*v = Integer(strconv.FormatInt(value, 10))
		return nil
	default:
		return fmt.Errorf("gartype: невозможно прочитать %T как Integer", src)
	}
}
