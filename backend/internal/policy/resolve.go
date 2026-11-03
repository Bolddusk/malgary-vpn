package policy

import (
	"slices"

	"github.com/google/uuid"
)

// Mode of a routing rule.
type Mode string

// Routing modes (data-model.md RoutingRule).
const (
	FullTunnel   Mode = "full_tunnel"
	Destinations Mode = "destinations"
)

// Rule is one routing rule at tenant, group or user scope.
type Rule struct {
	Scope        string // tenant | group | user
	ScopeRef     uuid.UUID
	Mode         Mode
	Destinations []string
}

// Effective is a user's resolved routing.
type Effective struct {
	Mode         Mode
	Destinations []string // sorted, de-duplicated
	Source       string   // user | group | tenant | none
	Conflict     bool     // several group rules disagreed; shown to the admin
}

// Group returns the routing group name for the effective rule ("" when nothing routes).
func (e Effective) Group() string {
	switch {
	case e.Source == "none":
		return ""
	case e.Mode == FullTunnel:
		return FullTunnelGroup
	default:
		return GroupForDestinations(e.Destinations)
	}
}

// Resolve applies FR-005's precedence: the user's own rule; otherwise the union of their
// groups' rules, where full tunnel wins and disagreement is flagged; otherwise the tenant rule.
func Resolve(user uuid.UUID, userGroups []uuid.UUID, rules []Rule) Effective {
	var tenant *Rule
	var groupRules []Rule
	for i := range rules {
		r := rules[i]
		switch r.Scope {
		case "user":
			if r.ScopeRef == user {
				return Effective{Mode: r.Mode, Destinations: norm(r.Destinations), Source: "user"}
			}
		case "group":
			if slices.Contains(userGroups, r.ScopeRef) {
				groupRules = append(groupRules, r)
			}
		case "tenant":
			tenant = &rules[i]
		}
	}
	if len(groupRules) > 0 {
		e := Effective{Mode: Destinations, Source: "group"}
		var union []string
		for _, r := range groupRules {
			if r.Mode == FullTunnel {
				e.Mode = FullTunnel
			}
			union = append(union, r.Destinations...)
		}
		for _, r := range groupRules[1:] {
			if r.Mode != groupRules[0].Mode || !slices.Equal(norm(r.Destinations), norm(groupRules[0].Destinations)) {
				e.Conflict = true
			}
		}
		if e.Mode == Destinations {
			e.Destinations = norm(union)
		}
		return e
	}
	if tenant != nil {
		return Effective{Mode: tenant.Mode, Destinations: norm(tenant.Destinations), Source: "tenant"}
	}
	return Effective{Source: "none"}
}

// Validate rejects a destinations rule with no destinations (FR-005, edge case).
func Validate(r Rule) bool {
	return r.Mode == FullTunnel || len(r.Destinations) > 0
}

func norm(d []string) []string {
	out := slices.Clone(d)
	slices.Sort(out)
	return slices.Compact(out)
}
