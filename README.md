# AdGuard Log Central

A single, central web UI that fetches query logs **live** from multiple
[AdGuard Home](https://adguard.com/en/adguard-home/overview.html) instances,
merges them, and presents a unified, filterable, paginating log view behind
OIDC login. Built for people running several AdGuard Home instances for
highly available DNS (for example kept in sync with
[adguardhome-sync](https://github.com/bakito/adguardhome-sync)) who are tired
of opening three web UIs to find where a DNS block happened.

No database, no disk writes: logs are fetched on demand from each instance on
every request. All configuration is via environment variables and secrets.

## Features

- **Unified query log** across all instances, merged in true time order with
  correct cross-instance pagination (Load more / infinite scroll).
- **Filtering** by search term (domain or client), response status
  (blocked / rewritten / allowed), and per-instance color chips (each instance
  has its own color; click to toggle it in or out of the view).
- **Exact-by-default search with `*` wildcards.** A plain term matches the whole
  value only, so `192.168.1.2` never matches `192.168.1.20`. Add `*` anywhere to
  widen the match: `192.168.1.2*` (prefix), `*.example.com` (suffix), or
  `*ads*` (substring).
- **Click any client or domain** in a log row to instantly filter by that exact
  value.
- **Block / unblock any domain** straight from a log row. Pick which instance to
  apply the rule to (remembered in your browser via LocalStorage); with
  adguardhome-sync the change fans out to the rest.
- **Live tail** via an auto-refresh toggle, plus a manual refresh button.
- **Instance health bar** showing each instance's reachability, version, and
  protection state, refreshed every 15 seconds.
- **Aggregate stats page**: merged totals (queries, blocked, blocked share,
  average processing time) plus combined top-10 blocked domains, clients, and
  queried domains, each with a per-instance breakdown.
- **Partial-results handling**: if an instance is unreachable, the reachable
  instances still render and a banner names the ones that failed.
- **OIDC login only** (tested with [Pocket-ID](https://pocket-id.org/)), with
  stateless, encrypted session cookies. Dark mode aware. No client-side build
  step: the frontend is server-rendered Go templates plus vendored htmx.

## Configuration

All configuration is via environment variables. Instances are declared with an
indexed prefix and must be contiguous starting at 1.

| Variable | Required | Default | Notes |
|---|---|---|---|
| `ADGUARD_<n>_URL` | yes | - | Base URL of instance n (n = 1..). Scan stops at the first unset URL. |
| `ADGUARD_<n>_USERNAME` | yes | - | Basic auth username for instance n. |
| `ADGUARD_<n>_PASSWORD` | yes | - | Basic auth password for instance n (use a secret). |
| `ADGUARD_<n>_NAME` | no | URL host | Display name for instance n; must be unique. |
| `ADGUARD_TIMEOUT` | no | `5s` | Per-request outbound timeout to instances. |
| `OIDC_ISSUER_URL` | yes | - | OIDC issuer (Pocket-ID or any OIDC IdP). |
| `OIDC_CLIENT_ID` | yes | - | OIDC client ID. |
| `OIDC_CLIENT_SECRET` | yes | - | OIDC client secret (use a secret). |
| `OIDC_REDIRECT_URL` | yes | - | Must equal `<public-url>/auth/callback`. |
| `SESSION_SECRET` | yes | - | At least 32 characters; hashed to the AES-256 session key. |
| `SESSION_DURATION` | no | `12h` | Session lifetime. |
| `COOKIE_SECURE` | no | `true` | Set `false` only for local HTTP development. |
| `LISTEN_ADDR` | no | `:8080` | Listen address. |
| `PAGE_SIZE` | no | `50` | Per-instance fetch limit and page size (1..1000). |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, or `error`. |

Configuration is validated at startup and **all** problems are reported at once
(missing credentials, index gaps, duplicate names, short session secret, and so
on), so you fix them in a single pass.

## Container images

Multi-architecture images (`linux/amd64` and `linux/arm64`) are published to the
GitHub Container Registry:

```
ghcr.io/kenlasko/adguard-logcentral:latest      # newest release, and tip of main between releases
ghcr.io/kenlasko/adguard-logcentral:1.2.3       # a specific release
ghcr.io/kenlasko/adguard-logcentral:1.2         # latest patch of the 1.2 line
ghcr.io/kenlasko/adguard-logcentral:sha-abc1234 # exact commit on main
```

Every push to `main` moves `latest` (plus a branch and short-SHA tag) so the tip
of `main` is always pullable. Versioned `1.2.3` / `1.2` tags come from a formal
release (see [Releasing](#releasing)). Each image ships with a signed SBOM and
build provenance attestation, and is scanned with Trivy before publication;
release images are additionally signed with cosign.

The running version is reported at startup and on the unauthenticated
`/healthz` endpoint:

```sh
curl -s http://localhost:8080/healthz
# {"status":"ok","build":{"version":"1.2.3","commit":"abc1234","date":"2026-07-10T12:00:00Z"}}
```

## Releasing

Releases are cut manually by running the **CI** workflow against `main`
(Actions -> CI -> Run workflow, or `gh workflow run CI --ref main`) -- a manual
run IS a release. It runs the full check suite as a gate, then builds, signs,
scans, and pushes the versioned image, and only then creates the `v<version>`
git tag and GitHub Release. Pick a `patch`/`minor`/`major` bump or pass an
explicit `version`, and optionally supply release notes. See
[`docs/release-notes/README.md`](docs/release-notes/README.md) for the full
process, version-input reference, and release-notes precedence.

## Running

### docker run

```sh
docker run --rm -p 8080:8080 \
  -e ADGUARD_1_URL=http://10.0.0.2:3000 \
  -e ADGUARD_1_USERNAME=admin \
  -e ADGUARD_1_PASSWORD="$DNS1_PASSWORD" \
  -e ADGUARD_1_NAME=dns1 \
  -e ADGUARD_2_URL=http://10.0.0.3:3000 \
  -e ADGUARD_2_USERNAME=admin \
  -e ADGUARD_2_PASSWORD="$DNS2_PASSWORD" \
  -e ADGUARD_2_NAME=dns2 \
  -e OIDC_ISSUER_URL=https://id.example.com \
  -e OIDC_CLIENT_ID=adguard-logs \
  -e OIDC_CLIENT_SECRET="$OIDC_SECRET" \
  -e OIDC_REDIRECT_URL=https://logs.example.com/auth/callback \
  -e SESSION_SECRET="$(openssl rand -hex 32)" \
  ghcr.io/kenlasko/adguard-logcentral:latest
```

### docker compose

```yaml
services:
  adguard-logcentral:
    image: ghcr.io/kenlasko/adguard-logcentral:latest
    ports:
      - "8080:8080"
    environment:
      ADGUARD_1_URL: http://10.0.0.2:3000
      ADGUARD_1_USERNAME: admin
      ADGUARD_1_NAME: dns1
      ADGUARD_2_URL: http://10.0.0.3:3000
      ADGUARD_2_USERNAME: admin
      ADGUARD_2_NAME: dns2
      OIDC_ISSUER_URL: https://id.example.com
      OIDC_CLIENT_ID: adguard-logs
      OIDC_REDIRECT_URL: https://logs.example.com/auth/callback
    secrets:
      - adguard_1_password
      - adguard_2_password
      - oidc_client_secret
      - session_secret
    # Map each secret file to its variable, e.g. with an entrypoint wrapper or
    # your orchestrator's *_FILE convention. Passwords should never be inlined.
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: adguard-logcentral
spec:
  replicas: 1
  selector:
    matchLabels: { app: adguard-logcentral }
  template:
    metadata:
      labels: { app: adguard-logcentral }
    spec:
      containers:
        - name: app
          image: ghcr.io/kenlasko/adguard-logcentral:latest
          ports:
            - containerPort: 8080
          env:
            - name: ADGUARD_1_URL
              value: http://adguard-dns1:3000
            - name: ADGUARD_1_USERNAME
              value: admin
            - name: ADGUARD_1_PASSWORD
              valueFrom: { secretKeyRef: { name: adguard-logs, key: dns1-password } }
            - name: OIDC_ISSUER_URL
              value: https://id.example.com
            - name: OIDC_CLIENT_ID
              value: adguard-logs
            - name: OIDC_CLIENT_SECRET
              valueFrom: { secretKeyRef: { name: adguard-logs, key: oidc-client-secret } }
            - name: OIDC_REDIRECT_URL
              value: https://logs.example.com/auth/callback
            - name: SESSION_SECRET
              valueFrom: { secretKeyRef: { name: adguard-logs, key: session-secret } }
          readinessProbe:
            httpGet: { path: /healthz, port: 8080 }
          livenessProbe:
            httpGet: { path: /healthz, port: 8080 }
          securityContext:
            runAsNonRoot: true
            allowPrivilegeEscalation: false
```

Each password maps cleanly to one `secretKeyRef`, which is the reason instances
are declared with indexed variables rather than a single JSON blob.

## Pocket-ID (or any OIDC IdP) setup

1. Create a **confidential** OIDC client in your IdP.
2. Set its redirect URI to exactly `<public-url>/auth/callback` (matching
   `OIDC_REDIRECT_URL`).
3. Ensure the `openid`, `profile`, and `email` scopes are allowed.
4. Copy the client ID and secret into `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET`.

`/healthz` is unauthenticated (for liveness probes); every other route requires
a valid session.

## Local development

Requires **Go 1.26+** (the module pins toolchain `go1.26.5`; older installs
fetch it automatically). Building the container needs Docker only.

Two fake AdGuard instances are included for end-to-end testing without touching
real DNS servers:

```sh
go run ./cmd/fakeadguard -addr :8081 -name dns1 &
go run ./cmd/fakeadguard -addr :8082 -name dns2 &
```

Point `ADGUARD_1_URL=http://localhost:8081` (user `admin`, pass `password`) and
`ADGUARD_2_URL=http://localhost:8082`, run a throwaway IdP (Pocket-ID or Dex),
set `COOKIE_SECURE=false` and `SESSION_SECRET=$(openssl rand -hex 32)`, then
`go run ./cmd/server`.

Run the test suite with `go test ./...`.

## Continuous integration

GitHub Actions runs on every pull request and push to `main`:

- **CI** (`.github/workflows/ci.yml`): `gofmt` and `go vet`, `go mod tidy`/
  `verify` checks, `golangci-lint` (which bundles staticcheck, errcheck, revive,
  gocritic, and more), the full test suite under the race detector with a
  coverage report, `govulncheck` for known-vulnerable dependencies, and `gosec`
  for static security analysis (results uploaded to GitHub code scanning).
- **CodeQL** (`.github/workflows/codeql.yml`): GitHub's security-and-quality
  analysis for Go, also on a weekly schedule.
- **Docker** (`.github/workflows/docker.yml`): builds and publishes the
  multi-arch image described above, with SBOM, provenance, and a Trivy scan.

Dependencies, GitHub Actions, and the Docker base image are kept current by
Dependabot (`.github/dependabot.yml`).

## Pagination note

Pagination uses each instance's raw `time` value as an `older_than` cursor,
preserved byte-for-byte to avoid precision loss. One accepted limitation: if a
single instance emits two entries with the *identical* nanosecond timestamp and
they straddle a page boundary, one may be dropped from the view. This is
vanishingly rare and harmless for a log viewer, and it is the deliberate
trade-off for a stateless, no-storage design.
