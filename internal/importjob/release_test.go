package importjob

import "testing"

func TestReleaseOrder(t *testing.T) {
	for _, tc := range []struct {
		mode, applied, base, target string
		ok                          bool
	}{
		{"delta", "20261002", "20261002", "20261003", true},
		{"delta", "20261003", "20261001", "20261002", false},
		{"delta", "20261001", "20261002", "20261003", false},
		{"snapshot", "20261003", "", "20261002", false},
		{"snapshot", "20261003", "", "20261003", true},
		{"delta", "", "20261002", "20261003", false},
	} {
		if err := ValidateReleaseOrder(tc.mode, tc.applied, tc.base, tc.target); (err == nil) != tc.ok {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}
