package gartype

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// Flag — логический признак выгрузки ГАР. В файлах данных значения
// записываются как "1"/"0" (ISACTUAL, ISACTIVE), в справочниках —
// как "true"/"false". Оба варианта разбираются одинаково.
type Flag bool

// UnmarshalText разбирает "1"/"0"/"true"/"false"; пустое значение — false.
func (f *Flag) UnmarshalText(text []byte) error {
	switch trimXMLSpace(string(text)) {
	case "", "0", "false":
		*f = false
	case "1", "true":
		*f = true
	default:
		// В файлах данных это xs:integer, где +1, 01 и -0 также допустимы.
		value, err := parseInteger(string(text))
		if err != nil || (value != "0" && value != "1") {
			return fmt.Errorf("неверный признак %q", text)
		}
		*f = value == "1"
	}
	return nil
}

// MarshalText кодирует признак как "1"/"0" (формат файлов данных).
func (f Flag) MarshalText() ([]byte, error) {
	if f {
		return []byte("1"), nil
	}
	return []byte("0"), nil
}

// MarshalJSON сохраняет логический тип для поисковых движков.
func (f Flag) MarshalJSON() ([]byte, error) { return json.Marshal(bool(f)) }

func (f *Flag) UnmarshalJSON(text []byte) error {
	if trimXMLSpace(string(text)) == "null" {
		return fmt.Errorf("null не является признаком")
	}
	var value bool
	if err := json.Unmarshal(text, &value); err != nil {
		return err
	}
	*f = Flag(value)
	return nil
}

// Value и Scan — поддержка database/sql для столбцов типа boolean.

func (f Flag) Value() (driver.Value, error) { return bool(f), nil }

func (f *Flag) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*f = false
	case bool:
		*f = Flag(v)
	case []byte:
		return f.UnmarshalText(v)
	case string:
		return f.UnmarshalText([]byte(v))
	case int64:
		if v != 0 && v != 1 {
			return fmt.Errorf("неверный признак %d", v)
		}
		*f = v == 1
	default:
		return fmt.Errorf("gartype: невозможно прочитать %T как Flag", src)
	}
	return nil
}
