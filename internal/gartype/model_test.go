package gartype

import (
	"testing"
)

func TestDateUnmarshal(t *testing.T) {
	cases := []struct {
		in      string
		want    Date
		wantErr bool
	}{
		{in: "2024-11-23", want: Date("2024-11-23")},
		{in: "1900-01-01", want: Date("1900-01-01")},
		{in: "9999-12-31", want: Date("9999-12-31")},
		{in: "", want: Date("")},
		{in: "2024-11-23T00:00:00", wantErr: true},
		{in: "23.11.2024", wantErr: true},
		{in: "2024-13-01", wantErr: true},
	}
	for _, c := range cases {
		var d Date
		err := d.UnmarshalText([]byte(c.in))
		if c.wantErr {
			if err == nil {
				t.Errorf("UnmarshalText(%q): want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("UnmarshalText(%q): %v", c.in, err)
			continue
		}
		if d != c.want {
			t.Errorf("UnmarshalText(%q) = %v, want %v", c.in, d, c.want)
		}
		if got := d.String(); got != c.in {
			t.Errorf("String() = %q, want %q", got, c.in)
		}
	}
}

func TestFlagUnmarshal(t *testing.T) {
	cases := []struct {
		in      string
		want    Flag
		wantErr bool
	}{
		{in: "1", want: true},
		{in: "0", want: false},
		{in: "true", want: true},
		{in: "false", want: false},
		{in: " +1 ", want: true},
		{in: "01", want: true},
		{in: "-0", want: false},
		{in: "", want: false},
		{in: "2", wantErr: true},
	}
	for _, c := range cases {
		var f Flag
		err := f.UnmarshalText([]byte(c.in))
		if c.wantErr {
			if err == nil {
				t.Errorf("UnmarshalText(%q): want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("UnmarshalText(%q): %v", c.in, err)
		}
		if f != c.want {
			t.Errorf("UnmarshalText(%q) = %v, want %v", c.in, f, c.want)
		}
	}
}
