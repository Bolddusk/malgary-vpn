package interim

import (
	"context"
	"errors"
	"testing"
	"time"

	"malgary/backend/internal/config"
	"malgary/backend/internal/httpapi"
	"malgary/backend/internal/httpapi/gen"
)

func limits(now string) *Limits {
	n, _ := time.Parse(time.DateOnly, now)
	return &Limits{
		Config: config.Interim{
			Active: true, MaxUsers: 10, Until: time.Date(2027, 2, 28, 0, 0, 0, 0, time.UTC),
			TestTenants: []string{"onedge", "testco"}, TestDomains: []string{"gmail.com"}, PaidTenants: []string{"onedge"},
		},
		Now: func() time.Time { return n.Add(15 * time.Hour) },
	}
}

func code(err error) gen.ProblemCode {
	var e *httpapi.APIError
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestDaysLeft(t *testing.T) {
	for now, want := range map[string]int{"2027-01-29": 30, "2027-02-28": 0, "2027-03-01": -1} {
		if got := limits(now).DaysLeft(); got != want {
			t.Errorf("%s: days left %d, want %d", now, got, want)
		}
	}
}

func TestCheckImportWithoutDatabase(t *testing.T) {
	ctx := context.Background()
	// These paths return before the database is touched, so a nil *db.Queries is safe.
	if err := limits("2027-03-01").CheckImport(ctx, nil, "onedge", []string{"a@gmail.com"}); code(err) != gen.InterimExpired {
		t.Errorf("after the end date: %v", err)
	}
	if err := limits("2026-11-04").CheckImport(ctx, nil, "acme", []string{"a@gmail.com"}); code(err) != gen.InterimTenantNotAllowed {
		t.Errorf("non-test tenant: %v", err)
	}
	if err := limits("2026-11-04").CheckImport(ctx, nil, "onedge", nil); err != nil {
		t.Errorf("nothing created: %v", err)
	}
	off := limits("2027-03-01")
	off.Config.Active = false
	if err := off.CheckImport(ctx, nil, "acme", []string{"a@corp.com"}); err != nil {
		t.Errorf("inactive: %v", err)
	}
	var none *Limits
	if none.Active() {
		t.Error("nil limits must be inactive")
	}
}

func TestIsTestEmail(t *testing.T) {
	l := limits("2026-11-04")
	for email, want := range map[string]bool{
		"a@gmail.com": true, "A@GMAIL.COM": true, "a@onedge.com": false, "a@gmail.com.evil.io": false, "nodomain": false,
	} {
		if got := l.isTestEmail(email); got != want {
			t.Errorf("%s: %v, want %v", email, got, want)
		}
	}
}

func TestCheckPlanPrice(t *testing.T) {
	l := limits("2026-11-04")
	if err := l.CheckPlanPrice("onedge", 400); err != nil {
		t.Errorf("onedge paid: %v", err)
	}
	if err := l.CheckPlanPrice("testco", 0); err != nil {
		t.Errorf("free plan: %v", err)
	}
	if err := l.CheckPlanPrice("testco", 400); code(err) != gen.InterimTenantNotAllowed {
		t.Errorf("testco paid: %v", err)
	}
}
