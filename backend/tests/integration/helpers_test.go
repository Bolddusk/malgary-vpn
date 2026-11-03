//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"malgary/backend/internal/store"
)

// startPostgres runs PostgreSQL 17, creates the two roles the migrations expect and applies
// migrations as the owner. It returns a store connected as the application role.
func startPostgres(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	c, err := tcpg.Run(ctx, "postgres:17-alpine",
		tcpg.WithDatabase("malgary"), tcpg.WithUsername("postgres"), tcpg.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(240*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	admin, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"CREATE ROLE malgary_owner LOGIN PASSWORD 'owner'",
		"CREATE ROLE malgary_app LOGIN PASSWORD 'app'",
		"ALTER DATABASE malgary OWNER TO malgary_owner",
		"GRANT ALL ON SCHEMA public TO malgary_owner",
		"CREATE EXTENSION IF NOT EXISTS citext",
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatal(q, err)
		}
	}
	conn.Close(ctx)
	owner := strings.Replace(admin, "postgres:postgres@", "malgary_owner:owner@", 1)
	app := strings.Replace(admin, "postgres:postgres@", "malgary_app:app@", 1)
	if err := store.Migrate(ctx, owner); err != nil {
		t.Fatal("migrate:", err)
	}
	s, err := store.Open(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// fakeNetBird emulates the parts of a fresh NetBird v0.82 management API that bootstrap uses.
type fakeNetBird struct {
	mu        sync.Mutex
	setupDone bool
	groups    map[string]string
	keys      int
	idps      []map[string]any
	routes    []map[string]any
	checks    []map[string]any
	policies  []map[string]any
	peerHosts []string // gateway hostnames that have joined; default: the c01 pair
	calls     []string
}

func newFakeNetBird(t *testing.T) (*fakeNetBird, *httptest.Server) {
	f := &fakeNetBird{groups: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeNetBird) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	reply := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/instance":
		reply(map[string]any{"setup_required": !f.setupDone})
	case r.Method == "POST" && r.URL.Path == "/api/setup":
		if f.setupDone {
			w.WriteHeader(412)
			return
		}
		f.setupDone = true
		reply(map[string]any{"user_id": "owner", "personal_access_token": "nbp_setup"})
	case r.Method == "GET" && r.URL.Path == "/api/users":
		reply([]any{})
	case r.Method == "POST" && r.URL.Path == "/api/users":
		reply(map[string]any{"id": "svc", "email": "", "name": "malgary-backend", "role": "admin", "status": "active", "auto_groups": []string{}, "is_service_user": true, "is_blocked": false, "issued": "api"})
	case r.Method == "POST" && r.URL.Path == "/api/users/svc/tokens":
		reply(map[string]any{"plain_token": "nbp_service", "personal_access_token": map[string]any{"id": "t1", "name": "malgary-backend", "created_by": "owner", "created_at": time.Now(), "expiration_date": time.Now().Add(365 * 24 * time.Hour)}})
	case r.Method == "GET" && r.URL.Path == "/api/groups":
		out := []map[string]any{}
		want := r.URL.Query().Get("name")
		for name, id := range f.groups {
			if want == "" || want == name {
				out = append(out, map[string]any{"id": id, "name": name, "peers_count": 0, "resources_count": 0, "issued": "api"})
			}
		}
		reply(out)
	case r.Method == "POST" && r.URL.Path == "/api/groups":
		var g struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&g)
		id := "g-" + g.Name
		f.groups[g.Name] = id
		reply(map[string]any{"id": id, "name": g.Name, "peers_count": 0, "resources_count": 0, "issued": "api"})
	case r.Method == "POST" && r.URL.Path == "/api/setup-keys":
		f.keys++
		reply(map[string]any{"id": "k1", "key": "SETUPKEY-1", "name": "malgary-gateways", "type": "reusable", "valid": true, "revoked": false, "auto_groups": []string{}, "state": "valid"})
	case r.Method == "GET" && r.URL.Path == "/api/peers":
		hosts := f.peerHosts
		if hosts == nil {
			hosts = []string{"gw-nyc-1a.c01", "gw-nyc-1b.c01"}
		}
		var peers []map[string]any
		for i, h := range hosts {
			peers = append(peers, map[string]any{"id": fmt.Sprintf("p-%d", i), "hostname": h, "name": h, "connected": true, "groups": []any{}})
		}
		reply(peers)
	case r.Method == "GET" && r.URL.Path == "/api/routes":
		reply(f.routes)
	case r.Method == "POST" && r.URL.Path == "/api/routes":
		var rt map[string]any
		_ = json.NewDecoder(r.Body).Decode(&rt)
		rt["id"] = fmt.Sprintf("r%d", len(f.routes)+1)
		rt["network_type"] = "IPv4"
		f.routes = append(f.routes, rt)
		reply(rt)
	case r.Method == "GET" && (r.URL.Path == "/api/posture-checks"):
		reply(f.checks)
	case r.Method == "POST" && r.URL.Path == "/api/posture-checks":
		var pc map[string]any
		_ = json.NewDecoder(r.Body).Decode(&pc)
		pc["id"] = fmt.Sprintf("pc%d", len(f.checks)+1)
		f.checks = append(f.checks, pc)
		reply(pc)
	case r.Method == "GET" && r.URL.Path == "/api/policies":
		reply(f.policies)
	case (r.Method == "POST" && r.URL.Path == "/api/policies") || (r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/policies/")):
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		p["id"] = fmt.Sprintf("pol%d", len(f.policies)+1)
		replace := -1
		if r.Method == "PUT" {
			p["id"] = strings.TrimPrefix(r.URL.Path, "/api/policies/")
			for i, old := range f.policies {
				if old["id"] == p["id"] {
					replace = i
				}
			}
		}
		// The real API answers with group objects, not IDs.
		for _, rule := range p["rules"].([]any) {
			rm := rule.(map[string]any)
			for _, k := range []string{"sources", "destinations"} {
				var objs []map[string]any
				for _, id := range rm[k].([]any) {
					objs = append(objs, map[string]any{"id": id, "name": id, "peers_count": 0, "resources_count": 0})
				}
				rm[k] = objs
			}
		}
		if replace >= 0 {
			f.policies[replace] = p
		} else {
			f.policies = append(f.policies, p)
		}
		reply(p)
	case r.Method == "GET" && r.URL.Path == "/api/accounts":
		reply([]map[string]any{{"id": "acc1", "domain": "netbird.selfhosted", "domain_category": "private", "created_at": time.Now(), "created_by": "owner",
			"settings": map[string]any{"peer_login_expiration": 86400, "peer_login_expiration_enabled": true, "peer_inactivity_expiration": 0, "peer_inactivity_expiration_enabled": false, "regular_users_view_blocked": false, "peer_expose_enabled": false, "peer_expose_groups": []string{}}}})
	case r.Method == "PUT" && r.URL.Path == "/api/accounts/acc1":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		reply(map[string]any{"id": "acc1", "domain": "netbird.selfhosted", "domain_category": "private", "created_at": time.Now(), "created_by": "owner", "settings": body["settings"]})
	case r.Method == "GET" && r.URL.Path == "/api/identity-providers":
		reply(f.idps)
	case r.Method == "POST" && r.URL.Path == "/api/identity-providers":
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.idps = append(f.idps, p)
		reply(p)
	default:
		w.WriteHeader(404)
	}
}
