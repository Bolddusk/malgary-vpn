package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"malgary/backend/internal/client"
)

// usersCmd: malgaryctl users import <tenant-id> <users.csv>
// CSV columns: email,name,role,groups (groups separated by ';'). A header row is optional.
func usersCmd(ctx context.Context, c *apiClient, args []string) error {
	if len(args) < 3 || args[0] != "import" {
		return errors.New("usage: malgaryctl users import <tenant-id> <users.csv>")
	}
	tenant, err := uuid.Parse(args[1])
	if err != nil {
		return errors.New("tenant must be a tenant ID (see `malgaryctl tenant list`)")
	}
	f, err := os.Open(args[2]) //nolint:gosec // the operator names their own CSV file
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	var users []client.UserImport
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if len(rec) == 0 || strings.EqualFold(strings.TrimSpace(rec[0]), "email") {
			continue
		}
		u := client.UserImport{Email: openapi_types.Email(strings.TrimSpace(rec[0]))}
		if len(rec) > 1 && rec[1] != "" {
			n := strings.TrimSpace(rec[1])
			u.Name = &n
		}
		if len(rec) > 2 && rec[2] != "" {
			role := client.UserImportRole(strings.TrimSpace(rec[2]))
			u.Role = &role
		}
		if len(rec) > 3 && rec[3] != "" {
			g := strings.Split(rec[3], ";")
			u.Groups = &g
		}
		users = append(users, u)
	}
	key := idempotencyKey([]byte(fmt.Sprint(tenant, users)))
	res, err := c.ImportUsersWithResponse(ctx, tenant, &client.ImportUsersParams{IdempotencyKey: &key}, client.ImportUsersJSONRequestBody{Users: users})
	if err != nil {
		return err
	}
	if res.JSON200 == nil {
		return problemError(res.StatusCode(), res.Body)
	}
	fmt.Printf("created %d, already present %d, seats used %d of %d\n",
		res.JSON200.Created, res.JSON200.Existing, res.JSON200.SeatsUsed, res.JSON200.SeatsPaid)
	return nil
}

// gatewayCmd: malgaryctl gateway mark-pta-registered <pair> --date YYYY-MM-DD --ref REF
func gatewayCmd(ctx context.Context, c *apiClient, args []string) error {
	if len(args) < 2 || args[0] != "mark-pta-registered" {
		return errors.New("usage: malgaryctl gateway mark-pta-registered <pair> --date YYYY-MM-DD --ref REF")
	}
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)
	date := fs.String("date", "", "registration date (YYYY-MM-DD)")
	ref := fs.String("ref", "", "PTA reference")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	at, err := time.Parse("2006-01-02", *date)
	if err != nil || *ref == "" {
		return errors.New("--date YYYY-MM-DD and --ref are required")
	}
	res, err := c.MarkGatewayPairPTAWithResponse(ctx, args[1], client.MarkGatewayPairPTAJSONRequestBody{RegisteredAt: at, Reference: *ref})
	if err != nil {
		return err
	}
	if res.StatusCode() != 200 {
		return problemError(res.StatusCode(), res.Body)
	}
	fmt.Printf("%s marked as PTA-registered (%s)\n", args[1], *ref)
	return nil
}
