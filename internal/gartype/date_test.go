package gartype

import (
	"testing"
	"time"
)

func TestDateSQLBeforeCommonEra(t *testing.T) {
	value, err := Date("-0001-01-01").Value()
	if err != nil || value != "0001-01-01 BC" {
		t.Fatalf("SQL value = %v, %v", value, err)
	}
	for _, source := range []any{value, []byte("0001-01-01 BC"), time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)} {
		var got Date
		if err := got.Scan(source); err != nil || got != "-0001-01-01" {
			t.Errorf("scan %v: %s, %v", source, got, err)
		}
	}
}
