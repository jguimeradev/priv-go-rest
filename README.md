# go-learn-api — Project Wiki

A Go REST API (users resource, MySQL) built as a learning project to master API development end-to-end: HTTP fundamentals, layered architecture, error contracts, validation, auth, and eventually async patterns for a cloud audio-processing pipeline.

This document is the **stable reference**: architecture, decisions, and established patterns, organized by topic. The **working log** — session-by-session progress, open bugs, and todo checklists — lives in [SESSION_001.md](SESSION_001.md). When they disagree, the session log is more current.

---

## 1. API Design

### Endpoints

```
POST   /users           — Register (public)
POST   /auth/login      — Login (public)                    [Phase 2]
GET    /users/{id}      — Read own profile (authenticated)
PATCH  /users/{id}      — Update own profile (authenticated)
DELETE /users/{id}      — Delete own account (authenticated)
GET    /users           — List all users (admin only)
```

Auth is Phase 2; until then all endpoints are public.

### Contract conventions

- **Responses never contain the password** — in any form, hashed or not.
- **JSON tags are the API contract.** Field names are lowercase (`"id"`, not `"ID"`); renaming a JSON field after clients depend on it is a breaking change (versioning territory).
- **`201 Created` carries a `Location: /users/{id}` header** built by the server from the new id.
- **List endpoints return `[]`, never `null`** — result slices are initialized, because a nil slice serializes to JSON `null`.
- **PATCH is a true partial update**: the client sends only the fields to change. Absent = keep; explicitly empty = `400`; valid value = validate and apply. (See §5.)

### Status code mapping

| Situation | Code |
|---|---|
| Unparseable path id, malformed JSON, validation failure | 400 |
| Resource doesn't exist | 404 |
| Duplicate email (unique constraint) | 409 |
| Successful create | 201 + Location |
| Successful read/list | 200 |
| Any unrecognized error | 500 (never leak internals) |

---

## 2. Architecture

### Layers

```
HTTP request
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
middleware   — (Phase 2) runs before handlers: auth, logging
main.go      — composition root: the only place that knows every concrete type
```

### Dependency inversion — how the layers connect

Each layer defines an **interface for what it needs** (the consumer owns the interface, Go convention); the layer below satisfies it implicitly — Go checks method signatures, no `implements` declaration exists.

```
create *sql.DB
  → NewUserRepo(db)        → *UserRepo     (satisfies service.UserRepository)
    → NewUserSvc(repo)     → *UserSvc      (satisfies handler-facing service.UserService)
      → NewUserHandler(svc)→ *UserHandler
        → RegisterRoutes(mux), start server
```

- Concrete types flow **downward** through constructors; contracts flow **upward** through interfaces; they meet only in `main.go`.
- No layer imports another for concrete types — only for contracts and domain types.
- Payoff: any layer can be swapped with a fake in tests without touching the layers above it.

### Rules of the road

- **`main.go` is the composition root** — it creates resources (DB pool, repo, service, handler) and hands them down. Nothing finds its own dependencies.
- **Every injectable struct has a constructor** (`NewX(dep) *X`) with unexported dependency fields — implementation details, not public API.
- **Dependencies belong in signatures, not context.** The service never reads `context.Context` values; middleware will extract identity from the request and the handler passes it as an explicit parameter.
- **The handler owns route registration** (`RegisterRoutes(mux)`); `main.go` just calls it.
- **Shared types live in `internal/domain`** — if neither layer A nor B should import the other, the shared type goes in a neutral third package. Everything sits under `internal/` because this is an application, not a library.

### Project layout

```
go-learn-api/
├── cmd/main.go              — composition root
├── internal/
│   ├── config/              — config loading & validation
│   ├── domain/              — shared types + sentinel errors
│   ├── handler/             — HTTP handlers, request DTOs
│   ├── service/             — business logic, layer interfaces
│   ├── repository/          — SQL, error translation at the DB boundary
│   └── middleware/          — (Phase 2) auth middleware
├── migrations/              — SQL DDL (never embedded in Go code)
├── scripts/                 — setup.sh, test.sh (curl test suites)
├── Makefile                 — run / build / lint / clean
└── .env                     — local dev config (platform injects in prod)
```

---

## 3. Types & Contracts

### The rule: one struct per *contract*, not per method

A new type earns its existence only when **shape** (which fields), **optionality** (required vs. maybe), **trust** (untrusted client input vs. internal data), or **serialization** (wire format) genuinely differs. If two operations share a contract, they share the type.

| Type | Package | Contract | What makes it distinct |
|---|---|---|---|
| `User` | domain | full entity | has password hash; never leaves the service layer |
| `UserResponse` | domain | what the API returns | JSON tags = public contract; no password |
| `CreateUserRequest` | handler | what a client must send to create | untrusted; `required` validation tags |
| `UpdateUserRequest` | handler | what a client *may* send to update | `*string` fields (three-state optionality) + json/validate tags |
| `UpdateUserInput` | domain | "what the client may have sent", handler→service | bare `*string`, **no tags** — HTTP concerns stay in handler |
| `UpdateUserParams` | domain | "write exactly these values", service→repo | plain strings, maps 1:1 to the SQL SET clause |

These are DTOs / boundary types (call them *intermediate types*, not "middleware structs" — middleware here means HTTP middleware). The mapping boilerplate is the point: contract mismatches become compile errors instead of leaks.

**History that motivates the rule** — every early bug was one struct serving two contracts:
- `json:"-"` on the repo struct to hide the password → rejected, bakes an HTTP concern into the data layer
- Echoing `CreateUserRequest` back as the response → leaked the plaintext password
- Reusing `CreateUserRequest` for PATCH → `required` tags physically can't express partial update

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

The repo only ever sees definite values. If the repo took the pointer type, the merge logic would be forced down into SQL — decided against (see §5, merge options).

---

## 4. Error Handling

### Translation happens at the boundary that knows the source

```
repository  →  sql.ErrNoRows        →  domain.ErrUserNotFound      (only the repo knows it's database/sql)
repository  →  MySQL error 1062     →  domain.ErrUserAlreadyExists (errors.As → *mysql.MySQLError → .Number)
service     →  passes domain errors through unchanged              (already domain language)
handler     →  domain.ErrUserNotFound → 404, ErrUserAlreadyExists → 409, anything else → 500
```

- **Translate only what carries domain meaning; pass through everything else.** "Not found" is a business event (→404); a connection failure is just a failure (→500). A handler must never return 404 for a database crash.
- **Sentinel test:** does the top-level caller need to *branch* on this outcome? `Read` not-found → yes (404 vs 500) → sentinel. `All` returning zero rows → no (200 + `[]` either way) → no sentinel; an empty collection is a valid state, not an error.
- **Sentinels are package-level `var`s in `internal/domain`** (`ErrUserNotFound`, `ErrUserAlreadyExists`, `ErrSamePassword`, `ErrInvalidPassword`) — inline `errors.New()` creates a fresh value nobody can compare against.
- **Always `errors.Is` for sentinel comparison, never `==`** — wrapping (`fmt.Errorf("...: %w", err)`) breaks `==`; `errors.Is` unwraps the chain. `errors.As` is for typed errors you need to inspect (`*mysql.MySQLError`), `errors.Is` for sentinel values.
- **Context wrapping (`"Read: %w"`) belongs at the layer doing the work** — the repo wraps because that's where the SQL runs; layers above return errors they didn't cause unwrapped.
- **UPDATE/DELETE existence checks:** `Exec` matching 0 rows is not an error — check `RowsAffected() == 0` → `ErrUserNotFound`. This lives in the repo (it's SQL knowledge).
- **Encode errors after headers are sent are unrecoverable** — log them; don't call `http.Error` (status line already went out).

### The service's error posture

Detection and reaction are separate: a function that detects a problem returns errors; the caller decides what to do. `config.Load()` returns `[]error` (all problems collected, not fail-on-first); `main.go` owns `os.Exit(1)`.

---

## 5. Validation

### Tooling & philosophy

- **Library:** `github.com/go-playground/validator/v10`, rules as struct tags on request DTOs, invoked via a `validateRequest()` method per DTO. (Hand-rolling was abandoned — too time-consuming relative to the learning goal.)
- **Deliberately simple for now** (decision 2026-07-03): no character-class password rules; raw validator `err.Error()` returned to clients. Translating `validator.ValidationErrors` into a client-friendly error contract is deferred to Phase 3.
- **Validate at trust boundaries:** handler validates request *shape* (format, lengths); service validates *business rules* (uniqueness via the DB, permissions).
- **Check ordering: cheap before expensive** — path id parse (400) → JSON decode (400) → tag validation (400) → only then any DB round trip.
- **Try-then-handle, not check-then-act:** don't pre-flight-check email uniqueness — the DB's UNIQUE constraint enforces it atomically; attempt the INSERT, translate error 1062.

### Field rules

| Field | Rules |
|---|---|
| name | required, min 5, max 100 |
| email | required, valid format, max 255 |
| password | required, min 8, **max 72** (bcrypt silently truncates past 72 bytes; the DB's CHAR(60) is the *hash* length, not the input limit) |

For PATCH, the same rules apply but only to fields actually sent (`omitempty` leads each tag chain).

### The three-state problem (PATCH)

Decoding JSON into a plain `string` destroys "was the key present?" — absent and `""` both become `""`. The contract needs three states, so update DTOs use `*string`:

| JSON | Pointer | Meaning | Outcome |
|---|---|---|---|
| key absent | `nil` | keep current value | `omitempty` skips remaining rules |
| `"name": ""` | `&""` | explicitly empty | `min=5` runs against `""` → 400 |
| `"name": "Valid"` | `&"Valid"` | change it | rules run → validate & apply |

`encoding/json` fills this correctly for free — absent keys are never touched, so they stay `nil`. Always nil-check before dereferencing.

**`omitempty` with pointers:** "empty" means *nil pointer*, not empty string. validator/v10 dereferences pointer fields before applying rules; `omitempty` is the only tag inspecting the pointer itself. And `omitempty` is validation-only — it never "keeps the old value"; the service merge does that. Validation decides if the *request* is acceptable; the merge decides what the *write* looks like.

### Where the merge lives — options considered

1. **Read-modify-write in the service (chosen):** `Read` current user, overlay non-nil fields, `Update`. Two queries; merge readable and testable in Go. Bonus: the initial `Read` doubles as the existence check — 404 falls out for free, no separate pre-flight.
2. Dynamic SQL SET clause — one query, merge hidden in query construction, hard to test.
3. `COALESCE(NULLIF(?, ''), column)` — one query, merge hidden in SQL.
4. Require all fields — that's PUT, rejected; the point was real PATCH semantics.

---

## 6. Layer Reference

### Repository (`internal/repository/user.go`)

- `*sql.DB` is a **connection pool**, injected via `NewUserRepo(db)` — the repo never calls `sql.Open()`.
- Method map: `Exec` → INSERT/UPDATE/DELETE; `QueryRow`+`Scan` → single row; `Query`+loop+`Scan` → many rows with `defer rows.Close()` and a final `rows.Err()` check.
- **Always `?` placeholders — never string concatenation** (SQL injection).
- **The repo fetches faithfully:** `Read` returns the full `domain.User` including hash — the *service* decides what to expose per context. Never strip fields at the repo level for one caller's benefit.
- `All()` deliberately selects only id/name/email (passwords excluded from list queries).
- Profile updates and password writes are **separate methods** (`Update` vs `UpdatePassword`): they're different operations with different service flows, and a full-`User` update risked overwriting the password with a zero value.
- Params structs pass **by value** unless large, mutated, or nil-is-meaningful.

### Service (`internal/service/user.go`)

Interface (consumer-facing):

| Method | Returns | Notes |
|---|---|---|
| `CreateUser(name, email, password)` | `(int, error)` | id feeds the Location header |
| `ReadUser(id)` | `(UserResponse, error)` | maps entity → response, drops hash |
| `FetchAllUsers()` | `([]UserResponse, error)` | initialized slice, never nil |
| `UpdateUser(id, …)` | `error` | owns the PATCH merge (see §5) |
| `DeleteUser(id)` | `error` | |
| `ChangePassword(id, old, new)` | `error` | owns the full flow below |

- **Passwords are hashed here** (bcrypt, `bcrypt.DefaultCost` — a work factor, not a length; output is always 60 chars). The repo never sees plaintext. Shared `hashPassword` helper.
- **ChangePassword flow:** `Read(id)` → `bcrypt.CompareHashAndPassword` (only `ErrMismatchedHashAndPassword` becomes `ErrInvalidPassword`; other bcrypt errors are wrapped) → same-password check (plain `==` is valid there: both plaintexts, one already verified) → hash new → `UpdatePassword`.
- **Authorization lives here** (Phase 2): "is user X allowed to do this?" is a business rule. Authentication ("who are you?") is middleware's job — it stamps identity into request context; the handler extracts it and passes it as an explicit parameter.

### Handler (`internal/handler/user.go`)

- Holds the `service.UserService` **interface**, not the concrete struct.
- Standard shape per handler: parse path values (`r.PathValue`, Go 1.22+ mux patterns like `GET /users/{id}`) → decode → validate → call service → map errors → write response.
- **ResponseWriter ordering: headers → `WriteHeader(status)` → body.** Headers set after `WriteHeader` are silently dropped; the first body write without `WriteHeader` implies 200 — a create must send 201 explicitly.
- `Header().Set` for single-valued headers (Location); `Add` appends.
- The response body is what the server *created* (a `UserResponse` built from the service result), never an echo of the request.

---

## 7. Configuration

- **`.env` is local-dev only**; production platforms inject env vars directly. `os.Getenv()` reads the process environment either way — same code path, different source.
- **`APP_ENV` bootstrap:** read from the OS env *first* (can't load `.env` to discover which env you're in). Empty → development → load `.env` via `godotenv`, fail fast if missing. Set → production → skip `.env`, trust the platform.
- Eight required vars: `APP_ENV`, `APP_PORT`, `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `NET_PROT`. Validation collects **all** missing fields into `[]error` before reporting; the app exits before the server starts.
- **DB connection (idiomatic path):** `mysql.NewConfig()` populated from the validated `Config` struct (never re-read `os.Getenv` after `Load()`), → `mysql.NewConnector()` → `sql.OpenDB()` → `db.Ping()`. `sql.Open` is lazy — only `Ping` proves the DB is reachable.

---

## 8. Data Model

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

Seed data: 5 users, all with password `password123` (same bcrypt hash).

**On ids:** an id is an identifier, not a sequence number. AUTO_INCREMENT values are consumed even by failed inserts (InnoDB reserves under a short lock before attempting; gap-free numbering is traded for insert throughput — Postgres sequences behave the same). Gaps also come from deletes and rollbacks; nothing may depend on contiguity, never `MAX(id)+1`. Forward pointer (Phase 12): sequential ids are enumerable (OWASP BOLA/IDOR) — real systems expose UUIDs or opaque public ids.

---

## 9. Go Idioms Adopted

- Early returns; no `else` after `return`.
- Unexported = package-private; exported = public API; if callers don't need it, lowercase it.
- Blank imports (`_ "pkg"`) only for side effects (driver registration) — never for packages you call.
- `strconv.Itoa`/`Atoi` for int↔string — `string(65)` is `"A"` (code point), and `go vet` catches it.
- `fmt` → stdout; `log` → stderr with timestamp; startup failures belong on stderr.
- Pointer receivers by convention for shared types, even when not strictly needed — prevents silent bugs if mutable state is added later.
- Comments explain the non-obvious *why*, never restate the code.
- Test packages: `package x_test` (black-box) when only exported API is needed; `package x` for internal access.
- When debugging library errors, unwrap structured errors (`errors.As` → inspect fields) instead of squinting at a flat `err.Error()` string — make the system testify.

---

## 10. Tooling

| Command | Does |
|---|---|
| `make run` | `go run cmd/main.go` (from root, so `.env` is found) |
| `make build` / `make clean` | compile to / remove `bin/app` |
| `make lint` | `go vet ./...` |
| `bash scripts/test.sh` | self-checking curl suite for PATCH — each test declares its expected (finished-behavior) status; failures are the todo list. `BASE_URL` overridable; nonzero exit on failure. |

---

## 11. Roadmap

| Phase | Scope | Status |
|---|---|---|
| **1 — Core CRUD** | repo → service → handlers → wiring, verified with curl. Order: GET /users/{id} ✅, POST /users ✅, PATCH 🔄, DELETE, GET /users | in progress |
| **2 — Authentication** | JWT concepts → POST /auth/login → auth middleware → lock down endpoints | |
| **3 — Refinements** | consistent error contracts (translate `ValidationErrors`), stricter validation, structured logging, tests (unit for service, integration for repo) | |
| Later | versioning, OpenAPI, observability, security (rate limiting, OWASP API Top 10), async patterns, gRPC | |

**Deferred topics:** `context.Context` mechanics; testing strategy (mocking the repo via its interface); JWT internals.


