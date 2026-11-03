// Package compose deploys tenant stacks as Docker Compose projects on a host shared with other
// applications (feature 002, contracts/compose-deployer.md). It is the second implementation
// of tenancy.Deployer and consumes the same StackSpec as the Ansible deployer.
package compose

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"regexp"
	"strings"
	"text/template"

	"malgary/backend/internal/tenancy"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var (
	textTmpl = template.Must(template.New("").Funcs(template.FuncMap{"join": strings.Join}).ParseFS(templateFS,
		"templates/compose.yaml.tmpl", "templates/config.yaml.tmpl", "templates/dashboard.env.tmpl", "templates/peer.env.tmpl"))
	setupTmpl = htmltemplate.Must(htmltemplate.ParseFS(templateFS, "templates/setup.html.tmpl"))
)

// slugRE matches tenant slugs (migration 0001); checked before any path is built from one.
var slugRE = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}[a-z0-9]$`)

// Host holds the shared host's settings (config.Compose).
type Host struct {
	TraefikNetwork  string
	TraefikEntry    string
	TraefikResolver string
	GatewayNetwork  string
	HostPublicIP    string
	OperatorAllow   []string
	EgressNote      string
}

// Memory limits in MB per component (research I11).
type Memory struct {
	Server, Dashboard, Setup, Peer, Guard int
}

// DefaultMemory is the interim budget per tenant: 256 + 64 + 16 + 96 + 32 MB.
var DefaultMemory = Memory{Server: 256, Dashboard: 64, Setup: 16, Peer: 96, Guard: 32}

// Total is the memory a rendered stack may use.
func (m Memory) Total(withDashboard, withPeer bool) int {
	t := m.Server + m.Setup
	if withDashboard {
		t += m.Dashboard
	}
	if withPeer {
		t += m.Peer + m.Guard
	}
	return t
}

// Secrets are the values rendered into private files. Never logged.
type Secrets struct {
	RelayAuth, SessionKey, DatastoreKey, DBPassword, SetupKey string
}

type data struct {
	Spec             tenancy.StackSpec
	Slug             string
	StunPort         int
	Host             Host
	Memory           Memory
	Secrets          Secrets
	Peer             *tenancy.RoutingPeer
	DashboardVersion string
	GuardImage       string
	EgressIP         string
	EgressNote       string
}

// File is one rendered file and its mode. Private files hold secrets.
type File struct {
	Body    []byte
	Private bool
}

// StunPort derives the stack's STUN port from its HTTP port, so stacks on one host never
// collide (3478 for the first stack, research I5).
func StunPort(httpPort int) int { return 3478 + httpPort - 20000 }

// Render produces every file of a tenant's stack directory. The output depends only on its
// inputs, so an unchanged spec renders byte-for-byte the same files (obligation 1).
func Render(spec tenancy.StackSpec, host Host, mem Memory, sec Secrets, dashboardVersion, guardImage string) (map[string]File, error) {
	if !slugRE.MatchString(spec.Tenant) {
		return nil, fmt.Errorf("invalid tenant slug %q", spec.Tenant)
	}
	if len(spec.RoutingPeers) > 1 {
		return nil, fmt.Errorf("the interim host runs one routing peer per tenant, spec has %d", len(spec.RoutingPeers))
	}
	d := data{
		Spec: spec, Slug: spec.Tenant, StunPort: StunPort(spec.HTTPPort), Host: host, Memory: mem, Secrets: sec,
		DashboardVersion: dashboardVersion, GuardImage: guardImage, EgressIP: host.HostPublicIP, EgressNote: host.EgressNote,
	}
	if len(spec.RoutingPeers) == 1 {
		p := spec.RoutingPeers[0]
		d.Peer = &p
		if sec.SetupKey == "" {
			return nil, fmt.Errorf("tenant %s: routing peer without a setup key", spec.Tenant)
		}
	}
	out := map[string]File{}
	for name, private := range map[string]bool{"compose.yaml": false, "config.yaml": true, "dashboard.env": false, "peer.env": true} {
		if name == "peer.env" && d.Peer == nil {
			continue
		}
		var b bytes.Buffer
		if err := textTmpl.ExecuteTemplate(&b, name+".tmpl", d); err != nil {
			return nil, fmt.Errorf("render %s: %w", name, err)
		}
		out[name] = File{Body: b.Bytes(), Private: private}
	}
	var b bytes.Buffer
	if err := setupTmpl.ExecuteTemplate(&b, "setup.html.tmpl", d); err != nil {
		return nil, fmt.Errorf("render setup page: %w", err)
	}
	out["setup/index.html"] = File{Body: b.Bytes()}
	return out, nil
}
