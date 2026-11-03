package compose

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"malgary/backend/internal/secrets"
	"malgary/backend/internal/tenancy"
)

// Runner runs `docker compose` in a stack directory. Replaced in tests.
type Runner interface {
	Compose(ctx context.Context, dir, project string, args ...string) ([]byte, error)
}

// DockerCLI runs the docker CLI against the host's daemon (socket mounted into malgaryd).
type DockerCLI struct{}

// Compose runs `docker compose -p project -f compose.yaml args...` in dir.
func (DockerCLI) Compose(ctx context.Context, dir, project string, args ...string) ([]byte, error) {
	full := append([]string{"compose", "-p", project, "-f", "compose.yaml"}, args...)
	cmd := exec.CommandContext(ctx, "docker", full...) //nolint:gosec // fixed binary; project and dir come from a validated slug
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.Bytes(), err
}

// Deployer implements tenancy.Deployer with Docker Compose (contracts/compose-deployer.md).
// StackDir must be mounted at the same path in malgaryd and on the host, because the docker
// daemon resolves the compose file's relative bind mounts on the host.
type Deployer struct {
	StackDir         string
	Host             Host
	Memory           Memory
	Secrets          secrets.Resolver
	DB               Provisioner
	DashboardVersion string
	GuardImage       string
	Runner           Runner
	// Health checks the stack's management API; default GET http://nb-<slug>-server/api/instance
	// over the shared Traefik network.
	Health        func(ctx context.Context, slug string) error
	HealthTimeout time.Duration
	// BudgetMB caps the memory limits of all stacks plus PlatformMB (FR-005); 0 disables.
	BudgetMB   int
	PlatformMB int
}

var _ tenancy.Deployer = (*Deployer)(nil)

// Deploy renders the stack, skips work when nothing changed, starts it and rolls back to the
// previous files if the health check fails (obligations 1–4).
func (d *Deployer) Deploy(ctx context.Context, spec tenancy.StackSpec, _ []byte) error {
	if !slugRE.MatchString(spec.Tenant) {
		return fmt.Errorf("invalid tenant slug %q", spec.Tenant)
	}
	sec, err := d.secretsFor(spec)
	if err != nil {
		return err
	}
	if spec.Database.User != spec.Database.Name {
		return fmt.Errorf("tenant %s: database user must equal the database name", spec.Tenant)
	}
	if err := d.DB.Ensure(ctx, spec.Database.Name, sec.DBPassword); err != nil {
		return err
	}
	files, err := Render(spec, d.Host, d.Memory, sec, d.DashboardVersion, d.GuardImage)
	if err != nil {
		return err
	}
	if err := d.checkBudget(spec.Tenant, files["compose.yaml"].Body); err != nil {
		return err
	}
	dir := filepath.Join(d.StackDir, spec.Tenant)
	if unchanged(dir, files) {
		return nil
	}
	// The server reads config.yaml through a bind mount, which compose doesn't track: restart
	// it when the file changes (env files are part of compose's own change detection).
	configChanged := !sameFile(filepath.Join(dir, "config.yaml"), files["config.yaml"].Body)
	hadPrev, err := writeFiles(dir, files)
	if err != nil {
		return err
	}
	project := "nb-" + spec.Tenant
	if err := d.up(ctx, dir, project, spec.Tenant, configChanged && hadPrev); err != nil {
		if !hadPrev {
			return err
		}
		slog.WarnContext(ctx, "stack update failed; rolling back", "tenant", spec.Tenant, "err", err)
		if rerr := restorePrev(dir, files); rerr != nil {
			return errors.Join(err, fmt.Errorf("restore previous files: %w", rerr))
		}
		if rerr := d.up(ctx, dir, project, spec.Tenant, configChanged); rerr != nil {
			return errors.Join(err, fmt.Errorf("previous version also failed: %w", rerr))
		}
		return fmt.Errorf("stack %s rolled back to the previous version: %w", spec.Tenant, err)
	}
	return nil
}

// Remove stops the stack and deletes its volumes, files, database and secrets (FR-017).
func (d *Deployer) Remove(ctx context.Context, slug, dbName string) error {
	if !slugRE.MatchString(slug) {
		return fmt.Errorf("invalid tenant slug %q", slug)
	}
	dir := filepath.Join(d.StackDir, slug)
	if _, err := os.Stat(filepath.Join(dir, "compose.yaml")); err == nil {
		if out, err := d.Runner.Compose(ctx, dir, "nb-"+slug, "down", "-v", "--remove-orphans"); err != nil {
			return fmt.Errorf("compose down %s: %w\n%s", slug, err, tail(out))
		}
	}
	if err := d.DB.Drop(ctx, dbName); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(d.Secrets.Dir, "tenants", slug))
}

func (d *Deployer) up(ctx context.Context, dir, project, slug string, restartServer bool) error {
	if out, err := d.Runner.Compose(ctx, dir, project, "up", "-d", "--remove-orphans"); err != nil {
		return fmt.Errorf("compose up %s: %w\n%s", slug, err, tail(out))
	}
	if restartServer {
		if out, err := d.Runner.Compose(ctx, dir, project, "up", "-d", "--no-deps", "--force-recreate", "netbird-server"); err != nil {
			return fmt.Errorf("restart netbird-server %s: %w\n%s", slug, err, tail(out))
		}
	}
	timeout := d.HealthTimeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	health := d.Health
	if health == nil {
		health = httpHealth
	}
	deadline := time.Now().Add(timeout)
	for {
		err := health(ctx, slug)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("stack %s not healthy after %s: %w", slug, timeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func httpHealth(ctx context.Context, slug string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://nb-"+slug+"-server/api/instance", nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /api/instance: %s", res.Status)
	}
	return nil
}

// secretsFor resolves the stack's secrets, generating the per-stack ones on first use.
func (d *Deployer) secretsFor(spec tenancy.StackSpec) (Secrets, error) {
	var s Secrets
	gen := func(name string, n int) (string, error) {
		ref := secrets.Ref("tenants/" + spec.Tenant + "/" + name)
		if !d.Secrets.Exists(ref) {
			if err := d.Secrets.Put(ref, []byte(randomString(n))); err != nil {
				return "", err
			}
		}
		v, err := d.Secrets.Resolve(ref)
		return string(v), err
	}
	var err error
	if s.RelayAuth, err = gen("relay-auth", 48); err != nil {
		return s, err
	}
	if s.SessionKey, err = gen("session-key", 32); err != nil {
		return s, err
	}
	raw, err := gen("datastore-key", 32)
	if err != nil {
		return s, err
	}
	s.DatastoreKey = base64.StdEncoding.EncodeToString([]byte(raw))
	pw, err := d.Secrets.Resolve(spec.Database.PasswordRef)
	if err != nil {
		return s, fmt.Errorf("tenant %s database password: %w", spec.Tenant, err)
	}
	s.DBPassword = string(pw)
	if len(spec.RoutingPeers) > 0 {
		key, err := d.Secrets.Resolve(spec.RoutingPeers[0].SetupKeyRef)
		if err != nil {
			return s, fmt.Errorf("tenant %s gateway setup key: %w", spec.Tenant, err)
		}
		s.SetupKey = string(bytes.TrimSpace(key))
	}
	return s, nil
}

const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alnum[int(b[i])%len(alnum)]
	}
	return string(b)
}

func mode(f File) os.FileMode {
	if f.Private {
		return 0o600
	}
	return 0o644
}

func sameFile(path string, body []byte) bool {
	cur, err := os.ReadFile(path) //nolint:gosec // path built from a validated slug
	return err == nil && bytes.Equal(cur, body)
}

// unchanged reports whether every rendered file already exists with the same content.
func unchanged(dir string, files map[string]File) bool {
	for name, f := range files {
		cur, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // path built from a validated slug
		if err != nil || !bytes.Equal(cur, f.Body) {
			return false
		}
	}
	return true
}

// writeFiles keeps a .prev copy of each existing file, then writes the new ones. It reports
// whether a previous compose.yaml existed (so a rollback is possible).
func writeFiles(dir string, files map[string]File) (bool, error) {
	// 0755: nginx in the setup container runs as its own user and must read the page.
	if err := os.MkdirAll(filepath.Join(dir, "setup"), 0o755); err != nil { //nolint:gosec // public setup page
		return false, err
	}
	_, statErr := os.Stat(filepath.Join(dir, "compose.yaml"))
	hadPrev := statErr == nil
	for name, f := range files {
		p := filepath.Join(dir, name)
		if cur, err := os.ReadFile(p); err == nil { //nolint:gosec // path built from a validated slug
			if err := os.WriteFile(p+".prev", cur, mode(f)); err != nil { //nolint:gosec // path built from a validated slug
				return hadPrev, err
			}
		}
		if err := os.WriteFile(p, f.Body, mode(f)); err != nil {
			return hadPrev, err
		}
		if err := os.Chmod(p, mode(f)); err != nil { // WriteFile keeps an existing file's mode
			return hadPrev, err
		}
	}
	return hadPrev, nil
}

func restorePrev(dir string, files map[string]File) error {
	for name := range files {
		p := filepath.Join(dir, name)
		prev, err := os.ReadFile(p + ".prev") //nolint:gosec // path built from a validated slug
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(p, prev, mode(files[name])); err != nil { //nolint:gosec // path built from a validated slug
			return err
		}
	}
	return nil
}

func tail(b []byte) []byte {
	if len(b) > 4000 {
		return b[len(b)-4000:]
	}
	return b
}
