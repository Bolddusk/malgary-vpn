#!/bin/sh
# Tests the guard's nftables rules (feature 002 T040). Needs Docker: it loads the rules into a
# throwaway network namespace with nft --check, then asserts what they block.
set -eu
cd "$(dirname "$0")"
rules=$(python3 guard.py --print wt-test,178.104.48.140)

fail() { echo "FAIL: $*" >&2; exit 1; }
for cidr in 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 100.64.0.0/10 169.254.0.0/16 127.0.0.0/8 224.0.0.0/4 178.104.48.140/32; do
  echo "$rules" | grep -q "$cidr" || fail "rules do not block $cidr"
done
echo "$rules" | grep -q 'iifname "wt-test" ip daddr @blocked_v4 counter drop' || fail "forward drop rule missing"
echo "$rules" | grep -q 'iifname "wt-test" meta nfproto ipv6 counter drop' || fail "IPv6 drop rule missing"
echo "$rules" | grep -q 'hook forward' || fail "rules must be on the forward hook only"
if echo "$rules" | grep -Eq 'hook (input|output|prerouting|postrouting)'; then fail "rules touch more than forward"; fi
echo "$rules" | grep -q '^table inet malgary_guard' || fail "wrong table"
if python3 guard.py --print 'wt;rm,178.104.48.140' >/dev/null 2>&1; then fail "bad interface name accepted"; fi
if python3 guard.py --print 'wt-test,not-an-ip' >/dev/null 2>&1; then fail "bad host IP accepted"; fi

if command -v docker >/dev/null 2>&1; then
  echo "$rules" | docker run --rm -i --cap-add NET_ADMIN alpine:3.22 sh -c 'apk add -q --no-cache nftables >/dev/null && nft -c -f -' \
    || fail "nft rejected the rules"
fi
echo "guard rules: ok"
