package garxml

import "testing"

func TestParseFileName(t *testing.T) {
	cases := []struct {
		in      string
		kind    Kind
		date    string
		wantErr bool
	}{
		{in: "AS_ADDR_OBJ_20260925_4f8b6f5a7f0b4f7d9e3c1a2b.xml", kind: KindAddrObj, date: "20260925"},
		{in: "AS_HOUSES_20260925_01.XML", kind: KindHouses, date: "20260925"},
		{in: "AS_MUN_HIERARCHY_20260925_x.xml", kind: KindMunHierarchy, date: "20260925"},
		{in: "AS_NORMATIVE_DOCS_TYPES_20260925_x.xml", kind: KindNormativeDocTypes, date: "20260925"},
		{in: "AS_UNKNOWN_20260925_x.xml", wantErr: true},
		{in: "AS_HOUSES_2026-09-25_x.xml", wantErr: true},
		{in: "AS_HOUSES_20260925.csv", wantErr: true},
		{in: "AS_HOUSES.xml", wantErr: true},
	}
	for _, c := range cases {
		kind, date, seq, err := ParseFileName(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseFileName(%q): want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseFileName(%q): %v", c.in, err)
			continue
		}
		if kind != c.kind || date != c.date || seq == "" {
			t.Errorf("ParseFileName(%q) = %q,%q,%q", c.in, kind, date, seq)
		}
	}
}

func TestRegionCode(t *testing.T) {
	if n, err := RegionCode("77/AS_HOUSES_20260925_x.xml"); err != nil || n != 77 {
		t.Errorf("region(77/...) = %d, %v", n, err)
	}
	if n, err := RegionCode("1/AS_HOUSES_20260925_x.xml"); err != nil || n != 1 {
		t.Errorf("region(1/...) = %d, %v", n, err)
	}
	if n, err := RegionCode("AS_ADDR_OBJ_TYPES_20260925_x.xml"); err != nil || n != 0 {
		t.Errorf("region(root file) = %d, %v", n, err)
	}
	if _, err := RegionCode("weird/AS_HOUSES_20260925_x.xml"); err == nil {
		t.Error("region(weird/...) must fail")
	}
}

func TestLayout(t *testing.T) {
	if !KindHouses.Known() {
		t.Error("AS_HOUSES must be known")
	}
	if KindHouses.RootElement() != "HOUSES" || KindHouses.RecordElement() != "HOUSE" {
		t.Errorf("layout(AS_HOUSES) = %q/%q", KindHouses.RootElement(), KindHouses.RecordElement())
	}
	if KindAddrObj.RootElement() != "ADDRESSOBJECTS" || KindAddrObj.RecordElement() != "OBJECT" {
		t.Errorf("layout(AS_ADDR_OBJ) = %q/%q", KindAddrObj.RootElement(), KindAddrObj.RecordElement())
	}
}
