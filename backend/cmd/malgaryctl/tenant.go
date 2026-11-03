package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/google/uuid"
	"sigs.k8s.io/yaml"

	"malgary/backend/internal/client"
)

func tenantCmd(ctx context.Context, c *apiClient, args []string) error {
	if len(args) == 0 {
		return errors.New("tenant: expected apply, get or list")
	}
	switch args[0] {
	case "apply":
		fs := flag.NewFlagSet("tenant apply", flag.ContinueOnError)
		file := fs.String("f", "", "tenant definition YAML")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		raw, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		js, err := yaml.YAMLToJSON(raw)
		if err != nil {
			return err
		}
		var def client.TenantDefinition
		dec := json.NewDecoder(bytesReader(js))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&def); err != nil {
			return fmt.Errorf("%s: %w", *file, err)
		}
		// No Idempotency-Key: apply is idempotent by diff on the server (T076), and a replayed
		// response would show stale status.
		res, err := c.CreateTenantWithResponse(ctx, &client.CreateTenantParams{}, def)
		if err != nil {
			return err
		}
		if res.JSON201 == nil && res.JSON200 == nil {
			return problemError(res.StatusCode(), res.Body)
		}
		return printJSON(res.Body)
	case "get":
		if len(args) < 2 {
			return errors.New("tenant get <id>")
		}
		id, err := uuid.Parse(args[1])
		if err != nil {
			return errors.New("tenant get expects a tenant ID")
		}
		res, err := c.GetTenantWithResponse(ctx, id)
		if err != nil {
			return err
		}
		if res.JSON200 == nil {
			return problemError(res.StatusCode(), res.Body)
		}
		return printJSON(res.Body)
	case "list":
		res, err := c.ListTenantsWithResponse(ctx, &client.ListTenantsParams{})
		if err != nil {
			return err
		}
		if res.JSON200 == nil {
			return problemError(res.StatusCode(), res.Body)
		}
		for _, t := range res.JSON200.Items {
			fmt.Printf("%-20s %-12s %-10s %s\n", t.Slug, t.Status, t.StackStatus, t.ManagementUrl)
		}
		return nil
	}
	return fmt.Errorf("tenant: unknown subcommand %q", args[0])
}
