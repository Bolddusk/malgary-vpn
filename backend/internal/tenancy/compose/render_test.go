package compose

import (
	"bytes"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"malgary/backend/internal/tenancy"
)

var testHost = Host{
	TraefikNetwork: "coolify", TraefikEntry: "https", TraefikResolver: "letsencrypt",
	GatewayNetwork: "malgary-gw", HostPublicIP: "178.104.48.140", OperatorAllow: []string{"203.0.113.7/32"},
	EgressNote: "Temporary test IP; do not allowlist",
}

var testSecrets = Secrets{RelayAuth: "RELAYSECRET", SessionKey: "SESSIONSECRET", DatastoreKey: "DATASTORESECRET", DBPassword: "DBSECRET", SetupKey: "SETUPKEYSECRET"}

func testSpec(withPeer bool) tenancy.StackSpec {
	s := tenancy.StackSpec{
		Tenant: "testco", NetbirdVersion: "0.82.0", ControlHost: "ctl.i01", ManagementDomain: "testco.vpn.malgary.com",
		HTTPPort: 20001, Database: tenancy.SpecDatabase{Host: "pg", Name: "nb_testco", User: "nb_testco", PasswordRef: "secret://tenants/testco/db"},
		Relay: tenancy.SpecRelay{Mode: "embedded"},
	}
	if withPeer {
		s.RoutingPeers = []tenancy.RoutingPeer{{Host: "gw.i01", WGInterface: "wt-testco", WGPort: 51822, Metric: 100, SetupKeyRef: "secret://tenants/testco/gw"}}
	}
	sealed, _, _ := s.Seal()
	return sealed
}

type composeFile struct {
	Services map[string]struct {
		Image       string            `json:"image"`
		Restart     string            `json:"restart"`
		MemLimit    string            `json:"mem_limit"`
		NetworkMode string            `json:"network_mode"`
		Privileged  bool              `json:"privileged"`
		Networks    any               `json:"networks"`
		Ports       []string          `json:"ports"`
		CapAdd      []string          `json:"cap_add"`
		Labels      map[string]string `json:"labels"`
		Environment map[string]string `json:"environment"`
	} `json:"services"`
	Networks map[string]struct {
		Name     string `json:"name"`
		External bool   `json:"external"`
	} `json:"networks"`
}

func render(t *testing.T, spec tenancy.StackSpec, host Host) (map[string]File, composeFile) {
	t.Helper()
	files, err := Render(spec, host, DefaultMemory, testSecrets, "v2.40.0", "malgary/gw-guard:local")
	if err != nil {
		t.Fatal(err)
	}
	var c composeFile
	if err := yaml.Unmarshal(files["compose.yaml"].Body, &c); err != nil {
		t.Fatalf("compose.yaml is not valid YAML: %v\n%s", err, files["compose.yaml"].Body)
	}
	return files, c
}

func TestRenderDeterministic(t *testing.T) {
	a, _ := render(t, testSpec(true), testHost)
	b, _ := render(t, testSpec(true), testHost)
	for name := range a {
		if !bytes.Equal(a[name].Body, b[name].Body) {
			t.Errorf("%s differs between two renders of the same spec", name)
		}
	}
}

func TestRenderSecretsOnlyInPrivateFiles(t *testing.T) {
	files, _ := render(t, testSpec(true), testHost)
	for _, secret := range []string{"RELAYSECRET", "SESSIONSECRET", "DATASTORESECRET", "DBSECRET", "SETUPKEYSECRET"} {
		found := false
		for name, f := range files {
			if bytes.Contains(f.Body, []byte(secret)) {
				found = true
				if !f.Private {
					t.Errorf("%s appears in %s, which is not private", secret, name)
				}
			}
		}
		if !found {
			t.Errorf("%s not rendered anywhere", secret)
		}
	}
}

// TestRenderSafety also guards US2 and US3: limits everywhere, no host networking, gateway
// only on the isolated network.
func TestRenderSafety(t *testing.T) {
	_, c := render(t, testSpec(true), testHost)
	want := []string{"netbird-server", "setup", "dashboard", "routing-peer", "guard"}
	if len(c.Services) != len(want) {
		t.Fatalf("services: %v", keys(c.Services))
	}
	for _, name := range want {
		s, ok := c.Services[name]
		if !ok {
			t.Fatalf("missing service %s", name)
		}
		if s.MemLimit == "" {
			t.Errorf("%s has no memory limit", name)
		}
		if s.Restart != "unless-stopped" {
			t.Errorf("%s restart = %q", name, s.Restart)
		}
		if s.NetworkMode == "host" || s.Privileged {
			t.Errorf("%s uses host networking or privileged mode", name)
		}
		if s.Labels["com.malgary.managed"] != "true" || s.Labels["com.malgary.tenant"] != "testco" {
			t.Errorf("%s lacks Malgary labels: %v", name, s.Labels)
		}
	}
	rp := c.Services["routing-peer"]
	if nets, _ := rp.Networks.([]any); len(nets) != 1 || nets[0] != "gateway" {
		t.Errorf("routing peer networks: %v", rp.Networks)
	}
	if c.Networks["gateway"].Name != "malgary-gw" || !c.Networks["gateway"].External {
		t.Errorf("gateway network: %+v", c.Networks["gateway"])
	}
	if strings.Join(rp.Ports, ",") != "51822:51822/udp" || rp.Environment["NB_WIREGUARD_PORT"] != "51822" || rp.Environment["NB_HOSTNAME"] != "gw.i01" {
		t.Errorf("routing peer port: %v %v", rp.Ports, rp.Environment)
	}
	if g := c.Services["guard"]; g.NetworkMode != "service:routing-peer" || g.Environment["GUARD_HOST_IP"] != "178.104.48.140" {
		t.Errorf("guard: %+v", g)
	}
	if p := c.Services["netbird-server"].Ports; strings.Join(p, ",") != "3479:3479/udp" {
		t.Errorf("STUN port: %v", p)
	}
	for _, s := range []string{"netbird-server", "setup", "dashboard"} {
		l := c.Services[s].Labels
		if l["traefik.enable"] != "true" || l["traefik.docker.network"] != "coolify" {
			t.Errorf("%s Traefik labels: %v", s, l)
		}
	}
	if r := c.Services["netbird-server"].Labels["traefik.http.routers.nb-testco-grpc.tls.certresolver"]; r != "letsencrypt" {
		t.Errorf("cert resolver: %q", r)
	}
	if a := c.Services["dashboard"].Labels["traefik.http.middlewares.nb-testco-operators.ipallowlist.sourcerange"]; a != "203.0.113.7/32" {
		t.Errorf("dashboard allowlist: %q", a)
	}
}

func TestRenderWithoutPeerOrAllowlist(t *testing.T) {
	h := testHost
	h.OperatorAllow = nil
	files, c := render(t, testSpec(false), h)
	if _, ok := c.Services["dashboard"]; ok {
		t.Error("the dashboard must not be routed without an operator allowlist")
	}
	if _, ok := c.Services["routing-peer"]; ok {
		t.Error("no routing peer before bootstrap")
	}
	if _, ok := files["peer.env"]; ok {
		t.Error("peer.env without a peer")
	}
	if MemoryOf(files["compose.yaml"].Body) != DefaultMemory.Total(false, false) {
		t.Errorf("memory %d", MemoryOf(files["compose.yaml"].Body))
	}
}

func TestRenderSetupPage(t *testing.T) {
	files, _ := render(t, testSpec(true), testHost)
	page := string(files["setup/index.html"].Body)
	for _, want := range []string{"https://testco.vpn.malgary.com", "178.104.48.140", "Temporary test IP; do not allowlist"} {
		if !strings.Contains(page, want) {
			t.Errorf("setup page lacks %q", want)
		}
	}
}

func TestRenderRejects(t *testing.T) {
	s := testSpec(true)
	s.Tenant = "../etc"
	if _, err := Render(s, testHost, DefaultMemory, testSecrets, "v", "g"); err == nil {
		t.Error("invalid slug accepted")
	}
	s = testSpec(true)
	s.RoutingPeers = append(s.RoutingPeers, s.RoutingPeers[0])
	if _, err := Render(s, testHost, DefaultMemory, testSecrets, "v", "g"); err == nil {
		t.Error("two routing peers accepted on the interim host")
	}
	sec := testSecrets
	sec.SetupKey = ""
	if _, err := Render(testSpec(true), testHost, DefaultMemory, sec, "v", "g"); err == nil {
		t.Error("routing peer without setup key accepted")
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
