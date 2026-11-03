package tenancy

import "testing"

func TestWGInterfaceName(t *testing.T) {
	for _, slug := range []string{"onedge", "a-very-long-tenant-slug-name", "abc"} {
		n := wgInterfaceName(slug)
		if len(n) > 15 {
			t.Errorf("%s -> %s is %d chars", slug, n, len(n))
		}
	}
	if wgInterfaceName("abcdefgh-one") == wgInterfaceName("abcdefgh-two") {
		t.Error("tenants sharing an 8-char prefix must still get different interfaces")
	}
}

func TestLowestFree(t *testing.T) {
	p, err := lowestFree([]int32{51821, 51822, 51824}, 51821, 52820)
	if err != nil || p != 51823 {
		t.Fatalf("got %d %v", p, err)
	}
	if _, err := lowestFree([]int32{1, 2}, 1, 2); err == nil {
		t.Fatal("expected exhaustion error")
	}
}

func TestStackSpecHashIsStable(t *testing.T) {
	s := StackSpec{Tenant: "onedge", NetbirdVersion: "0.82.0", HTTPPort: 20000}
	a, _, _ := s.Seal()
	b, _, _ := s.Seal()
	if a.Hash == "" || a.Hash != b.Hash {
		t.Fatalf("unstable hash %q vs %q", a.Hash, b.Hash)
	}
	s.HTTPPort = 20001
	c, _, _ := s.Seal()
	if c.Hash == a.Hash {
		t.Fatal("hash must change when the spec changes")
	}
}
