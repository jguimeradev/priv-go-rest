# BACKLOG — todo list

One small task per line. Mark `[x]` with the date when done. Details and reasons live in SESSION_001.md and docs/.

---

## Phase 2 — auth (in build order)

### Repo
- [x] `ReadByEmail` in `repository/user.go` — done 2026-07-14

### Service — `AuthSvc` (`service/auth.go`)
- [x] `AuthSvc` struct + `NewAuthService` constructor — done 2026-07-14
- [x] One-method `AuthRepository` interface (`ReadByEmail` only) — done 2026-07-14
- [x] Sentinel `domain.ErrInvalidCredentials` — done 2026-07-14
- [x] `Login`: fetch by email, collapse not-found → `ErrInvalidCredentials` — done 2026-07-14
- [x] `Login`: bcrypt compare, mismatch → `ErrInvalidCredentials`, other errors wrapped — done 2026-07-14
- [x] `Login`: mint JWT (`RegisteredClaims` sub+exp, HS256, `SignedString`) — done 2026-07-14
- [x] Fix: use `a.tokenLifetime` instead of hardcoded `15 * time.Minute` — done 2026-07-14
- [x] Fix: rename var `login` → `tokenString` — done 2026-07-14
- [x] Add `// TODO: dummy bcrypt compare (timing)` in the not-found branch — done 2026-07-14
- [x] Close the TODO: dummy bcrypt compare on the not-found path — SKIPPED by decision 2026-07-16: learning project, no real attackers; the concept (timing side-channel, why the fix is "do the same work" not `time.Sleep`) was walked through and understood. The `// TODO` comment stays in `Login` as the marker; the item lives on under "Later / ideas"

### Config
- [x] `.env`: add `JWT_SECRET` (generated with `openssl rand -base64 32`) — done 2026-07-14
- [x] `.env`: add `TOKEN_LIFETIME=15m` — done 2026-07-14
- [x] Fix found on the way: `.env` was tracked by git — added to `.gitignore` + `git rm --cached` — done 2026-07-14
- [x] `config.go`: load both new vars into the `Config` struct — done 2026-07-14
- [x] `config.go`: validate (secret ≥ 32 chars, lifetime > 0) — done 2026-07-14
- [x] `config.go`: `time.ParseDuration` once, parse error wrapped and collected — done 2026-07-14
- [x] `main.go`: call `service.NewAuthService(repo, secret, lifetime)` — done 2026-07-14
- [x] `main.go`: delete the four debug `fmt.Println("main - ...")` lines — they print the AuthSvc struct INCLUDING the JWT secret to stdout (secrets never go into logs) — done 2026-07-15
- [x] Incident: `.env` deleted from disk — recovered 2026-07-15: pre-JWT keys restored from git (`e1e58f4^`), `JWT_SECRET` regenerated (old one was never committed, and 15-min tokens make rotation free), `TOKEN_LIFETIME=15m` re-added; config loads, DB connects
- [x] Commit `.env.example` with placeholder values (documents required config, carries no real value) — existed since 2026-07-14 (backlog was stale); placeholders fixed 2026-07-15: secret ≥32 chars so it passes validation, `15m` (not `5min` — `ParseDuration` has no `min` unit)

### Handler (`handler/auth.go`)
- [x] `AuthService` interface (one method, `Login`) in `service/auth.go`; handler depends on it, not on the struct — done 2026-07-15
- [x] `AuthHandler` struct + constructor (own handler, not on `UserHandler`) — done 2026-07-15
- [x] Login request struct (email, password) + validation tags (`required` only — password rules are for registration, not login) — done 2026-07-15
- [x] `HandlePostLogin`: decode → validate → call `Login` — done 2026-07-15
- [x] Error ladder: `ErrInvalidCredentials` → 401 + `WWW-Authenticate: Bearer`; else → 500 (first draft used 409 then 201 — fixed after review) — done 2026-07-15
- [x] Success: 200 + `AuthResponse{token}` object (first draft encoded the bare string) — done 2026-07-15
- [x] Register `POST /auth/login` route (route registration is step zero — the 405 lesson; handler was built and thrown away in `main.go` twice before being stored and registered) — done 2026-07-15
- [x] Verified live end-to-end with curl: 200+token / 401+header wrong password / same 401 unknown email / 400 missing field; decoded payload = `{"sub","exp"}` only — done 2026-07-15
- [x] curl test suite `scripts/test_login.sh` (self-contained: creates its own user, deletes it after) — done 2026-07-16, 10/10 green; finished by Claude under the new CLAUDE.md shell-script exception (ask-first)

### Middleware
- [x] `VerifyToken(tokenString) (int, error)` on `AuthSvc` — parse+verify via `ParseWithClaims` (keyfunc returns the secret, claims by pointer), all parse errors → new sentinel `domain.ErrInvalidToken`, sub converted to int — done 2026-07-16 (detailed writeup in DOCS.md §12)
- [x] New `middleware` package — skeleton `func Middleware(next http.Handler) http.Handler` exists in `internal/middleware/auth.go` — 2026-07-16
- [x] `middleware.Auth(next, TokenVerifier)`: header → `CutPrefix("Bearer ")` → `VerifyToken` → three identical 401s via `unauthorized(w)` helper, handler never runs — done 2026-07-16 (body compiles; not wired yet)
- [x] Fix: move `WWW-Authenticate` into the `unauthorized` helper — done 2026-07-16 (session end; first line of the helper, before `http.Error`)
- [x] Remove the `fmt.Println(id)` placeholder when the context step lands — done 2026-07-17
- [x] Put user id into the request context — done 2026-07-17 (private `ctxKey` type + `userIDKey` const, `context.WithValue` + `r.WithContext`)
- [x] Protect the user routes; `POST /users` and `POST /auth/login` stay public — done 2026-07-17 (`RegisterRoutes(mux, t middleware.TokenVerifier)`, four routes wrapped via `mux.Handle` + `middleware.Auth`; `/health` still doesn't exist)
- [x] Test suite for protected routes — done 2026-07-17: `scripts/test_middleware.sh` 12/12 green (no token / wrong scheme / bare token / garbage / tampered signature / good token / public routes / token-authorized cleanup). Expired-token case NOT covered — needs a frozen token or config override; see Later
- [x] Fix the four older suites the guard broke (login-in-setup + `Bearer` header on guarded calls) — done 2026-07-17, all 5 suites green, 38 checks total

### Phase 2 close-out
- [x] Merge to master — done 2026-07-17: fast-forward `d6dbf05..15a066d` (`feat/middleware` already contained all of `feat/auth`, no chain needed)
- [x] Release — done 2026-07-17: annotated tag `v0.2.0` "login system and middleware", pushed. HIS call v0.2.0 over v1.0.0: password part not done, contract still moving

## Decisions pending

- [x] `master` == `feat/auth` wip commits — resolved 2026-07-16: master moved back to `d6dbf05` (`v.0.1.0`); master holds finished work only

## After Phase 2

- [ ] Password-change endpoint — design first: URL (`/users/{id}/password`?), PUT vs POST, wrong-old-password status (400/401/403), same-password (400 vs 422), success 204?
- [ ] Route + handler for password change (service `ChangePassword` already exists)
- [ ] Authorization design question (posed 2026-07-16, still unanswered): user 12 with a valid token can `GET`/`PATCH`/`DELETE` `/users/10` — today ANY logged-in user reaches ANY user route. Authn is done; authz is not
- [ ] `UserIDFromContext(ctx)` exported helper in `middleware` — the read side of the context value (key is private, only the package can look it up); needed by `/me` and ownership checks
- [ ] Solo checkpoint project — small CRUD API, new resource, from `git init`, no mentor, curl suites included

## Refactors

- [ ] Add `ctx` to ALL repo methods in one pass (`QueryRowContext`/`ExecContext`/`QueryContext`), threaded from `r.Context()` through the service
- [ ] Error contract (Phase 3): translate `validator.ValidationErrors` into one client-friendly shape, stop returning raw `err.Error()`
- [ ] Repo nit: wrap prefix `"Read by Email:"` → `"ReadByEmail:"` (grep-to-function convention)
- [ ] Health: IMPLEMENT `GET /health` — discovered 2026-07-15 it NEVER existed: only a comment line in `handler/user.go`, answers 404. Decide placement (own handler, not `UserHandler`), shallow liveness, explicit 200, handle the `Encode` error
- [ ] Health: readiness variant that pings the DB (own small interface)
- [ ] Optional: `NewUserResponse` mapper into `domain` next to the types

## Dev tooling

- [ ] `air` watch-rebuild-restart (`air init`, exclude `bin/` `scripts/` `.claude/`) as `make dev`
- [ ] `make db-reset` — pipe migrations 001 + 002 into mysql
- [ ] `make test-api` — run all `scripts/test_*.sh`
- [ ] `git mv scripts/test.sh scripts/test_patch.sh`

## Later / ideas

- [ ] `GET /me` endpoint — returns the user who owns the token (id comes from the middleware's request context; no id in the URL). Decided 2026-07-16: NOT in Phase 2 — idea surfaced in test_login.sh review; design it properly after the middleware exists. Middleware exists now (2026-07-17) — unblocked, needs `UserIDFromContext` first
- [ ] Expired-token test case for `test_middleware.sh` — 15m lifetime makes it un-testable live; options: a `TOKEN_LIFETIME` override for a test run, or a frozen pre-expired token
- [ ] Timing side-channel fix in `Login`: dummy bcrypt compare on the unknown-email path (constant = any REAL bcrypt hash, same cost — not a junk string: `CompareHashAndPassword` rejects a malformed hash instantly and the timing gap comes back). Skipped 2026-07-16 as out of scope for a learning project; concept understood. Verify with `curl -w '%{time_total}'` on both 401 paths when done
- [ ] Refresh tokens (deliberately not in Phase 2)
- [ ] Opaque/UUID public ids (sequential ids are enumerable — OWASP BOLA/IDOR)
- [ ] Unit tests for the service layer (fake repo via the interfaces)
- [ ] Integration tests for the repository
- [ ] Final exam: the cloud audio-processing pipeline, mostly solo
