package tenancy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/netbirdio/netbird/shared/management/client/rest"
	nbapi "github.com/netbirdio/netbird/shared/management/http/api"
)

// Isolation test plumbing (feature 002, FR-009). The probe is a throwaway NetBird client in
// the group "isolation-test". It gets every IPv4 destination through the tenant's gateway —
// the worst case any routing rule could produce — and its own access policy, so it can run
// while users are still locked out by the isolation gate.
const (
	groupIsolation   = "isolation-test"
	isolationTag     = "isolation-test" // description of the temporary route and policy
	isolationNetwork = "0.0.0.0/0"
)

// ProbeTarget is what the probe client needs to join the tenant's stack.
type ProbeTarget struct {
	ManagementURL string
	SetupKey      string // one-off, ephemeral; a credential, never logged
}

// PrepareIsolationProbe creates the temporary group, route, policy and setup key. Call
// CleanupIsolationProbe afterwards, whatever the outcome.
func (s *Service) PrepareIsolationProbe(ctx context.Context, tenantID uuid.UUID) (ProbeTarget, error) {
	c, mgmtURL, err := s.clientFor(ctx, tenantID)
	if err != nil {
		return ProbeTarget{}, err
	}
	if err := s.cleanupIsolation(ctx, c); err != nil { // leftovers of an aborted run
		return ProbeTarget{}, err
	}
	gid, err := ensureGroup(ctx, c, groupIsolation)
	if err != nil {
		return ProbeTarget{}, err
	}
	gw, err := ensureGroup(ctx, c, groupGateways)
	if err != nil {
		return ProbeTarget{}, err
	}
	peers, err := s.Store.ListRoutingPeersByTenant(ctx, tenantID)
	if err != nil {
		return ProbeTarget{}, err
	}
	if len(peers) == 0 {
		return ProbeTarget{}, errors.New("the tenant has no routing peer yet")
	}
	nbPeers, err := c.Peers.List(ctx)
	if err != nil {
		return ProbeTarget{}, fmt.Errorf("list peers: %w", err)
	}
	for _, rp := range peers {
		var peerID string
		for _, p := range nbPeers {
			if p.Hostname == rp.HostName {
				peerID = p.Id
			}
		}
		if peerID == "" {
			return ProbeTarget{}, fmt.Errorf("routing peer %s has not joined the stack yet", rp.HostName)
		}
		network := isolationNetwork
		if _, err := c.Routes.Create(ctx, nbapi.RouteRequest{
			Description: isolationTag, NetworkId: "mg-isolation-test", Network: &network, Peer: &peerID,
			Metric: int(rp.Metric), Groups: []string{gid}, Masquerade: true, Enabled: true,
		}); err != nil {
			return ProbeTarget{}, fmt.Errorf("isolation route: %w", err)
		}
	}
	checks := []string{}
	if _, err := c.Policies.Create(ctx, nbapi.PolicyUpdate{
		Name: isolationTag, Description: ptr(isolationTag), Enabled: true, SourcePostureChecks: &checks,
		Rules: []nbapi.PolicyRuleUpdate{{
			Name: isolationTag, Enabled: true, Action: nbapi.PolicyRuleUpdateActionAccept,
			Protocol: nbapi.PolicyRuleUpdateProtocolAll, Sources: &[]string{gid}, Destinations: &[]string{gw},
		}},
	}); err != nil {
		return ProbeTarget{}, fmt.Errorf("isolation policy: %w", err)
	}
	eph := true
	key, err := c.SetupKeys.Create(ctx, nbapi.CreateSetupKeyRequest{
		Name: isolationTag, Type: "one-off", AutoGroups: []string{gid}, Ephemeral: &eph, ExpiresIn: 3600, UsageLimit: 1,
	})
	if err != nil {
		return ProbeTarget{}, fmt.Errorf("isolation setup key: %w", err)
	}
	return ProbeTarget{ManagementURL: mgmtURL, SetupKey: key.Key}, nil
}

// CleanupIsolationProbe removes the temporary route, policy and probe peers.
func (s *Service) CleanupIsolationProbe(ctx context.Context, tenantID uuid.UUID) error {
	c, _, err := s.clientFor(ctx, tenantID)
	if err != nil {
		return err
	}
	return s.cleanupIsolation(ctx, c)
}

func (s *Service) cleanupIsolation(ctx context.Context, c *rest.Client) error {
	var errs []error
	if routes, err := c.Routes.List(ctx); err == nil {
		for _, r := range routes {
			if r.Description == isolationTag {
				errs = append(errs, c.Routes.Delete(ctx, r.Id))
			}
		}
	} else {
		errs = append(errs, err)
	}
	if pols, err := c.Policies.List(ctx); err == nil {
		for _, p := range pols {
			if p.Name == isolationTag && p.Id != nil {
				errs = append(errs, c.Policies.Delete(ctx, *p.Id))
			}
		}
	} else {
		errs = append(errs, err)
	}
	if peers, err := c.Peers.List(ctx); err == nil {
		for _, p := range peers {
			for _, g := range p.Groups {
				if g.Name == groupIsolation {
					errs = append(errs, c.Peers.Delete(ctx, p.Id))
					break
				}
			}
		}
	} else {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// clientFor returns a NetBird API client for the tenant's stack and its management URL.
func (s *Service) clientFor(ctx context.Context, tenantID uuid.UUID) (*rest.Client, string, error) {
	t, err := s.Store.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, "", err
	}
	stack, err := s.Store.GetTenantStack(ctx, tenantID)
	if err != nil {
		return nil, "", err
	}
	pat, err := s.Secrets.Resolve(refNetbirdPAT(t.Slug))
	if err != nil {
		return nil, "", err
	}
	c, err := s.NetBird.For(ctx, stack.ManagementUrl, strings.TrimSpace(string(pat)))
	return c, stack.ManagementUrl, err
}

func ptr[T any](v T) *T { return &v }
