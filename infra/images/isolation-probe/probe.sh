#!/bin/sh
# Isolation probe (feature 002, FR-009). Joins the tenant's stack as a member of the
# "isolation-test" group, which routes every IPv4 destination through the tenant's gateway,
# then checks from inside the VPN:
#   - every PROBE_BLOCKED target (host:port) must NOT be reachable;
#   - every PROBE_REACHABLE URL must be reachable;
#   - the public egress IP must equal PROBE_EGRESS_EXPECT.
# A TCP reset or any answer counts as reachable: only a silent drop counts as blocked, so a
# missing guard can't pass. Prints one JSON line on stdout; logs go to stderr.
set -u
log() { echo "probe: $*" >&2; }

: "${NB_SETUP_KEY:?}" "${NB_MANAGEMENT_URL:?}"
IFACE=${NB_INTERFACE_NAME:-wt0}

netbird up --foreground-mode --setup-key "$NB_SETUP_KEY" --management-url "$NB_MANAGEMENT_URL" \
  --hostname "isolation-probe" --interface-name "$IFACE" --log-file console >/tmp/netbird.log 2>&1 &
unset NB_SETUP_KEY

# Wait until traffic to a public address leaves through the VPN interface.
i=0
until ip route get 1.1.1.1 2>/dev/null | grep -q "dev $IFACE"; do
  i=$((i + 1))
  if [ "$i" -gt 90 ]; then
    log "VPN route never came up"; tail -n 30 /tmp/netbird.log >&2
    echo '{"passed":false,"error":"vpn route not up","results":[]}'
    exit 2
  fi
  sleep 2
done
log "VPN route is up after ${i} checks"
sleep 3

results=""
passed=true
add() { # target expected got
  [ -n "$results" ] && results="$results,"
  results="$results{\"target\":\"$1\",\"expected\":\"$2\",\"got\":\"$3\"}"
  [ "$2" = "$3" ] || passed=false
  log "$1: expected $2, got $3"
}

for t in ${PROBE_BLOCKED:-}; do
  # curl exit 28 = timed out (silently dropped). Anything else means packets got through.
  curl -s -o /dev/null --connect-timeout 4 "telnet://$t" </dev/null >/dev/null 2>&1
  rc=$?
  if [ "$rc" -eq 28 ]; then add "$t" blocked blocked; else add "$t" blocked reachable; fi
done

for u in ${PROBE_REACHABLE:-}; do
  if curl -s -o /dev/null --max-time 10 "$u"; then add "$u" reachable reachable; else add "$u" reachable blocked; fi
done

egress=$(curl -s --max-time 10 https://api.ipify.org || true)
if [ -n "${PROBE_EGRESS_EXPECT:-}" ]; then
  if [ "$egress" = "$PROBE_EGRESS_EXPECT" ]; then add "egress=$PROBE_EGRESS_EXPECT" reachable reachable
  else add "egress=$PROBE_EGRESS_EXPECT" reachable blocked; fi
fi

echo "{\"passed\":$passed,\"egressIp\":\"$egress\",\"results\":[$results]}"
netbird down >/dev/null 2>&1 || true
