// Package usage records who is connected, when each user was last seen, and how much traffic
// went through the gateways (research R9: NetBird OSS gives connected/last_seen; byte counts
// come from the routing peers' WireGuard counters, exported per peer IP on each gateway).
package usage

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/netbird"
	"malgary/backend/internal/secrets"
	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
)

// Sampler collects usage every Interval.
type Sampler struct {
	Store      *store.Store
	Secrets    secrets.Resolver
	NetBird    *netbird.Factory
	BaseDomain string // gateway exporters are reached at <host>.<BaseDomain>:9586 over the VPC
	HTTP       *http.Client
	Interval   time.Duration
}

// Run samples until ctx ends, and rolls samples older than 90 days into daily totals.
func (s *Sampler) Run(ctx context.Context) {
	if s.Interval == 0 {
		s.Interval = 5 * time.Minute
	}
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	for {
		s.SampleAll(ctx)
		cutoff := time.Now().AddDate(0, 0, -90)
		if err := s.Store.RollupUsage(ctx, cutoff); err == nil {
			_ = s.Store.PruneUsage(ctx, cutoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// SampleAll samples every active tenant; one tenant's failure doesn't stop the others.
func (s *Sampler) SampleAll(ctx context.Context) {
	tenants, err := s.Store.ListActiveTenantsWithStacks(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "usage: list tenants", "err", err)
		return
	}
	counters := s.scrapeGateways(ctx)
	for _, t := range tenants {
		if err := s.sampleTenant(ctx, t.ID, t.Slug, t.ManagementUrl, counters); err != nil {
			slog.WarnContext(ctx, "usage sample failed", "tenant", t.Slug, "err", err)
		}
	}
}

type wgCount struct{ rx, tx int64 }

func (s *Sampler) sampleTenant(ctx context.Context, tenantID uuid.UUID, slug, mgmtURL string, counters map[string]wgCount) error {
	pat, err := s.Secrets.Resolve(secrets.Ref("tenants/" + slug + "/netbird-pat"))
	if err != nil {
		return err
	}
	c, err := s.NetBird.For(ctx, mgmtURL, strings.TrimSpace(string(pat)))
	if err != nil {
		return err
	}
	nbUsers, err := c.Users.List(ctx)
	if err != nil {
		return err
	}
	emailByNBUser := map[string]string{}
	for _, u := range nbUsers {
		emailByNBUser[u.Id] = strings.ToLower(u.Email)
	}
	peers, err := c.Peers.List(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Minute)
	for _, p := range peers {
		email := emailByNBUser[p.UserId]
		if email == "" {
			continue // routing peers and service users
		}
		var uid *uuid.UUID
		if u, err := s.Store.GetTenantUserByEmail(ctx, db.GetTenantUserByEmailParams{TenantID: tenantID, Email: email}); err == nil {
			uid = &u.ID
		}
		ls := p.LastSeen
		row := db.InsertUsageSampleParams{TenantID: tenantID, TenantUserID: uid, NetbirdPeerID: p.Id, SampledAt: now, Connected: p.Connected, LastSeen: &ls}
		if wc, ok := counters[slug+"|"+p.Ip]; ok {
			row.RxBytes, row.TxBytes = &wc.rx, &wc.tx
		}
		if err := s.Store.InsertUsageSample(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

// scrapeGateways reads malgary_wg_peer_{rx,tx}_bytes{tenant,peer_ip} from every gateway's
// exporter and sums across gateways (the standby carries traffic after a failover).
func (s *Sampler) scrapeGateways(ctx context.Context) map[string]wgCount {
	out := map[string]wgCount{}
	hosts, err := s.Store.ListAllGatewayHosts(ctx)
	if err != nil {
		return out
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	for _, h := range hosts {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s.%s:9586/metrics", h, s.BaseDomain), nil)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			name, labels, val, ok := parseLine(sc.Text())
			if !ok {
				continue
			}
			k := labels["tenant"] + "|" + labels["peer_ip"]
			c := out[k]
			switch name {
			case "malgary_wg_peer_rx_bytes":
				c.rx += val
			case "malgary_wg_peer_tx_bytes":
				c.tx += val
			}
			out[k] = c
		}
		_ = resp.Body.Close()
	}
	return out
}

func parseLine(l string) (string, map[string]string, int64, bool) {
	if strings.HasPrefix(l, "#") {
		return "", nil, 0, false
	}
	open := strings.IndexByte(l, '{')
	end := strings.LastIndexByte(l, '}')
	if open < 0 || end < open {
		return "", nil, 0, false
	}
	labels := map[string]string{}
	for _, kv := range strings.Split(l[open+1:end], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			labels[strings.TrimSpace(k)] = strings.Trim(v, `"`)
		}
	}
	v, err := strconv.ParseInt(strings.TrimSpace(l[end+1:]), 10, 64)
	return l[:open], labels, v, err == nil
}

// Report answers GET /v1/tenants/{id}/usage: latest status per user, bytes in the window, and
// the never-installed and inactive-30-day lists the customer admin asked for.
func Report(ctx context.Context, st *store.Store, tenantID uuid.UUID, from, to time.Time) (gen.Usage, error) {
	users, err := st.ListTenantUsers(ctx, tenantID)
	if err != nil {
		return gen.Usage{}, err
	}
	if _, err := st.GetTenant(ctx, tenantID); store.IsNotFound(err) {
		return gen.Usage{}, httpapi.ErrNotFound
	}
	latest, err := st.LatestUsagePerUser(ctx, db.LatestUsagePerUserParams{TenantID: tenantID, SampledAt: to.AddDate(0, 0, -90)})
	if err != nil {
		return gen.Usage{}, err
	}
	totals, err := st.UsageTotalsPerUser(ctx, db.UsageTotalsPerUserParams{TenantID: tenantID, SampledAt: from, SampledAt_2: to})
	if err != nil {
		return gen.Usage{}, err
	}
	lat := map[uuid.UUID]db.LatestUsagePerUserRow{}
	for _, l := range latest {
		if l.TenantUserID != nil {
			lat[*l.TenantUserID] = l
		}
	}
	tot := map[uuid.UUID]db.UsageTotalsPerUserRow{}
	for _, t := range totals {
		if t.TenantUserID != nil {
			tot[*t.TenantUserID] = t
		}
	}
	out := gen.Usage{Users: nil, NeverInstalled: []string{}, Inactive30d: []string{}}
	cut := to.AddDate(0, 0, -30)
	for _, u := range users {
		row := struct {
			Connected *bool              `json:"connected,omitempty"`
			Email     string             `json:"email"`
			LastSeen  *time.Time         `json:"lastSeen,omitempty"`
			RxBytes   *int64             `json:"rxBytes,omitempty"`
			TxBytes   *int64             `json:"txBytes,omitempty"`
			UserId    openapi_types.UUID `json:"userId"`
		}{Email: u.Email, UserId: u.ID}
		l, seen := lat[u.ID]
		if !seen {
			out.NeverInstalled = append(out.NeverInstalled, u.Email)
		} else {
			c := l.Connected
			row.Connected, row.LastSeen = &c, l.LastSeen
			if l.LastSeen == nil || l.LastSeen.Before(cut) {
				out.Inactive30d = append(out.Inactive30d, u.Email)
			}
		}
		if t, ok := tot[u.ID]; ok {
			row.RxBytes, row.TxBytes = &t.Rx, &t.Tx
		}
		out.Users = append(out.Users, row)
	}
	return out, nil
}
