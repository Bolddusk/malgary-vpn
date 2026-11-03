package tenancy

import (
	"context"
	"fmt"
	"net/netip"
	"os"

	"github.com/google/uuid"
	"sigs.k8s.io/yaml"

	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
)

// CellInventory mirrors what OpenTofu built for one cell (deploy/cells/<code>.yaml, generated
// from `tofu output`). The backend needs it to place tenants; it never creates servers itself.
type CellInventory struct {
	Code     string `json:"code"`
	Region   string `json:"region"`
	MaxUsers int32  `json:"maxUsers"`
	// Interim marks a single shared host (feature 002): gateway pairs have no standby.
	Interim bool `json:"interim,omitempty"`
	Hosts   []struct {
		Name     string `json:"name"`
		Role     string `json:"role"`
		Size     string `json:"size"`
		MaxUsers int32  `json:"maxUsers"`
	} `json:"hosts"`
	GatewayPairs []struct {
		Name     string `json:"name"`
		EgressIP string `json:"egressIp"`
		Primary  string `json:"primary"`
		Standby  string `json:"standby"`
	} `json:"gatewayPairs"`
}

// SyncInventory upserts a cell, its hosts and gateway pairs from a YAML file.
func SyncInventory(ctx context.Context, s *store.Store, path string) error {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-provided inventory path
	if err != nil {
		return err
	}
	var inv CellInventory
	if err := yaml.UnmarshalStrict(raw, &inv); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return s.WithTx(ctx, func(q *db.Queries, _ pgxTx) error {
		cell, err := q.UpsertCell(ctx, db.UpsertCellParams{ID: uuid.Must(uuid.NewV7()), Code: inv.Code, Region: inv.Region, MaxUsers: inv.MaxUsers, Interim: inv.Interim})
		if err != nil {
			return err
		}
		ids := map[string]uuid.UUID{}
		for _, h := range inv.Hosts {
			mu := h.MaxUsers
			if mu == 0 {
				mu = 2500
			}
			row, err := q.UpsertHost(ctx, db.UpsertHostParams{
				ID: uuid.Must(uuid.NewV7()), CellID: cell.ID, Role: db.HostRole(h.Role), Name: h.Name, SizeSlug: h.Size, MaxUsers: mu,
			})
			if err != nil {
				return fmt.Errorf("host %s: %w", h.Name, err)
			}
			ids[h.Name] = row.ID
		}
		for _, p := range inv.GatewayPairs {
			ip, err := netip.ParseAddr(p.EgressIP)
			if err != nil {
				return fmt.Errorf("pair %s egress IP: %w", p.Name, err)
			}
			pri, ok := ids[p.Primary]
			if !ok {
				return fmt.Errorf("pair %s: unknown primary host %q", p.Name, p.Primary)
			}
			var sb *uuid.UUID
			switch {
			case p.Standby != "":
				id, ok := ids[p.Standby]
				if !ok {
					return fmt.Errorf("pair %s: unknown standby host %q", p.Name, p.Standby)
				}
				sb = &id
			case !inv.Interim:
				// Principle IV: only the interim host may run without a standby gateway.
				return fmt.Errorf("pair %s: a standby host is required outside an interim cell", p.Name)
			}
			if _, err := q.UpsertGatewayPair(ctx, db.UpsertGatewayPairParams{
				ID: uuid.Must(uuid.NewV7()), CellID: cell.ID, Name: p.Name, EgressIp: ip, PrimaryHostID: pri, StandbyHostID: sb,
			}); err != nil {
				return fmt.Errorf("pair %s: %w", p.Name, err)
			}
		}
		return nil
	})
}
