package gartype

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Date хранит календарную дату xs:date. Часовой пояс проверяется и
// отбрасывается без изменения дня. JSON: строка, отсутствие даты — null.
// Поддерживаются отрицательные и расширенные годы XSD 1.0.
type Date string

var datePattern = regexp.MustCompile(`^(-?)([0-9]{4,})-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])(Z|[+-][0-9]{2}:[0-9]{2})?$`)

func parseDate(text string) (Date, error) {
	text = trimXMLSpace(text)
	m := datePattern.FindStringSubmatch(text)
	if m == nil || strings.TrimLeft(m[2], "0") == "" || (len(m[2]) > 4 && m[2][0] == '0') {
		return "", fmt.Errorf("неверная дата xs:date %q", text)
	}
	if zone := m[5]; zone != "" && zone != "Z" {
		h, _ := strconv.Atoi(zone[1:3])
		minute, _ := strconv.Atoi(zone[4:6])
		if h > 14 || minute > 59 || (h == 14 && minute != 0) {
			return "", fmt.Errorf("неверный часовой пояс xs:date %q", zone)
		}
	}
	// Делимость года на 400 определяется последними четырьмя цифрами.
	year := m[2]
	if len(year) > 4 {
		year = year[len(year)-4:]
	}
	y, _ := strconv.Atoi(year)
	month, _ := strconv.Atoi(m[3])
	day, _ := strconv.Atoi(m[4])
	days := [...]int{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if y%4 == 0 && (y%100 != 0 || y%400 == 0) {
		days[2] = 29
	}
	if day > days[month] {
		return "", fmt.Errorf("неверный день xs:date %q", text)
	}
	return Date(m[1] + m[2] + "-" + m[3] + "-" + m[4]), nil
}

// UnmarshalText читает xs:date; пустое значение сохраняет отсутствие даты.
func (d *Date) UnmarshalText(text []byte) error {
	if trimXMLSpace(string(text)) == "" {
		*d = ""
		return nil
	}
	v, err := parseDate(string(text))
	if err == nil {
		*d = v
	}
	return err
}

func (d Date) MarshalText() ([]byte, error) {
	if d.IsZero() {
		return nil, nil
	}
	v, err := parseDate(string(d))
	return []byte(v), err
}

func (d Date) String() string { return string(d) }
func (d Date) IsZero() bool   { return d == "" }

func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	v, err := d.MarshalText()
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(v))
}

func (d *Date) UnmarshalJSON(text []byte) error {
	if trimXMLSpace(string(text)) == "null" {
		*d = ""
		return nil
	}
	var value string
	if err := json.Unmarshal(text, &value); err != nil {
		return err
	}
	return d.UnmarshalText([]byte(value))
}

// Time преобразует дату в time.Time для годов 1..9999; расширенные годы
// сохраняются в Date и требуют отдельного адаптера целевого хранилища.
func (d Date) Time() (time.Time, error) {
	v, err := d.MarshalText()
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse("2006-01-02", string(v))
}

// Value и Scan — представление PostgreSQL date через database/sql.
func (d Date) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	v, err := d.MarshalText()
	if len(v) > 0 && v[0] == '-' {
		// PostgreSQL обозначает годы до нашей эры суффиксом BC.
		return string(v[1:]) + " BC", err
	}
	return string(v), err
}

func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*d = ""
		return nil
	case time.Time:
		// SQL date — календарный день, а не момент времени в UTC.
		year, sign := v.Year(), ""
		if year <= 0 {
			// Go/pgx используют год 0 для 1 BC; xs:date 1.0 не имеет года 0.
			year, sign = 1-year, "-"
		}
		return d.UnmarshalText([]byte(fmt.Sprintf("%s%04d-%02d-%02d", sign, year, v.Month(), v.Day())))
	case []byte:
		return d.Scan(string(v))
	case string:
		if strings.HasSuffix(v, " BC") {
			v = "-" + strings.TrimSuffix(v, " BC")
		}
		return d.UnmarshalText([]byte(v))
	default:
		return fmt.Errorf("gartype: cannot scan %T into Date", src)
	}
}
