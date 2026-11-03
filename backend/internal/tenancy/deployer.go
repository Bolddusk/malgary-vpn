package tenancy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Deployer applies a sealed StackSpec to hosts. The pilot uses Ansible; Nomad or Kubernetes
// can replace it later without changing the tenant directory (research R7).
type Deployer interface {
	Deploy(ctx context.Context, spec StackSpec, doc []byte) error
}

// AnsibleDeployer writes the spec to SpecDir and runs the tenant-stack playbook through the
// root-owned wrapper /usr/local/sbin/malgary-deploy, the only command malgaryd may run with
// sudo. Tenant secrets are read on the control host from the secret directory, so no operator
// key is needed.
type AnsibleDeployer struct {
	SpecDir string
	Timeout time.Duration
}

// Deploy is idempotent: the roles skip work when the rendered files are unchanged
// (contracts/tenant-stack.md obligation 6).
func (d AnsibleDeployer) Deploy(ctx context.Context, spec StackSpec, doc []byte) error {
	if err := os.MkdirAll(d.SpecDir, 0o750); err != nil {
		return err
	}
	path := filepath.Join(d.SpecDir, spec.Tenant+".json")
	if err := os.WriteFile(path, doc, 0o600); err != nil {
		return err
	}
	timeout := d.Timeout
	if timeout == 0 {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// The wrapper re-validates that path is under /opt/malgary/specs.
	cmd := exec.CommandContext(ctx, "sudo", "-n", "/usr/local/sbin/malgary-deploy", path) //nolint:gosec // fixed binary, path built from a validated slug
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("deploy %s: %w\n%s", spec.Tenant, err, tail(out.Bytes(), 4000))
	}
	return nil
}

func tail(b []byte, n int) []byte {
	if len(b) > n {
		return b[len(b)-n:]
	}
	return b
}
