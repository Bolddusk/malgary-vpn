// Package config loads malgaryd settings from the environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
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
	}
	return c, c.Validate()
}

// Validate reports every missing required setting at once.
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
	return errors.Join(errs...)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
