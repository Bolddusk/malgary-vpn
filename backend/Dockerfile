# malgaryd and malgaryctl for the interim host (feature 002, T025). The image carries the docker
# CLI and compose plugin because the ComposeDeployer runs `docker compose` against the host's
# daemon through the mounted socket (research I6).
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/malgaryd ./cmd/malgaryd \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/malgaryctl ./cmd/malgaryctl

FROM docker:27-cli AS docker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=docker /usr/local/bin/docker /usr/local/bin/docker
COPY --from=docker /usr/local/libexec/docker/cli-plugins/docker-compose /usr/local/libexec/docker/cli-plugins/docker-compose
COPY --from=build /out/malgaryd /out/malgaryctl /usr/local/bin/
ENV DOCKER_CONFIG=/tmp/.docker
# Runs as nonroot (65532); the compose service adds the host's docker group for the socket.
USER nonroot
ENTRYPOINT ["/usr/local/bin/malgaryd"]
