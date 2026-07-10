# AdGuard Log Aggregator — Design

## Context

The user runs 3 AdGuard Home instances for HA DNS (synced via adguardhome-sync) and must currently open three web UIs to find out where a DNS block happened. This project is a single central web UI that fetches query logs live from all instances, merges them, and displays them behind OIDC login (Pocket-ID).

Hard requirements: Go; all config via env vars/secrets; no persistent storage (fetch live per request); OIDC-only auth. Style rules: many small files (200-400 lines, 800 max), organized by feature/domain, tests for everything, no emojis, immutability, no secrets in repo.

**Confirmed decisions**: frontend = server-rendered Go `html/template` + htmx (vendored, embedded via `go:embed`, no Node build); v1 scope = unified log view + instance health/status bar + aggregate stats page.

## Architecture summary

Single static Go binary. Module `github.com/kenlasko/adguard-log-aggregator`, Go 1.26+, stdlib `net/http` ServeMux (Go 1.22+ pattern routing — no router dep).

**Dependencies (minimal)**: `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2` (OIDC code flow + PKCE). Everything else stdlib. htmx 2.x vendored as a static file.

**AdGuard API facts (verified against official OpenAPI spec)**:
- Base path `/control`, HTTP Basic Auth per request (stateless; skip cookie login flow).
- `GET /control/querylog` — params `older_than` (raw timestamp cursor of last item), `limit`, `search` (domain or client IP), `response_status` (all|filtered|blocked|blocked_safebrowsing|blocked_parental|whitelisted|rewritten|safe_search|processed). Response `{oldest, data: [QueryLogItem]}`; items have `time` (RFC3339Nano), `client`, `client_info`, `question{name,type}`, `reason`, `rules[{filter_list_id,text}]`, `status`, `elapsedMs`, `cached`, `upstream`.
- `GET /control/stats` — scalar counters + top-N lists as arrays of single-key `{"<domain-or-ip>": count}` maps.
- `GET /control/status` — `running`, `version`, `protection_enabled` (health probe).

## Key design decisions

### 1. Env scheme for N instances: indexed prefix
```
ADGUARD_1_URL=http://10.0.0.2:3000   ADGUARD_1_USERNAME=...  ADGUARD_1_PASSWORD=...  ADGUARD_1_NAME=dns1
ADGUARD_2_URL=...                    (scan n=1..64, stop at first unset URL)
```
Startup fails (collecting ALL errors, not fail-fast) on: URL present but username/password missing; index gaps (1 and 3 set but not 2 — catches typos); duplicate names. Chosen over one JSON/CSV var because each password maps 1:1 to a k8s `secretKeyRef`/Docker secret and there's no quoting pain.

### 2. Sessions: hand-rolled AES-256-GCM cookie codec (stateless)
Small (~140 lines + tests) codec using stdlib crypto: payload `{sub, name, email, expiry}`, key = SHA-256(`SESSION_SECRET`), base64url encoding. Reused for the short-lived OIDC state cookie (state + PKCE verifier + return-to). No server-side session store — consistent with "no persistent storage". Skips gorilla/sessions (2 deps for unneeded store abstraction). Cookies: `HttpOnly`, `SameSite=Lax` (callback is cross-site top-level nav), `Secure` from `COOKIE_SECURE` (default true). Startup fails if secret < 32 chars.

### 3. Fan-out + merge + composite cursor (core algorithm — `internal/aggregate`)
- Parse each entry's `time` to `time.Time` for sorting but **keep the raw string** — it is fed back verbatim as that instance's `older_than` cursor (reformatting risks precision loss, causing skipped/duplicated entries).
- **Cursor** = base64url(JSON) map carried in the htmx load-more request: `{"v":1,"i":{"dns1":{"o":"<raw time of last served entry>","d":false},...}}` where `d`=instance exhausted. Filters are NOT in the cursor; the filter form is re-sent via `hx-include`, and any filter change omits the cursor (reset to page 1). Decode failure/version mismatch = treat as page 1.
- **Per page** (size `P`=`PAGE_SIZE`, default 50): for each selected non-done instance concurrently (WaitGroup + shared `context.WithTimeout(ADGUARD_TIMEOUT)`): `GET querylog?limit=P&search=&response_status=&older_than=cursor[i].o`. Per-instance error: empty entries, cursor carried forward unchanged (retry on next load-more), instance named in a partial-results banner. Merge all tagged entries, `slices.SortFunc` descending by time (at most N*P, roughly 150 items — no heap merge needed), deterministic tie-break, trim to P. Next cursor: `o` = raw time of each instance's last *consumed* entry; `d` = returned < P and fully consumed. `hasMore` = any instance not done. Over-fetch+discard is deliberate (no caching allowed anyway).
- Accepted limitation (document in code + README): identical-nanosecond timestamps on the same instance straddling a page boundary can drop an entry — vanishingly rare, harmless for a log viewer.
- **Stats merge**: fan out `/control/stats`; sum scalars; merge top-N lists by summing per-key, sort desc, top 10; `avg_processing_time` weighted by query count; show per-instance sub-counts.
- **Health**: fan out `/control/status` with ~2s timeout, producing `{Name, Reachable, Version, ProtectionEnabled, Err}` per instance.

## File tree (feature-organized, all files small)

```
cmd/server/main.go              ~80: config load, wiring, graceful shutdown (signal.NotifyContext)
cmd/fakeadguard/main.go         ~60: dev tool serving the adguardtest fixture with generated data
internal/config/config.go       ~140: immutable Config, FromEnv, validation (collect all errors)
internal/config/instances.go    ~90: indexed ADGUARD_n_* scan + gap/duplicate checks
internal/adguard/client.go      ~110: Client{name, baseURL, creds, *http.Client}, get() w/ Basic Auth
internal/adguard/types.go       ~130: QueryLogItem (RawTime string + ParsedTime), Stats, Status
internal/adguard/querylog.go    ~70   internal/adguard/stats.go ~40   internal/adguard/status.go ~40
internal/adguard/adguardtest/fake.go  ~220: httptest fake AdGuard — seeded entries, real older_than/
                                      limit/search/response_status semantics, failure injection
internal/aggregate/fanout.go    ~90: concurrent fan-out over instances -> []Result[T]
internal/aggregate/logs.go      ~150: FetchLogs (algorithm above) -> LogsPage{Entries, NextCursor, HasMore, Errors}
internal/aggregate/cursor.go    ~90   internal/aggregate/merge.go ~60 (pure, exhaustively testable)
internal/aggregate/stats.go     ~120  internal/aggregate/health.go ~70
internal/auth/session.go        ~140: AES-GCM Codec (Seal/Open), cookie helpers
internal/auth/oidc.go           ~170: discovery, /auth/login (state+PKCE), /auth/callback, /auth/logout
internal/auth/verifier.go       ~30: IDTokenVerifier interface seam for testability
internal/auth/middleware.go     ~70: RequireAuth; allowlist /auth/*, /healthz, /static/;
                                     HX-Request -> 401 + HX-Redirect instead of 302
internal/auth/oidctest/fake.go  ~160: httptest OIDC issuer (discovery, JWKS, /token, signed ID tokens)
internal/web/server.go          ~110: routes, http.Server timeouts, static handler
internal/web/templates.go       ~60: go:embed + FuncMap    internal/web/render.go ~50: page vs HX fragment
internal/web/logs.go            ~150  internal/web/health.go ~60  internal/web/stats.go ~80
internal/web/templates/         layout.html, logs.html, logs_rows.html, stats.html,
                                stats_panels.html, health_bar.html
internal/web/static/            htmx.min.js (vendored), app.css (~150 lines hand-written, dark-mode aware)
+ _test.go beside every source file; Dockerfile, .dockerignore, .gitignore, README rewrite
```

## htmx UI wiring
- **Layout**: nav (Logs | Stats | logout) + `<div id="health-bar" hx-get="/partials/health" hx-trigger="load, every 15s">`.
- **Logs page**: filter form (search box, status select, instance checkboxes, auto-refresh toggle) with `hx-get="/partials/logs" hx-target="#log-body"`, triggers on change/debounced keyup. Columns: time, instance badge, client, domain, type, reason/status, elapsed, rule (title attr). Row color-coding: blocked=red tint, rewritten=amber.
- **Load more**: sentinel row with `hx-get="/partials/logs?cursor=..." hx-include="#filters" hx-target="#load-more" hx-swap="outerHTML"` — response replaces sentinel with rows + its own next sentinel (append semantics). Handler distinguishes append vs reset purely by presence of `cursor` param.
- **Auto-refresh**: htmx conditional polling `every 10s [document.getElementById("auto").checked]` — resets to page 1 (live-tail behavior). No custom JS.
- **Partial-results banner** row when any instance errored. **Stats page**: fragment polled every 60s; totals tiles + three top-10 tables with per-instance breakdown.
- `render.go`: HX-Request header means fragment only; otherwise fragment wrapped in layout (URLs work on full page load).

## Env var reference
| Var | Req | Default | Notes |
|---|---|---|---|
| `ADGUARD_<n>_URL/_USERNAME/_PASSWORD` | yes | — | n = 1.. contiguous |
| `ADGUARD_<n>_NAME` | no | URL host | display name |
| `ADGUARD_TIMEOUT` | no | `5s` | outbound timeout |
| `OIDC_ISSUER_URL`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_REDIRECT_URL` | yes | — | Pocket-ID or any OIDC IdP |
| `SESSION_SECRET` | yes | — | at least 32 chars; hashed to AES key |
| `SESSION_DURATION` | no | `12h` | |
| `COOKIE_SECURE` | no | `true` | false only for local HTTP dev |
| `LISTEN_ADDR` | no | `:8080` | |
| `PAGE_SIZE` | no | `50` | per-instance fetch limit / page size |
| `LOG_LEVEL` | no | `info` | slog |

## Testing strategy
- `adguardtest/fake.go` is the shared fixture (built early): deterministic entries, real pagination/search/filter semantics, Basic Auth check, injectable failures (500/timeout/auth-reject).
- Table-driven: config scanning (gaps, duplicates, error aggregation); adguard client (param encoding, Basic Auth header, raw time preserved, non-200 becomes typed error); merge (ordering, ties, trim accounting); cursor (round-trip, garbage/tamper treated as page 1, no panic); **logs fan-out core scenarios**: (a) 3 instances interleaved timestamps paginate 3 pages with no loss/dup, (b) one instance down produces partial page + error surfaced + cursor unchanged, (c) exhausted instance marked done and skipped, (d) hasMore=false when all done.
- Auth: session seal/open/expiry/tamper; `oidctest` fake issuer exercises full login-to-callback (state mismatch, nonce, cookie set); middleware (302 for browser, 401+HX-Redirect for htmx, allowlist).
- Web handlers: httptest against adguardtest fakes with pre-forged session cookie; fragment vs full-page rendering.

## Operational
- **Dockerfile**: multi-stage, `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"` into `gcr.io/distroless/static-debian12:nonroot` (CA certs + nonroot for free).
- `/healthz`: unauthenticated liveness, no upstream fan-out. `http.Server` timeouts set; graceful shutdown 10s.
- README rewrite: features, env table, docker run/compose/k8s snippets, Pocket-ID client setup, pagination limitation note.

## Build order (each step compiles + tests green before next)
1. Scaffold: `go mod init`, .gitignore, main.go with /healthz + graceful shutdown.
2. `internal/config` + tests; wire into main.
3. `internal/adguard` (types, client, querylog, stats, status) + **adguardtest fake** + `cmd/fakeadguard` + tests.
4. `internal/aggregate` — cursor + merge first (pure), then fanout/logs/stats/health + full test suite. Highest-risk logic proven before any UI exists.
5. `internal/auth` — session, verifier seam, middleware, oidc + oidctest + tests.
6. `internal/web` — server, templates, render, logs page + partial + handler tests (app usable end-to-end).
7. Health bar + stats page.
8. app.css, vendored htmx, empty/error states.
9. Dockerfile, README, `go vet ./... && go test ./...`, docker build.

## Verification (end-to-end)
1. `go test ./...` green.
2. `go run ./cmd/fakeadguard -addr :8081 -name dns1` and `-addr :8082 -name dns2`.
3. Throwaway IdP (Pocket-ID or Dex in Docker), client registered with redirect `http://localhost:8080/auth/callback`.
4. Run app with the env vars above (`COOKIE_SECURE=false`, `SESSION_SECRET=$(openssl rand -hex 32)`).
5. Browser: unauthenticated `/` redirects to IdP and back with session; interleaved dns1/dns2 rows with badges; search + status filter narrow results; unchecking an instance removes its rows; Load more appends 50 with no boundary dups; killing dns2 fake turns its health pill red within 15s + partial-results banner while dns1 still loads; `/stats` shows merged tops with per-instance breakdown; `/healthz` 200 without session; auto-refresh live-tails.
6. `docker build` + rerun checks in container.
