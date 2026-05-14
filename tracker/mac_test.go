package tracker

import "testing"

func TestNormalizeMAC(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff"},
		{"aa-bb-cc-dd-ee-ff", "aa:bb:cc:dd:ee:ff"},
		{"aabb.ccdd.eeff", "aa:bb:cc:dd:ee:ff"},
		{"AABBCCDDEEFF", "aa:bb:cc:dd:ee:ff"},
		{"  aa:bb:cc:dd:ee:ff  ", "aa:bb:cc:dd:ee:ff"},
		{"not a mac", ""},
		{"aa:bb:cc:dd:ee", ""},
	}
	for _, c := range cases {
		got := NormalizeMAC(c.in)
		if got != c.want {
			t.Errorf("NormalizeMAC(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
