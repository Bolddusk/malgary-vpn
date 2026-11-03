package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"malgary/backend/internal/client"
)

// isolationCmd: malgaryctl isolation-test <tenant-slug-or-id> [--history]
// Runs the interim host's isolation test (feature 002, FR-009): a throwaway VPN client tries to
// reach private destinations through the tenant's gateway. Users can connect only after a pass.
func isolationCmd(ctx context.Context, c *apiClient, args []string) error {
	if len(args) < 1 {
		return errors.New("usage: malgaryctl isolation-test <tenant> [--history]")
	}
	id, err := resolveTenant(ctx, c, args[0])
	if err != nil {
		return err
	}
	if len(args) > 1 && args[1] == "--history" {
		res, err := c.ListIsolationTestsWithResponse(ctx, id)
		if err != nil {
			return err
		}
		if res.JSON200 == nil {
			return problemError(res.StatusCode(), res.Body)
		}
		for _, r := range *res.JSON200 {
			fmt.Printf("%s  %-6s  %s  by %s\n", r.RanAt.Format("2006-01-02 15:04"), verdict(r.Passed), deref(r.StackHash), r.RanBy)
		}
		return nil
	}
	fmt.Println("running the isolation test (up to five minutes)...")
	res, err := c.RunIsolationTestWithResponse(ctx, id)
	if err != nil {
		return err
	}
	if res.JSON201 == nil {
		return problemError(res.StatusCode(), res.Body)
	}
	r := res.JSON201
	for _, p := range r.Results {
		mark := "ok  "
		if p.Got != client.IsolationProbeGot(p.Expected) {
			mark = "FAIL"
		}
		fmt.Printf("  %s  %-28s expected %-9s got %s\n", mark, p.Target, p.Expected, p.Got)
	}
	if r.EgressIp != nil {
		fmt.Println("  egress IP:", *r.EgressIp)
	}
	fmt.Printf("result: %s for stack %s\n", verdict(r.Passed), deref(r.StackHash))
	if !r.Passed {
		return errors.New("isolation test failed: users stay blocked; fix the reachable destinations above")
	}
	fmt.Println("users of this tenant can now connect")
	return nil
}

func verdict(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// resolveTenant accepts a tenant ID or slug.
func resolveTenant(ctx context.Context, c *apiClient, s string) (uuid.UUID, error) {
	if id, err := uuid.Parse(s); err == nil {
		return id, nil
	}
	res, err := c.ListTenantsWithResponse(ctx, &client.ListTenantsParams{})
	if err != nil {
		return uuid.Nil, err
	}
	if res.JSON200 == nil {
		return uuid.Nil, problemError(res.StatusCode(), res.Body)
	}
	for _, t := range res.JSON200.Items {
		if t.Slug == s {
			return t.Id, nil
		}
	}
	return uuid.Nil, fmt.Errorf("no tenant %q", s)
}
