#!/usr/bin/env bash
# Compare two host snapshots from interim-site.yml (feature 002, T037, quickstart V3). Passes
# when the only differences are Docker's own rules for Malgary's published ports and networks.
# Usage: infra/tools/interim-diff.sh <before.txt> <after.txt>
set -euo pipefail
before=$1 after=$2
# Lines Docker adds for Malgary: the gateway bridge, published UDP ports 3478-3480 and
# 51821-51830, and the probe network.
allowed='br-malgary-gw|10\.250\.0\.|dport (347[89]|3480|518(2[1-9]|30))|udp dpt:(347[89]|3480|518(2[1-9]|30))|192\.0\.2\.'
changes=$(diff <(sort "$before") <(sort "$after") | grep -E '^[<>]' || true)
unexpected=$(printf '%s\n' "$changes" | grep -vE "$allowed" | grep -vE '^[<>] *$' || true)
# Container status lines for Malgary's own containers are excluded by the snapshot itself.
if [ -n "$unexpected" ]; then
  echo "FAIL: changes outside Malgary's Docker rules:"
  printf '%s\n' "$unexpected"
  exit 1
fi
echo "ok: $(printf '%s\n' "$changes" | grep -c . || true) changed lines, all Docker rules for Malgary"
