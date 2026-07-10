# Development Task List

Implementation handoff for the design in [docs/DESIGN.md](DESIGN.md). Work the phases in order; every phase must end with `go build ./...` and `go test ./...` green before starting the next. Commit at least once per phase with a descriptive message.

Ground rules (from CLAUDE.md — non-negotiable):
- Many small files (200-400 lines typical, 800 hard max). Organize by feature/domain.
- Tests accompany every new piece of functionality, written in the same phase.
- No emojis in code, comments, or docs. Prefer immutability (return new values, do not mutate inputs). No secrets committed, ever.
- Module path: `github.com/kenlasko/adguard-logcentral`. Only external deps allowed: `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2` (plus their transitive deps). Everything else stdlib.

Refer to DESIGN.md for: the composite-cursor algorithm, the indexed env-var scheme, the session cookie format, the htmx wiring, and the full env var table. Do not re-decide anything settled there.

## Phase 0 — Scaffold

- [ ] **T0.1 Module + hygiene files.** `go mod init github.com/kenlasko/adguard-logcentral` (Go 1.26+). Add `.gitignore` (compiled binary, `coverage.out`, editor droppings) and `.dockerignore` (`.git`, docs, README).
- [ ] **T0.2 Minimal server.** `cmd/server/main.go`: `log/slog` JSON logger, `http.Server` with Read/Write/Idle/ReadHeader timeouts, `GET /healthz` returning `200 {"status":"ok"}`, graceful shutdown via `signal.NotifyContext(SIGINT, SIGTERM)` with 10s grace.

Exit criteria: `go build ./...` passes; `curl localhost:8080/healthz` returns 200; Ctrl+C shuts down cleanly.

## Phase 1 — Configuration (`internal/config`)

- [ ] **T1.1 Instance scanning** (`instances.go`): scan `ADGUARD_<n>_URL/_USERNAME/_PASSWORD/_NAME` for n=1..64, stopping at the first unset URL. `NAME` defaults to the URL host. Errors (collected, not fail-fast): URL set but username or password missing; index gap (e.g. 1 and 3 set, 2 unset); duplicate names; unparseable URL. Require at least one instance.
- [ ] **T1.2 Full config** (`config.go`): immutable `Config` struct built by `FromEnv()` covering every variable in the DESIGN.md env table, with defaults (`ADGUARD_TIMEOUT=5s`, `SESSION_DURATION=12h`, `COOKIE_SECURE=true`, `LISTEN_ADDR=:8080`, `PAGE_SIZE=50`, `LOG_LEVEL=info`). Validation: required OIDC vars present; `SESSION_SECRET` at least 32 chars; durations parse; `PAGE_SIZE` in 1..1000. Return ALL problems in one error (use `errors.Join` or a collected slice).
- [ ] **T1.3 Tests** (`instances_test.go`, `config_test.go`): table-driven; cover happy path, each validation failure, multi-error aggregation, defaults, name defaulting. Use `t.Setenv`.
- [ ] **T1.4 Wire into main:** config load failure prints every problem and exits non-zero; `LOG_LEVEL` drives slog level; `LISTEN_ADDR` drives the server.

Exit criteria: `go test ./internal/config` green; starting the binary with a bad env prints all errors at once.

## Phase 2 — AdGuard client (`internal/adguard`)

- [ ] **T2.1 Types** (`types.go`): `QueryLogItem` with `RawTime string` (JSON `time`) plus a `ParsedTime time.Time` populated after decode (RFC3339Nano) — the raw string must survive untouched for cursor use. Also `Question`, `ClientInfo`, `Rule`, `QueryLogResponse{Oldest, Data}`, `Stats` (scalars + top-N lists decoded from arrays of single-key maps into ordered `[]TopEntry{Name, Count}`), `Status`.
- [ ] **T2.2 Client** (`client.go`): `Client{name, baseURL, username, password, httpClient}` constructed from a config instance + shared `*http.Client`; unexported `get(ctx, path, query, &out)` helper setting Basic Auth, checking status (non-200 becomes a typed error including instance name and status code), decoding JSON.
- [ ] **T2.3 Endpoints** (`querylog.go`, `stats.go`, `status.go`): `QueryLog(ctx, QueryLogParams{OlderThan, Limit, Search, ResponseStatus})` hitting `/control/querylog`; `Stats(ctx)` hitting `/control/stats`; `Status(ctx)` hitting `/control/status`. Params encode only when non-zero.
- [ ] **T2.4 Fake AdGuard fixture** (`adguardtest/fake.go`): `httptest`-based fake used by ALL later test layers. Seeded deterministic entries per instance; honors `older_than` (strictly-older semantics), `limit`, substring `search` on domain and client, `response_status`; enforces Basic Auth; failure injection knobs (per-endpoint 500, hang-past-timeout, 401). Include an entry generator for realistic reasons/domains/clients.
- [ ] **T2.5 Dev tool** (`cmd/fakeadguard/main.go`): flags `-addr`, `-name`; serves the fixture with ~500 generated entries for manual end-to-end runs.
- [ ] **T2.6 Tests** (`client_test.go`, `querylog_test.go`, `stats_test.go`): param encoding (older_than/limit/search/response_status appear iff set), Basic Auth header present, non-200 yields typed error, `client_info` null handled, RawTime preserved byte-for-byte, stats top-N map decoding.

Exit criteria: `go test ./internal/adguard/...` green; `go run ./cmd/fakeadguard -addr :8081 -name dns1` serves a paginating querylog.

## Phase 3 — Aggregation core (`internal/aggregate`) — highest-risk logic, prove before any UI

- [ ] **T3.1 Cursor** (`cursor.go`): `Cursor{V int, I map[string]InstanceCursor{O string, D bool}}`; `Encode()` base64url(JSON); `Decode(string)` returning nil (page 1) on empty input, garbage, or version mismatch — must never panic on adversarial input.
- [ ] **T3.2 Merge** (`merge.go`): pure function taking per-instance entry slices (tagged with instance name), returning entries sorted descending by ParsedTime with deterministic tie-break (instance name, then RawTime), trimmed to page size, plus per-instance consumed counts. No mutation of inputs.
- [ ] **T3.3 Fan-out** (`fanout.go`): generic concurrent map over instances (`sync.WaitGroup`, one goroutine each, shared `context.WithTimeout`), returning `[]Result[T]{Instance, Value, Err}` in stable instance order.
- [ ] **T3.4 Logs page assembly** (`logs.go`): `FetchLogs(ctx, clients, Filter{Search, Status, Instances}, cursor, pageSize)` implementing exactly the DESIGN.md algorithm: skip done instances, per-instance `older_than` from cursor, failed instance keeps its old cursor and is reported in `Errors`, next-cursor from consumed counts, `d` set when an instance returned fewer than pageSize and was fully consumed, `HasMore` derived. Returns immutable `LogsPage{Entries, NextCursor, HasMore, Errors}`.
- [ ] **T3.5 Stats merge** (`stats.go`): sum scalar counters; merge top-N lists by summing per key, sort descending, keep top 10; weighted `avg_processing_time` by query count; retain per-instance sub-counts for the UI breakdown.
- [ ] **T3.6 Health** (`health.go`): fan out `Status()` with a short (2s) timeout; per-instance `{Name, Reachable, Version, ProtectionEnabled, Err}`.
- [ ] **T3.7 Tests** (one `_test.go` per file, using the adguardtest fake where HTTP is involved). Mandatory scenarios for `logs_test.go`: (a) 3 instances with interleaved timestamps paginate across 3+ pages with zero loss and zero duplication (assert exact sequence); (b) one instance down: partial page served, its error surfaced, its cursor unchanged, and a retry after "recovery" resumes correctly; (c) an instance exhausts mid-run: marked done, never queried again; (d) all done: HasMore=false, no sentinel. Cursor tests: round-trip, tamper, garbage, wrong version. Merge tests: ordering, ties, trim accounting. Stats tests: top-N merging, weighted average. Health tests: unreachable instance reported.

Exit criteria: `go test ./internal/aggregate/...` green including all mandatory scenarios.

## Phase 4 — Auth (`internal/auth`)

- [ ] **T4.1 Session codec** (`session.go`): `Session{Sub, Name, Email, Expiry}`; AES-256-GCM `Codec` keyed by SHA-256(`SESSION_SECRET`); `Seal`/`Open` with base64url encoding and expiry check on open; cookie read/write helpers (`HttpOnly`, `SameSite=Lax`, `Secure` from config, `Path=/`).
- [ ] **T4.2 Verifier seam** (`verifier.go`): small `IDTokenVerifier` interface (`Verify(ctx, rawIDToken) (*Claims, error)`) so middleware/handler tests never need a live issuer.
- [ ] **T4.3 OIDC flow** (`oidc.go`): provider discovery at startup; `GET /auth/login` generates state + PKCE verifier + nonce, stores them (plus return-to URL) in a short-lived sealed cookie, redirects to the IdP; `GET /auth/callback` validates state, exchanges code (with PKCE), verifies ID token + nonce, sets the session cookie, redirects to return-to; `GET /auth/logout` clears the cookie.
- [ ] **T4.4 Middleware** (`middleware.go`): `RequireAuth` allowlisting `/auth/*`, `/healthz`, `/static/`; no/invalid/expired session: browser requests get 302 to `/auth/login`, requests with `HX-Request: true` get 401 + `HX-Redirect: /auth/login`; valid session placed in request context.
- [ ] **T4.5 Fake issuer** (`oidctest/fake.go`): httptest OIDC issuer with `/.well-known/openid-configuration`, JWKS (generated RSA key), `/token` minting signed ID tokens; knobs for wrong nonce/audience.
- [ ] **T4.6 Tests**: session round-trip/expiry/tamper/wrong-key; full login-callback flow against the fake issuer (happy path, state mismatch rejected, bad nonce rejected); middleware matrix (browser vs HX-Request vs allowlist vs valid session).

Exit criteria: `go test ./internal/auth/...` green.

## Phase 5 — Web layer, logs feature (`internal/web`)

- [ ] **T5.1 Server** (`server.go`): route table on stdlib ServeMux (`GET /`, `GET /partials/logs`, `GET /partials/health`, `GET /stats`, `GET /partials/stats`, `/auth/*`, `GET /healthz`, `GET /static/`), auth middleware wrapped around everything (allowlist handles exceptions), server timeouts, embedded static handler.
- [ ] **T5.2 Templates plumbing** (`templates.go`, `render.go`): `go:embed templates/* static/*`; FuncMap (local time formatting, reason-to-label and reason-to-CSS-class mapping, number formatting); `render` chooses fragment-only when `HX-Request` header present, otherwise wraps fragment in `layout.html` so every URL works as a full page load.
- [ ] **T5.3 Logs handlers** (`logs.go`): parse filter form (search, status, instance checkboxes) + optional `cursor` param; call `aggregate.FetchLogs`; `GET /` renders full page, `GET /partials/logs` renders `logs_rows.html`. Cursor present = append semantics (rows replace the load-more sentinel); absent = page 1 (rows replace table body).
- [ ] **T5.4 Templates** (`layout.html`, `logs.html`, `logs_rows.html`): per DESIGN.md htmx wiring — filter form with debounced triggers, load-more sentinel row carrying the encoded next cursor with `hx-include="#filters"`, auto-refresh conditional polling checkbox, partial-results warning row when `Errors` non-empty, empty-state row.
- [ ] **T5.5 Tests** (`logs_test.go`): httptest server against adguardtest fakes with a pre-forged session cookie: merged rows from multiple instances appear with instance badges; filter params are forwarded to the fakes; the cursor embedded in the rendered sentinel round-trips through a second request with no duplicate/missing rows at the boundary; HX-Request yields fragment, plain GET yields full page; downed instance yields warning row plus surviving rows.

Exit criteria: `go test ./internal/web/...` green; manual run against two fakeadguard instances shows a working merged, filterable, paginating log view (auth via real or disabled-for-dev IdP config).

## Phase 6 — Health bar + stats page

- [ ] **T6.1 Health** (`health.go` handler + `health_bar.html`): `GET /healthz` liveness without upstream calls; `GET /partials/health` fans out and renders per-instance pills (name, up/down, version, protection state), polled every 15s from the layout.
- [ ] **T6.2 Stats** (`stats.go` handler + `stats.html`, `stats_panels.html`): totals tiles (queries, blocked, blocked percent, avg processing time) and three top-10 tables (blocked domains, clients, queried domains), each row showing combined count plus per-instance breakdown; fragment polled every 60s.
- [ ] **T6.3 Tests** (`health_test.go`, `stats_test.go`): reachable/unreachable rendering; merged totals and top-N contents against fakes.

Exit criteria: `go test ./...` green; health pill turns red when a fake is killed; stats page shows merged numbers.

## Phase 7 — UI polish

- [ ] **T7.1 Vendor htmx**: download htmx 2.x minified into `internal/web/static/htmx.min.js` (pin the version in a comment).
- [ ] **T7.2 CSS** (`static/app.css`, ~150 lines, hand-written, no framework): layout, table, filter bar, badges/pills, row color-coding (blocked = red tint, rewritten = amber), stat tiles, `prefers-color-scheme: dark` block.
- [ ] **T7.3 States**: empty-state, error-banner, and loading indicator (`htmx-indicator`) rendering verified.

Exit criteria: UI is readable in light and dark mode; no external network requests from the browser besides the app itself.

## Phase 8 — Ops + docs

- [ ] **T8.1 Dockerfile**: multi-stage — `golang:1.26` build (`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" ./cmd/server`) into `gcr.io/distroless/static-debian12:nonroot`; `EXPOSE 8080`; `USER nonroot`.
- [ ] **T8.2 README rewrite**: what it does, screenshot placeholder, full env var table (copy from DESIGN.md), docker run + compose + k8s deployment snippets, Pocket-ID client setup notes (confidential client, redirect URI), the same-timestamp pagination limitation note.
- [ ] **T8.3 Final gate**: `go vet ./...`, `go test ./...`, `docker build .` all pass; run the DESIGN.md end-to-end verification checklist (two fakeadguard instances + throwaway IdP) and confirm every browser check.

Exit criteria: all three commands green; verification checklist complete.

## Definition of done (whole project)
- `go build ./...`, `go vet ./...`, `go test ./...` pass; docker image builds and runs as non-root.
- Every source file has a sibling test file; no file exceeds 800 lines (target 200-400).
- Zero configuration files read at runtime — env vars only. Zero writes to disk at runtime.
- All routes except `/auth/*`, `/healthz`, `/static/` require a valid OIDC session.
- The DESIGN.md verification checklist passes end-to-end.
