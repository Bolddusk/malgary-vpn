# Guard sidecar for a tenant routing peer on the interim host (feature 002, research I4).
FROM alpine:3.22
RUN apk add --no-cache nftables wireguard-tools-wg iproute2 python3
COPY guard.py /usr/local/bin/guard
RUN chmod 0755 /usr/local/bin/guard
EXPOSE 9586
ENTRYPOINT ["/usr/local/bin/guard"]
