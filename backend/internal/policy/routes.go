package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/netbirdio/netbird/shared/management/client/rest"
	nbapi "github.com/netbirdio/netbird/shared/management/http/api"
)

// RoutingGroup is one distinct routing outcome in a tenant (data-model.md RoutingRule): every
// user is in exactly one, so NetBird's additive routes can't mix (FR-005).
type RoutingGroup struct {
	Name         string         // route:full or route:dest:<hash>
	FullTunnel   bool           // all public IPv4 except call exclusions
	Destinations []netip.Prefix // used when !FullTunnel
}

// FullTunnelGroup is the routing group for full tunnel.
const FullTunnelGroup = "route:full"

// GroupForDestinations names the routing group for a destination list (order-insensitive).
func GroupForDestinations(dests []string) string {
	d := slices.Clone(dests)
	slices.Sort(d)
	sum := sha256.Sum256([]byte(strings.Join(d, ",")))
	return "route:dest:" + hex.EncodeToString(sum[:])[:10]
}

// GatewayPeer is a tenant routing peer as NetBird sees it.
type GatewayPeer struct {
	Hostname string // NB_HOSTNAME set by the routing-peer role, e.g. gw-nyc-1a.c01
	Metric   int    // 100 primary, 200 standby: lower wins within the HA group
}

const managedPrefix = "malgary:"

// ApplyRoutes makes the stack's routes exactly match groups, with every prefix served by every
// gateway peer as one HA route group (research R2). Routes not created by Malgary are left alone.
func ApplyRoutes(ctx context.Context, c *rest.Client, groups []RoutingGroup, exclusions []netip.Prefix, gateways []GatewayPeer) error {
	peers, err := c.Peers.List(ctx)
	if err != nil {
		return fmt.Errorf("list peers: %w", err)
	}
	peerID := map[string]string{}
	for _, p := range peers {
		peerID[p.Hostname] = p.Id
	}
	type key struct{ networkID, peer string }
	desired := map[key]nbapi.RouteRequest{}
	for _, g := range groups {
		gid, err := ensureGroupID(ctx, c, g.Name)
		if err != nil {
			return err
		}
		prefixes := g.Destinations
		if g.FullTunnel {
			if prefixes, err = Complement(netip.MustParsePrefix("0.0.0.0/0"), exclusions); err != nil {
				return err
			}
		}
		for _, pfx := range prefixes {
			netID := networkID(g.Name, pfx)
			for _, gw := range gateways {
				id, ok := peerID[gw.Hostname]
				if !ok {
					return fmt.Errorf("routing peer %s has not joined the stack yet", gw.Hostname)
				}
				network := pfx.String()
				desired[key{netID, id}] = nbapi.RouteRequest{
					Description: managedPrefix + g.Name, NetworkId: netID, Network: &network,
					Peer: &id, Metric: gw.Metric, Groups: []string{gid}, Masquerade: true, Enabled: true,
				}
			}
		}
	}

	current, err := c.Routes.List(ctx)
	if err != nil {
		return fmt.Errorf("list routes: %w", err)
	}
	for _, r := range current {
		if !strings.HasPrefix(r.Description, managedPrefix) || r.Peer == nil {
			continue
		}
		k := key{r.NetworkId, *r.Peer}
		want, ok := desired[k]
		switch {
		case !ok:
			if err := c.Routes.Delete(ctx, r.Id); err != nil {
				return fmt.Errorf("delete route %s: %w", r.Id, err)
			}
		case sameRoute(r, want):
			delete(desired, k)
		default:
			if _, err := c.Routes.Update(ctx, r.Id, want); err != nil {
				return fmt.Errorf("update route %s: %w", r.Id, err)
			}
			delete(desired, k)
		}
	}
	for _, req := range desired {
		if _, err := c.Routes.Create(ctx, req); err != nil {
			return fmt.Errorf("create route %s: %w", req.NetworkId, err)
		}
	}
	return nil
}

func sameRoute(r nbapi.Route, w nbapi.RouteRequest) bool {
	return r.Description == w.Description && r.Metric == w.Metric && r.Enabled && r.Masquerade &&
		r.Network != nil && w.Network != nil && *r.Network == *w.Network && slices.Equal(r.Groups, w.Groups)
}

// networkID is NetBird's HA key (max 40 chars): the same prefix in the same routing group.
func networkID(group string, p netip.Prefix) string {
	sum := sha256.Sum256([]byte(group + "|" + p.String()))
	return "mg-" + hex.EncodeToString(sum[:])[:16]
}

func ensureGroupID(ctx context.Context, c *rest.Client, name string) (string, error) {
	if g, err := c.Groups.GetByName(ctx, name); err == nil && g != nil {
		return g.Id, nil
	}
	g, err := c.Groups.Create(ctx, nbapi.GroupRequest{Name: name})
	if err != nil {
		return "", fmt.Errorf("group %s: %w", name, err)
	}
	return g.Id, nil
}
