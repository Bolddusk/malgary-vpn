// Command malgaryctl is the owner and operator CLI for the Malgary backend API.
// It signs in with the OIDC device flow against Malgary sign-in (Zitadel) and calls the same
// API the portal will use (contracts/backend-api.md).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
)

const usage = `malgaryctl — Malgary VPN owner/operator CLI

Usage:
  malgaryctl login
  malgaryctl tenant apply -f <tenant.yaml>
  malgaryctl tenant get <slug-or-id>
  malgaryctl tenant list
  malgaryctl users import <tenant-id> <users.csv>   (email,name,role,groups)
  malgaryctl plan apply -f <plan.yaml>
  malgaryctl plan list
  malgaryctl plan publish <plan-code> <version>
  malgaryctl subscription change <tenant-id> [--quantity N] [--plan CODE --version V] --reason TEXT
  malgaryctl invoice draft <tenant-id> --period YYYY-MM-01
  malgaryctl invoice issue <invoice-id>
  malgaryctl payment record <invoice-id> --amount 4000.00 --currency USD --reference REF [--proof FILE]
  malgaryctl gateway mark-pta-registered <pair> --date YYYY-MM-DD --ref REF
  malgaryctl stack reconcile <tenant>
  malgaryctl isolation-test <tenant> [--history]     (interim host only)

Environment:
  MALGARY_API      API base URL (default https://api.vpn.malgary.com/v1)
  MALGARY_ISSUER   Sign-in issuer (default https://id.vpn.malgary.com)
  MALGARY_CLIENT   OIDC client ID of the malgaryctl app in Zitadel
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(usage)
		return nil
	}
	cfg := loadConfig()
	switch args[0] {
	case "login":
		return login(ctx, cfg)
	}
	c, err := newClient(ctx, cfg)
	if err != nil {
		return err
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
	return cmd(ctx, c, args[1:])
}

// commands maps top-level nouns to handlers. Each lives in its own file.
var commands = map[string]func(context.Context, *apiClient, []string) error{
	"tenant":         tenantCmd,
	"users":          usersCmd,
	"plan":           planCmd,
	"subscription":   subscriptionCmd,
	"invoice":        invoiceCmd,
	"payment":        paymentCmd,
	"gateway":        gatewayCmd,
	"isolation-test": isolationCmd,
	"stack":          notYet("stack"),
}

func notYet(name string) func(context.Context, *apiClient, []string) error {
	return func(context.Context, *apiClient, []string) error {
		return errors.New(name + ": not implemented yet (see tasks.md)")
	}
}
