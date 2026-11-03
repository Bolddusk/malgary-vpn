package secrets

import "testing"

func TestResolverRoundTripAndTraversal(t *testing.T) {
	r := Resolver{Dir: t.TempDir()}
	ref := Ref("tenants/onedge/db")
	if err := r.Put(ref, []byte("s3cret")); err != nil {
		t.Fatal(err)
	}
	got, err := r.Resolve(ref)
	if err != nil || string(got) != "s3cret" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, bad := range []string{"secret://../etc/passwd", "secret:///etc/passwd", "plain-value"} {
		if _, err := r.Resolve(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
