// Package secrets resolves secret:// references against the control host's secret directory.
// Values are returned to callers but never logged or written anywhere else (Principle II).
package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const scheme = "secret://"

// Resolver reads and writes secrets under Dir.
type Resolver struct {
	Dir string
}

// Ref builds a reference for a relative path, e.g. Ref("tenants/onedge/db").
func Ref(path string) string { return scheme + path }

// Resolve returns the value behind a secret:// reference.
func (r Resolver) Resolve(ref string) ([]byte, error) {
	p, err := r.path(ref)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p) //nolint:gosec // p is confined to Dir by path()
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", ref, errors.Unwrap(err))
	}
	return b, nil
}

// Put stores a value behind a reference with 0600 permissions, creating parent directories.
func (r Resolver) Put(ref string, value []byte) error {
	p, err := r.path(ref)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, value, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Exists reports whether a reference has a stored value.
func (r Resolver) Exists(ref string) bool {
	p, err := r.path(ref)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func (r Resolver) path(ref string) (string, error) {
	if !strings.HasPrefix(ref, scheme) {
		return "", fmt.Errorf("not a secret reference: %q", redact(ref))
	}
	rel := filepath.Clean(strings.TrimPrefix(ref, scheme))
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid secret reference path: %q", rel)
	}
	return filepath.Join(r.Dir, rel), nil
}

// redact keeps error messages from echoing something that might be a raw secret.
func redact(s string) string {
	if len(s) > 12 {
		return s[:12] + "…"
	}
	return s
}
