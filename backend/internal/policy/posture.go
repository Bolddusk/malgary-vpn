package policy

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/netbirdio/netbird/shared/management/client/rest"
	nbapi "github.com/netbirdio/netbird/shared/management/http/api"
)

// Posture is one tenant posture rule (FR-018): minimum OS versions and NetBird app version,
// for the whole tenant (Group "") or one group.
type Posture struct {
	Group            string            // "" = all users
	MinClientVersion string            // e.g. 0.82.0
	MinOS            map[string]string // windows, macos, linux (kernel), ios, android
}

// ApplyAccess makes users reach the gateways only through Malgary's access policies, with the
// tenant's posture checks attached. Peers that fail a check lose access to the gateways, so
// they can't reach any protected destination until fixed (FR-018). It also removes NetBird's
// default all-to-all policy, so users' devices can't reach each other. With enabled false the
// policies exist but are switched off, so nobody reaches the gateways (interim isolation gate,
// feature 002 FR-009).
func ApplyAccess(ctx context.Context, c *rest.Client, rules []Posture, gatewaysGroup, allUsersGroup string, enabled bool) error {
	checks, err := c.PostureChecks.List(ctx)
	if err != nil {
		return fmt.Errorf("list posture checks: %w", err)
	}
	pols, err := c.Policies.List(ctx)
	if err != nil {
		return fmt.Errorf("list policies: %w", err)
	}
	gw, err := ensureGroupID(ctx, c, gatewaysGroup)
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		rules = []Posture{{}} // access without posture checks
	}
	wantPolicies := map[string]bool{}
	for _, r := range rules {
		suffix := "all"
		srcName := allUsersGroup
		if r.Group != "" {
			suffix, srcName = r.Group, r.Group
		}
		var checkIDs []string
		if r.MinClientVersion != "" || len(r.MinOS) > 0 {
			id, err := upsertCheck(ctx, c, checks, "malgary:posture:"+suffix, r)
			if err != nil {
				return err
			}
			checkIDs = []string{id}
		}
		src, err := ensureGroupID(ctx, c, srcName)
		if err != nil {
			return err
		}
		name := "malgary:access:" + suffix
		wantPolicies[name] = true
		req := nbapi.PolicyUpdate{
			Name: name, Enabled: enabled, SourcePostureChecks: &checkIDs,
			Rules: []nbapi.PolicyRuleUpdate{{
				Name: name, Enabled: enabled, Action: nbapi.PolicyRuleUpdateActionAccept, Bidirectional: false,
				Protocol: nbapi.PolicyRuleUpdateProtocolAll, Sources: &[]string{src}, Destinations: &[]string{gw},
			}},
		}
		if err := upsertPolicy(ctx, c, pols, req); err != nil {
			return err
		}
	}
	for _, p := range pols {
		stale := strings.HasPrefix(p.Name, "malgary:access:") && !wantPolicies[p.Name]
		if (p.Name == "Default" || stale) && p.Id != nil {
			if err := c.Policies.Delete(ctx, *p.Id); err != nil {
				return fmt.Errorf("delete policy %s: %w", p.Name, err)
			}
		}
	}
	return nil
}

func upsertCheck(ctx context.Context, c *rest.Client, existing []nbapi.PostureCheck, name string, r Posture) (string, error) {
	checks := nbapi.Checks{}
	if r.MinClientVersion != "" {
		checks.NbVersionCheck = &nbapi.NBVersionCheck{MinVersion: r.MinClientVersion}
	}
	if len(r.MinOS) > 0 {
		os := nbapi.OSVersionCheck{}
		for k, v := range r.MinOS {
			switch k {
			case "windows":
				os.Windows = &nbapi.MinKernelVersionCheck{MinKernelVersion: v}
			case "linux":
				os.Linux = &nbapi.MinKernelVersionCheck{MinKernelVersion: v}
			case "macos", "darwin":
				os.Darwin = &nbapi.MinVersionCheck{MinVersion: v}
			case "ios":
				os.Ios = &nbapi.MinVersionCheck{MinVersion: v}
			case "android":
				os.Android = &nbapi.MinVersionCheck{MinVersion: v}
			default:
				return "", fmt.Errorf("unknown OS %q in posture rule", k)
			}
		}
		checks.OsVersionCheck = &os
	}
	req := nbapi.PostureCheckUpdate{Name: name, Description: "Managed by Malgary", Checks: &checks}
	for _, e := range existing {
		if e.Name == name {
			if _, err := c.PostureChecks.Update(ctx, e.Id, req); err != nil {
				return "", fmt.Errorf("update posture check: %w", err)
			}
			return e.Id, nil
		}
	}
	pc, err := c.PostureChecks.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("create posture check: %w", err)
	}
	return pc.Id, nil
}

func upsertPolicy(ctx context.Context, c *rest.Client, existing []nbapi.Policy, req nbapi.PolicyUpdate) error {
	for _, p := range existing {
		if p.Name == req.Name && p.Id != nil {
			if slices.Equal(p.SourcePostureChecks, *req.SourcePostureChecks) && p.Enabled == req.Enabled {
				return nil
			}
			_, err := c.Policies.Update(ctx, *p.Id, nbapi.PolicyCreate(req))
			return err
		}
	}
	_, err := c.Policies.Create(ctx, req)
	return err
}
