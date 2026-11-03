// Command malgaryd is the Malgary backend: system of record, API and reconciler
// (specs/001-tier1-pilot/plan.md).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"malgary/backend/internal/billing"
	"malgary/backend/internal/billing/providers"
	"malgary/backend/internal/billing/providers/manual"
	"malgary/backend/internal/config"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/identity"
	"malgary/backend/internal/interim"
	"malgary/backend/internal/netbird"
	"malgary/backend/internal/reconcile"
	"malgary/backend/internal/secrets"
	"malgary/backend/internal/store"
	"malgary/backend/internal/tenancy"
	"malgary/backend/internal/tenancy/compose"
	"malgary/backend/internal/usage"
	"malgary/backend/internal/users"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if err := run(); err != nil {
		slog.Error("malgaryd stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if cfg.MigrateDatabaseURL != "" {
		if err := store.Migrate(ctx, cfg.MigrateDatabaseURL); err != nil {
			return err
		}
	}
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	if inv := os.Getenv("MALGARY_CELL_INVENTORY"); inv != "" {
		files, _ := filepath.Glob(inv)
		for _, f := range files {
			if err := tenancy.SyncInventory(ctx, st, f); err != nil {
				return err
			}
		}
	}

	res := secrets.Resolver{Dir: cfg.SecretDir}
	keyPath := filepath.Join(cfg.SecretDir, strings.TrimPrefix(cfg.ZitadelKeyRef, "secret://"))
	idp, err := identity.NewZitadel(ctx, cfg.ZitadelIssuer, keyPath, identity.SharedIdPs{
		Google: os.Getenv("MALGARY_ZITADEL_GOOGLE_IDP_ID"),
		Entra:  os.Getenv("MALGARY_ZITADEL_ENTRA_IDP_ID"),
	})
	if err != nil {
		return err
	}
	audience := os.Getenv("MALGARY_API_AUDIENCE")
	if audience == "" {
		if audience, err = ensureAPIProject(ctx, idp, res); err != nil {
			return err
		}
	}
	verifier, err := httpapi.NewOIDCVerifier(ctx, cfg.ZitadelIssuer, audience)
	if err != nil {
		return err
	}

	deployer, dbHost, err := newDeployer(cfg, res)
	if err != nil {
		return err
	}
	ten := &tenancy.Service{
		Store: st, Secrets: res, Identity: idp, NetBird: netbird.NewFactory(20),
		Deployer:       deployer,
		BaseDomain:     cfg.BaseDomain,
		NetbirdVersion: cfg.NetbirdVersion,
		StackDBHost:    dbHost,
		// On the interim host users reach the gateway only after an isolation test (FR-009).
		IsolationRequired: cfg.Interim.Active,
	}
	claimsKey, err := res.Resolve(secrets.Ref("zitadel-claims-signing-key"))
	if err != nil {
		slog.Warn("groups claim hook disabled: no signing key", "err", err)
	}
	limits := &interim.Limits{Config: cfg.Interim}
	if limits.Active() {
		slog.Info("interim hosting limits on", "maxUsers", cfg.Interim.MaxUsers, "until", cfg.Interim.Until.Format(time.DateOnly))
	}
	bill := &billing.Service{Store: st, Providers: map[string]providers.Provider{"manual": manual.Provider{}}, Interim: limits}
	if ep := os.Getenv("MALGARY_SPACES_ENDPOINT"); ep != "" {
		ak, _ := res.Resolve(secrets.Ref("spaces-access-key"))
		sk, _ := res.Resolve(secrets.Ref("spaces-secret-key"))
		sp, err := billing.NewSpaces(ep, os.Getenv("MALGARY_ARTIFACTS_BUCKET"), strings.TrimSpace(string(ak)), strings.TrimSpace(string(sk)))
		if err != nil {
			return err
		}
		bill.Proofs = sp
	}
	ten.Billing = bill
	us := &users.Service{Store: st, Identity: idp, Seats: bill, Interim: limits, InviteAsLink: cfg.InviteDelivery == "link"}
	loop := &reconcile.Loop{Store: st, Repair: ten}
	interimSvc := &interim.Service{Limits: limits, Store: st, OnIsolationPassed: func(ctx context.Context, id uuid.UUID) {
		if _, err := loop.One(ctx, id); err != nil {
			slog.WarnContext(ctx, "re-apply after isolation pass", "tenant", id, "err", err)
		}
	}}
	if cfg.Deployer == "compose" {
		probeTmp := filepath.Join(cfg.Compose.StackDir, ".probe")
		if err := os.MkdirAll(probeTmp, 0o700); err != nil {
			return err
		}
		interimSvc.Env = ten
		interimSvc.Prober = compose.Prober{
			Image: envOr("MALGARY_PROBE_IMAGE", "malgary/isolation-probe:local"), HostIP: cfg.Compose.HostPublicIP,
			DBHost: cfg.Compose.StackDBHost, Network: cfg.Compose.TraefikNetwork, TmpDir: probeTmp,
		}
	}
	go func() {
		for t := time.NewTicker(time.Minute); ; {
			limits.UpdateMetrics(ctx, st.Queries)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
	}()
	sampler := &usage.Sampler{Store: st, Secrets: res, NetBird: ten.NetBird, BaseDomain: cfg.BaseDomain}
	go loop.Run(ctx)
	go sampler.Run(ctx)
	go serveMetrics(ctx, cfg.MetricsListen, limits.Active())

	h := &httpapi.Handlers{Deps: httpapi.Deps{Store: st, Tenancy: ten, Users: us, Billing: bill,
		Ops: ops{st: st, loop: loop}, Interim: interimSvc, ClaimsSigningKey: claimsKey}}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           httpapi.NewRouter(h, verifier),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Listen)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	select {
	case <-ctx.Done():
	case err := <-errc:
		return err
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

// ensureAPIProject creates the backend's project, roles and malgaryctl app in Malgary sign-in
// on a fresh installation and remembers their IDs in the secret store. The owner named by
// MALGARY_OWNER_EMAIL gets the owner role. Returns the API token audience.
func ensureAPIProject(ctx context.Context, idp *identity.Zitadel, res secrets.Resolver) (string, error) {
	read := func(name string) string {
		b, err := res.Resolve(secrets.Ref(name))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	cur := identity.APIProject{ProjectID: read("api-project-id"), CLIClientID: read("cli-client-id")}
	if cur.ProjectID != "" && cur.CLIClientID != "" {
		return cur.ProjectID, nil
	}
	p, err := idp.EnsureAPIProject(ctx, cur, os.Getenv("MALGARY_OWNER_EMAIL"))
	for name, v := range map[string]string{"api-project-id": p.ProjectID, "cli-client-id": p.CLIClientID} {
		if v != "" {
			if perr := res.Put(secrets.Ref(name), []byte(v)); perr != nil {
				return "", perr
			}
		}
	}
	if err != nil {
		return "", fmt.Errorf("API project in sign-in: %w", err)
	}
	slog.Info("API project ready; set MALGARY_CLIENT for malgaryctl", "projectId", p.ProjectID, "malgaryctlClientId", p.CLIClientID)
	return p.ProjectID, nil
}

// newDeployer picks the Ansible deployer (dedicated cells) or the Compose deployer (interim
// host, feature 002) and returns the database host the stacks use.
func newDeployer(cfg config.Config, res secrets.Resolver) (tenancy.Deployer, string, error) {
	if cfg.Deployer != "compose" {
		return tenancy.AnsibleDeployer{SpecDir: cfg.SpecDir}, "host.docker.internal", nil
	}
	pw, err := res.Resolve(cfg.Compose.ProvisionerPassRef)
	if err != nil {
		return nil, "", fmt.Errorf("provisioner password: %w", err)
	}
	dsn := (&url.URL{
		Scheme: "postgres", User: url.UserPassword("malgary_provisioner", strings.TrimSpace(string(pw))),
		Host: cfg.Compose.StackDBHost + ":5432", Path: "/postgres", RawQuery: "sslmode=disable",
	}).String()
	d := &compose.Deployer{
		StackDir: cfg.Compose.StackDir, Secrets: res, Memory: compose.DefaultMemory,
		DB: compose.Provisioner{DSN: dsn}, Runner: compose.DockerCLI{},
		DashboardVersion: envOr("MALGARY_NETBIRD_DASHBOARD_VERSION", "v2.40.0"),
		GuardImage:       envOr("MALGARY_GUARD_IMAGE", "malgary/gw-guard:local"),
		Host: compose.Host{
			TraefikNetwork: cfg.Compose.TraefikNetwork, TraefikEntry: cfg.Compose.TraefikEntry,
			TraefikResolver: cfg.Compose.TraefikResolver, GatewayNetwork: cfg.Compose.GatewayNetwork,
			HostPublicIP: cfg.Compose.HostPublicIP, OperatorAllow: cfg.Compose.OperatorAllow,
			EgressNote: cfg.Interim.EgressNote,
		},
	}
	if cfg.Interim.Active {
		// Zitadel + login (1,024 MB), malgaryd (256 MB) and Alloy (128 MB), research I11.
		d.BudgetMB, d.PlatformMB = cfg.Interim.MemoryBudget, 1408
	}
	return d, cfg.Compose.StackDBHost, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ops adapts usage and reconcile to httpapi.OpsService.
type ops struct {
	st   *store.Store
	loop *reconcile.Loop
}

func (o ops) Usage(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (gen.Usage, error) {
	return usage.Report(ctx, o.st, tenantID, from, to)
}

func (o ops) Reconcile(ctx context.Context, tenantID uuid.UUID) (string, error) {
	return o.loop.One(ctx, tenantID)
}

// serveMetrics exposes malgaryd's own metrics for Alloy (alerts in infra/observability).
func serveMetrics(ctx context.Context, addr string, interimOn bool) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "malgary_up 1\n")
		_, _ = fmt.Fprintf(w, "malgary_tenant_seat_limit_hit_total %d\n", users.SeatLimitHitTotal())
		_, _ = fmt.Fprintf(w, "malgary_reconcile_drift_tenants %d\n", reconcile.DriftOpen.Load())
		if interimOn {
			_, _ = fmt.Fprintf(w, "malgary_interim_days_left %d\n", interim.DaysLeft.Load())
			_, _ = fmt.Fprintf(w, "malgary_interim_users_used %d\n", interim.UsersUsed.Load())
		}
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("metrics server", "err", err)
	}
}
