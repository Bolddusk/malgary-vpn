# ADR-0002: Run the pilot on the shared Coolify server until the cloud cell exists

**Status**: Accepted · **Date**: 2026-11-03 · **Source**: [feature 002 research](../../specs/002-interim-pilot-178/research.md), constitution v2.2.0 Interim Hosting Exception

## Context

The DigitalOcean cell (feature 001) isn't funded yet, and testing should start now. The owner
already runs a Hetzner server (178.104.48.140) with Coolify, its Traefik proxy, a shared
PostgreSQL 18 and about 48 other applications.

## Decision

Run the same platform on that server for at most 10 test users, until 2027-02-28:

- tenant stacks are Docker Compose projects rendered by a second `Deployer` implementation
  (`internal/tenancy/compose`), routed by Coolify's Traefik through labels; Coolify itself and
  the host firewall are not touched;
- tenant databases live in the shared PostgreSQL, created by a login that has only CREATEDB and
  CREATEROLE;
- each tenant's gateway runs in an isolated bridge network with a guard sidecar that drops
  forwarded traffic to private ranges and the host, and users can connect only after an
  automated isolation test passes for the stack's current configuration;
- backups and email are deferred to the dedicated servers; real company users are refused until
  a restore test has passed.

## Consequences

- No standby gateway and a temporary egress IP (exempted by the constitution); tenants are told
  not to allowlist it.
- malgaryd holds the Docker socket on this host (root-equivalent); the dedicated cell goes back
  to the Ansible deployer with a sudo wrapper.
- Leaving is a restore plus three DNS changes ([interim-exit runbook](../runbooks/interim-exit.md));
  clients keep their addresses.
