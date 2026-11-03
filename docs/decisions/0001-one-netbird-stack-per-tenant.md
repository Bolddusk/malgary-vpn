# ADR-0001: One NetBird stack per tenant on shared hosts

**Status**: Accepted · **Date**: 2026-11-03 · **Source**: [research R1](../../specs/001-tier1-pilot/research.md), constitution v2.0.0 Principle V

## Context

Malgary sells an isolated VPN account to every customer company. Self-hosted NetBird v0.82:

- runs the embedded IdP in the combined server, and the embedded IdP refuses multi-account mode;
- has no API to create accounts (`GET /api/accounts` returns one account);
- keeps its MSP / multi-tenant API cloud-only; the Enterprise licence doesn't add it.

## Decision

Each tenant gets its own NetBird stack (`netbird-server` plus its own PostgreSQL database and
embedded IdP), rendered from the backend's tenant directory and deployed onto shared control
hosts. Each tenant also gets one routing-peer container per gateway host. All routing peers on a
gateway pair leave through the pair's reserved IP.

## Consequences

- Hard isolation, per-tenant SSO connector, per-tenant upgrades and restores.
- More processes to run: deployment must be fully automated (Principle II) and upgrades staged.
- Rejected: one account split by groups (violates Principle I), legacy multi-account mode,
  NetBird Cloud MSP, forking NetBird.
