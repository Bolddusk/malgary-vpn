#!/usr/bin/env bash
# Memory of every Malgary container against its limit and the 3 GB budget (feature 002, T038,
# quickstart V4, SC-004). Run on the interim host. Exits non-zero when over budget.
set -euo pipefail
budget_mb=${1:-3072}
docker stats --no-stream --format '{{.Name}}\t{{.MemUsage}}' \
  $(docker ps -q --filter label=com.malgary.managed=true) |
awk -F'\t' -v budget="$budget_mb" '
  function mb(s,  v) { v = s + 0; if (s ~ /GiB/) v *= 1024; else if (s ~ /KiB/) v /= 1024; else if (s ~ /B$/ && s !~ /[KMG]iB/) v /= 1048576; return v }
  { split($2, a, " / "); used = mb(a[1]); limit = mb(a[2]); total += used; lim += limit
    printf "%-45s %8.0f MB of %6.0f MB\n", $1, used, limit }
  END { printf "\nused %0.f MB, limits %0.f MB, budget %d MB\n", total, lim, budget
        if (lim > budget) { print "FAIL: limits exceed the budget"; exit 1 } }'
