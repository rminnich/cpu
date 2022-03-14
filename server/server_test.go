package server

import (
	"reflect"
	"testing"
)

func TestParseBind(t *testing.T) {
	var tests = []struct {
		in    string
		out   []Bind
		error string
	}{
		{in: "", out: []Bind{}},
		{in: ":", out: []Bind{}, error: "bind: element 0 is zero length"},
		{in: "l=:", out: []Bind{}, error: "bind: element 0:name in \"l=\": zero-length remote name"},
		{in: "=r:", out: []Bind{}, error: "bind: element 0:name in \"=r\": zero-length local name"},
		{
			in: "/bin",
			out: []Bind{
				Bind{Local: "/bin", Remote: "/bin"},
			},
		},
		{
			in: "/bin", out: []Bind{
				Bind{Local: "/bin", Remote: "/bin"},
			},
		},

		{
			in: "/bin=/home/user/bin",
			out: []Bind{
				Bind{Local: "/bin", Remote: "/home/user/bin"},
			},
		},
		{
			in: "/bin=/home/user/bin:/home",
			out: []Bind{
				Bind{Local: "/bin", Remote: "/home/user/bin"},
				Bind{Local: "/home", Remote: "/home"},
			},
		},
	}
	for i, tt := range tests {
		b, err := ParseBinds(tt.in)
		t.Logf("Test %d:%q => (%q, %v), want %q", i, tt.in, b, err, tt.out)
		if len(tt.error) == 0 {
			if err != nil {
				t.Errorf("%d:ParseBinds(%q): err %v != nil", i, tt.in, err)
				continue
			}
			if !reflect.DeepEqual(b, tt.out) {
				t.Errorf("%d:ParseBinds(%q): Binds %q != %q", i, tt.in, b, tt.out)
				continue
			}
			continue
		}
		if err == nil {
			t.Errorf("%d:ParseBinds(%q): err nil != %q", i, tt.in, tt.error)
			continue
		}
		if err.Error() != tt.error {
			t.Errorf("%d:ParseBinds(%q): err %s != %s", i, tt.in, err.Error(), tt.error)
			continue
		}

	}
}
