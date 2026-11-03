// Command malgaryd is the Malgary backend: system of record, API and reconciler
// (specs/001-tier1-pilot/plan.md).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	"malgary/backend/internal/netbird"
	"malgary/backend/internal/reconcile"
	"malgary/backend/internal/secrets"
	"malgary/backend/internal/store"
	"malgary/backend/internal/tenancy"
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
	verifier, err := httpapi.NewOIDCVerifier(ctx, cfg.ZitadelIssuer, os.Getenv("MALGARY_API_AUDIENCE"))
	if err != nil {
		return err
	}

	ten := &tenancy.Service{
		Store: st, Secrets: res, Identity: idp, NetBird: netbird.NewFactory(20),
		Deployer:       tenancy.AnsibleDeployer{SpecDir: cfg.SpecDir},
		BaseDomain:     cfg.BaseDomain,
		NetbirdVersion: cfg.NetbirdVersion,
		StackDBHost:    "host.docker.internal",
	}
	claimsKey, err := res.Resolve(secrets.Ref("zitadel-claims-signing-key"))
	if err != nil {
		slog.Warn("groups claim hook disabled: no signing key", "err", err)
	}
	bill := &billing.Service{Store: st, Providers: map[string]providers.Provider{"manual": manual.Provider{}}}
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
	us := &users.Service{Store: st, Identity: idp, Seats: bill}
	loop := &reconcile.Loop{Store: st, Repair: ten}
	sampler := &usage.Sampler{Store: st, Secrets: res, NetBird: ten.NetBird, BaseDomain: cfg.BaseDomain}
	go loop.Run(ctx)
	go sampler.Run(ctx)
	go serveMetrics(ctx, cfg.MetricsListen)

	h := &httpapi.Handlers{Deps: httpapi.Deps{Store: st, Tenancy: ten, Users: us, Billing: bill,
		Ops: ops{st: st, loop: loop}, ClaimsSigningKey: claimsKey}}

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
func serveMetrics(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "malgary_up 1\n")
		_, _ = fmt.Fprintf(w, "malgary_tenant_seat_limit_hit_total %d\n", users.SeatLimitHitTotal())
		_, _ = fmt.Fprintf(w, "malgary_reconcile_drift_tenants %d\n", reconcile.DriftOpen.Load())
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("metrics server", "err", err)
	}
}
