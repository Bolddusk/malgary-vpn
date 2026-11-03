package policy

import (
	"net/netip"
	"testing"
)

func TestComplementCoversEverythingButExclusions(t *testing.T) {
	all := netip.MustParsePrefix("0.0.0.0/0")
	ex := []netip.Prefix{netip.MustParsePrefix("142.250.82.0/24"), netip.MustParsePrefix("52.112.0.0/14")}
	got, err := Complement(all, ex)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || len(got) > 64 {
		t.Fatalf("unexpected route count %d", len(got))
	}
	in := func(a string) bool {
		ip := netip.MustParseAddr(a)
		for _, p := range got {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}
	for _, a := range []string{"8.8.8.8", "142.250.81.255", "142.250.83.0", "52.111.255.255", "52.116.0.0", "203.0.113.5"} {
		if !in(a) {
			t.Errorf("%s should be routed through the VPN", a)
		}
	}
	for _, a := range []string{"142.250.82.10", "52.112.0.1", "52.115.255.254"} {
		if in(a) {
			t.Errorf("%s is a call range and must stay off the VPN (FR-006)", a)
		}
	}
	// No overlaps between returned prefixes.
	for i := range got {
		for j := i + 1; j < len(got); j++ {
			if got[i].Overlaps(got[j]) {
				t.Fatalf("%s overlaps %s", got[i], got[j])
			}
		}
	}
}

func TestComplementNoExclusions(t *testing.T) {
	got, _ := Complement(netip.MustParsePrefix("0.0.0.0/0"), nil)
	if len(got) != 1 || got[0].String() != "0.0.0.0/0" {
		t.Fatalf("got %v", got)
	}
}
