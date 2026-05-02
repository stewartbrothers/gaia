# Multi-stage build for gaia-mcp + gaia.
#
# Build stage: pinned Go, mod-download cached, both binaries written
# with -ldflags injection of the version metadata (so `gaia version`
# inside the container reports the actual build, not "dev").
#
# Runtime stage: distroless-style alpine (smaller than scratch +
# ca-certs), non-root uid:gid 1000, single binary on PATH. The default
# CMD binds to :8080 — the bind policy refuses to start without auth,
# so the operator must mount a token file (or pass
# --allow-public-no-auth on a reverse-proxied deployment). This is
# deliberate: a `docker run gaia-mcp` with no further config fails
# loud, never silently exposes an unauthenticated daemon.

ARG GO_VERSION=1.23
FROM golang:${GO_VERSION}-alpine AS build

WORKDIR /src

# Cache deps independently of source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 \
    go build \
    -ldflags="-s -w \
      -X 'github.com/stewartbrothers/gaia/internal/version.Version=${VERSION}' \
      -X 'github.com/stewartbrothers/gaia/internal/version.Commit=${COMMIT}'" \
    -o /out/gaia-mcp ./cmd/gaia-mcp \
 && CGO_ENABLED=0 \
    go build \
    -ldflags="-s -w \
      -X 'github.com/stewartbrothers/gaia/internal/version.Version=${VERSION}' \
      -X 'github.com/stewartbrothers/gaia/internal/version.Commit=${COMMIT}'" \
    -o /out/gaia ./cmd/gaia


FROM alpine:3.20

# ca-certificates: needed for HTTPS to the forge.
# wget: used by the HEALTHCHECK directive (busybox wget, already in alpine,
#       called out here as a documentation seam).
RUN apk add --no-cache ca-certificates \
 && addgroup -S -g 1000 gaia \
 && adduser  -S -u 1000 -G gaia -h /home/gaia gaia

COPY --from=build /out/gaia-mcp /usr/local/bin/gaia-mcp
COPY --from=build /out/gaia     /usr/local/bin/gaia

USER gaia
WORKDIR /home/gaia

EXPOSE 8080

# Container HEALTHCHECK probes /healthz on the same listener. 30s
# start-period covers the brief startup window before the listener is
# accepting connections. 10s interval / 5s timeout / 3 retries gives
# the orchestrator ~30s to flip unhealthy → restart on a real failure.
#
# /healthz is liveness (process alive). For readiness (forge
# reachable + token valid), use /readyz from your orchestrator's
# readiness probe — Docker's HEALTHCHECK only models liveness.
HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=3 \
    CMD wget --quiet --spider http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/gaia-mcp"]
CMD ["--http", ":8080"]
