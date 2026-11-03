package policy

import (
	"testing"

	"github.com/google/uuid"
)

func TestResolvePrecedence(t *testing.T) {
	u, other := uuid.New(), uuid.New()
	gA, gB, gC := uuid.New(), uuid.New(), uuid.New()
	rules := []Rule{
		{Scope: "tenant", Mode: FullTunnel},
		{Scope: "group", ScopeRef: gA, Mode: Destinations, Destinations: []string{"10.1.0.0/16"}},
		{Scope: "group", ScopeRef: gB, Mode: Destinations, Destinations: []string{"10.2.0.0/16", "10.1.0.0/16"}},
		{Scope: "group", ScopeRef: gC, Mode: FullTunnel},
		{Scope: "user", ScopeRef: u, Mode: Destinations, Destinations: []string{"192.0.2.0/24"}},
	}

	if e := Resolve(u, []uuid.UUID{gA, gC}, rules); e.Source != "user" || e.Destinations[0] != "192.0.2.0/24" {
		t.Fatalf("user rule must win: %+v", e)
	}
	if e := Resolve(other, []uuid.UUID{gA}, rules); e.Source != "group" || e.Mode != Destinations || len(e.Destinations) != 1 || e.Conflict {
		t.Fatalf("single group rule: %+v", e)
	}
	e := Resolve(other, []uuid.UUID{gA, gB}, rules)
	if e.Mode != Destinations || len(e.Destinations) != 2 || !e.Conflict {
		t.Fatalf("two destination groups: union and conflict flagged: %+v", e)
	}
	if e := Resolve(other, []uuid.UUID{gA, gC}, rules); e.Mode != FullTunnel || !e.Conflict {
		t.Fatalf("full tunnel wins over destinations: %+v", e)
	}
	if e := Resolve(other, nil, rules); e.Source != "tenant" || e.Group() != FullTunnelGroup {
		t.Fatalf("tenant default: %+v", e)
	}
	if e := Resolve(other, nil, nil); e.Source != "none" || e.Group() != "" {
		t.Fatalf("no rules: %+v", e)
	}
	if Validate(Rule{Mode: Destinations}) {
		t.Fatal("empty destinations must be invalid")
	}
	if GroupForDestinations([]string{"b", "a"}) != GroupForDestinations([]string{"a", "b"}) {
		t.Fatal("routing group name must not depend on order")
	}
}
