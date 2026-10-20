package search

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ValidateSuggestInput bounds user input on both HTTP and gRPC boundaries.
func ValidateSuggestInput(query, region string) error {
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 200 || len(strings.Fields(query)) > 20 {
		return fmt.Errorf("query: максимум 200 символов и 20 слов")
	}
	if len(region) > 3 {
		return fmt.Errorf("region: от 1 до 3 цифр")
	}
	for _, r := range region {
		if r < '0' || r > '9' {
			return fmt.Errorf("region: от 1 до 3 цифр")
		}
	}
	return nil
}
