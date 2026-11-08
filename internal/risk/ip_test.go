package risk

import "testing"

func TestSubnet(t *testing.T) {
	cases := map[string]string{
		"192.168.12.34":        "192.168.12.0/24",
		"::ffff:192.168.12.34": "192.168.12.0/24",
		"2001:db8:abcd:12::1":  "2001:db8:abcd:12::/64",
		"bad-ip":               "",
	}
	for in, want := range cases {
		if got := Subnet(in); got != want {
			t.Errorf("Subnet(%q) = %q, want %q", in, got, want)
		}
	}
}
