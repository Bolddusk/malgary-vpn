// Package config loads malgaryd settings from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every setting malgaryd needs. Secret values are never stored here, only
// references (secret://...) or DSNs read from the root-only env file.
type Config struct {
	Listen             string // API listen address, e.g. 127.0.0.1:18090
	MetricsListen      string
	DatabaseURL        string // malgary_app
	MigrateDatabaseURL string // malgary_owner; used only to run migrations
	ZitadelIssuer      string // https://id.vpn.malgary.com
	ZitadelKeyRef      string // secret://zitadel-machine-key.json
	BaseDomain         string // vpn.malgary.com
	SecretDir          string // /etc/malgary/secrets
	SpecDir            string // where StackSpecs are written for the deployer
	NetbirdVersion     string // default NetBird version for new stacks
	InviteDelivery     string // "email" (Zitadel sends it) or "link" (returned to the admin), FR-012a
	Deployer           string // "ansible" (dedicated cell) or "compose" (interim host)
	Interim            Interim
	Compose            Compose
}

// Interim holds the limits of the Interim Hosting Exception (feature 002, research I9).
// All of them are off on dedicated cells.
type Interim struct {
	Active       bool
	MaxUsers     int
	Until        time.Time // last day new users may be added
	TestTenants  []string
	TestDomains  []string // email domains that count as test users (FR-015)
	PaidTenants  []string // tenants allowed a non-zero price, as a test invoice (FR-010)
	EgressNote   string
	MemoryBudget int // MB for all Malgary containers (FR-005)
}

// Compose holds host settings for the ComposeDeployer (contracts/compose-deployer.md).
type Compose struct {
	StackDir           string
	TraefikNetwork     string
	TraefikEntry       string
	TraefikResolver    string
	StackDBHost        string
	GatewayNetwork     string
	HostPublicIP       string
	OperatorAllow      []string // CIDRs allowed to open NetBird dashboards
	ProvisionerPassRef string   // secret:// reference to the malgary_provisioner password
}

// FromEnv reads MALGARY_* variables and validates them.
func FromEnv() (Config, error) {
	c := Config{
		Listen:             env("MALGARY_LISTEN", "127.0.0.1:18090"),
		MetricsListen:      env("MALGARY_METRICS_LISTEN", "127.0.0.1:9100"),
		DatabaseURL:        os.Getenv("MALGARY_DATABASE_URL"),
		MigrateDatabaseURL: os.Getenv("MALGARY_MIGRATE_DATABASE_URL"),
		ZitadelIssuer:      strings.TrimSuffix(os.Getenv("MALGARY_ZITADEL_ISSUER"), "/"),
		ZitadelKeyRef:      os.Getenv("MALGARY_ZITADEL_KEY_REF"),
		BaseDomain:         os.Getenv("MALGARY_BASE_DOMAIN"),
		SecretDir:          env("MALGARY_SECRET_DIR", "/etc/malgary/secrets"),
		SpecDir:            env("MALGARY_SPEC_DIR", "/opt/malgary/specs"),
		NetbirdVersion:     env("MALGARY_NETBIRD_VERSION", "0.82.0"),
		InviteDelivery:     env("MALGARY_INVITE_DELIVERY", "email"),
		Deployer:           env("MALGARY_DEPLOYER", "ansible"),
		Compose: Compose{
			StackDir:           env("MALGARY_STACK_DIR", "/opt/malgary/stacks"),
			TraefikNetwork:     env("MALGARY_TRAEFIK_NETWORK", "coolify"),
			TraefikEntry:       env("MALGARY_TRAEFIK_ENTRYPOINT", "https"),
			TraefikResolver:    env("MALGARY_TRAEFIK_CERTRESOLVER", "letsencrypt"),
			StackDBHost:        os.Getenv("MALGARY_STACK_DB_HOST"),
			GatewayNetwork:     env("MALGARY_GW_NETWORK", "malgary-gw"),
			HostPublicIP:       os.Getenv("MALGARY_HOST_PUBLIC_IP"),
			OperatorAllow:      list(os.Getenv("MALGARY_OPERATOR_ALLOWLIST")),
			ProvisionerPassRef: env("MALGARY_PROVISIONER_PASSWORD_REF", "secret://provisioner-password"),
		},
	}
	var errs []error
	c.Interim, errs = interimFromEnv()
	if err := c.Validate(); err != nil {
		errs = append(errs, err)
	}
	return c, errors.Join(errs...)
}

func interimFromEnv() (Interim, []error) {
	var errs []error
	in := Interim{
		TestTenants: list(os.Getenv("MALGARY_INTERIM_TEST_TENANTS")),
		TestDomains: list(strings.ToLower(os.Getenv("MALGARY_INTERIM_TEST_DOMAINS"))),
		PaidTenants: list(env("MALGARY_INTERIM_PAID_TENANTS", "onedge")),
		EgressNote:  os.Getenv("MALGARY_INTERIM_EGRESS_NOTE"),
	}
	var err error
	if in.Active, err = boolEnv("MALGARY_INTERIM", false); err != nil {
		errs = append(errs, err)
	}
	if in.MaxUsers, err = intEnv("MALGARY_INTERIM_MAX_USERS", 10); err != nil {
		errs = append(errs, err)
	}
	if in.MemoryBudget, err = intEnv("MALGARY_MEMORY_BUDGET_MB", 3072); err != nil {
		errs = append(errs, err)
	}
	if v := os.Getenv("MALGARY_INTERIM_UNTIL"); v != "" {
		if in.Until, err = time.Parse(time.DateOnly, v); err != nil {
			errs = append(errs, fmt.Errorf("MALGARY_INTERIM_UNTIL must be YYYY-MM-DD: %w", err))
		}
	}
	return in, errs
}

// Validate reports every missing or invalid setting at once.
func (c Config) Validate() error {
	var errs []error
	req := map[string]string{
		"MALGARY_DATABASE_URL":    c.DatabaseURL,
		"MALGARY_ZITADEL_ISSUER":  c.ZitadelIssuer,
		"MALGARY_ZITADEL_KEY_REF": c.ZitadelKeyRef,
		"MALGARY_BASE_DOMAIN":     c.BaseDomain,
	}
	for k, v := range req {
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", k))
		}
	}
	if c.ZitadelIssuer != "" && !strings.HasPrefix(c.ZitadelIssuer, "https://") {
		errs = append(errs, errors.New("MALGARY_ZITADEL_ISSUER must be https"))
	}
	if c.InviteDelivery != "email" && c.InviteDelivery != "link" {
		errs = append(errs, fmt.Errorf("MALGARY_INVITE_DELIVERY must be email or link, got %q", c.InviteDelivery))
	}
	switch c.Deployer {
	case "ansible":
	case "compose":
		if c.Compose.StackDBHost == "" {
			errs = append(errs, errors.New("MALGARY_STACK_DB_HOST is required with the compose deployer"))
		}
		if c.Compose.HostPublicIP == "" {
			errs = append(errs, errors.New("MALGARY_HOST_PUBLIC_IP is required with the compose deployer"))
		}
	default:
		errs = append(errs, fmt.Errorf("MALGARY_DEPLOYER must be ansible or compose, got %q", c.Deployer))
	}
	if c.Interim.Active {
		if c.Interim.Until.IsZero() {
			errs = append(errs, errors.New("MALGARY_INTERIM_UNTIL is required when MALGARY_INTERIM is on"))
		}
		if c.Interim.MaxUsers < 1 {
			errs = append(errs, errors.New("MALGARY_INTERIM_MAX_USERS must be at least 1"))
		}
	}
	return errors.Join(errs...)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func boolEnv(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def, fmt.Errorf("%s must be true or false: %w", key, err)
	}
	return b, nil
}

func intEnv(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def, fmt.Errorf("%s must be a number: %w", key, err)
	}
	return n, nil
}

// list splits a comma-separated value, dropping blanks.
func list(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
