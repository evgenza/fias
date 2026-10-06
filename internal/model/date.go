package model

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// Date — календарная дата без времени и часового пояса (xs:date выгрузки ГАР,
// формат YYYY-MM-DD). Соответствует типу date в PostgreSQL.
//
// Нулевое значение означает отсутствие даты (пустой атрибут XML).
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// UnmarshalText разбирает дату в формате YYYY-MM-DD. Пустое значение
// обнуляет дату и ошибкой не считается.
func (d *Date) UnmarshalText(text []byte) error {
	*d = Date{}
	if len(text) == 0 {
		return nil
	}
	t, err := time.Parse("2006-01-02", string(text))
	if err != nil {
		return fmt.Errorf("invalid date %q: %w", text, err)
	}
	d.Year, d.Month, d.Day = t.Date()
	return nil
}

// MarshalText возвращает дату в формате YYYY-MM-DD (пустая строка для нулевой даты).
func (d Date) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// IsZero сообщает, что дата не задана.
func (d Date) IsZero() bool { return d == Date{} }

// Time возвращает дату как time.Time (полночь UTC).
func (d Date) Time() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// Value и Scan — поддержка database/sql для столбцов типа date.

func (d Date) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	return d.Time(), nil
}

func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*d = Date{}
	case time.Time:
		v = v.UTC()
		*d = Date{Year: v.Year(), Month: v.Month(), Day: v.Day()}
	case []byte:
		return d.UnmarshalText(v)
	case string:
		return d.UnmarshalText([]byte(v))
	default:
		return fmt.Errorf("model: cannot scan %T into Date", src)
	}
	return nil
}
