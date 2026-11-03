package interim

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/netip"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"malgary/backend/internal/audit"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store"
	"malgary/backend/internal/store/db"
)

// Service implements httpapi.InterimService: status, isolation results and restore tests.
type Service struct {
	*Limits
	Store *store.Store
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
		if _, err := q.GetTenant(ctx, tenantID); err != nil {
			if store.IsNotFound(err) {
				return httpapi.ErrNotFound
			}
			return err
		}
		row, err = q.CreateIsolationTestResult(ctx, db.CreateIsolationTestResultParams{
			ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, StackHash: in.StackHash, Passed: passed,
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
	v := gen.IsolationTest{Id: r.ID, StackHash: r.StackHash, Passed: r.Passed, RanAt: r.RanAt, RanBy: r.RanBy, Results: []gen.IsolationProbe{}}
	_ = json.Unmarshal(r.Results, &v.Results)
	if r.EgressIp != nil {
		ip := r.EgressIp.String()
		v.EgressIp = &ip
	}
	return v
}
