# Throwaway NetBird client for `malgaryctl isolation-test` (feature 002, FR-009).
ARG NETBIRD_VERSION=0.82.0
FROM netbirdio/netbird:${NETBIRD_VERSION}
RUN apk add --no-cache curl iproute2
COPY probe.sh /usr/local/bin/probe
RUN chmod 0755 /usr/local/bin/probe
ENTRYPOINT ["/usr/local/bin/probe"]
