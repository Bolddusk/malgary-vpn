// Package policy turns Malgary routing and posture rules into NetBird configuration.
package policy

import (
	"fmt"
	"net/netip"
	"slices"
)

// Complement returns the smallest set of IPv4 prefixes that covers all of `within` except the
// excluded prefixes. It implements research R12's fallback: "full tunnel minus call ranges" as
// ordinary routes, because a NetBird exit node has no exclusion list.
func Complement(within netip.Prefix, exclude []netip.Prefix) ([]netip.Prefix, error) {
	if !within.Addr().Is4() {
		return nil, fmt.Errorf("only IPv4 is supported, got %s", within)
	}
	var ex []netip.Prefix
	for _, e := range exclude {
		e = e.Masked()
		if !e.Addr().Is4() {
			return nil, fmt.Errorf("only IPv4 exclusions are supported, got %s", e)
		}
		if within.Overlaps(e) {
			ex = append(ex, e)
		}
	}
	out := split(within.Masked(), ex)
	slices.SortFunc(out, func(a, b netip.Prefix) int { return a.Addr().Compare(b.Addr()) })
	return out, nil
}

func split(p netip.Prefix, ex []netip.Prefix) []netip.Prefix {
	overlapping := ex[:0:0]
	for _, e := range ex {
		if e.Bits() <= p.Bits() && e.Contains(p.Addr()) {
			return nil // p is entirely excluded
		}
		if p.Overlaps(e) {
			overlapping = append(overlapping, e)
		}
	}
	if len(overlapping) == 0 {
		return []netip.Prefix{p}
	}
	lo, hi := halves(p)
	return append(split(lo, overlapping), split(hi, overlapping)...)
}

func halves(p netip.Prefix) (netip.Prefix, netip.Prefix) {
	bits := p.Bits() + 1
	lo := netip.PrefixFrom(p.Addr(), bits)
	a := p.Addr().As4()
	idx := (bits - 1) / 8
	a[idx] |= 1 << (7 - uint((bits-1)%8))
	hi := netip.PrefixFrom(netip.AddrFrom4(a), bits)
	return lo, hi
}
