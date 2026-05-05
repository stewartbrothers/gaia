# Deploying gaia-mcp HTTP

This doc covers running `gaia-mcp --http` as a container alongside
Forgejo (or any other forge gaia supports). For protocol-level
details — auth model, bind policy, audit log — see [`mcp.md`](mcp.md).

The TL;DR:

- Container image: `ghcr.io/stewartbrothers/gaia-mcp` (or build from `Dockerfile`).
- Two deployment patterns: loopback-only (workstation) and public
  via reverse proxy (small team).
- Pass-through auth means there are **no secrets to inject** at
  the gaia-mcp container — clients send their own forge PATs.
- Examples in `deploy/docker-compose.example.yml` and
  `deploy/nginx.conf`.

## When to deploy as HTTP instead of stdio

For a developer running an MCP-aware agent on their own machine,
**stdio is simpler and faster** — the agent spawns `gaia-mcp` as
a subprocess, no container, no auth, no networking.

HTTP makes sense when:

- A team of 5+ developers shares one gaia-mcp endpoint and each
  brings their own forge PAT.
- An agent that can't spawn subprocesses (a hosted Claude, a
  serverless function) needs to reach a forge.
- You want central audit-log aggregation across multiple agents.

If none of those apply, stay on stdio.

## Get the image

**Pull from GHCR** (fastest — pre-built multi-arch for linux/amd64 and linux/arm64):

```bash
docker pull ghcr.io/stewartbrothers/gaia-mcp:v0.2.0
# or track latest:
docker pull ghcr.io/stewartbrothers/gaia-mcp:latest
```

**Build locally** from source:

```bash
docker build -t gaia-mcp:local .
```

The build is multi-stage: `golang:1.25-alpine` builds both
binaries, `alpine:3.20` runs them. Final image is ~23 MB. Both
`gaia` and `gaia-mcp` are on the runtime PATH so you can
`docker exec` for ad-hoc operations.

For a versioned local build:

```bash
docker build \
  --build-arg VERSION=v0.2.0 \
  --build-arg COMMIT=$(git rev-parse --short HEAD) \
  -t gaia-mcp:v0.2.0 .
```

`gaia version` inside the container then reports those values.

## Pattern 1: loopback only

For a single-developer workstation. Agent runs locally; gaia-mcp
binds to `127.0.0.1`; no TLS needed.

```yaml
services:
  gaia-mcp:
    image: ghcr.io/stewartbrothers/gaia-mcp:latest
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"   # 127.0.0.1: prefix is critical
    command: ["--http", ":8080"]
```

The `127.0.0.1:` prefix on the host port is **required**. Without
it, Docker binds the published port to `0.0.0.0` (all interfaces)
on the host — defeating the bind policy's loopback gate at the
host level.

The bind policy on the gaia-mcp side sees `:8080` (loopback inside
the container), accepts it, and starts cleanly. The Docker port
publish layer is what gates external reachability.

Test it:

```bash
$ curl http://127.0.0.1:8080/healthz                     # → 200 ok
$ curl http://127.0.0.1:8080/mcp -X POST                 # → 401 (no bearer)
$ curl -H "Authorization: Bearer $YOUR_FORGE_PAT" \
       http://127.0.0.1:8080/mcp -X POST \
       -H 'Content-Type: application/json' \
       -H 'Accept: application/json, text/event-stream' \
       -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
            "protocolVersion":"2025-03-26","capabilities":{},
            "clientInfo":{"name":"test","version":"0"}}}'   # → 200
```

## Pattern 2: public + TLS via nginx

For a shared team deployment. nginx terminates TLS on `:443`,
proxies to gaia-mcp on a private docker network.

See [`deploy/docker-compose.example.yml`](../deploy/docker-compose.example.yml)
and [`deploy/nginx.conf`](../deploy/nginx.conf) for the full
config. Key points:

- `expose: 8080` (not `ports:`) — gaia-mcp is reachable only on
  the docker network, never directly from the host.
- gaia-mcp's `--allow-public-no-tls` flag acknowledges that TLS
  is terminating in nginx, not in gaia-mcp.
- nginx **strips client-supplied X-Forwarded-For** and sets it
  from `$remote_addr`. Without this a client could spoof their IP
  in gaia-mcp's audit log.
- nginx passes `Authorization: Bearer …` through verbatim; the
  bearer is the user's forge PAT and gaia-mcp uses it directly
  for the upstream call.

### Cert-manager integration

The compose example mounts `/etc/letsencrypt` read-only into
nginx. If you're already running certbot/Let's Encrypt for the
Forgejo deployment, point gaia-mcp's nginx at the same renewal
hook. Add a `server_name gaia-mcp.example.com` block to your
existing certbot run and bake the cert path into `nginx.conf`.

For Caddy or Traefik users: the same pattern works, with
`tls_passthrough` off and the upstream pointing at the gaia-mcp
container's internal name.

## Health probes

Two endpoints, both unauthenticated, both opaque-bodied:

| Endpoint | Purpose | When orchestrator uses it |
|---|---|---|
| `/healthz` | Liveness — process is alive | Restart probe (k8s `livenessProbe`, ECS health check) |
| `/readyz` | Readiness — forge reachable + auth works | Traffic probe (k8s `readinessProbe`, LB target health) |

`/readyz` does a real `Whoami` against the upstream forge with a
5s deadline using the operator's host-side credentials. A brief
forge outage flips readyz to 503 → orchestrator stops sending
traffic, but doesn't restart the container. When the forge
recovers, readyz returns 200 and traffic resumes.

The Dockerfile's `HEALTHCHECK` directive probes `/healthz`
(liveness only). For full readiness routing, configure the
orchestrator's separate readiness probe to hit `/readyz` —
Docker's HEALTHCHECK doesn't model the liveness/readiness split.

### Kubernetes example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: gaia-mcp }
spec:
  replicas: 1
  selector: { matchLabels: { app: gaia-mcp } }
  template:
    metadata: { labels: { app: gaia-mcp } }
    spec:
      containers:
        - name: gaia-mcp
          image: ghcr.io/stewartbrothers/gaia-mcp:v0.2.0
          args: ["--http", ":8080", "--allow-public-no-tls"]
          ports:
            - containerPort: 8080
          livenessProbe:
            httpGet: { path: /healthz, port: 8080 }
            initialDelaySeconds: 10
            periodSeconds: 10
          readinessProbe:
            httpGet: { path: /readyz, port: 8080 }
            initialDelaySeconds: 5
            periodSeconds: 5
```

Pair with an Ingress that terminates TLS and forwards
`Authorization` and a clean `X-Forwarded-For`.

## Operator credentials (for /readyz)

`/readyz` calls upstream with the operator's host-side
credentials — *not* the per-request bearer. To populate them,
run once after the container is up:

```bash
docker exec -it <container> gaia auth forgejo https://your-forgejo/api/v1
# paste a PAT when prompted; gaia stores it in the gaia user's home
# inside the container.
```

For ephemeral container deployments (rolling deploys, node
replacements), bake credentials into a volume mount instead:

```yaml
services:
  gaia-mcp:
    # ...
    volumes:
      - gaia-creds:/home/gaia/.config/gaia:ro

volumes:
  gaia-creds: # populate this volume once via a setup container
```

The operator credential needs only `read:user` scope on the
forge — `/readyz` calls `Whoami`, nothing else. If the operator
PAT lacks even that scope, readyz reports 503 with a clear
"forge_ping" reason in the audit log.

## Upgrade / rolling deploy

The graceful-shutdown wiring (SIGTERM → 10s drain → SIGKILL)
makes rolling deploys lossless:

1. Orchestrator sends SIGTERM to the old pod.
2. gaia-mcp logs `{"msg":"shutdown","signal":"terminated"}` and
   stops accepting new connections.
3. In-flight requests have up to `--shutdown-timeout` (default
   10s) to finish.
4. Orchestrator's readiness probe on the new pod returns 200 and
   traffic shifts.

If `--shutdown-timeout` is shorter than the longest expected tool
call, you'll get truncated responses on shutdown. Bump it (or
your orchestrator's grace period — most default to 30s, fine for
gaia-mcp's 10s) if you see this.

## Logs

gaia-mcp writes structured JSON to stderr. In docker-compose:

```bash
docker compose logs -f gaia-mcp.public
```

Lines you'll see:

```json
{"level":"INFO","msg":"listening","addr":":8080","path":"/mcp",...}
{"level":"WARN","msg":"auth_failure","reason":"empty_bearer",
 "remote":"203.0.113.7:54321","path":"/mcp"}
{"level":"WARN","msg":"readyz_unready","reason":"forge_ping",
 "err":"GET /user: HTTP 401: ..."}
{"level":"INFO","msg":"shutdown","signal":"terminated",...}
```

Pipe stderr to a log aggregator (Loki, CloudWatch, Datadog) and
alert on bursts of `auth_failure` or sustained `readyz_unready`.
**Do not log gaia-mcp's stderr to a public Pastebin / GitHub gist
without redaction** — though tokens never appear, request paths
might still leak repo names you'd rather keep internal.

## What's NOT here
- **Per-tenant credential routing** — pass-through means each
  bearer is its own identity, so this isn't needed. Closed as
  superseded.
- **Backup / restore** — gaia-mcp is stateless. The only state
  worth backing up is the operator's host-side credentials
  (`~/.config/gaia/`) and only because re-running `gaia auth
  forgejo` is a manual step.
