package runtime

import "testing"

func TestParseName(t *testing.T) {
	cases := []struct {
		in   string
		want Name
		ok   bool
	}{
		{"", NameDocker, true},
		{"docker", NameDocker, true},
		{"host", NameHost, true},
		{"microvm", NameMicroVM, true},
		{"firecracker", NameMicroVM, true},
		{"weird", "", false},
	}
	for _, tc := range cases {
		got, err := ParseName(tc.in)
		if tc.ok {
			if err != nil || got != tc.want {
				t.Fatalf("%q: got (%q,%v) want %q", tc.in, got, err, tc.want)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%q: expected error", tc.in)
		}
	}
}
