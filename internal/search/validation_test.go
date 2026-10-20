package search

import (
	"strings"
	"testing"
)

func TestValidateSuggestInput(t *testing.T) {
	for _, tc := range []struct {
		q, r string
		bad  bool
	}{
		{"Москва", "077", false}, {strings.Repeat("я", 200), "1", false}, {strings.Repeat("я", 201), "", true}, {strings.Repeat("а ", 21), "", true}, {"Москва", "1234", true}, {"Москва", "1x", true}, {"Москва", "１２", true},
	} {
		if err := ValidateSuggestInput(tc.q, tc.r); (err != nil) != tc.bad {
			t.Errorf("q length=%d region=%q error=%v", len(tc.q), tc.r, err)
		}
	}
}
