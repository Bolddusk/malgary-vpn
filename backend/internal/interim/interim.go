// Package interim enforces the limits of the constitution's Interim Hosting Exception while
// the platform runs on the shared host (feature 002, research I9). Every check is a no-op when
// interim mode is off, so dedicated cells are unaffected.
package interim

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"malgary/backend/internal/config"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
	"malgary/backend/internal/store/db"
)

// Gauges read by malgaryd's metrics endpoint: malgary_interim_days_left (FR-011 warning) and
// malgary_interim_users_used (FR-010).
var DaysLeft, UsersUsed atomic.Int64

// Limits checks imports and plans against the interim settings.
type Limits struct {
	Config config.Interim
	Now    func() time.Time
}

// Active reports whether interim mode is on. A nil Limits is inactive.
func (l *Limits) Active() bool { return l != nil && l.Config.Active }

func (l *Limits) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// DaysLeft counts whole days until the last allowed day; negative once it has passed.
func (l *Limits) DaysLeft() int {
	y, m, d := l.now().UTC().Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return int(l.Config.Until.Sub(today).Hours() / 24)
}

// CheckImport runs inside the import transaction after the new users are inserted, so the
// count already includes them. emails are the newly created users only.
func (l *Limits) CheckImport(ctx context.Context, q *db.Queries, tenantSlug string, emails []string) error {
	if !l.Active() || len(emails) == 0 {
		return nil
	}
	if l.DaysLeft() < 0 {
		return httpapi.Errf(http.StatusUnprocessableEntity, gen.InterimExpired,
			"the interim host stopped accepting users after "+l.Config.Until.Format(time.DateOnly))
	}
	testTenant := slices.Contains(l.Config.TestTenants, tenantSlug)
	if !testTenant {
		return httpapi.Errf(http.StatusUnprocessableEntity, gen.InterimTenantNotAllowed,
			"tenant "+tenantSlug+" is not a test tenant on the interim host")
	}
	used, err := q.CountSeatsHeldAll(ctx)
	if err != nil {
		return err
	}
	UsersUsed.Store(used)
	if used > int64(l.Config.MaxUsers) {
		return httpapi.Errf(http.StatusUnprocessableEntity, gen.InterimUserLimit,
			fmt.Sprintf("the interim host allows %d users across all tenants; this import would make %d", l.Config.MaxUsers, used))
	}
	var real []string
	for _, e := range emails {
		if !l.isTestEmail(e) {
			real = append(real, e)
		}
	}
	if len(real) == 0 {
		return nil
	}
	passed, err := q.AnyRestoreTestPassed(ctx)
	if err != nil {
		return err
	}
	if !passed {
		return httpapi.Errf(http.StatusUnprocessableEntity, gen.RestoreTestRequired,
			"real company users ("+strings.Join(real, ", ")+") need a tested backup first; test users must use "+
				strings.Join(l.Config.TestDomains, ", ")+" addresses")
	}
	return nil
}

func (l *Limits) isTestEmail(email string) bool {
	_, domain, ok := strings.Cut(strings.ToLower(email), "@")
	return ok && slices.Contains(l.Config.TestDomains, domain)
}

// CheckPlanPrice refuses a paying plan for any tenant other than the allowed ones (FR-010).
func (l *Limits) CheckPlanPrice(tenantSlug string, pricePerSeatMinor int64) error {
	if !l.Active() || pricePerSeatMinor == 0 || slices.Contains(l.Config.PaidTenants, tenantSlug) {
		return nil
	}
	return httpapi.Errf(http.StatusUnprocessableEntity, gen.InterimTenantNotAllowed,
		"paying plans aren't allowed for "+tenantSlug+" on the interim host")
}

// Status is the GET /v1/interim view.
func (l *Limits) Status(ctx context.Context, q *db.Queries) (gen.InterimStatus, error) {
	if !l.Active() {
		return gen.InterimStatus{Active: false}, nil
	}
	used, err := q.CountSeatsHeldAll(ctx)
	if err != nil {
		return gen.InterimStatus{}, err
	}
	passed, err := q.AnyRestoreTestPassed(ctx)
	if err != nil {
		return gen.InterimStatus{}, err
	}
	usersUsed, days := int(used), l.DaysLeft()
	until := l.Config.Until
	return gen.InterimStatus{
		Active: true, MaxUsers: &l.Config.MaxUsers, UsersUsed: &usersUsed, DaysLeft: &days,
		Until: &openapi_types.Date{Time: until}, EgressNote: &l.Config.EgressNote, RestoreTestPassed: &passed,
	}, nil
}

// UpdateMetrics refreshes the gauges; malgaryd calls it on a timer.
func (l *Limits) UpdateMetrics(ctx context.Context, q *db.Queries) {
	if !l.Active() {
		return
	}
	DaysLeft.Store(int64(l.DaysLeft()))
	if used, err := q.CountSeatsHeldAll(ctx); err == nil {
		UsersUsed.Store(used)
	}
}
