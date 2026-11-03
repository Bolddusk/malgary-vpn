#!/usr/bin/env python3
"""Guard for a tenant routing peer on the interim host (feature 002, research I4).

Runs in the routing peer's network namespace. It installs the nftables table
`inet malgary_guard`, which drops traffic that VPN users send *through* the gateway (the
forward path) to private ranges, the Docker networks and the host itself, so they reach the
public internet only (FR-008). The peer's own traffic (to its management server) is not
forwarded, so it is unaffected. The table is re-applied every 30 seconds in case NetBird or a
restart changes the ruleset; each re-apply is counted.

It also serves per-peer WireGuard byte counters on :9586 (research R9), counting bytes only.

Environment: GUARD_WG_IFACE (the tenant's WireGuard interface), GUARD_HOST_IP (the host's
public IP), GUARD_TENANT (label), GUARD_INTERVAL (seconds, default 30).
"""
import http.server
import ipaddress
import os
import re
import subprocess
import sys
import threading
import time

TABLE = "malgary_guard"

# Everything a VPN user must never reach through the gateway.
BLOCKED_V4 = [
    "0.0.0.0/8",        # "this" network
    "10.0.0.0/8",       # private, includes the malgary-gw network 10.250.0.0/24
    "100.64.0.0/10",    # carrier-grade NAT, NetBird overlay addresses
    "127.0.0.0/8",      # loopback
    "169.254.0.0/16",   # link-local, includes cloud metadata 169.254.169.254
    "172.16.0.0/12",    # private, includes Docker's default networks
    "192.0.0.0/24",     # IETF protocol assignments
    "192.168.0.0/16",   # private
    "198.18.0.0/15",    # benchmarking
    "224.0.0.0/4",      # multicast
    "240.0.0.0/4",      # reserved, includes broadcast
]

state = {"reapplied": 0, "last_ok": 0.0, "expected": None}


def ruleset(iface: str, host_ip: str) -> str:
    """The nftables script for the guard table. Pure, so it can be tested."""
    ipaddress.ip_address(host_ip)  # refuse to run with a bad address
    if not iface or not all(c.isalnum() or c in "-_." for c in iface):
        raise ValueError("bad interface name %r" % iface)
    blocked = ", ".join(BLOCKED_V4 + [host_ip + "/32"])
    return f"""table inet {TABLE} {{
  set blocked_v4 {{
    type ipv4_addr
    flags interval
    elements = {{ {blocked} }}
  }}
  chain forward {{
    type filter hook forward priority -10; policy accept;
    iifname "{iface}" ip daddr @blocked_v4 counter drop
    iifname "{iface}" meta nfproto ipv6 counter drop
  }}
}}
"""


def nft(args, stdin=None):
    return subprocess.run(["nft"] + args, input=stdin, capture_output=True, text=True, timeout=10)


def apply(iface, host_ip, first=False):
    """Install the table if it is missing or differs from what was last installed."""
    cur = nft(["list", "table", "inet", TABLE])
    if cur.returncode == 0 and state.get("expected") == normalise(cur.stdout):
        state["last_ok"] = time.time()
        return
    if not first:
        state["reapplied"] += 1
        print("guard: rules missing or changed; re-applying", file=sys.stderr, flush=True)
    # Replace the table in one transaction: delete it (if present) and add it again.
    script = (f"delete table inet {TABLE}\n" if cur.returncode == 0 else "") + ruleset(iface, host_ip)
    res = nft(["-f", "-"], stdin=script)
    if res.returncode != 0:
        print("guard: nft failed: " + res.stderr, file=sys.stderr, flush=True)
        return
    # nft prints rules in its own format; remember that form for the next comparison.
    state["expected"] = normalise(nft(["list", "table", "inet", TABLE]).stdout)
    state["last_ok"] = time.time()


def normalise(text):
    """Rule structure without counter values or whitespace differences."""
    text = re.sub(r"counter packets \d+ bytes \d+", "counter", text)
    return "\n".join(line.strip() for line in text.splitlines() if line.strip())


def kernel_wireguard(iface):
    """True when the interface is a kernel WireGuard device. In userspace mode NetBird routes
    through its own network stack, the forward hook never sees the traffic, and only the
    isolation test can show whether users are contained."""
    res = subprocess.run(["ip", "-d", "link", "show", iface], capture_output=True, text=True, timeout=5)
    return res.returncode == 0 and " wireguard " in res.stdout


def metrics(iface, tenant):
    lines = [
        "# TYPE malgary_guard_kernel_wireguard gauge",
        "malgary_guard_kernel_wireguard{tenant=\"%s\"} %d" % (tenant, 1 if kernel_wireguard(iface) else 0),
        "# TYPE malgary_guard_reapplied_total counter",
        "malgary_guard_reapplied_total{tenant=\"%s\"} %d" % (tenant, state["reapplied"]),
        "# TYPE malgary_guard_last_ok_seconds gauge",
        "malgary_guard_last_ok_seconds{tenant=\"%s\"} %d" % (tenant, state["last_ok"]),
        "# TYPE malgary_wg_peer_rx_bytes counter",
        "# TYPE malgary_wg_peer_tx_bytes counter",
    ]
    try:
        dump = subprocess.run(["wg", "show", iface, "dump"], capture_output=True, text=True, timeout=5).stdout
    except Exception as e:  # keep serving; an empty scrape beats none
        print(e, file=sys.stderr)
        dump = ""
    for row in dump.splitlines()[1:]:  # first line is the interface itself
        f = row.split("\t")
        if len(f) < 8:
            continue
        ip = f[3].split(",")[0].split("/")[0]
        lab = 'tenant="%s",peer_ip="%s"' % (tenant, ip)
        lines.append("malgary_wg_peer_rx_bytes{%s} %s" % (lab, f[5]))
        lines.append("malgary_wg_peer_tx_bytes{%s} %s" % (lab, f[6]))
    return "\n".join(lines) + "\n"


def serve(iface, tenant):
    class H(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path != "/metrics":
                self.send_response(404)
                self.end_headers()
                return
            body = metrics(iface, tenant).encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/plain; version=0.0.4")
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *a):
            pass

    http.server.ThreadingHTTPServer(("0.0.0.0", 9586), H).serve_forever()


def main():
    iface = os.environ["GUARD_WG_IFACE"]
    host_ip = os.environ["GUARD_HOST_IP"]
    tenant = os.environ.get("GUARD_TENANT", "")
    interval = int(os.environ.get("GUARD_INTERVAL", "30"))
    ruleset(iface, host_ip)  # validate before doing anything
    apply(iface, host_ip, first=True)
    threading.Thread(target=serve, args=(iface, tenant), daemon=True).start()
    print(f"guard: protecting {iface} for {tenant}", flush=True)
    warned = False
    while True:
        time.sleep(interval)
        apply(iface, host_ip)
        if not kernel_wireguard(iface) and not warned:
            print(f"guard: {iface} is not a kernel WireGuard interface; forward rules may not apply", file=sys.stderr, flush=True)
            warned = True


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "--print":  # used by the tests
        iface, host_ip = sys.argv[2].split(",")
        sys.stdout.write(ruleset(iface, host_ip))
    else:
        main()
