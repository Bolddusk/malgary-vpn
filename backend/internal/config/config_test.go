package config

import (
	"strings"
	"testing"
	"time"
)

func setBase(t *testing.T) {
	t.Helper()
	t.Setenv("MALGARY_DATABASE_URL", "postgres://x")
	t.Setenv("MALGARY_ZITADEL_ISSUER", "https://id.vpn.malgary.com")
	t.Setenv("MALGARY_ZITADEL_KEY_REF", "secret://k.json")
	t.Setenv("MALGARY_BASE_DOMAIN", "vpn.malgary.com")
}

func TestDefaults(t *testing.T) {
	setBase(t)
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.Deployer != "ansible" || c.InviteDelivery != "email" {
		t.Fatalf("defaults: deployer=%q invite=%q", c.Deployer, c.InviteDelivery)
	}
	if c.Interim.Active || c.Interim.MaxUsers != 10 || c.Interim.MemoryBudget != 3072 {
		t.Fatalf("interim defaults: %+v", c.Interim)
	}
	if c.Compose.TraefikNetwork != "coolify" || c.Compose.GatewayNetwork != "malgary-gw" {
		t.Fatalf("compose defaults: %+v", c.Compose)
	}
}

func TestInterim(t *testing.T) {
	setBase(t)
	t.Setenv("MALGARY_INTERIM", "true")
	t.Setenv("MALGARY_INTERIM_UNTIL", "2027-02-28")
	t.Setenv("MALGARY_INTERIM_TEST_TENANTS", "onedge, testco,,malgary-ops")
	t.Setenv("MALGARY_INTERIM_TEST_DOMAINS", "Gmail.com")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Interim.Until.Equal(time.Date(2027, 2, 28, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("until: %v", c.Interim.Until)
	}
	if strings.Join(c.Interim.TestTenants, "|") != "onedge|testco|malgary-ops" {
		t.Fatalf("tenants: %v", c.Interim.TestTenants)
	}
	if c.Interim.TestDomains[0] != "gmail.com" {
		t.Fatalf("domains not lower-cased: %v", c.Interim.TestDomains)
	}
}

func TestInvalid(t *testing.T) {
	cases := map[string]map[string]string{
		"interim without end date": {"MALGARY_INTERIM": "true"},
		"bad end date":             {"MALGARY_INTERIM_UNTIL": "28/02/2027"},
		"bad bool":                 {"MALGARY_INTERIM": "maybe"},
		"bad invite delivery":      {"MALGARY_INVITE_DELIVERY": "sms"},
		"bad deployer":             {"MALGARY_DEPLOYER": "nomad"},
		"compose without db host":  {"MALGARY_DEPLOYER": "compose", "MALGARY_HOST_PUBLIC_IP": "1.2.3.4"},
		"compose without host ip":  {"MALGARY_DEPLOYER": "compose", "MALGARY_STACK_DB_HOST": "pg"},
		"zero max users":           {"MALGARY_INTERIM": "true", "MALGARY_INTERIM_UNTIL": "2027-02-28", "MALGARY_INTERIM_MAX_USERS": "0"},
	}
	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			setBase(t)
			for k, v := range vars {
				t.Setenv(k, v)
			}
			if _, err := FromEnv(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
