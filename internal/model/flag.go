package model

import (
	"database/sql/driver"
	"fmt"
)

// Flag — логический признак выгрузки ГАР. В файлах данных значения
// записываются как "1"/"0" (ISACTUAL, ISACTIVE), в справочниках —
// как "true"/"false". Оба варианта разбираются одинаково.
type Flag bool

// UnmarshalText разбирает "1"/"0"/"true"/"false"; пустое значение — false.
func (f *Flag) UnmarshalText(text []byte) error {
	switch string(text) {
	case "", "0", "false":
		*f = false
	case "1", "true":
		*f = true
	default:
		return fmt.Errorf("invalid flag %q", text)
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
		*f = v != 0
	default:
		return fmt.Errorf("model: cannot scan %T into Flag", src)
	}
	return nil
}
