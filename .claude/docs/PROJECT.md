# go-learn-api — Project

A Go REST API (users resource, MySQL) built as a learning project to master API development end-to-end: HTTP fundamentals, layered architecture, error contracts, validation, auth, and eventually async patterns for a cloud audio-processing pipeline.

> **This file merges the former PROJECT.md, SESSION_001.md, docs/DESIGN.md and docs/PHASE2.md** (merged 2026-07-13). It is ordered: design → architecture → patterns → build history → current phase → roadmap. Dates mark when each decision or lesson happened.

---

## 1. API Design

### Endpoints

```
POST   /users           — Register (public)
POST   /auth/login      — Login (public)                    [DONE 2026-07-15, verified live]
GET    /users/{id}      — Read own profile (authenticated)
PATCH  /users/{id}      — Update own profile (authenticated)
DELETE /users/{id}      — Delete own account (authenticated)
GET    /users           — List all users (admin only)
GET    /health          — Liveness probe: process up, no DB claim (always public — load balancers can't log in)
                          ⚠ DISCOVERED 2026-07-15: never actually implemented — only a comment in handler/user.go, answers 404. On the backlog.
```

Auth arrives in Phase 2; until the middleware lands, all endpoints are public.

**User resource:** id, name, email, password. The password never appears in any response, in any form.

### Contract conventions

- **JSON tags are the API contract.** Field names are lowercase (`"id"`, not `"ID"`); renaming a JSON field after clients depend on it is a breaking change.
- **201 Created carries `Location: /users/{id}`**, built by the server from the new id.
- **List endpoints return `[]`, never `null`** — a nil Go slice serializes to JSON `null`, so result slices are always initialized.
- **PATCH is a true partial update**: absent field = keep current value; explicitly empty = 400; valid value = validate and apply (see §5).
- **Responses are receipts, not hopes** (2026-07-06): a status is written only after the work actually succeeded, which is why clients can trust it in one round trip. Verification reads belong in the test suite, never in the client workflow.

### Status code mapping

| Situation | Code |
|---|---|
| Unparseable path id, malformed JSON, validation failure | 400 |
| Resource doesn't exist | 404 |
| Duplicate email (unique constraint) | 409 |
| Successful create | 201 + Location |
| Successful read/list | 200 |
| Successful update (PATCH) | 200 + post-merge body |
| Successful delete | 204, no body, no Content-Type |
| Failed login — wrong password OR unknown email | 401, identical body and timing — user-enumeration defense; never 404 |
| Missing/invalid/expired token on a protected route (Phase 2) | 401 — "I don't know who you are" |
| Valid token, action not permitted (Phase 2) | 403 — "I know who you are, and no" |
| Path matches, method not registered | 405 (sent by the Go 1.22 mux automatically, with an `Allow` header) |
| Any unrecognized error | 500 — internals never leak to the client |

### Data model

`migrations/001_create_users.sql` (DDL lives in migrations, never in Go code):

```sql
CREATE TABLE users (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    name        VARCHAR(100)  NOT NULL,
    email       VARCHAR(255)  NOT NULL UNIQUE,
    password    CHAR(60)      NOT NULL,          -- bcrypt hashes are always 60 chars
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);
```

Seed data: 5 users in `migrations/002_seed_users.sql`, all with password `password123`. Seed data is developer convenience — **tests never depend on it** (see §10).

**On ids (2026-07-03):** an id is an identifier, not a sequence number. AUTO_INCREMENT values are consumed even by failed inserts (InnoDB reserves the id under a short lock before attempting; gap-free numbering is traded for insert throughput — Postgres sequences behave the same). Gaps also come from deletes and rollbacks. Nothing may depend on contiguity; never `MAX(id)+1`. Forward pointer (security phase): sequential ids are enumerable (OWASP BOLA/IDOR) — real systems expose UUIDs or opaque public ids.

---

## 2. Architecture

### Layers — one job each

```
HTTP request
   ↓
middleware   — (Phase 2) runs before handlers: verify token, put identity in context, 401 early
   ↓
handler      — thin HTTP layer: parse, validate shape, call service, map errors to status codes, write response
   ↓
service      — business logic: hashing, merges, authorization rules; knows nothing about HTTP or SQL
   ↓
repository   — database access only; translates DB errors into domain errors
   ↓
MySQL

domain       — shared types + sentinel errors; imported by all layers, imports none of them
config       — loads and validates configuration at startup
main.go      — composition root: the only place that knows every concrete type
```

### Startup steps

1. Load config (from environment).
2. Connect to the database and `Ping` it.
3. Wire the layers: repository → service → handler, via constructors.
4. Register routes.
5. Start the HTTP server.

### Dependency inversion — how the layers connect

Each layer defines an **interface for what it needs**; the layer below satisfies it implicitly — Go checks method signatures, no `implements` declaration exists. The consumer owns the interface: the handler package defines the `UserService` interface it needs; the service implements it without ever knowing the interface exists.

```
create *sql.DB
  → NewUserRepo(db)        → *UserRepo     (satisfies service.UserRepository)
    → NewUserSvc(repo)     → *UserSvc      (satisfies handler-facing service.UserService)
      → NewUserHandler(svc)→ *UserHandler
        → RegisterRoutes(mux), start server
```

- Concrete types flow **downward** through constructors; contracts flow **upward** through interfaces; they meet only in `main.go`.
- Why interfaces at all: with concrete types welded in, a handler test needs a real database. With an interface, a test hands the handler a fake service that simulates "not found" or "email taken" — no database. The interface also *documents* the contract between the layers.
- This is the lightweight Go take on ports-and-adapters (hexagonal/clean): arrows point inward, and the import graph is not the call graph — interfaces let calls go where imports refuse.

### Rules of the road

- **`main.go` is the composition root** — it creates resources and hands them down. Nothing finds its own dependencies.
- **Every injectable struct has a constructor** (`NewX(dep) *X`) with unexported dependency fields.
- **Dependencies belong in signatures, not context.** Middleware and handler are chained by the router, not by function calls, so the standard way to pass request-scoped data between them is `context.Context` (`r.Context()`). Middleware stores the authenticated user id there; the **handler** extracts it and passes it to the service as a plain parameter. The service never reads context values — a service that secretly reads context hides its dependencies and forces every unit test to build a context with magic keys. Explicit beats implicit.
- **The handler owns route registration** (`RegisterRoutes(mux)`); `main.go` just calls it. Registration is step zero of a new endpoint — a missing registration shows up as 405 (see §9, DELETE).
- **Shared types live in `internal/domain`** — when neither layer A nor B should import the other, the shared type goes in a neutral third package. Everything sits under `internal/` (Go forbids importing it from other modules) because this is an application, not a library.

### Project layout

```
go-learn-api/
├── cmd/main.go              — composition root
├── internal/
│   ├── config/              — config loading & validation
│   ├── domain/              — shared types + sentinel errors
│   ├── handler/             — HTTP handlers, request DTOs (user.go, auth.go)
│   ├── service/             — business logic, layer interfaces
│   ├── repository/          — SQL, error translation at the DB boundary
│   └── middleware/          — (Phase 2) auth middleware
├── migrations/              — SQL DDL + seed data
├── scripts/                 — setup.sh + curl test suites
├── Makefile                 — run / build / lint / clean
└── .env                     — local dev config (platform injects in prod; git-ignored)
```

---

## 3. Types & Contracts

### The rule: one struct per *contract*, not per table or method (2026-07-04)

A new type earns its existence only when **shape** (which fields), **optionality** (required vs maybe), **trust** (untrusted client input vs internal data), or **serialization** (wire format) genuinely differs. If two operations share a contract, they share the type.

| Type | Package | Contract | What makes it distinct |
|---|---|---|---|
| `User` | domain | full entity | has the password hash; never leaves the service layer |
| `UserResponse` | domain | what the API returns | JSON tags = public contract; no password |
| `CreateUserRequest` | handler | what a client must send to create | untrusted; `required` validation tags |
| `UpdateUserRequest` | handler | what a client *may* send to update | `*string` fields (three-state optionality) + json/validate tags |
| `UpdateUserInput` | domain | "what the client may have sent", handler→service | bare `*string`, **no tags** — HTTP concerns stay in handler |
| `UpdateUserParams` | domain | "write exactly these values", service→repo | plain strings, maps 1:1 to the SQL SET clause |

These are DTOs / boundary types (never call them "middleware structs" — middleware here means HTTP middleware). The mapping boilerplate is the point: contract mismatches become compile errors instead of leaks.

**Every early bug was one struct serving two contracts:**
- `json:"-"` on the repo struct to hide the password → rejected; it bakes an HTTP concern into the data layer.
- Echoing `CreateUserRequest` back as the response → leaked the plaintext password.
- Reusing `CreateUserRequest` for PATCH → `required` tags physically can't express "optional".

**Why both `UpdateUserRequest` and `UpdateUserInput` when the fields match:** the service needs the pointer struct, but the tagged version lives in `handler`, and service→handler is a backwards import arrow. Same disease as `UpdateUserParams` once stuck in the repo, same cure: the shared type moves to `domain`. It's a **split, not a move** — the json/validate tags stay on the handler twin (the compiler itself enforces part of this: methods must be declared in the type's own package, so the handler's `validateRequest` couldn't follow the type into domain, and dragging the validator library into domain would poison the core).

### The PATCH pipeline — pointers stop at the service

```
handler.UpdateUserRequest    *string + json/validate tags   (wire contract, untrusted)
      ↓ handler maps (pointer-to-pointer copy, no dereference)
domain.UpdateUserInput       *string, no tags               ("maybes")
      ↓ service merges: Read current user → overlay non-nil fields
domain.UpdateUserParams      plain string                   ("answers")
      ↓ repo executes
UPDATE users SET name=?, email=?                            (definite values only)
```

The merge is the moment every "maybe" becomes a definite value; the repository never sees a pointer. The merge's opening `Read` doubles as the existence check — the 404 falls out of a query the merge needed anyway.

---

## 4. Error Handling

### Translation happens at the boundary that knows the source

```
repository  →  sql.ErrNoRows        →  domain.ErrUserNotFound      (only the repo knows it's database/sql)
repository  →  MySQL error 1062     →  domain.ErrMailAlreadyExists (errors.As → *mysql.MySQLError → .Number)
service     →  passes domain errors through unchanged              (already domain language)
handler     →  ErrUserNotFound → 404, ErrMailAlreadyExists → 409, anything else → 500
```

- **Translate only what carries domain meaning; pass through everything else.** "Not found" is a business event (→404); a connection failure is just a failure (→500). A handler must never answer 404 for a database crash.
- **Sentinel test — when does an outcome deserve its own error value?** Ask: does the top-level caller need to *branch* on it? `Read` not-found → yes (404 vs 500) → sentinel. `All` returning zero rows → no (200 + `[]` either way) → no sentinel. An empty collection is a valid state, not an error.
- **Sentinels are package-level `var`s in `internal/domain`** (`ErrUserNotFound`, `ErrMailAlreadyExists`, `ErrSamePassword`, `ErrInvalidPassword`, and Phase 2's `ErrInvalidCredentials`). An inline `errors.New()` creates a fresh value every call that nobody can compare against. Naming: after the broken *invariant* — `ErrMailAlreadyExists` (renamed 2026-07-06 from `ErrUserAlreadyExists`) reads correctly from both POST and PATCH.
- **Always `errors.Is` for sentinel comparison, never `==`** — wrapping with `%w` breaks `==`; `errors.Is` unwraps the chain. **`errors.As`** is for typed errors whose fields you must inspect (`*mysql.MySQLError`).
- **Context wrapping (`"Read: %w"`) belongs at the layer doing the work** — the repo wraps because the SQL runs there; layers above return errors they didn't cause, unwrapped.
- **Detection and reaction are separate:** a function that finds a problem returns it; the caller decides. `config.Load()` returns `[]error`; `main.go` owns `os.Exit(1)`.
- **Never return data and an error together:** if the error is non-nil, every other return value is a zero value. Failure paths return early with `(Zero{}, err)`; the success value is built only on the success path (lesson, 2026-07-06).
- **Every rung of a handler's error ladder ends in `return`** — the swallowed-error bug (2026-07-05): an error that was printed but not acted on let the function fall off the end, and a Go handler that returns without writing sends an implicit 200. *An error you print but don't act on is still swallowed.*
- **Encode errors after headers are sent are unrecoverable** — log them; don't call `http.Error` (the status line already went out).

### Startup failure mechanics (from the original design notes)

| Mechanism | Behavior | When |
|---|---|---|
| `os.Exit(1)` | terminates immediately, no cleanup, no trace | after printing collected config errors |
| `panic` | unwinds the stack with a trace, recoverable | development debugging |
| `log.Fatal(...)` | logs the reason, then exits | preferred for startup failures — the log line explains *why* the process died |

`fmt` writes to stdout; `log` writes to stderr with a timestamp — startup failures belong on stderr.

### RowsAffected — changed vs matched (found 2026-07-05, fixed 2026-07-06)

An UPDATE/DELETE matching zero rows is **not** a SQL error; the repo detects "doesn't exist" via `RowsAffected() == 0 → ErrUserNotFound`. Trap: the MySQL driver by default counts rows *changed*, not rows *matched* — so a no-op PATCH (same values) returned 0 and produced a false 404 on a user that provably existed. Fix: **`cfg.ClientFoundRows = true`** on the `mysql.Config` in `main.go`; `RowsAffected` then means *matched*, the repo's check is truthful on its own, and the race where a row vanishes between the service's `Read` and the repo's `Update` is closed. Caveat: it's a **connection-level flag** — it changes semantics for every statement on the pool. DELETE is unaffected (matched = removed); nothing may silently rely on "changed" semantics without revisiting this flag.

---

## 5. Validation

### Tooling & philosophy

- **Library:** `github.com/go-playground/validator/v10`, rules as struct tags on request DTOs, invoked via a `validateRequest()` method per DTO. Hand-rolling was abandoned (2026-07-02→03) — too time-consuming relative to the learning goal.
- **Deliberately simple for now** (decision 2026-07-03): no character-class password rules; the raw validator message is returned to clients. Translating `validator.ValidationErrors` into a client-friendly error contract is deferred to Phase 3 — the unwrap loop written during the 2026-07-04 debugging session is its seed.
- **Validate at trust boundaries:** the handler checks request *shape* (format, lengths); the service checks *business rules* (uniqueness via the DB, permissions). Garbage never reaches the business logic.
- **Check ordering — cheap before expensive:** path id parse (400) → JSON decode (400) → tag validation (400) → only then any DB round trip.
- **Try-then-handle, not check-then-act:** don't pre-flight-check email uniqueness — that's a race; the DB's UNIQUE constraint enforces it atomically. Attempt the INSERT, translate error 1062.

### Field rules

| Field | Rules |
|---|---|
| name | required, min 5, max 100 |
| email | required, valid format, max 255 |
| password | required, min 8, **max 72** — bcrypt silently ignores input past 72 bytes; the DB's CHAR(60) is the *hash* length, not the input limit |

For PATCH the same rules apply, but only to fields actually sent (`omitempty` leads each tag chain).

### The three-state problem (PATCH, 2026-07-03)

Decoding JSON into a plain `string` destroys "was the key present?" — absent and `""` both become `""`. The contract needs three states, so update DTOs use `*string`:

| JSON | Pointer | Meaning | Outcome |
|---|---|---|---|
| key absent | `nil` | keep current value | `omitempty` skips remaining rules |
| `"name": ""` | `&""` | explicitly empty | `min=5` runs against `""` → 400 |
| `"name": "Valid"` | `&"Valid"` | change it | rules run → validate & apply |

`encoding/json` fills this correctly for free — absent keys are never touched, so they stay nil. Always nil-check before dereferencing (nil dereference panics). The dead end that taught it: plain strings + "`""` means bad request" would 400 on *absent* fields too, forcing clients to send everything — PUT wearing a PATCH name.

**`omitempty` with pointers:** "empty" means *nil pointer*. validator/v10 dereferences pointer fields before applying rules; `omitempty` is the only tag inspecting the pointer itself. And `omitempty` is validation-only — it never "keeps the old value"; the service merge does that. Validation decides if the *request* is acceptable; the merge decides what the *write* looks like — two questions, two homes.

### Where the merge lives — options considered (2026-07-03)

1. **Read-modify-write in the service (chosen):** `Read` current user, overlay non-nil fields, `Update`. Two queries, but the merge is ordinary Go — readable, testable; and the initial `Read` doubles as the existence check.
2. Dynamic SQL SET clause — one query, merge hidden in string construction.
3. `COALESCE(NULLIF(?, ''), column)` — one query, merge hidden in SQL.
4. Require all fields — that's PUT; rejected, the point was real PATCH semantics.

---

## 6. Layer Reference

### Repository (`internal/repository/user.go`)

- `*sql.DB` is a **connection pool**, injected via `NewUserRepo(db)` — the repo never calls `sql.Open()` itself.
- Method map: `Exec` → INSERT/UPDATE/DELETE; `QueryRow`+`Scan` → single row; `Query`+loop+`Scan` → many rows, `defer rows.Close()` immediately after the error check, final `rows.Err()` after the loop.
- **Always `?` placeholders — never string concatenation.** Concatenated SQL is SQL injection.
- **The repo fetches faithfully:** `Read` returns the full `domain.User` including the hash — the *service* decides what to expose per context. Never strip fields at the repo level for one caller; another caller (ChangePassword, login) needs them.
- `All()` deliberately selects only id/name/email — the password column isn't in the list query at all.
- Profile updates and password writes are **separate methods** (`Update` vs `UpdatePassword`): different operations with different service flows, and a full-`User` update risked overwriting the password with a zero value.
- Params structs pass **by value** unless large, mutated, or nil-is-meaningful.

### Service (`internal/service/user.go`)

| Method | Returns | Notes |
|---|---|---|
| `CreateUser(name, email, password)` | `(int, error)` | id feeds the Location header |
| `ReadUser(id)` | `(UserResponse, error)` | maps entity → response, drops the hash |
| `FetchAllUsers()` | `([]UserResponse, error)` | initialized slice, never nil |
| `UpdateUser(id, UpdateUserInput)` | `(UserResponse, error)` | owns the PATCH merge; returns post-merge state |
| `DeleteUser(id)` | `error` | |
| `ChangePassword(id, old, new)` | `error` | flow implemented, **no route yet** (deferred) |

- **Passwords are hashed here** with bcrypt — the repo never sees plaintext. `bcrypt.GenerateFromPassword`'s second argument is a **cost factor** (work, not output length; output is always 60 chars) — use `bcrypt.DefaultCost`, never a bare number. Shared `hashPassword` helper (bcrypt errors on input > 72 bytes).
- **Password verification** is `bcrypt.CompareHashAndPassword` — never string comparison against a hash. Only `bcrypt.ErrMismatchedHashAndPassword` means "wrong password"; any other bcrypt error (e.g. malformed hash) is infrastructure and must not masquerade as it — distinguish with `errors.Is`.
- **ChangePassword flow:** `Read(id)` → compare old vs stored hash → same-password check (plain `==` is valid *there*: two plaintexts, one just verified) → hash new → `UpdatePassword`. Open design questions for its future route: resource vs action (`/users/{id}/password`?), PUT vs POST (the body mixes new state with proof), status for wrong-old-password (400/401/403) and same-password (400 vs 422), 204 on success?
- **Authorization lives here** (Phase 2): "may user X do this?" is a business rule. Authentication ("who are you?") is middleware's job.

### Handler (`internal/handler/user.go`, `auth.go`)

- Holds the `service.UserService` **interface**, never the concrete struct.
- Standard shape: parse path values (`r.PathValue`, Go 1.22+ patterns like `GET /users/{id}`; bad id → 400 via `strconv.Atoi`) → decode → validate → call service → error ladder → write response.
- **ResponseWriter ordering: headers → `WriteHeader(status)` → body.** Headers set after `WriteHeader` are silently dropped (this ate the Location header once); a body write without `WriteHeader` implies 200 — a create must send 201 explicitly.
- `Header().Set` for single-valued headers (Location, Content-Type); `Add` appends.
- The response body is what the server *created* (a `UserResponse` built from the service result) — never an echo of the request (that leaked a plaintext password once).
- Servers *construct* Location themselves (`"/users/" + strconv.Itoa(id)`); `r.Response` is nil server-side — it exists for client-side redirect handling.

---

## 7. Configuration

- **`.env` is local-dev only** and **git-ignored** (it holds secrets). Production platforms (ECS task definitions, Kubernetes secrets, Azure App Settings…) inject env vars directly; no `.env` is ever deployed. `os.Getenv()` reads the process environment either way — the code doesn't change, only the variable's source does.
- **`APP_ENV` bootstrap problem:** `APP_ENV` itself may live in `.env`, so you can't load `.env` to discover which environment you're in. Read it from the OS env *first*: empty → development → load `.env` via `godotenv`, fail fast if the file is missing; set → production → skip `.env`, trust the platform. (godotenv detail: `Load` won't overwrite variables already set; `Overload` will.)
- Ten required vars (2026-07-14): `APP_ENV`, `APP_PORT`, `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `NET_PROT`, and Phase 2's **`JWT_SECRET`** + **`TOKEN_LIFETIME`**. Validation collects **all** problems into `[]error` before reporting; the app exits before the server starts.
- **`JWT_SECRET` rules (2026-07-14):** generated OUTSIDE the app, once (`openssl rand -base64 32`) — if the app minted its own at startup, every restart would log everyone out, and each instance behind a load balancer would reject the others' tokens. Quality check in `Validate()` (the first check on a value, not just presence): shorter than 32 chars → refuse to start. Why 32+: an attacker holding one token can brute-force the secret OFFLINE (compute HMAC with guesses until the signature matches) — a short or human-word secret falls in seconds, then they mint tokens for any user.
- **`TOKEN_LIFETIME` (2026-07-14):** the env string (`"15m"`) is `time.ParseDuration`d ONCE at startup into a `time.Duration` on the `Config` struct; parse error is wrapped (`%w`, keeps the cause) and appended to the collected error list. `time.Time` is a point on the calendar; `time.Duration` is an amount — they meet in `Login`: `time.Now().Add(lifetime)` = the expiry point.
- **Incident (2026-07-14): `.env` was tracked by git** since the early commits — `.gitignore` never listed it; the dev DB password sat in pushed history. Fixed: `.gitignore` + `git rm --cached .env` + commit; the JWT secret was still uncommitted and never entered history. Old value = burned (accepted: local dev DB, private repo). Lessons: *a secret that has ever touched git is not a secret anymore*; verify ignore rules BEFORE a secret exists on disk. Pending: commit `.env.example` with placeholders.
- **Secrets never go into logs (2026-07-14):** `fmt` prints unexported struct fields — the scaffolding `fmt.Println("main - newauthsvc:", l)` printed the `AuthSvc` including the JWT secret to stdout on every startup; stdout is the log stream in production. The four wiring debug prints are scheduled for deletion (BACKLOG).
- **DB connection (idiomatic path):** `mysql.NewConfig()` populated from the validated `Config` struct (never re-read `os.Getenv` after `Load()`) → `mysql.NewConnector()` → `sql.OpenDB()` → `db.Ping()`. `sql.Open` is lazy — only `Ping` proves the DB is reachable.
- **`cfg.ClientFoundRows = true`** is set on the driver config (see §4 for the full story).

---

## 8. Go Idioms Adopted

- Early returns; no `else` after `return`.
- Unexported = package-private; exported = public API; if callers don't need it, lowercase it.
- Blank imports (`_ "pkg"`) only for side effects (driver registration) — never for packages you call.
- `strconv.Itoa`/`Atoi` for int↔string — `string(65)` is `"A"` (a code point), and `go vet` catches it.
- `fmt` → stdout; `log` → stderr with a timestamp.
- Pointer receivers by convention for shared types — prevents silent bugs if mutable state is added later.
- Comments explain the non-obvious *why*, never restate the code.
- Test packages: `package x_test` (black-box, the user's view) when exported API suffices; `package x` for internal access.
- When debugging library errors, unwrap structured errors (`errors.As` → print fields) instead of squinting at a flat `err.Error()` string — **make the system testify**, and confirm a "failure" isn't correct behavior before theorizing (the validator misdiagnosis, 2026-07-04).

---

## 9. Build History & Lessons (Phase 1, closed 2026-07-06)

### Timeline

| Date | Milestone |
|---|---|
| 2026-06-18 | Design, scaffolding, config strategy |
| 2026-06-29 | Service interface sketched |
| 2026-07-01 | MySQL connected; repository finalized; error doctrine learned |
| 2026-07-03 | POST /users done; validator switch; PATCH designed |
| 2026-07-04 | DTO rule found; PATCH data flow; validator misdiagnosis resolved |
| 2026-07-05 | PATCH rollback + clean rebuild; swallowed-error and rows-changed lessons |
| 2026-07-06 | PATCH (`57159f2`), DELETE (`adcf17d`), GET list + health (`e48d0ab`), suites 15/15 (`d6dbf05`) — **Phase 1 closed**, `v0.1.0` released, Phase 2 kicked off |
| 2026-07-13 | Auth contract closed; AuthService split decided; build step 1 in progress |

### Per-endpoint lessons (what each one taught beyond its code)

- **GET /users/{id}:** bad path id is the *client's* error → 400, not 500. Content-Type before body; an encode failure after headers is only loggable.
- **POST /users (2026-07-03):** header/status/body ordering; never echo the request; JSON tag casing is a public promise; Location is constructed, not read.
- **PATCH /users/{id} (2026-07-03→06):** the three-state contract and everything in §3/§5. Plus: **copy-paste is a contract smell** (POST's 201+Location pasted onto PATCH — nothing was created; the suite caught it); **recompile before re-testing** (a "failing" fix was a stale binary — three times; `air` auto-restart queued); the rollback-and-rebuild (2026-07-05) proved a settled *design* survives a code reset.
- **DELETE /users/{id} (2026-07-06):** 204 decided by "what would the body even say?"; headers must not contradict the status (no Content-Type on a 204); **double-delete 204→404 does not break idempotency** — idempotency (RFC 9110) promises about *server state* (N calls = 1 call), not about responses; practical payoff: idempotent requests are safely retryable after a timeout. The ladder is *shorter* than PATCH's (no 409 possible) — reasoned per endpoint, never pasted. And **405 located a missing route registration in one step** — the mux answers it when the path matches but the method isn't bound; status codes are diagnostic inputs.
- **GET /users + GET /health (2026-07-06):** one-rung ladder — an empty table is a valid answer, not an error; `[]`-never-`null`; health is deliberately shallow **liveness** (no DB claim). Parked: a readiness variant (needs a DB-pinger — smallest-interface question), the receiver-less method smell, the implicit 200.

### Release `v0.1.0` (2026-07-06)

A tag names one commit forever (tags don't move, branches do — a branch can sprout from a tag any time). Semver `0.x` = "contracts may still change"; `1.0.0` is a stability promise that can't be made before auth. A GitHub Release is a presentation wrapper over a git tag. Go modules read `vX.Y.Z` tags.

### The learning plan (agreed 2026-07-06)

No Phase 1 redo. After Phase 2: a **solo checkpoint project** — small CRUD API, different resource, from `git init`, no mentor, no peeking at this repo, same standards including curl suites; the gaps found are the findings. Final exam: the audio-processing pipeline, built mostly solo with the mentor as reviewer only.

---

## 10. Testing

- Every endpoint has a **self-checking curl suite**: each test declares its expected status, prints PASS/FAIL (+ body on failure), exits nonzero on any failure (composable by `make`'s fail-fast). Expected codes describe the *finished* behavior — written early, the failures are the to-do list.
- **Suites create the data they need.** The fixture incident (2026-07-06): the PATCH suite went 5/7 red because seed user 4 had been deleted hours earlier by a manual curl — a *fixture* regression, not a code regression. Fix: seed data became a runnable migration ("state that exists only because someone once typed it is state you can't recreate"), and all suites use the **disposable-user pattern** — setup POSTs a throwaway user with a unique email (`$$-$RANDOM`), tests target it. Seed data is dev convenience, never a test dependency.
- The DELETE suite's lifecycle triple encodes the concepts it tests: delete → 204 (receipt), GET → 404 (state really changed), delete again → 404 (idempotent state, informative response). The suites exist because POST returns the id — the API's own contract makes it testable.
- Phase 1 closed at **15/15 green**: `test.sh` (PATCH, 7), `test_delete.sh` (5), `test_get_users.sh` (3).

| Command | Does |
|---|---|
| `make run` | `go run cmd/main.go` (from root, so `.env` is found) |
| `make build` / `make clean` | compile to / remove `bin/app` |
| `make lint` | `go vet ./...` |
| `bash scripts/test.sh` | PATCH suite (rename to `test_patch.sh` pending) |
| `bash scripts/test_delete.sh` | DELETE suite |
| `bash scripts/test_get_users.sh` | list suite |
| `mysql … < migrations/001…` then `002…` | reset to known DB state (`make db-reset` target pending) |

---

## 11. Phase 2 — Authentication (kickoff 2026-07-06 · contract closed 2026-07-13 · CLOSED 2026-07-17, released `v0.2.0`)

### Why tokens (2026-07-06)

HTTP is stateless — request N has no memory of N−1, so something must carry identity. **Sessions** keep it server-side; behind a load balancer that means sticky routing or a shared store (Redis). **Tokens** are a self-contained, tamper-proof proof the *client* carries; any instance verifies alone → JWT, which fits this project's cloud goals.

### JWT anatomy (2026-07-06)

One string, three base64 chunks: `header.payload.signature`.
- **Header:** which algorithm signed it (HS256 = HMAC-SHA256).
- **Payload = claims:** statements the server asserts. RFC 7519 standard names: `sub` (subject), `exp` (expiry), `iat` (issued at); custom claims allowed.
- **Signature:** HMAC over the first two chunks with the server's secret — anyone can read, only the server can mint, nobody can tamper.
- **The metaphor:** *the claims are a note I write to my future self; the client is the courier.* Tomorrow I read my own note: "this is user 42, trust it until Friday."
- The payload is **encoded, not encrypted** — anyone holding the token reads it (jwt.io). Nothing sensitive goes in it. The secret is crown jewels: config/`.env`, never git.

### The contract — three questions, three answers

**Q1 (2026-07-06): failed login → 401, always — unknown email gets the SAME 401.**
Telling "no such email" apart from "wrong password" is a **user-enumeration** oracle (OWASP). Sameness on three levels: status, body, **timing** — bcrypt (~100ms, deliberately slow) only runs when the email exists, so the missing-email path is measurably faster; fix = dummy bcrypt compare on that path. 401 vs 403, once and for all: **401 = "I don't know who you are"** (bad login, missing/invalid/expired token — despite the misleading name "Unauthorized", it means unauthenticated); **403 = "I know who you are, and no."** Code consequence: the login service method **collapses** `ErrUserNotFound` + wrong-password into one sentinel **`ErrInvalidCredentials`** — the repo stays truthful, the service does the hiding; if `ErrUserNotFound` escaped, the ladder would map it to 404 and leak the very fact being hidden. First time in the project that hiding information is the design goal. (Spec nicety: 401 carries `WWW-Authenticate: Bearer`.)

**Q2 (2026-07-13): claims = `sub` (user id) + `exp`. Nothing else.**
Two gates for any claim: (1) the courier reads the note → public-safe only; (2) needed on every request → the id is the key to everything else; email/name are fetchable. Strongest argument: the note is **frozen** at signing — a PATCHed email would ride stale in the token until expiry. **Rule: only facts that cannot change while the token lives belong in claims.**

**Q3 (2026-07-13): lifetime = 15 minutes.**
Stolen-token window vs re-login friction; 15 min is the production-grade choice. Refresh tokens are the standard cure for the friction — **deliberately out of scope for Phase 2**; token dies → log in again. Lifetime + secret live in config.

### Design decisions (2026-07-13)

- **Login gets its own `AuthService`.** `UserService` answers "do something to the users resource" (CRUD); login answers "is this person who they claim to be? here's a token." One struct per *contract*, not per table — same test that split health-check off `UserHandler` and drives the DTO rule (§3). The split is **service-layer only**: one users table → one `UserRepo`; `AuthService` is its second consumer. No `AuthRepo`.
- **The repo fetches, the service compares.** The repo's whole part in login: "give me the user with this email" — returns the row **including the password hash**, or `ErrUserNotFound`; it never sees the plaintext. The service holds both pieces (stored hash + submitted plaintext) and runs the bcrypt comparison itself.
- **`AuthService` dependencies** (handed by `main.go`): the user repo, the JWT secret, the token lifetime. Minting is a library call — `golang-jwt` is the standard package.
- **`AuthHandler` is its own handler** in `handler/auth.go` — don't bolt login onto `UserHandler` (the health-check lesson).

### Build plan (locked 2026-07-13, branch `feat/auth` — steps 1–2 built 2026-07-14)

1. ✅ **Repo — read-by-email** (`repository/user.go`, DONE 2026-07-14): `ReadByEmail(email)` next to `Read`; returns the user *including the password-hash column* (the one query allowed to select it; DTOs already strip it from responses). `SELECT` with `?`, scan; `sql.ErrNoRows` → `ErrUserNotFound`; anything else wrapped. **No `ctx`** — decision 2026-07-14: match the existing methods now; add `ctx` to ALL repo methods in one future pass (BACKLOG.md).
2. ✅ **Service — `AuthSvc`** (`service/auth.go`, DONE 2026-07-14; only the timing TODO stays open until the endpoint ships): struct holds repo + secret + lifetime (`time.Duration`); own one-method `AuthRepository` interface (only `ReadByEmail` — consumer declares its needs; `*UserRepo` satisfies it implicitly, still the only concrete repo). `Login(email, password)`: fetch by email → bcrypt compare → either failure → `ErrInvalidCredentials` (dummy bcrypt on the not-found path = open `// TODO` in the code) → success mints the JWT (`jwt.RegisteredClaims`: `Subject` = id as string, `ExpiresAt` = `time.Now().Add(a.tokenLifetime)`; HS256; `SignedString([]byte(secret))`) and returns the token string.
3. ✅ **Config — JWT secret + token lifetime** (DONE 2026-07-14): `.env` has `JWT_SECRET` (openssl-generated) + `TOKEN_LIFETIME=15m`; `config.go` loads + validates (secret ≥ 32 chars, lifetime > 0), `ParseDuration` once, parse error wrapped + collected; `main.go` wires `service.NewAuthService(r, c.JwtSecret, c.TokenLifetime)`. Side quest: `.env` untracked from git (see §7). Leftover: delete the four wiring debug prints (one leaks the secret to stdout).
4. ✅ **Handler — `handler/auth.go`** (DONE 2026-07-15, verified live with curl): one-method `AuthService` interface in `service/auth.go` (handler depends on it, not on the struct); `LoginRequest` (`required` only — password quality rules belong to registration); `POST /auth/login` registered; decode → validate → `Login`; success → 200 + `AuthResponse{token}` object; `ErrInvalidCredentials` → 401 + `WWW-Authenticate: Bearer`; else → 500. NO 404 rung — `ErrUserNotFound` never escapes `Login`; a 404 here would be the enumeration leak. Still open before middleware: `scripts/test_login.sh`, the timing TODO (dummy bcrypt), `.env.example`.
5. ✅ **Middleware — new package** (DONE 2026-07-17, verified live): `VerifyToken` + `middleware.Auth` built 2026-07-16; context step + route wiring 2026-07-17. User id travels in the request context (private `ctxKey` type, `context.WithValue`, `r.WithContext`); guard wraps the four protected user routes at registration time (`RegisterRoutes(mux, t middleware.TokenVerifier)`, `mux.Handle` + `Auth(http.HandlerFunc(method), t)`); public: `POST /users`, `POST /auth/login` (`/health` still unbuilt). `scripts/test_middleware.sh` 12/12; all five suites green (38 checks).

### Build lessons (2026-07-14)

- **Login's error policy in one sentence:** `ErrInvalidCredentials` appears exactly twice (unknown email, wrong password — the two client-fault cases, deliberately indistinguishable); every other error wraps with `fmt.Errorf("Login: %w", err)` and bubbles to the handler's fallback 500. Bcrypt split: `ErrMismatchedHashAndPassword` → sentinel; a malformed stored hash is a server problem → 500, never 401. A signing failure fires *after* both checks passed — the client did everything right — so it is never `ErrInvalidCredentials`. **Rule: choose an error by its true story.**
- **`domain.ErrInternalServer` — created, then deleted:** replacing unknown errors with a generic sentinel destroys the original cause (gone from every future log) and puts HTTP language ("internal server error" = the name of 500) into the domain layer. Unknown failures need no sentinel — the fallback rung handles them; they just have to arrive intact.
- **An interface is a claim:** `ReadByEmail` was briefly added to `UserRepository` too — false claim (`UserSvc` never calls it) and every future test fake would be forced to implement it. Removed; `AuthRepository` (one method) is the only home. Same muscle as the health-check split.
- **Docs examples teach shape, not text:** the golang-jwt example (custom claims type, "johndoe", hardcoded values) got pasted into `main.go` → syntax error, wrong file, wrong content. Claims are built *inside* `Login`, locally (nobody outside sees them); every value comes from variables in scope (`u.ID`, `a.tokenLifetime`); the contract decides the fields, not the example. `jwt.RegisteredClaims` chosen over `MapClaims` (typed, compiler-checked); a custom claims type is only needed for non-standard claims.
- **Config values travel through the struct:** `main.go` reads config once and injects; `Login` reads its own fields. The service never imports config or calls `os.Getenv` — same rule as the repo not opening its own DB connection; payoff is one-line test construction.
- **HS256 key must be `[]byte`** — a plain string compiles but fails at runtime ("key is of invalid type").
- **The timing TODO lives in the hole it marks:** inside the `ErrUserNotFound` branch, on the exact line where the dummy compare will go — the fast path must visibly say "known gap, fix planned", not look deliberate.
- **Implicit interfaces seen live (2026-07-14):** the same `*UserRepo` feeds `NewUserSvc` (7-method `UserRepository`) and `NewAuthService` (1-method `AuthRepository`) — an interface parameter means "anything with these methods"; one person, two ID cards, each door checks only its own. Deletion test: removing `ReadByEmail` from the repo breaks ONLY the auth line — a deletion breaks exactly the consumers whose interface names the method, which is why false interface claims make changes look more dangerous than they are.
- **Config validation grew its first quality check (2026-07-14):** presence checks ask "is it set?"; the secret's length check asks "is it any good?" — both fail startup. Three review rounds on the way: the length check first landed on `AppEnv` (app could never start), `string(duration)` repeated the `string(65)` trap on a number type, and the parse error went from ignored → replaced-with-fixed-text → properly wrapped with `%w` into the collected list.

### Build lessons (2026-07-15)

- **Each layer names only what it needs from the layer below:** first handler draft depended on the concrete `service.AuthSvc`; replaced with a one-method `AuthService` interface (the handler uses exactly `Login` — not the secret, not the repo). Same move as `AuthRepository`, one layer up. Free fix included: `NewAuthService` returns a pointer, and the pointer is what carries the method — the interface field accepts it directly.
- **Say the status code out loud before using it:** first draft answered 409 ("your request fights the resource's state — fix and retry"; right for duplicate email, unrelated to identity) and 201 ("I created a resource with an address"; a token has no address). The contract already written (Q1: 401-always + `WWW-Authenticate: Bearer`; success 200) had decided both — the draft contradicted its own design doc.
- **Response bodies are objects, not bare values:** `Encode(token)` produced `"eyJ..."` — legal JSON, unusable contract. `AuthResponse{Token}` gives clients a field to pick and the server room to grow. Lives next to `LoginRequest` in the handler file: HTTP contract, not domain.
- **Login validation is `required` only:** password rules (min 8/max 72) are for *creating* passwords; at login the stored hash is the truth — a short guess just fails bcrypt into the same 401. A `min=8` tag would answer 400 to that request: two answers for one fact, free information for an attacker.
- **A constructor's return must land in a variable:** `handler.NewAuthHandler(a)` built the handler and threw it away — routes never registered, endpoint a 404. Route registration is step zero (the 405 lesson, third appearance).
- **`.env` deleted and recovered (2026-07-15):** git had only the pre-JWT version (the secret was added after untracking — so it never leaked, which is the system working). Regenerated the secret; rotation cost zero because 15-min tokens die on their own. Traps met: no trailing newline in the committed file glued the appended key onto the last line. Consequence: `.env.example` promoted from nicety to necessity.
- **Notes can lie; the code is the record:** every doc said `GET /health` shipped 2026-07-06 — it was only ever a comment line; no handler, no route, 404 in every commit. Found by actually curling it. Corrected everywhere; implementation now on the backlog.
- **Seen on the wire:** the token's middle chunk base64-decodes to `{"sub":"9","exp":...}` — the whole claims contract, physically visible, nothing extra.

### Build lessons (2026-07-16)

- **`test_login.sh` done, 10/10 green** — written by Claude under a new CLAUDE.md exception (may write `.sh` files, always asking first; Go stays fully under the no-code rule — deliberate rule change, not a broken rule). Suite: register 201 + no-leak, login 200 + JWT-shape check, wrong-password 401 + `WWW-Authenticate`, unknown-email IDENTICAL 401, missing-field 400, cleanup DELETE 204.
- **A red test is a to-do only when it encodes an AGREED contract** — the draft suite asserted `GET /me`, a route never designed; that's a guess smuggled in through a test file. `/me` went to the backlog as an idea; its tests left the suite.
- **Timing TODO skipped by decision** (scope, not ignorance): concept fully walked; fix recipe parked in backlog "Later / ideas"; `// TODO` stays in `Login`.
- **A script's exit code is its LAST command's code** — heredoc scratch-comments pasted after `[[ $FAIL -eq 0 ]]` in `test.sh` silently forced exit 0 on every run: the suite lied to machines while telling humans the truth.
- **A hash is a fingerprint, not a container** — seed users carried a tutorial bcrypt hash whose plaintext nobody knows; unrecoverable by design. Fix: mint a hash through the API itself (`POST /users` with a chosen password, read the hash from the DB, put it in the seed — the API is the bcrypt generator you already own).
- **`master` cleaned:** moved back to `d6dbf05` (`v.0.1.0`) — master only holds finished work; wip auth commits live on `feat/auth`; `feat/middleware` branched off for step 5. Idle `dev` branch deleted (local+remote): an unused branch is a lie waiting to happen. Safe because a branch is a name-tag — `feat/auth` still names every commit.
- **`middleware.Auth` body built** — signature `Auth(next http.Handler, t TokenVerifier) http.Handler`: handler in, handler out; the returned closure holds both. Middleware OWNS its one-method `TokenVerifier` interface (consumer-owned interfaces, third instance — handler's `AuthService` keeps only `Login`). Flow: missing header → 401; `strings.CutPrefix(header, "Bearer ")` (the space is part of the prefix!) not found → 401; `VerifyToken` error → 401; else `next.ServeHTTP`. All denies through one `unauthorized(w)` helper — three identical answers, no hints. Traps met: `== " "` (space) instead of `== ""` let tokenless requests through; a branch that sets headers but never writes a status sends an implicit 200 — *the guard forgot to say no*. Open: `WWW-Authenticate` into the helper; `fmt.Println(id)` placeholder dies at the context step; not wired into `main.go` yet.
- **`VerifyToken(tokenString) (int, error)` built in `AuthSvc`** — full walkthrough in DOCS.md §12 ("how token verification actually works"): verification = the token compared WITH ITSELF under the secret (recompute chunk 3, compare, check `exp`) — nothing stored, nothing looked up; library does the compare inside `ParseWithClaims`; keyfunc hands over `[]byte(secret)` (first draft minted inside it — every token would have failed); claims struct passed as pointer = the fill-my-box pattern (`Decode(&l)` again); all parse errors collapse to new sentinel `domain.ErrInvalidToken` (wristband failure ≠ credentials failure); sub converted string→int so JWT details stay in the service.

### Build lessons (2026-07-17) — context, wiring, release

- **Request scope is the third variable scope:** globals are shared by all requests (wrong), threading a new parameter through every signature doesn't survive change (unsustainable) — so servers need data scoped to *one request's whole path*. `context.Context` is that scope as a value: born with the request, rides inside it (`r.Context()`), dies with it — which is also why it never violates statelessness (nothing survives between requests). Correction pair: goroutine = the request's own worker; context = the request's own luggage.
- **Contexts are extended, never mutated:** `context.WithValue(old, key, val)` returns a NEW context wrapping the old; `r.WithContext(ctx)` returns a NEW request. Three lines replaced the `fmt.Println(id)` placeholder.
- **Context keys are types, not strings:** keys compare by value AND type; a string key `"userID"` collides with any library that picked the same obvious name. A private `type ctxKey int` makes collision impossible — nobody outside the package can construct the type. The value of the const is irrelevant (`0`); the type is the uniqueness. Consequence: reading the value back from outside needs an exported helper (`UserIDFromContext` — backlog, the door to `/me` and ownership checks).
- **The guard wraps routes, not the mux:** wrapping the whole mux would lock `POST /users` and `POST /auth/login` — you'd need a token to get a token. Wrapping happens per-route at registration: protected routes switch `mux.HandleFunc(pattern, method)` → `mux.Handle(pattern, middleware.Auth(http.HandlerFunc(method), t))`. Two mechanics: `Handle` not `HandleFunc` (Auth returns an `http.Handler`), and the `http.HandlerFunc(...)` conversion (the costume, second appearance). `RegisterRoutes` gained a `middleware.TokenVerifier` param — `main.go` hands in `authSvc`, which satisfies it implicitly (fourth consumer-owned-interface payoff).
- **A breaking change is measured in broken clients:** turning the guard on broke four existing curl suites mid-session — they called guarded routes tokenless. The API's contract changed and every existing client broke: that is what "breaking change" means, felt live. Directly fed the release decision.
- **`v0.2.0`, not `v1.0.0` (his call, correct reasoning):** password change, `/health`, `/me`, authz are all still coming — the contract is moving, and 1.0.0 is a stability promise you make *after* it stops. Annotated tag (`git tag -a`: author, date, message — a release is a statement) over lightweight. Tag-style fix: `v0.2.0`, no stray dot after the v.
- **Merge chains can be shorter than they look:** `feat/middleware` grew on top of `feat/auth`'s tip, so it already *contained* the whole auth history — one fast-forward merge to master (`d6dbf05..15a066d`), no intermediate merge needed.
- **Suites now log in to test:** the four Phase 1 suites create their disposable user, log in as it, and send `Bearer` on every guarded call — self-containment (the user-4 lesson) now includes identity.

---

## 12. Roadmap

| Phase | Scope | Status |
|---|---|---|
| **1 — Core CRUD** | five endpoints + health, deliberate status contracts, curl suites 15/15 | ✅ **CLOSED 2026-07-06** (`v0.1.0`) |
| **2 — Authentication** | JWT contract → repo read-by-email → AuthService → login handler → middleware → protect routes | ✅ **CLOSED 2026-07-17** (`v0.2.0` "login system and middleware"); all 5 suites green (38 checks); timing TODO + expired-token test + authz deliberately parked |
| **3 — Refinements** | consistent error contracts (translate `ValidationErrors`), stricter validation, structured logging, tests (unit for service, integration for repo) | |
| Later | versioning, OpenAPI, observability, security (rate limiting, OWASP API Top 10), async patterns, gRPC | |

**Deferred / parked items:** password-change route + its design questions (§6); authorization (user 12 can still reach `/users/10` — the open authn-vs-authz question); `UserIDFromContext` helper; `GET /health` implementation; `/me`; expired-token test case; `air` hot-reload dev loop; `git mv test.sh test_patch.sh`; `make db-reset` + `make test-api`; readiness health variant; refresh tokens.

**Open questions (running list):**
- What should a consistent, client-facing error response look like? (Phase 3)
- How to structure DB access so it's testable without a real database? How to unit-test the service without the DB? (mock the repo via its interface)
- ~~`context.Context` mechanics end-to-end~~ — ANSWERED in two halves: cancellation 2026-07-14; values-in-context 2026-07-17 (request scope, immutable-extend, typed private keys — §11 build lessons). Still pending in practice: the repo-wide ctx retrofit (BACKLOG.md).
- ~~How exactly does middleware validate a JWT?~~ — ANSWERED 2026-07-16/17: `VerifyToken` recomputes the signature under the secret + checks `exp` (§11); guard wraps per-route, id travels in the context.
- Authorization: the token proves WHO — nothing yet decides MAY. What should user 12 get for `GET /users/10`? (Opener for the next design session.)
