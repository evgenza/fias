package app

import (
	"reflect"
	"testing"

	"gar-importer/internal/address"
	"gar-importer/internal/gar"
)

func TestRoutingDependencies(t *testing.T) {
	s := &service{builder: address.NewBuilder(nil)}
	for _, tc := range []struct {
		kind gar.Kind
		want []string
	}{
		{gar.KindAddrObj, []string{"addr_obj", "house", "apartment", "room", "stead", "carplace"}},
		{gar.KindReestrObjects, []string{"addr_obj", "house", "apartment", "room", "stead", "carplace"}},
		{gar.KindHouses, []string{"house", "apartment", "room", "carplace"}},
		{gar.KindApartments, []string{"apartment", "room"}},
		{gar.KindParam, []string{"addr_obj", "house", "apartment", "room", "stead", "carplace"}},
		{gar.KindMunHierarchy, []string{"addr_obj", "house", "apartment", "room", "stead", "carplace"}},
	} {
		tasks, known := s.routeTasks(tc.kind)
		if !known {
			t.Fatalf("unknown %s", tc.kind)
		}
		var got []string
		for _, task := range tasks {
			got = append(got, task.docKind)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v want %v", tc.kind, got, tc.want)
		}
	}
}
