package interim

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"malgary/backend/internal/audit"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
	"malgary/backend/internal/tenancy"
	"malgary/backend/internal/tenancy/compose"
)

// ProbeEnv prepares and removes the probe's access to a tenant's stack (internal/tenancy).
type ProbeEnv interface {
	PrepareIsolationProbe(ctx context.Context, tenantID uuid.UUID) (tenancy.ProbeTarget, error)
	CleanupIsolationProbe(ctx context.Context, tenantID uuid.UUID) error
}

// ProbeRunner runs the probe client (internal/tenancy/compose).
type ProbeRunner interface {
	Run(ctx context.Context, slug, managementURL, setupKey string) (compose.ProbeResult, error)
}

// Service implements httpapi.InterimService: status, isolation results and restore tests.
type Service struct {
	*Limits
	Store  *store.Store
	Env    ProbeEnv
	Prober ProbeRunner
	// OnIsolationPassed re-applies the tenant so its access policy is enabled (FR-009).
	OnIsolationPassed func(ctx context.Context, tenantID uuid.UUID)
}

// Status is the GET /v1/interim view.
func (s *Service) Status(ctx context.Context) (gen.InterimStatus, error) {
	return s.Limits.Status(ctx, s.Store.Queries)
}

// RecordIsolationTest stores a result. The server decides "passed" itself: every probe must
// have got what it expected, whatever the client claimed.
func (s *Service) RecordIsolationTest(ctx context.Context, tenantID uuid.UUID, in gen.IsolationTestInput, ranBy string) (gen.IsolationTest, error) {
	if len(in.Results) == 0 {
		return gen.IsolationTest{}, httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "results are required")
	}
	passed := in.Passed
	for _, p := range in.Results {
		if string(p.Got) != string(p.Expected) {
			passed = false
		}
	}
	var egress *netip.Addr
	if in.EgressIp != nil && *in.EgressIp != "" {
		a, err := netip.ParseAddr(*in.EgressIp)
		if err != nil {
			return gen.IsolationTest{}, httpapi.Errf(http.StatusUnprocessableEntity, gen.InvalidRequest, "egressIp is not an IP address")
		}
		egress = &a
	}
	results, err := json.Marshal(in.Results)
	if err != nil {
		return gen.IsolationTest{}, err
	}
	var row db.IsolationTestResult
	err = s.Store.WithTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		stack, err := q.GetTenantStack(ctx, tenantID)
		if store.IsNotFound(err) {
			return httpapi.ErrNotFound
		}
		if err != nil {
			return err
		}
		if stack.DesiredHash == nil {
			return httpapi.Errf(http.StatusConflict, gen.StackNotReady, "the tenant's stack has not been deployed yet")
		}
		// The result applies to the stack as deployed now, whatever the client sent.
		in.StackHash = stack.DesiredHash
		row, err = q.CreateIsolationTestResult(ctx, db.CreateIsolationTestResultParams{
			ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, StackHash: *stack.DesiredHash, Passed: passed,
			EgressIp: egress, Results: results, RanBy: ranBy,
		})
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{TenantID: &tenantID, Action: "isolation_test.recorded",
			TargetType: "isolation_test", TargetID: row.ID.String(), After: map[string]any{"stackHash": in.StackHash, "passed": passed}})
	})
	if err != nil {
		return gen.IsolationTest{}, err
	}
	if passed && s.OnIsolationPassed != nil {
		go s.OnIsolationPassed(context.WithoutCancel(ctx), tenantID)
	} else if !passed {
		slog.WarnContext(ctx, "isolation test failed", "tenant", tenantID, "hash", in.StackHash)
	}
	return isolationView(row), nil
}

// RunIsolationTest prepares the probe, runs it, records the result and cleans up. Only one
// test runs at a time on the host (the probe network is fixed).
func (s *Service) RunIsolationTest(ctx context.Context, tenantID uuid.UUID, ranBy string) (gen.IsolationTest, error) {
	if s.Env == nil || s.Prober == nil {
		return gen.IsolationTest{}, httpapi.Errf(http.StatusNotImplemented, gen.Internal, "isolation probe not configured on this host")
	}
	if !runLock.TryLock() {
		return gen.IsolationTest{}, httpapi.Errf(http.StatusConflict, gen.InvalidRequest, "another isolation test is running")
	}
	defer runLock.Unlock()
	t, err := s.Store.GetTenant(ctx, tenantID)
	if store.IsNotFound(err) {
		return gen.IsolationTest{}, httpapi.ErrNotFound
	}
	if err != nil {
		return gen.IsolationTest{}, err
	}
	target, err := s.Env.PrepareIsolationProbe(ctx, tenantID)
	defer func() {
		if cerr := s.Env.CleanupIsolationProbe(context.WithoutCancel(ctx), tenantID); cerr != nil {
			slog.WarnContext(ctx, "isolation probe cleanup", "tenant", t.Slug, "err", cerr)
		}
	}()
	if err != nil {
		return gen.IsolationTest{}, fmt.Errorf("prepare isolation probe: %w", err)
	}
	res, err := s.Prober.Run(ctx, t.Slug, target.ManagementURL, target.SetupKey)
	if err != nil && len(res.Results) == 0 {
		return gen.IsolationTest{}, err
	}
	in := gen.IsolationTestInput{Passed: res.Passed && err == nil, Results: res.Results}
	if res.EgressIP != "" {
		in.EgressIp = &res.EgressIP
	}
	return s.RecordIsolationTest(ctx, tenantID, in, ranBy)
}

var runLock sync.Mutex

// ListIsolationTests returns the newest 100 results.
func (s *Service) ListIsolationTests(ctx context.Context, tenantID uuid.UUID) ([]gen.IsolationTest, error) {
	rows, err := s.Store.ListIsolationTestResults(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.IsolationTest, 0, len(rows))
	for _, r := range rows {
		out = append(out, isolationView(r))
	}
	return out, nil
}

// RecordRestoreTest stores a restore test; one pass opens imports of real users (FR-015).
func (s *Service) RecordRestoreTest(ctx context.Context, in gen.RestoreTestInput) error {
	details, err := json.Marshal(in.Details)
	if err != nil {
		return err
	}
	return s.Store.WithTx(ctx, func(q *db.Queries, _ pgx.Tx) error {
		row, err := q.CreateRestoreTest(ctx, db.CreateRestoreTestParams{
			ID: uuid.Must(uuid.NewV7()), BackupKey: in.BackupKey, BackupTakenAt: in.BackupTakenAt, Passed: in.Passed, Details: details,
		})
		if err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: "restore_test.recorded", TargetType: "restore_test",
			TargetID: row.ID.String(), After: map[string]any{"backupKey": in.BackupKey, "passed": in.Passed}})
	})
}

func isolationView(r db.IsolationTestResult) gen.IsolationTest {
	v := gen.IsolationTest{Id: r.ID, StackHash: &r.StackHash, Passed: r.Passed, RanAt: r.RanAt, RanBy: r.RanBy, Results: []gen.IsolationProbe{}}
	_ = json.Unmarshal(r.Results, &v.Results)
	if r.EgressIp != nil {
		ip := r.EgressIp.String()
		v.EgressIp = &ip
	}
	return v
}
