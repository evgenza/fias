package importjob

import (
	"testing"

	"gar-importer/internal/gar"
)

func TestResumeSelection(t *testing.T) {
	saved := `{"regions":[77,50],"kinds":["AS_HOUSES"],"sources":["AS_HOUSES_PARAMS"],"limit_per_file":12}`
	options := Options{Regions: []int{50, 77, 77}, Kinds: []gar.Kind{gar.KindHouses}, Sources: []string{"AS_HOUSES_PARAMS"}, LimitPerFile: 12, BatchSize: 17}
	if err := validateResumeOptions(saved, options); err != nil {
		t.Fatal(err)
	}
	options.LimitPerFile = 13
	if err := validateResumeOptions(saved, options); err == nil {
		t.Fatal("changed sample limit accepted")
	}
}
