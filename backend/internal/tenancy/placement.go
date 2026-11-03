package tenancy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store/db"
)

// Port ranges (data-model.md RoutingPeer, migration 0001 CHECKs).
const (
	httpPortMin = 20000
	httpPortMax = 29999
	wgPortMin   = 51821
	wgPortMax   = 52820
)

// pickCell returns the named cell, or the active cell with the most free capacity.
func pickCell(ctx context.Context, q *db.Queries, code string) (db.Cell, error) {
	if code != "" {
		c, err := q.GetCellByCode(ctx, code)
		if err != nil {
			return db.Cell{}, httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "unknown cell "+code)
		}
		return c, nil
	}
	cells, err := q.ListActiveCells(ctx)
	if err != nil {
		return db.Cell{}, err
	}
	var best db.Cell
	bestFree := int64(-1)
	for _, c := range cells {
		used, err := q.CellUserCount(ctx, c.ID)
		if err != nil {
			return db.Cell{}, err
		}
		if free := int64(c.MaxUsers) - used; free > bestFree {
			best, bestFree = c, free
		}
	}
	if bestFree <= 0 {
		return db.Cell{}, errors.New("no active cell with free capacity: open a new cell")
	}
	return best, nil
}

// pickControlHost returns the control host in the cell with the most free capacity.
func pickControlHost(ctx context.Context, q *db.Queries, cellID uuid.UUID) (db.Host, error) {
	hosts, err := q.ListHostsByCellRole(ctx, db.ListHostsByCellRoleParams{CellID: cellID, Role: db.HostRoleControl})
	if err != nil {
		return db.Host{}, err
	}
	var best db.Host
	bestFree := int64(-1)
	for _, h := range hosts {
		used, err := q.HostUserCount(ctx, h.ID)
		if err != nil {
			return db.Host{}, err
		}
		if free := int64(h.MaxUsers) - used; free > bestFree {
			best, bestFree = h, free
		}
	}
	if bestFree <= 0 {
		return db.Host{}, errors.New("no control host with free capacity: add a control host")
	}
	return best, nil
}

// allocHTTPPort returns the lowest free local port for a tenant stack on the host.
func allocHTTPPort(ctx context.Context, q *db.Queries, hostID uuid.UUID) (int32, error) {
	used, err := q.UsedHTTPPorts(ctx, hostID)
	if err != nil {
		return 0, err
	}
	return lowestFree(used, httpPortMin, httpPortMax)
}

// allocWGPort returns the lowest port free on every host given (both members of a pair use the
// same port, so failover changes nothing for clients).
func allocWGPort(ctx context.Context, q *db.Queries, hostIDs ...uuid.UUID) (int32, error) {
	var used []int32
	for _, h := range hostIDs {
		u, err := q.UsedWGPortsOnHost(ctx, h)
		if err != nil {
			return 0, err
		}
		used = append(used, u...)
	}
	return lowestFree(used, wgPortMin, wgPortMax)
}

func lowestFree(used []int32, lo, hi int32) (int32, error) {
	taken := make(map[int32]bool, len(used))
	for _, p := range used {
		taken[p] = true
	}
	for p := lo; p <= hi; p++ {
		if !taken[p] {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port in %d-%d", lo, hi)
}

// wgInterfaceName is unique per tenant and at most 15 characters (Linux IFNAMSIZ).
// Format: wt-<first 8 of slug>-<3 hex of sha256(slug)>.
func wgInterfaceName(slug string) string {
	short := slug
	if len(short) > 8 {
		short = short[:8]
	}
	sum := sha256.Sum256([]byte(slug))
	return "wt-" + short + "-" + hex.EncodeToString(sum[:])[:3]
}
