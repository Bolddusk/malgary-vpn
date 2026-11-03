package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"sigs.k8s.io/yaml"

	"malgary/backend/internal/client"
)

// planFile is deploy/plans/<code>.yaml: the plan plus the version to draft. Plans are data
// applied through the API, never code (Principle XI); published versions are immutable, so a
// changed price means a new version.
type planFile struct {
	Code             string                  `json:"code"`
	Name             string                  `json:"name"`
	Visibility       string                  `json:"visibility"`
	EnterpriseTenant string                  `json:"enterpriseTenant,omitempty"`
	Version          client.PlanVersionInput `json:"version"`
	Publish          bool                    `json:"publish"`
}

func planCmd(ctx context.Context, c *apiClient, args []string) error {
	if len(args) == 0 {
		return errors.New("plan: expected apply -f FILE, publish CODE VERSION, or list")
	}
	switch args[0] {
	case "list":
		res, err := c.ListPlansWithResponse(ctx)
		if err != nil {
			return err
		}
		if res.JSON200 == nil {
			return problemError(res.StatusCode(), res.Body)
		}
		for _, p := range *res.JSON200 {
			for _, v := range p.Versions {
				fmt.Printf("%-20s v%-3d %-11s %8.2f %s/%s\n", p.Code, v.Version, v.Status, float64(v.PricePerSeatMinor)/100, v.Currency, v.BillingPeriod)
			}
		}
		return nil
	case "apply":
		fs := flag.NewFlagSet("plan apply", flag.ContinueOnError)
		file := fs.String("f", "", "plan YAML")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		raw, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		var pf planFile
		if err := yaml.UnmarshalStrict(raw, &pf); err != nil {
			return fmt.Errorf("%s: %w", *file, err)
		}
		plan, err := findPlan(ctx, c, pf.Code)
		if err != nil {
			return err
		}
		if plan == nil {
			in := client.PlanInput{Code: pf.Code, Name: pf.Name, Visibility: client.PlanInputVisibility(pf.Visibility)}
			if pf.EnterpriseTenant != "" {
				in.EnterpriseTenantSlug = &pf.EnterpriseTenant
			}
			res, err := c.CreatePlanWithResponse(ctx, &client.CreatePlanParams{}, in)
			if err != nil {
				return err
			}
			if res.JSON201 == nil {
				return problemError(res.StatusCode(), res.Body)
			}
			plan = res.JSON201
			fmt.Println("created plan", plan.Code)
		}
		// Draft only if no existing version already has identical terms.
		for _, v := range plan.Versions {
			if sameVersion(v, pf.Version) {
				fmt.Printf("plan %s v%d already matches (%s); nothing to do\n", plan.Code, v.Version, v.Status)
				if pf.Publish && v.Status == client.PlanVersionStatusDraft {
					return publish(ctx, c, plan.Id, v.Version)
				}
				return nil
			}
		}
		res, err := c.DraftPlanVersionWithResponse(ctx, plan.Id, &client.DraftPlanVersionParams{}, pf.Version)
		if err != nil {
			return err
		}
		if res.JSON201 == nil {
			return problemError(res.StatusCode(), res.Body)
		}
		fmt.Printf("drafted %s v%d\n", plan.Code, res.JSON201.Version)
		if pf.Publish {
			return publish(ctx, c, plan.Id, res.JSON201.Version)
		}
		return nil
	case "publish":
		if len(args) < 3 {
			return errors.New("plan publish <code> <version>")
		}
		plan, err := findPlan(ctx, c, args[1])
		if err != nil || plan == nil {
			return fmt.Errorf("unknown plan %s", args[1])
		}
		v, err := strconv.Atoi(args[2])
		if err != nil {
			return err
		}
		return publish(ctx, c, plan.Id, v)
	}
	return fmt.Errorf("plan: unknown subcommand %q", args[0])
}

func publish(ctx context.Context, c *apiClient, planID uuid.UUID, v int) error {
	res, err := c.PublishPlanVersionWithResponse(ctx, planID, v, &client.PublishPlanVersionParams{})
	if err != nil {
		return err
	}
	if res.JSON200 == nil {
		return problemError(res.StatusCode(), res.Body)
	}
	fmt.Printf("published v%d\n", v)
	return nil
}

func findPlan(ctx context.Context, c *apiClient, code string) (*client.Plan, error) {
	res, err := c.ListPlansWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if res.JSON200 == nil {
		return nil, problemError(res.StatusCode(), res.Body)
	}
	for _, p := range *res.JSON200 {
		if p.Code == code {
			return &p, nil
		}
	}
	return nil, nil
}

func sameVersion(v client.PlanVersion, in client.PlanVersionInput) bool {
	return v.PricePerSeatMinor == in.PricePerSeatMinor && v.Currency == in.Currency &&
		string(v.BillingPeriod) == string(in.BillingPeriod) && v.MinSeats == in.MinSeats &&
		fmt.Sprint(v.MaxSeats) == fmt.Sprint(in.MaxSeats) && fmt.Sprint(v.Entitlements) == fmt.Sprint(in.Entitlements)
}

func subscriptionCmd(ctx context.Context, c *apiClient, args []string) error {
	if len(args) < 2 || args[0] != "change" {
		return errors.New("usage: malgaryctl subscription change <tenant-id> [--quantity N] [--plan CODE --version V] --reason TEXT")
	}
	tenant, err := uuid.Parse(args[1])
	if err != nil {
		return errors.New("tenant must be a tenant ID")
	}
	fs := flag.NewFlagSet("subscription change", flag.ContinueOnError)
	qty := fs.Int("quantity", 0, "new seat quantity")
	plan := fs.String("plan", "", "plan code to migrate to")
	version := fs.Int("version", 0, "plan version to migrate to")
	reason := fs.String("reason", "", "why (kept in the subscription history)")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	in := client.SubscriptionChange{Reason: *reason}
	if *qty > 0 {
		in.Quantity = qty
	}
	if *plan != "" {
		in.PlanCode, in.PlanVersion = plan, version
	}
	key := idempotencyKey([]byte(fmt.Sprint(tenant, *qty, *plan, *version, *reason)))
	res, err := c.ChangeSubscriptionWithResponse(ctx, tenant, &client.ChangeSubscriptionParams{IdempotencyKey: &key}, in)
	if err != nil {
		return err
	}
	if res.JSON200 == nil {
		return problemError(res.StatusCode(), res.Body)
	}
	return printJSON(res.Body)
}

func invoiceCmd(ctx context.Context, c *apiClient, args []string) error {
	if len(args) < 2 {
		return errors.New("usage: malgaryctl invoice draft <tenant-id> --period YYYY-MM-01 | invoice issue <invoice-id>")
	}
	switch args[0] {
	case "draft":
		tenant, err := uuid.Parse(args[1])
		if err != nil {
			return errors.New("tenant must be a tenant ID")
		}
		fs := flag.NewFlagSet("invoice draft", flag.ContinueOnError)
		period := fs.String("period", "", "period start, YYYY-MM-DD")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		t, err := time.Parse("2006-01-02", *period)
		if err != nil {
			return errors.New("--period YYYY-MM-DD is required")
		}
		key := idempotencyKey([]byte(tenant.String() + *period))
		res, err := c.DraftInvoiceWithResponse(ctx, tenant, &client.DraftInvoiceParams{IdempotencyKey: &key},
			client.DraftInvoiceJSONRequestBody{PeriodStart: openapi_types.Date{Time: t}})
		if err != nil {
			return err
		}
		if res.JSON201 == nil {
			return problemError(res.StatusCode(), res.Body)
		}
		return printJSON(res.Body)
	case "issue":
		id, err := uuid.Parse(args[1])
		if err != nil {
			return errors.New("invoice issue <invoice-id>")
		}
		key := "issue-" + id.String()
		res, err := c.IssueInvoiceWithResponse(ctx, id, &client.IssueInvoiceParams{IdempotencyKey: &key})
		if err != nil {
			return err
		}
		if res.JSON200 == nil {
			return problemError(res.StatusCode(), res.Body)
		}
		return printJSON(res.Body)
	}
	return fmt.Errorf("invoice: unknown subcommand %q", args[0])
}

func paymentCmd(ctx context.Context, c *apiClient, args []string) error {
	if len(args) < 2 || args[0] != "record" {
		return errors.New("usage: malgaryctl payment record <invoice-id> --amount 4000.00 --currency USD --reference REF [--date YYYY-MM-DD] [--proof FILE] [--note TEXT]")
	}
	id, err := uuid.Parse(args[1])
	if err != nil {
		return errors.New("invoice ID required")
	}
	fs := flag.NewFlagSet("payment record", flag.ContinueOnError)
	amount := fs.Float64("amount", 0, "amount in major units, e.g. 4000.00")
	currency := fs.String("currency", "USD", "ISO currency")
	ref := fs.String("reference", "", "bank or transfer reference")
	date := fs.String("date", time.Now().Format("2006-01-02"), "date received")
	proof := fs.String("proof", "", "proof file (PDF or image)")
	note := fs.String("note", "", "method note, e.g. 'wire, Meezan Bank'")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	at, err := time.Parse("2006-01-02", *date)
	if err != nil || *amount <= 0 || *ref == "" {
		return errors.New("--amount, --reference and a valid --date are required")
	}
	in := client.PaymentInput{AmountMinor: int64(math.Round(*amount * 100)), Currency: *currency, ReceivedAt: at, Reference: *ref}
	if *note != "" {
		in.MethodNote = note
	}
	if *proof != "" {
		b, err := os.ReadFile(*proof)
		if err != nil {
			return err
		}
		enc, name := base64.StdEncoding.EncodeToString(b), filepath.Base(*proof)
		in.ProofBase64, in.ProofFilename = &enc, &name
	}
	key := idempotencyKey([]byte(id.String() + *ref))
	res, err := c.RecordPaymentWithResponse(ctx, id, &client.RecordPaymentParams{IdempotencyKey: &key}, in)
	if err != nil {
		return err
	}
	if res.JSON201 == nil {
		return problemError(res.StatusCode(), res.Body)
	}
	return printJSON(res.Body)
}
