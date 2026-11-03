package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"malgary/backend/internal/httpapi/gen"
)

// ProbeResult is the probe container's JSON output.
type ProbeResult struct {
	Passed   bool                 `json:"passed"`
	EgressIP string               `json:"egressIp"`
	Error    string               `json:"error,omitempty"`
	Results  []gen.IsolationProbe `json:"results"`
}

// Prober runs the isolation probe image as a throwaway container on its own bridge network
// (feature 002, research I10). The network uses a documentation range so that every private
// target is reached through the VPN, never directly.
type Prober struct {
	Image   string // malgary/isolation-probe:local
	HostIP  string // expected egress IP and a blocked target
	DBHost  string // shared PostgreSQL container name; its address is a blocked target
	Network string // the shared Traefik network; its bridge address is a blocked target
	TmpDir  string // for the 0600 env file holding the setup key
	Timeout time.Duration
}

// Targets lists what a VPN user must never reach: the host's Docker gateways and the shared
// database (all of which answer if packets get through), plus cloud metadata and private ranges.
func (p Prober) Targets(ctx context.Context) []string {
	t := []string{
		"10.250.0.1:22",        // the gateway network's bridge, i.e. the host
		"172.17.0.1:22",        // docker0, i.e. the host
		"169.254.169.254:80",   // Hetzner metadata service
		"192.168.0.1:80",       // a typical private address
		"10.0.0.1:80",          // ditto
		p.HostIP + ":5432",     // database port on the host's public IP
	}
	if p.Network != "" {
		if gw := dockerInspect(ctx, "network", p.Network, "{{(index .IPAM.Config 0).Gateway}}"); gw != "" {
			t = append(t, gw+":22") // the Traefik network's bridge, i.e. the host
		}
	}
	if p.DBHost != "" {
		if ip := dockerInspect(ctx, "container", p.DBHost, "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}"); ip != "" {
			t = append(t, strings.Fields(ip)[0]+":5432") // the shared PostgreSQL
		}
	}
	return t
}

// Run starts the probe for one tenant and returns its parsed result.
func (p Prober) Run(ctx context.Context, slug, managementURL, setupKey string) (ProbeResult, error) {
	if !slugRE.MatchString(slug) {
		return ProbeResult{}, fmt.Errorf("invalid tenant slug %q", slug)
	}
	timeout := p.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name := fmt.Sprintf("malgary-iso-%s-%d", slug, time.Now().Unix())
	envFile := filepath.Join(p.TmpDir, name+".env")
	env := "NB_SETUP_KEY=" + setupKey + "\n" +
		"NB_MANAGEMENT_URL=" + managementURL + "\n" +
		"PROBE_BLOCKED=" + strings.Join(p.Targets(ctx), " ") + "\n" +
		"PROBE_REACHABLE=https://1.1.1.1 https://www.google.com\n" +
		"PROBE_EGRESS_EXPECT=" + p.HostIP + "\n"
	if err := os.WriteFile(envFile, []byte(env), 0o600); err != nil {
		return ProbeResult{}, err
	}
	defer func() { _ = os.Remove(envFile) }()

	// 192.0.2.0/28 (TEST-NET-1) overlaps nothing private, so no target is "directly connected".
	if out, err := docker(ctx, "network", "create", "--driver", "bridge", "--subnet", "192.0.2.0/28",
		"--label", "com.malgary.managed=true", name); err != nil {
		return ProbeResult{}, fmt.Errorf("probe network: %w\n%s", err, out)
	}
	defer func() { _, _ = docker(context.WithoutCancel(ctx), "network", "rm", name) }()

	out, err := docker(ctx, "run", "--rm", "--name", name, "--network", name,
		"--cap-add", "NET_ADMIN", "--device", "/dev/net/tun", "--memory", "128m",
		"--label", "com.malgary.managed=true", "--env-file", envFile, p.Image)
	var res ProbeResult
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if jerr := json.Unmarshal([]byte(lines[len(lines)-1]), &res); jerr != nil {
		return ProbeResult{}, fmt.Errorf("probe output: %v (run error: %v)\n%s", jerr, err, tail(out))
	}
	if res.Error != "" {
		return res, fmt.Errorf("probe: %s", res.Error)
	}
	return res, nil
}

func docker(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // fixed binary, arguments built from validated values
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		return append(stdout.Bytes(), stderr.Bytes()...), err
	}
	return stdout.Bytes(), nil
}

func dockerInspect(ctx context.Context, kind, name, format string) string {
	out, err := docker(ctx, kind, "inspect", "--format", format, name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
