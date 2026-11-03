// Package tenancy owns the tenant directory: placement, stack specs, deployment and bootstrap
// of one NetBird stack per tenant (constitution Principle V, contracts/tenant-stack.md).
package tenancy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// StackSpec is the render contract between the backend and the deployer
// (contracts/tenant-stack.md). Field order is fixed so the hash is deterministic.
// It contains only secret references, never secret values.
type StackSpec struct {
	Tenant           string        `json:"tenant"`
	NetbirdVersion   string        `json:"netbirdVersion"`
	ControlHost      string        `json:"controlHost"`
	ManagementDomain string        `json:"managementDomain"`
	HTTPPort         int           `json:"httpPort"`
	Database         SpecDatabase  `json:"database"`
	Relay            SpecRelay     `json:"relay"`
	RoutingPeers     []RoutingPeer `json:"routingPeers"`
	Hash             string        `json:"hash,omitempty"`
}

// SpecDatabase points the stack at its own database.
type SpecDatabase struct {
	Host        string `json:"host"`
	Name        string `json:"name"`
	User        string `json:"user"`
	PasswordRef string `json:"passwordRef"`
}

// SpecRelay selects the relay mode. "embedded" until Tier 2 adds standalone relays.
type SpecRelay struct {
	Mode string `json:"mode"`
}

// RoutingPeer is one tenant routing-peer container on a gateway host.
type RoutingPeer struct {
	Host        string `json:"host"`
	WGInterface string `json:"wgInterface"`
	WGPort      int    `json:"wgPort"`
	Metric      int    `json:"metric"`
	SetupKeyRef string `json:"setupKeyRef"`
}

// Seal computes Hash over the canonical JSON of the spec without its hash and returns the
// final JSON document.
func (s StackSpec) Seal() (StackSpec, []byte, error) {
	s.Hash = ""
	if s.RoutingPeers == nil {
		s.RoutingPeers = []RoutingPeer{}
	}
	body, err := json.Marshal(s)
	if err != nil {
		return s, nil, err
	}
	sum := sha256.Sum256(body)
	s.Hash = "sha256:" + hex.EncodeToString(sum[:])
	out, err := json.MarshalIndent(s, "", "  ")
	return s, out, err
}
