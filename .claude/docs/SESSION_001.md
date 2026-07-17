# Session 001 — API Design & Project Setup

**Date:** 2026-06-18 / updated 2026-07-01
**Focus:** REST API design, project scaffolding, configuration management, config refinements, database connection, repository layer, service layer design

---

## Learning Recap

### HTTP & REST Fundamentals
- **HTTP methods map to operations:** GET (retrieve), POST (create), PATCH (partial update), DELETE (remove)
- **Method choice is based on semantics:** Safe (read-only) vs. idempotent (repeatable) operations drive the decision
- **PUT vs. PATCH distinction:** PUT replaces entire resource, PATCH only updates provided fields
- **URL design for resources:** Collection (`/users`) vs. specific resource (`/users/{id}`)

### Configuration & Environment Management
- **`.env` files are for local development only**
  - In production, the platform (AWS, Azure, Docker) injects environment variables directly
  - `os.Getenv()` works regardless of source — it reads from the process environment
  - Same code path, different config source in different environments
- **`godotenv` library** loads `.env` at startup if file exists; doesn't overwrite already-set vars
- **Configuration validation** should catch all missing required fields at once, not fail on the first one
- **`APP_ENV` bootstrap problem:** `APP_ENV` itself may live in `.env`, so you can't load `.env` to discover which env you're in
  - Solution: read `APP_ENV` from OS env first (always available before any file loads)
  - If empty → assume development → load `.env`, fail fast if missing
  - If set → assume production → skip `.env`, trust the platform
- **Error handling:** A missing `.env` is fatal in development, expected and ignored in production

### Go Error Handling Patterns
- **`error` interface is the standard contract** — prefer `[]error` over custom string types like `ErrMessages`
- **`fmt.Errorf`** creates an error value — it does not print; use it to construct errors
- **`fmt.Println` vs `log.Println`:** `fmt` writes to stdout, `log` writes to stderr with a timestamp — startup failures belong on stderr
- **`err.Error()`** explicitly extracts the message string from an error — more intentional than relying on `fmt`'s implicit handling
- **Separation of detection and reaction:** a function that detects a problem should return errors, not decide what to do about them — the caller decides
- **Comments earn their place** — if the code already says what it does, don't repeat it in a comment; only comment the non-obvious *why*

### Go Project Architecture
- **Layered architecture separates concerns:**
  - `handlers` — thin HTTP layer (parse request, call service, write response)
  - `service` — business logic, knows nothing about HTTP or databases
  - `repository` — database access only
  - `middleware` — runs before handlers (auth, logging, etc.)
  - `config` — loads and validates configuration at startup
- **Dependency injection via interfaces** decouples layers — each layer depends on a contract, not concrete implementations
- **Input validation happens at trust boundaries:**
  - Handler layer validates request shape (email format, non-null fields)
  - Service layer validates business rules (email uniqueness, permissions)
- **Go conventions:**
  - Unexported identifiers (lowercase) are package-private
  - Exported identifiers (PascalCase) are public
  - `internal/` package prevents external imports (enforces encapsulation)
  - Scripts use `$BASH_SOURCE` and `realpath` for portable path resolution

### Database Connection (database/sql + go-sql-driver/mysql)
- **`sql.Open()` is lazy** — it does not dial the database, only validates the driver and DSN
- **`db.Ping()` is the real connection check** — must be called to confirm the database is reachable
- **`mysql.NewConfig()` + `mysql.NewConnector()` + `sql.OpenDB()`** is the idiomatic path (vs raw DSN string)
- **`mysql.NewConfig()` sets `Net` to `"tcp"` by default** — requiring `NET_PROT` as a config field is explicit but optional
- **Always use the validated `Config` struct** — don't re-read `os.Getenv()` after `config.Load()` returns; use the fields on `c`
- **`main.go` is the composition root** — it creates the DB connection and hands it to whoever needs it via constructor injection

### Domain Package & Type Ownership

- **The type ownership problem:** `UpdateUserParams` started in the repository. The service needed it too. Two bad options: service imports repository (wrong direction) or repository imports service (also wrong — repo is the lowest layer).
- **Rule:** if neither layer should import the other, the shared type belongs in a **third package** — `internal/domain`.
- **`internal/domain` holds core business types** shared across layers — not a dumping ground; only types multiple layers genuinely share.
- **Contents of `internal/domain/user.go`:**
  - `User` — full entity (ID, Name, Email, Password) — used internally between repo and service
  - `UserResponse` — API-safe version (ID, Name, Email, no password) — returned from service to handler
  - `UpdateUserParams` — editable fields (Name, Email) — used at the service→repo boundary
  - `ErrUserNotFound` — domain-level sentinel error; repo sets it (translating `sql.ErrNoRows`), handler checks it with `errors.Is`
- **`json:"-"` tag rejected** as a way to hide the password: it bakes an HTTP serialization concern into the repository layer; structs should have one job.
- **`internal/` placement:** `domain` lives inside `internal/` — it's part of the application, not a public library.

### Service Layer Design

- **Service layer responsibilities** (sits between handler and repository):
  - Hash passwords with bcrypt before passing to the repo — repo never sees plaintext
  - Password change flow: `Read(id)` → bcrypt compare old password → bcrypt hash new password → `UpdatePassword(id, hash)`
  - Error translation: convert repo-level errors (`sql.ErrNoRows`, duplicate key) into domain errors the handler can map to HTTP status codes
  - Authorization: enforce who is allowed to do what (e.g. user 42 cannot modify user 99's profile)
- **"Check then act" vs "try then handle":**
  - Don't pre-flight check for email uniqueness — the DB has a `UNIQUE` constraint that enforces it atomically
  - Correct pattern: attempt the operation, inspect the error, translate it to a domain error
  - MySQL returns error code `1062` for duplicate key violations — catch and translate to "email already in use"
  - Same for existence checks: call `Read(id)`, if it returns `sql.ErrNoRows` translate to "not found"
- **Authentication vs Authorization — different layers, different jobs:**
  - Authentication ("who are you?") — JWT validation, lives in middleware before the handler runs
  - Authorization ("are you allowed?") — business rule, lives in the service layer
  - Middleware stamps the request with an identity; service uses that identity to decide if the operation is permitted
- **How identity flows through the pipeline:**
  - Middleware validates JWT → stores user ID in `context.Context` via `r.Context()`
  - Handler extracts user ID from context → passes it as an explicit parameter to the service
  - Service does NOT reach into context — its dependencies must be visible in its function signature
  - Reason: a service that reads from context is harder to test (must construct a context with the right value) and hides its dependencies
- **Why interfaces between layers:**
  - Handlers depend on a `UserService` interface, not the concrete `*UserService` struct
  - Enables swapping a real service for a fake in tests — without hitting the database
  - Documents the contract: what the handler needs, not how it's implemented
  - In Go, interfaces are defined by the **consumer** (handler package), not the producer (service package) — if the method signatures match, Go is satisfied; the service never declares what it implements
- **Layers connect through interfaces — the full picture:**

  Without interfaces, every layer holds a concrete type from the layer below. Change anything downstream and the change ripples upward through every layer. Everything is glued together.

  With interfaces, each layer defines what it *needs* — a contract — and doesn't care who fulfils it:
  ```
  handler     knows only: "I need something with CreateUser, ReadUser..."
  service     knows only: "I need something with Create, Read, All..."
  repository  knows only: "I need a *sql.DB"
  main.go     knows everything — creates real types, connects them
  ```

  **How Go satisfies interfaces implicitly:**
  You don't write `UserRepo implements UserRepository`. Go checks: does this type have all the required methods with matching signatures? If yes — it satisfies the interface automatically. `*repository.UserRepo` satisfies `service.UserRepository` without either package knowing about the other.

  **Why main.go is special — it's the wiring point:**
  ```
  create *sql.DB
    → NewUserRepo(db)    → *UserRepo
      → NewUserSvc(repo) → *UserSvc
        → NewUserHandler(svc) → *UserHandler
          → register routes, start server
  ```
  Each constructor receives an interface, returns a concrete type. Types flow downward through constructors. Contracts flow upward through interfaces. They only meet at `main.go`.

  **This pattern is called dependency inversion:** high-level layers (handler, service) don't depend on low-level layers (repo, DB) — both depend on abstractions (interfaces). The low-level layers are swappable, which is what makes testing possible.

  - No layer imports the one below it for concrete types — only for contracts
  - `*repository.UserRepo` satisfies `UserRepository` implicitly — Go checks method signatures, no declaration needed
  - `main.go` is the only place that knows about all concrete types at once
- **Exported vs unexported struct fields:**
  - A field starting with a capital letter is exported — anything outside the package can read or set it directly
  - Internal dependencies (like the repo inside `UserSvc`) should be unexported — they are implementation details, not part of the public API of the struct
  - Rule of thumb: if callers don't need to touch it, lowercase it
- **Every injectable struct needs a constructor:**
  - `NewUserSvc(repo UserRepository) *UserSvc` — same pattern as `NewUserRepo(db *sql.DB) *UserRepo`
  - The constructor is where the dependency is injected; without it `main.go` has no clean way to wire the layer
  - Returns a pointer — the struct is meant to be shared, not copied
- **Error translation across layers — each layer speaks its own language:**
  ```
  repository  →  catches sql.ErrNoRows, returns domain.ErrUserNotFound  (translation at the DB boundary)
  service     →  passes domain.ErrUserNotFound through unchanged          (already domain language)
  handler     →  translates domain.ErrUserNotFound to 404                (HTTP language)
  ```
  - Translation belongs in the **repository** — that's the only layer that knows it's using `database/sql`; `sql.ErrNoRows` is a database implementation detail and should never leak up
  - If the service checked `sql.ErrNoRows` directly, swapping the database driver would force changes in the service too — a violation of the dependency inversion principle
  - The sentinel `ErrUserNotFound` lives in `internal/domain` so both the repo (which sets it) and the service/handler (which check it) can reference it without any cross-layer import
  - The service's error block simplifies to: pass errors through; anything unknown gets wrapped with `fmt.Errorf("MethodName: %w", err)`; `domain.ErrUserNotFound` propagates as-is since it's already domain language
  - **`fmt.Errorf` context wrapping belongs at the layer that does the work:** the repo adds `"Read: %w"` because that's where the SQL runs; the service doesn't re-wrap errors it didn't cause
- **`ChangePassword` service flow:**
  1. `repo.Read(id)` — fetch current user to get the stored hash
  2. `bcrypt.CompareHashAndPassword(hash, oldPassword)` — verify old password; `errors.Is(err, bcrypt.ErrMismatchedHashAndPassword)` → `ErrInvalidPassword`; any other bcrypt error → wrap and return
  3. `oldPassword == newPassword` → `ErrSamePassword` (business rule: the service enforces it, not the handler)
  4. `hashPassword(newPassword)` — hash the new password
  5. `repo.UpdatePassword(id, newHash)` — persist
  - Can't use `strings.Compare` (or `==`) to verify the old password against the stored value — the stored value is a bcrypt hash; must use `bcrypt.CompareHashAndPassword`
  - The same-password check (`oldPassword == newPassword`) IS a string comparison of two plaintexts — valid because at that point bcrypt confirmed they match the same hash
  - `bcrypt.ErrMismatchedHashAndPassword` is the specific error for wrong password; other bcrypt errors (e.g. malformed hash) should not become `ErrInvalidPassword` — distinguish with `errors.Is`

- **`hashPassword` helper — extracted from `CreateUser`:**
  - Shared by `CreateUser` and `ChangePassword` — extracted to avoid duplication
  - Uses `bcrypt.DefaultCost`, not a magic number
  - Returns `(string, error)` — bcrypt can fail (e.g. password too long > 72 bytes)

- **Sentinel errors must be package-level variables**, not inline `errors.New()` calls:
    - `errors.New("user not found")` inside a function creates a new value every time — nothing can reliably compare against it
    - `var ErrUserNotFound = errors.New("user not found")` defined once at package level — callers check with `errors.Is(err, domain.ErrUserNotFound)`
  - **Translate only what you recognise — pass through everything else:**
    - Only `sql.ErrNoRows` → `domain.ErrUserNotFound`
    - Any other error (connection failure, query error) passes through as-is — the handler must not return 404 for a database crash
    - Pattern in repo: `if errors.Is(err, sql.ErrNoRows) { return domain.ErrUserNotFound }` then `return fmt.Errorf("Read: %w", err)` for anything else
  - **`errors.Is` vs `==` for error comparison:**
    - `err == sql.ErrNoRows` works only if the error is the exact same value — it breaks when errors are wrapped
    - Since Go 1.13, errors can be wrapped: `fmt.Errorf("context: %w", err)` creates a new error that contains the original; `==` won't see through the wrapper
    - `errors.Is(err, sql.ErrNoRows)` unwraps the chain recursively until it finds the target — works whether the error is raw or wrapped
    - Rule: **always use `errors.Is` for error comparison**, never `==`
  - **Blank imports `_ "package"` are for side effects only** — registering a database driver, triggering an `init()` function; never use them for packages you actually call functions on

- **Why `All()` doesn't need error translation like `Read()` did:**
  - `All()` has three internal failure points (query, scan, rows.Err) — but all three are handled inside the repo and collapsed into one `([]domain.User, error)` return; the service only ever sees one error value
  - `Read()` needed translation because `sql.ErrNoRows` carries *domain meaning*: "the user doesn't exist" is a business event, not just a failure; it maps to a 404
  - `All()` has no equivalent case — zero rows is a valid empty result, not an error; every error `All()` can return is a genuine unexpected failure
  - **The rule: translate errors when the error carries domain meaning. Pass them through when they're just failures.**
  - Corollary: a nil slice serializes to JSON `null`; an initialized empty slice serializes to `[]`; for list endpoints, always initialize the result slice so clients receive `[]` not `null` when there are no results

- **When a domain sentinel error is needed — the caller branching test:**
  - Ask: does the caller at the top of the chain need to *behave differently* based on this specific outcome?
  - `Read` not found → handler must branch: 404 vs 500 → sentinel needed
  - `All` returns empty → handler returns 200 with `[]` — no branch → no sentinel needed
  - "Not found" and "empty collection" are fundamentally different: not found means a specific expected resource doesn't exist; empty collection is a valid state of the world
  - Even though "not found" isn't a failure in the traditional sense, it still goes through `error` in Go because that's the only mechanism available; the sentinel is what lets the handler distinguish it from a genuine infrastructure failure without importing `database/sql`

- **`bcrypt.GenerateFromPassword` cost factor vs output length:**
  - The second argument is a *cost factor* (work factor), not the output length — bcrypt always produces a 60-char hash
  - Cost factor controls computational expense: higher = slower to brute-force, slower to compute
  - `bcrypt.DefaultCost` (10) is the idiomatic value — self-documenting and tracks the library's recommendation
  - Never use a magic number like `10`; use the named constant

- **`RowsAffected()` for UPDATE/DELETE existence checks:**
  - `db.Exec` for an UPDATE or DELETE that matches 0 rows does NOT return an error — it silently succeeds
  - To detect "user didn't exist", call `res.RowsAffected()` and check for `0` → return `domain.ErrUserNotFound`
  - This belongs in the repo: it's the layer that knows it's doing SQL
  - `RowsAffected()` itself can return an error (driver doesn't support it) — handle that too

- **`errors.Is` vs `errors.As` — different jobs:**
  - `errors.Is(err, target)` — checks *value equality* through the chain; use when you have a known sentinel value to compare against (e.g. `domain.ErrUserNotFound`)
  - `errors.As(err, &target)` — checks *type* through the chain and populates `target`; use when you need to unwrap into a concrete type to inspect its fields (e.g. `*mysql.MySQLError` to read `.Number`)
  - Rule of thumb: sentinel value → `errors.Is`; typed struct → `errors.As`

- **MySQL-specific error handling in `Create`:**
  - MySQL returns a typed `*mysql.MySQLError` for database-level failures; use `errors.As` to unwrap it
  - Error number `1062` = duplicate entry (unique constraint violated) → translate to `domain.ErrUserAlreadyExists`
  - All other MySQL errors are infrastructure failures → wrap with `fmt.Errorf("Create: %w", err)` and pass up
  - Non-MySQL errors (connection dropped before driver processes it, etc.) → return raw; message is already descriptive
  - Pattern: `errors.As` → check `.Number` → 1062 → sentinel; else → wrap; fall through → raw
  - Avoid `else` after `return` — idiomatic Go uses early returns and falls through naturally

- **The repo fetches faithfully — the service decides what to expose:**
  - `repo.Read(id)` returns `domain.User` including the password hash — always, regardless of caller
  - The service's `ReadUser` maps `domain.User` → `domain.UserResponse` (drops the password) before returning to the handler
  - Never strip fields at the repo level to satisfy a specific caller — another caller (`ChangePassword`) may need those fields
  - Rule: repo returns complete data; service shapes it for the context
- **Deferred topics** (will cover after CRUD pipeline works end-to-end):
  - `context.Context` — how values flow, how to use it for request-scoped data
  - Testing — unit tests for service, integration tests for repository

### Repository Layer Design
- **`*sql.DB` is a connection pool**, not a single connection — the driver manages connections internally
- **Three main methods on `*sql.DB`:**
  - `db.Exec(query, args...)` → INSERT, UPDATE, DELETE (returns `sql.Result`)
  - `db.QueryRow(query, args...)` → SELECT returning one row (call `.Scan()` on result)
  - `db.Query(query, args...)` → SELECT returning multiple rows (loop + `.Scan()` + `defer rows.Close()`)
- **Always use `?` placeholders** — never concatenate SQL strings; that's SQL injection
- **`rows.Close()` must be called** after `db.Query()` — use `defer rows.Close()` immediately after the error check
- **Dependency injection via constructor:** repository receives `*sql.DB` through `NewUserRepo(db)` — it never calls `sql.Open()` itself
- **Test package naming:**
  - `package config` — inside the package, access to unexported identifiers
  - `package config_test` — outside the package, black-box view of exported API only; preferred when you only need exported identifiers

---

## Service Layer Design — Interface Sketch (2026-06-29)

**UserService interface methods agreed upon:**

| Method | Inputs | Returns | Notes |
|---|---|---|---|
| `CreateUser` | name, email, password string | (int, error) | int is the new user's ID — handler uses it for Location header |
| `ReadUser` | id int | (UserResponse, error) | |
| `FetchAllUsers` | — | ([]UserResponse, error) | |
| `UpdateUser` | id int, params UpdateUserParams | error | nil = success |
| `DeleteUser` | id int | error | nil = success |
| `ChangePassword` | id int, oldPassword, newPassword string | error | service owns the full compare→hash→persist flow |

**UserResponse type (to be defined in service or handler layer):**
- Fields: `ID`, `Name`, `Email` — no `Password`
- Separate from the repo's `User` struct — keeps serialization concerns out of the data layer
- `json:"-"` tag on the repo struct was rejected: it bakes an HTTP concern into the repository layer; structs should have one job

**Status:** Interface designed, not yet written in code. Next step: write the interface in `internal/service/user.go`, then implement it.

---

## Decisions Made

### REST API Design
**Endpoints:**
```
POST   /users           — Register (public)
POST   /auth/login      — Login (public)
GET    /users/{id}      — Read own profile (authenticated)
PATCH  /users/{id}      — Update own profile (authenticated)
DELETE /users/{id}      — Delete own account (authenticated)
GET    /users           — List all users (admin only)
```

**User Resource Fields:** ID, name, email, password
- Password is never exposed in API responses

### Project Structure
```
go-learn-api/
├── cmd/
│   └── main.go                 — entry point: load config, wire layers, start server
├── internal/
│   ├── config/
│   │   └── config.go           — configuration loading & validation
│   ├── handler/
│   │   ├── user.go             — HTTP handlers for user endpoints
│   │   └── auth.go             — HTTP handlers for auth endpoints
│   ├── service/
│   │   └── user.go             — business logic for user operations
│   ├── repository/
│   │   └── user.go             — database access for users
│   └── middleware/
│       └── auth.go             — JWT authentication middleware
├── scripts/
│   └── setup.sh                — project scaffolding script
├── Makefile                    — build, run, lint, clean targets
├── .env                        — environment variables (local dev)
├── go.mod & go.sum             — Go module definitions
└── docs/
    └── DESIGN.md               — API design documentation
```

### Configuration Strategy
**Environment Variables (`.env` for local, platform injection for production):**
```
APP_ENV=development|production
APP_PORT=8080
DB_HOST=localhost
DB_PORT=3306
DB_USER=root
DB_PASSWORD=secret
DB_NAME=go_learn_api
NET_PROT=tcp
```

**Validation:** All eight fields are required. App exits immediately if any are missing.

**Error Reporting:** All validation errors are collected and reported together, not one at a time.

### Users Table (MySQL DDL)
```sql
CREATE TABLE users (
    id          INT AUTO_INCREMENT PRIMARY KEY,
    name        VARCHAR(100)  NOT NULL,
    email       VARCHAR(255)  NOT NULL UNIQUE,
    password    CHAR(60)      NOT NULL,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);
```
- `CHAR(60)` for password — bcrypt hashes are always 60 chars (fixed length)
- `ON UPDATE CURRENT_TIMESTAMP` — MySQL maintains `updated_at` automatically
- DDL belongs in `migrations/` or `scripts/` — not embedded in Go code

### Test Data (5 seed users)
```sql
INSERT INTO users (name, email, password) VALUES
('Alice Smith', 'alice@example.com', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lHHO'),
('Bob Jones', 'bob@example.com', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lHHO'),
('Carol White', 'carol@example.com', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lHHO'),
('David Brown', 'david@example.com', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lHHO'),
('Eve Davis', 'eve@example.com', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lHHO');
```
- All passwords are `password123` (bcrypt hash of `password123` with cost 10)

### Build Tooling
**Makefile targets:**
- `make run` — `go run cmd/main.go` (runs from project root, finds `.env`)
- `make build` — `go build -o bin/app cmd/main.go` (compiles binary)
- `make clean` — `rm -f ./bin/app` (cleans binary)
- `make lint` — `go vet ./...` (vets entire project)

---

## Implementation So Far

### Completed
1. ✅ Project scaffolding (folders, module init, `go.mod`)
2. ✅ `scripts/setup.sh` — automated project setup
3. ✅ `Makefile` — build, run, lint, clean
4. ✅ `.env` file with all required variables (including `NET_PROT`)
5. ✅ `internal/config/config.go` — loads and validates configuration
   - Reads `APP_ENV` from OS env first to determine environment
   - Development (APP_ENV empty): loads `.env`, fails fast if missing
   - Production (APP_ENV set): skips `.env`, trusts platform-injected vars
   - Reads all env vars into typed `Config` struct
   - `Validate()` returns `[]error` — detects problems, does not react
   - `Load()` returns `(Config, []error)` — caller owns the exit decision
   - Logs the active environment on successful startup
6. ✅ `cmd/main.go` — calls `config.Load()`, handles `[]error`, owns `os.Exit(1)`
7. ✅ MySQL connection in `main.go`
   - Uses `mysql.NewConfig()` populated from validated `Config` struct fields
   - `mysql.NewConnector(cfg)` → `sql.OpenDB(connector)` → `db.Ping()`
   - `log.Fatal` on any connection failure
8. ✅ `internal/repository/user.go` — fully implemented and finalized
   - Own `User` and `UpdateUserParams` structs removed; now uses `domain.User` and `domain.UpdateUserParams`
   - `UserRepo` struct holding `db *sql.DB` (unexported)
   - `NewUserRepo(db *sql.DB) *UserRepo` constructor
   - `Create(name, email, password string) (int, error)` — INSERT with `?` placeholders, returns `LastInsertId()`; detects MySQL 1062 → `domain.ErrUserAlreadyExists`; other MySQL errors wrapped with `"Create: %w"`
   - `All() ([]domain.User, error)` — SELECT id, name, email only (password excluded from list query)
   - `Read(id int) (domain.User, error)` — SELECT single row by id, includes password for auth use
   - `Update(id int, params domain.UpdateUserParams) error` — UPDATE name, email only; password never touched
   - `UpdatePassword(id int, password string) error` — dedicated method for password persistence
   - `Delete(id int) error` — DELETE by id
   - Placeholders used everywhere — no SQL string concatenation
9. ✅ `internal/domain/user.go` — shared business types and sentinel errors: `User`, `UserResponse`, `UpdateUserParams`, `ErrUserNotFound`, `ErrUserAlreadyExists`
10. ✅ `migrations/001_create_users.sql` — `DROP TABLE IF EXISTS` + `CREATE TABLE users`
11. ✅ `main.go` imports `repository` and calls `NewUserRepo(db)`
12. ✅ Design documentation (`docs/DESIGN.md`)
13. ✅ `internal/service/user.go` — fully implemented: interfaces, `hashPassword` helper, all six methods (`ReadUser`, `FetchAllUsers`, `CreateUser`, `UpdateUser`, `DeleteUser`, `ChangePassword`)
14. 🔄 `internal/handler/user.go` — `UserHandler` struct + `NewUserHandler` + `RegisterRoutes` + `HandleGetUser` + `HandlePostUser` implemented and verified with curl; `HandlePatchUser` skeleton in place (decode + validate only, debug prints still in) — rest of PATCH rolled back 2026-07-05, see checklist below

### Current State — Open Issues in `repository/user.go`
- ✅ `rows.Err()` is checked after the `rows.Next()` loop in `All` (lines 66–68) — was listed as open but already implemented
- ✅ `Update` and `UpdatePassword` refactored — see decisions below
- ✅ `Update` parameter renamed from `user` to `userParams` — clearer, not a `User` entity
- ✅ `Update` takes `UpdateUserParams` by value — pointer removed; small struct, no mutation, no nil needed

---

## Layer Responsibility Discussion — Password Management (Resolved)

**Decision:** Password changes use a separate `UpdatePassword(id int, password string) error` method. `Update` only touches `name` and `email`.

**Reasoning worked through:**
- The repository only fetches and saves — it makes no decisions about data meaning
- Password changes require distinct service-layer operations: read current hash → bcrypt compare → bcrypt hash new password → persist
- That last step (persist) is a different write than a profile update — separate repo method keeps the boundary clean
- Passing a full `*User` to `Update` was fragile: callers had to `Read` first or risk overwriting the password with an empty string; the SQL never touched password anyway

**`UpdateUserParams` struct:** introduced to hold only `name` and `email` — the exact fields `Update` changes. Avoids 20-parameter signatures as the table grows; the struct maps 1:1 to what the SQL touches.

**Pointer vs. value for params structs:**
- Initial instinct: pass as pointer because "structs are heavyweight"
- Correction: use pointer only when the struct is large enough to matter, the function needs to mutate the original, or nil is meaningful (optionality)
- `UpdateUserParams` is two strings — pass by value; no mutation, no optionality needed

**Service layer responsibility for password changes:**
1. Call `Read(id)` to get current hash
2. bcrypt-compare current hash against provided old password
3. bcrypt-hash the new password
4. Call `UpdatePassword(id, newHash)` to persist

---

## Next Steps

### Immediate (before moving on)
1. ~~Check `rows.Err()` after `rows.Next()` loop in `All`~~ — already implemented
2. ~~Decide: `Update` vs. `UpdatePassword`~~ — resolved: separate methods, `UpdateUserParams` struct
3. ~~Fix `Update` parameter: rename `user` → `params`, change `*UpdateUserParams` → `UpdateUserParams`~~ — done
4. ~~Move `users` table DDL to `migrations/001_create_users.sql`~~ — done; includes `DROP TABLE IF EXISTS` before `CREATE TABLE`

### Phase 1: Core CRUD (no auth yet)
1. **Repository** — ✅ implemented and finalized
2. **Service package** — ✅ fully implemented
3. **Handler package** — 🔄 next: start with `GET /users/{id}`
4. **Wire in `main.go`** — connect all layers
5. **Test locally** — verify endpoints work with curl or Postman

**Recommended implementation order:**
1. ~~`GET /users/{id}`~~ — ✅ done and verified
2. ~~`POST /users`~~ — ✅ done and verified (2026-07-03)
3. `PATCH /users/{id}` — update ← next
4. `DELETE /users/{id}` — delete
5. `GET /users` — list all

### Phase 2: Authentication (after core CRUD works)
1. Understand JWT tokens — what they are, how they work
2. Implement `POST /auth/login` — issue a token
3. Implement auth middleware — validate token, extract user identity
4. Lock down endpoints — protect with middleware

### Phase 3: Refinements
- Error response contracts (consistent error shapes across endpoints)
- Validation rules (email format, password strength, etc.)
- Observability (logging, structured output)
- Testing (unit tests for `Validate()`, integration tests for repository)

---

## Key Principles to Carry Forward

1. **Design before code** — lock in REST endpoints, layer structure, config strategy before writing handlers
2. **Separate concerns ruthlessly** — handlers don't access DB, service doesn't know HTTP, repository only does data access
3. **Validate at boundaries** — handlers validate input shape, service validates business rules
4. **Dependency injection via interfaces** — each layer depends on contracts, not concrete types
5. **Configuration is a feature** — flexible enough to work in dev and production without code changes
6. **Fail fast at startup** — configuration errors abort immediately, before the server starts
7. **Collect all errors before reporting** — don't fail on first problem, report all at once
8. **`main.go` is the composition root** — it creates resources and hands them down; nothing finds its own dependencies
9. **Try then handle, not check then act** — don't pre-flight check for conditions the DB enforces; attempt the operation, inspect the error, translate it
10. **Explicit over implicit** — a function's dependencies belong in its signature, not hidden in a context bag it secretly reads
11. **Interfaces belong to the consumer** — the handler defines the interface it needs; the service just satisfies it without knowing the interface exists
12. **Authentication ≠ Authorization** — middleware handles who you are; the service handles what you're allowed to do
13. **Shared types belong in a neutral package** — if neither layer A nor layer B should import the other, the shared type goes in `internal/domain`; don't let a params struct create a forbidden import dependency
14. **Pointer receivers: convention over necessity** — copying a struct with a pointer field copies the pointer, not the underlying value (both still share the pool); but types meant to be shared across a program use pointer receivers for consistency and to prevent silent bugs if mutable state is added later
15. **Each layer holds a reference to the layer below** — service holds repo, repo holds `*sql.DB`; dependencies flow downward via constructor injection
16. **Translate errors at the boundary where you know the source** — `sql.ErrNoRows` is a database detail; only the repo knows it's using `database/sql`, so only the repo translates it; sentinel errors that cross layers live in `internal/domain`, not the service package
17. **`fmt.Errorf` context wrapping belongs at the layer doing the work** — repo wraps with `"Read: %w"` because that's where the SQL runs; layers above just return errors they didn't cause

---

## Handler Layer — Design & Progress

**Handler pattern established:**
- `UserHandler` struct holds `service.UserService` interface (not concrete type)
- `NewUserHandler(svc service.UserService) *UserHandler` constructor
- `RegisterRoutes(mux *http.ServeMux)` — handler owns route registration; `main.go` just calls it
- `main.go` stays as pure composition root: create → wire → register routes → start server
- Handler methods are exported (`HandleGetUser`) so `RegisterRoutes` can reference them; named with `Handle` prefix

**`HandleGetUser` — implemented and verified:**
1. `r.PathValue("id")` — extract path param (Go 1.22+ stdlib)
2. `strconv.Atoi` — convert to int; 400 on failure (client error, not 500)
3. `service.ReadUser(id)` — call service
4. `errors.Is(err, domain.ErrUserNotFound)` → 404; any other error → 500
5. `w.Header().Set("Content-Type", "application/json")` before writing body
6. `json.NewEncoder(w).Encode(user)` — encode error is unrecoverable (headers already sent); log it, don't call `http.Error`
7. Verified: 400 for bad ID, 404 for missing user, 200 + JSON (no password) for valid user

**`POST /users` — implemented and verified (2026-07-03):**
- Public endpoint (no auth yet — Phase 2)
- Decode JSON body into `CreateUserRequest` struct (defined in handler package, not domain — it's an HTTP concern)
- Validate via `validateRequest()` → `validate.Struct()` (validator/v10 struct tags)
- Call `service.CreateUser` → capture the returned id (needed for Location and body)
- `domain.ErrUserAlreadyExists` → 409 Conflict; other errors → 500
- Success path: build `domain.UserResponse` literal in the handler (id from service, name/email from request) → set `Content-Type` and `Location: /users/{id}` headers → `WriteHeader(201)` → encode `UserResponse`
- Verified with curl: 201 + Location + password-free body; duplicate → 409; invalid input → 400; malformed JSON → 400

**Lessons learned finishing `HandlePostUser`:**
- **ResponseWriter ordering rule: headers → status → body.** Once `WriteHeader` sends the status line, later header writes are silently ignored (the Location header was being dropped). If you never call `WriteHeader`, the first body write defaults to 200 — a created resource must send 201 explicitly.
- **`r.Response` is nil on the server side** — it exists for client-side redirect handling. `Response.Location()` *reads* a Location header from a received response; a server *constructs* the Location itself from what it knows (`/users/` + id).
- **`string(intValue)` converts to a Unicode code point, not digits** — `string(65)` is `"A"`. Use `strconv.Itoa` (inverse of `strconv.Atoi`). `go vet` catches this.
- **Never encode the request struct back to the client** — echoing `CreateUserRequest` leaked the plaintext password. The response body is what the server *created* (a `UserResponse`), not what the client *sent*.
- **JSON tags are the API contract** — `UserResponse` had `json:"ID"` next to `json:"name"`; clients saw mixed casing. Fixed to `"id"` before any client depends on it — renaming a JSON field later is a breaking change (versioning territory).
- **`Header().Set` vs `Add`** — Set replaces, Add appends. Single-valued headers like Location take Set.
- **`NewUserResponse` mapper discussion:** a function whose signature only touches domain types (`*domain.User` → `domain.UserResponse`) arguably belongs in `domain` next to the types (constructors live beside what they construct). But it wasn't needed for POST — the handler has no `domain.User`, so a direct `UserResponse` literal was the smallest correct step. Moving the mapper stays optional.

**Database learning — AUTO_INCREMENT gaps (observed during testing):**
- A failed INSERT (e.g. duplicate-email 409) still consumes an auto-increment value — the next successful user gets last + 2 (or more)
- InnoDB reserves the id under a short-lived internal lock *before* attempting the insert; returning it on failure would mean holding the lock until commit/rollback, serializing all concurrent inserts. Gap-free numbering is traded for insert throughput. PostgreSQL sequences behave the same way.
- **The lesson: an id is an identifier, not a sequence number or row count.** Its only job is unique + stable. Gaps also come from deletes and rollbacks; nothing in the API depends on contiguity (`Location: /users/7` works whether the next id is 8 or 12). Never use `MAX(id)+1` logic.
- Forward pointer (Phase 12 / security): sequential ids leak information (user counts, enumerable ranges — related to OWASP BOLA/IDOR); real systems often expose UUIDs or opaque public ids instead.

**`CreateUserRequest` validation rules (simplified 2026-07-03, supersedes 2026-07-02 spec):**

| Field | Rules |
|---|---|
| `name` | required, min 5, max 100 chars |
| `email` | required, valid format, max 255 chars |
| `password` | required, min 8 chars, max 72 chars |

- Password max 72 chars because bcrypt silently truncates beyond that — the DB's `CHAR(60)` is the hash length, not the input limit
- Bug fixed in skeleton: `var c *CreateUserRequest` (nil pointer) → `var c CreateUserRequest` (value type) before passing to `json.Decode`

**Validation implementation decision (2026-07-02, revised 2026-07-03):**
- Switched from hand-rolled `Validate() []error` to `github.com/go-playground/validator/v10` library
- Reasoning: hand-rolling all rules (regex, strength checks, length) is too time-consuming given the learning focus of this project
- Rules expressed as struct tags on `CreateUserRequest` fields; `validate.Struct()` called from a `validateRequest()` method in the handler
- **2026-07-03 decision: keep validation deliberately simple for now** — the character-class password rules (uppercase/lowercase/digit/special) are dropped, and raw `err.Error()` from the validator is returned to the client as-is
- Deferred to the error-contract phase (Phase 3): translating `validator.ValidationErrors` into a client-friendly response shape, and any stricter password rules (would need `RegisterValidation` custom validators)
- Rationale: focus on getting the REST pipeline working end-to-end before polishing validation

**`PATCH /users/{id}` — design decisions (2026-07-03):**

- **Contract chosen:** true partial update — client sends only the fields it wants changed; explicitly sent empty fields are rejected with 400.
- **The absent-vs-empty problem:** decoding JSON into a plain `string` destroys the "was this key present?" information — absent key and `"email": ""` both land as `""` (zero value). A `string` has two states; the contract needs three.
- **Solution: pointer fields (`*string`) in the update request struct.** `nil` = key absent (keep old value); pointer to a value = sent (validate, use); pointer to `""` = explicitly sent empty (400). `encoding/json` fills this correctly for free — decode never touches absent fields, so they stay `nil`.
  - Check order matters: nil-check first, then dereference — dereferencing nil panics.
  - Cost of the extra state: `*req.Name` dereferences and mandatory nil-checks.
- **Where the missing values come from — options considered (the merge logic must live *somewhere*):**
  1. Read-modify-write (chosen): SELECT current user, overlay provided fields, UPDATE. Two queries; merge lives in Go; easiest to read/debug.
  2. Dynamic SQL: build SET clause only from provided fields. One query; merge lives in query construction; harder to read/test.
  3. SQL-side overlay: `COALESCE(NULLIF(?, ''), column)`. One query; merge hidden in SQL.
  4. Require all fields: no merge at all — but that's PUT semantics, should be called PUT. Rejected: wanted real PATCH.
- **Dead end hit on the way (worth remembering):** first instinct was plain strings + '"" means bad request' — but with plain strings, absent decodes to `""` too, so omitting a field would 400 and every request would need all fields = accidental PUT. Defining the three states precisely is what forced the pointer solution.
- **Validation:** rules (`min`, `email`, etc.) must run only on fields actually sent — investigate `omitempty` behavior with pointer fields (does it skip nil but still validate pointer-to-""? test it).
- **Error mapping to check:** does repo `Update` translate MySQL 1062 (duplicate email) like `Create` does, or does it fall through as 500?
- **Open:** where the overlay lives (handler vs service — "is merging onto current state an HTTP concern?"); success response 200+body vs 204.

**One struct per contract — why User has so many shapes (2026-07-04):**

- **The rule is NOT "one struct per method" — it's one struct per distinct *contract*.** A new type earns its existence only when shape (which fields), optionality (required vs. maybe), trust (validated client input vs. internal data), or serialization (wire format) genuinely differs. If two operations share a contract, share the type — `GET /users/{id}` and `GET /users` both return `UserResponse`; no need for per-endpoint copies.
- **The current User-shaped types and the contract each represents:**

  | Type | Contract | What makes it different |
  |---|---|---|
  | `domain.User` | full entity | has password hash; never leaves the service layer |
  | `domain.UserResponse` | what the API returns | JSON tags = public contract; no password |
  | `handler.CreateUserRequest` | what a client must send to create | untrusted input; `required` validation tags |
  | PATCH request struct (upcoming) | what a client *may* send to update | three-state optionality → `*string` |
  | `domain.UpdateUserParams` | what the repo writes | maps 1:1 to the SQL SET clause |

- **Every past bug in this project traced to one struct serving contracts that didn't match:**
  - `json:"-"` on the repo struct — rejected because it baked an HTTP concern into the data layer
  - Echoing `CreateUserRequest` as the response — leaked the plaintext password
  - `CreateUserRequest` for PATCH — plain strings + `required` tags physically can't express partial-update semantics
- **The boilerplate is the point:** separate structs + mapping code is the price of making contract mismatches impossible to compile. Payoff shows up in security (nothing leaks by accident), validation (tags live only on input types), and versioning (entity can change without breaking the wire format).
- **Mainstream name:** DTOs (data transfer objects) / request-response models, kept separate from the domain entity.
- **Open check question:** the PATCH request struct will look nearly identical to `UpdateUserParams` (name + email) — why still two types? (Hint: trust boundary and optionality differ even when the field list matches.) — *Answered 2026-07-04: optionality (pointers vs. definite values) and trust (unvalidated client input vs. resolved write instruction); see next section.*

**Intermediate types across layer boundaries — the PATCH data flow (2026-07-04):**

- **Terminology:** these are *intermediate types* / *boundary types* (layer-to-layer DTOs) — NOT "middleware structs"; in this project *middleware* means HTTP middleware (the future auth layer). Don't overload the term.
- **The problem that forced the pattern:** decided the merge (nil = keep current value) lives in the **service**. So the service must receive something with pointer fields — but the pointer struct (`UpdateUserRequest`) lives in `handler`, and `service` importing `handler` is a backwards arrow. Same problem as when `UpdateUserParams` was stuck in the repository; same medicine: the shared type goes to `internal/domain`.
- **But it's a split, not a move:** `UpdateUserRequest` carries `json:"..."` and `validate:"..."` tags — HTTP concerns that must stay in `handler` (same reasoning that rejected `json:"-"` on the repo struct). `domain` gets a *bare* pointer-fields type with no tags. The handler maps one to the other with nil-safe assignments.
- **The full pipeline — pointers stop at the service:**
  ```
  handler.UpdateUserRequest      *string + json/validate tags   (wire contract, untrusted)
        ↓ handler maps
  domain.UpdateUserInput         *string, no tags               ("what the client may have sent")
        ↓ service merges (Read current → overlay non-nil fields)
  domain.UpdateUserParams        plain string                   ("write exactly these values")
        ↓ repo executes
  UPDATE users SET name=?, email=?                              (definite values only)
  ```
  Three update shapes, each justified by the contract test (optionality / trust / serialization differ even when field lists match).
- **The existence check is free:** the merge's first step is `repo.Read(id)` — which already returns `domain.ErrUserNotFound` when the row is missing → handler maps to 404. "Check if user exists" is never a separate step; it's a side effect of a read the merge needs anyway (try-then-handle, not check-then-act).
- **Request-check ordering — cheap before expensive:** path id parse (400) → JSON decode (400) → tag validation (400) → only then any DB round trip.
- **`omitempty` resolution (was an open investigation):** for pointer fields, "empty" means *nil pointer*, not empty string. Placed first in the tag chain (`omitempty,min=5,max=100`): nil → remaining rules skipped (absent field passes); `&""` → pointer non-nil = present → `min=5` runs against `""` → 400. Exactly the three-state contract, enforced by tags. And `omitempty` is validation-only — it never "keeps the old value"; the service merge does that. Validation decides if the *request* is acceptable; the merge decides what the *write* looks like — two contracts, two homes.
- **Resolves open items from the 2026-07-03 design notes:** overlay lives in the *service* (not the handler); the pointer type lives in *domain* (split, not move). Still open: 200+body vs 204 on success; verify repo `Update` translates MySQL 1062.

**PATCH implementation progress (2026-07-04, end of session):**

- `UpdateUserRequest` finalized: `Name`/`Email` only, both `*string`, tag chains lead with `omitempty` — `ID` removed (path is authoritative) and `Password` removed (belongs to the `ChangePassword` flow, never PATCH)
- PATCH route registered; `HandlePatchUser` implemented so far: decode → validate. **Still missing:** parse `{id}` from path, map request → domain pointer type, service merge call, error mapping (404 not-found / 409 duplicate email / 500), response write. Until then, requests that pass validation return a silent 200 (a handler that returns without writing sends 200 + empty body).
- `scripts/test.sh` — self-checking curl suite for PATCH: each test declares its expected status, prints PASS/FAIL + response body on failure. Expected codes are the *finished* behavior, so failures are the to-do list — the suite goes green as the handler gets built. Targets user id 4; the "valid value" test really mutates data, so reseed if it drifts. `BASE_URL` overridable; exits nonzero on failure (future `make test-api` target).
- **~~OPEN BUG~~ RESOLVED (2026-07-04): the "validator checks the pointer address" theory was a misdiagnosis.** Debugged by unwrapping `validator.ValidationErrors` with `errors.As` and printing `Field()/Tag()/Value()` per error: output was `Field: Email, Tag: email, Value: not-an-email` — a dereferenced string, not an address, and it came from the test that deliberately sends a bad email (expected 400). validator/v10 dereferences pointer fields before applying rules; `omitempty` is the only tag that inspects the pointer itself (nil-ness). **Lessons:** (1) a flat `err.Error()` string invites misreading — unwrap structured errors and print Field/Tag/Value to make the system testify; (2) confirm a "failure" isn't correct behavior before theorizing; (3) the `ValidationErrors` loop written for this debug is the seed of the client-facing error contract (Phase 3). Bug found while debugging: the first version of the unwrap returned nil for non-`ValidationErrors` errors (`*validator.InvalidValidationError` path) — a swallowed error; fixed to pass through everything non-nil.

**PATCH /users/{id} — state of the code (2026-07-05, after rollback — supersedes the 2026-07-04 snapshot):**

The in-progress PATCH wiring from 2026-07-04 was removed; the working tree is clean at commit `5655e0c` (`wip: patch validation`). The *design* (three-state contract, pointer types, service-side merge) is fully resolved and still stands — only the code was rolled back to the decode→validate skeleton.

What survives in code:
- ✅ `handler.UpdateUserRequest` — `Name`/`Email` as `*string`, tag chains `omitempty,min=5,max=100` / `omitempty,email,max=255`; no `ID`, no `Password`
- ✅ PATCH route registered; `HandlePatchUser` = decode → validate only — no id parse, no service call, no response write; passing requests fall through to a silent 200
- ✅ `(*UpdateUserRequest).validateRequest()` method
- ✅ `scripts/test.sh` suite in place (7 tests, targets user 4) — only the 400-validation tests can currently pass
- ⚠️ Debug prints are back in `HandlePatchUser` (`log.Printf("decoded: %+v", p)`, `fmt.Println("validate err:", err)`) — the rollback restored them; remove when finishing the handler

What the rollback removed (to re-create) — *superseded 2026-07-05 later that day: all three re-created, see checklist items 1–3 below*:
- ❌ `domain.UpdateUserInput` — no longer exists in `domain/user.go`
- ❌ `service.UserService.UpdateUser` is back to taking `domain.UpdateUserParams` — needs the input type again
- ✔️ Silver lining: the 2026-07-04 bug (repo interface wrongly taking `UpdateUserInput`) vanished with the rollback — `service.UserRepository.Update` correctly takes `UpdateUserParams`. Keep it that way this time.
- `service.UpdateUser` body is the old pass-through (input straight to `repo.Update`, no merge) — it compiles, but it's PUT-on-two-fields, not the designed PATCH

Verified while re-reading the code (closes the old checklist item 7's question): repo `Update` does **NOT** translate MySQL 1062 — it returns the raw driver error, so a PATCH to a taken email would surface as the handler's fallback 500, not 409. Still to fix.

**`domain.UpdateUserInput` — purpose, nailed down (2026-07-05, while re-implementing item 1):**

The name for the pointer bridge type is settled: **`domain.UpdateUserInput`**. Its one job: carry "what the client may have sent" from handler to service **without** forcing service to import handler. Three types, one per hand-off — don't confuse the two domain ones:

| Type | Fields | Boundary | Meaning |
|---|---|---|---|
| `handler.UpdateUserRequest` | `*string` + json/validate tags | wire → handler | untrusted wire contract; decode + validate here |
| `domain.UpdateUserInput` | `*string`, no tags | handler → service | "what the client may have sent"; nil = field absent; pointers survive because absence is still meaningful |
| `domain.UpdateUserParams` | plain `string` | service → repository | "write exactly these values"; pointers are gone — the service merge resolved every maybe |

Pointers **stop at the service**: the merge (fetch current → overlay non-nil) is what converts maybes into definite values. The repository never sees a pointer.

Why it lives in `domain` and carries no tags: both handler and service already import domain, so the shared type adds zero new import arrows; tags (`json`/`validate`) are edge concerns that must stay on the handler twin. Strictly it *could* live in `service` (handler → service arrow already exists) — `domain` is a deliberate choice ("core vocabulary, not service plumbing"); `handler` is the only impossible home (service → handler is a backwards arrow).

Lessons from re-doing item 1 (2026-07-05):
- First attempt **moved** `UpdateUserRequest` to domain instead of **splitting**. The compiler caught it: `validateRequest` in handler can't be a method on a non-local type — Go requires methods to be declared in the type's own package. Moving the method to domain would drag `validator` (transport concern) into the core, so the fix is the split: restore the tagged twin in handler, keep the bare `UpdateUserInput` in domain.
- Also mixed up which type is the handler↔service bridge (said `UpdateUserParams` at first). The table above is the corrective.
- Import-arrow picture confirmed and named: this is the lightweight Go take on hexagonal / ports-and-adapters (a.k.a. clean/onion) — arrows point inward, `service` declares `UserRepository`, repo satisfies it implicitly, `main` wires. Handler → service is a *legal inward* arrow (that's why handler naming `service.UserService` is fine); service ↔ repository has *no* arrow in either direction. Import graph ≠ runtime call graph — the interface lets calls go where imports refuse.

**PATCH TODO — rubber-duck checklist 🦆 (updated 2026-07-05 for the clean slate; answer the question before writing the code):**

- [x] **1. Re-create `domain.UpdateUserInput`** — done 2026-07-05: bare `*string` Name/Email in `domain/user.go`, comment marks it as the handler↔service bridge. (First attempt moved instead of split — see the lessons entry above; handler twin restored with tags + method.)
- [x] **2. Change `service.UserService.UpdateUser` to take `UpdateUserInput`** — done 2026-07-05: interface takes `(id int, input domain.UpdateUserInput)`; `UserRepository.Update` untouched, still `UpdateUserParams`. Last time's bug did NOT recur.
- [x] **3. Rewrite `service.UpdateUser` body** — done 2026-07-05, compiles clean: `repo.Read(id)` (404 falls out here — repo translates missing row to `ErrUserNotFound`, service passes it up) → `params := UpdateUserParams{Name: u.Name, Email: u.Email}` (start from reality) → two nil-checks overlay dereferenced input fields → `return s.userRepo.Update(id, params)` directly. Naming follows the design table: `input` = pointer bridge, `params` = definite values.
- [x] **4. Handler: parse `{id}` at the top of `HandlePatchUser`** — done 2026-07-05: `r.PathValue` + `strconv.Atoi` → 400, first check in the pipeline (cheapest gate first; the existence read stayed in the service).
- [x] **5. Handler: map `UpdateUserRequest` → `domain.UpdateUserInput`** — done 2026-07-05: struct literal, pointer copied to pointer, no dereference (the maybe-ness must survive into the service; the `*` only appears where the maybe dies — inside the service's nil-checked overlay).
- [x] **6. Handler: call `UpdateUser(id, input)` and CHECK the error** — done 2026-07-05, but it took two passes (see the swallowed-error lesson below). Final ladder: `errors.Is` → `ErrUserNotFound` → 404, `ErrUserAlreadyExists` → 409, fallback 500, `return` on every rung. The 409 rung is wired but CANNOT FIRE until item 7 lands.
- [x] **7. Repo `Update`: MySQL 1062 translation** — done 2026-07-06, same `errors.As` pattern as `Create`. The pending sentinel-rename decision resolved with it: `ErrUserAlreadyExists` → **`ErrMailAlreadyExists`** ("email already in use"), renamed consistently across domain, repo, and both handler rungs — named after the *invariant* (email must be unique), so it reads correctly on both POST and PATCH.
- [x] **8. Success response** — done 2026-07-06: chose **200 + body**. The body-data question answered itself: the handler never fetched the user, so `service.UpdateUser` now returns `(domain.UserResponse, error)` built from the merged `params` — the post-merge truth, no extra query. Handler writes Content-Type → explicit `WriteHeader(http.StatusOK)` → encode. Explicit 200 chosen over implicit deliberately, given this handler's history of accidental status codes.
- [x] **9. Remove the debug prints from `HandlePatchUser`** — done 2026-07-05 (`fmt` import gone too).
- [x] **NEW: decide what to do with repo `Update`'s `rows == 0 → ErrUserNotFound` check** — resolved 2026-07-06: `cfg.ClientFoundRows = true` on the `mysql.Config` in `main.go`; the check stays as written and is now truthful. See the ClientFoundRows resolution entry below.
- [x] **10. `make lint`** — clean, 2026-07-06.
- [x] **11. `bash scripts/test.sh`** — all seven green 2026-07-06; three-state contract confirmed with own eyes.
- [x] **12. Commit** — done 2026-07-06: `57159f2` "PATCH /users/{id} complete", pushed to `origin/feat/handlers`. Checklist closed. 🦆

**Lessons closing out PATCH (2026-07-06):**
- **Copy-paste is a contract smell:** the first success write was POST's block verbatim — `Location` header + 201 on a PATCH. Nothing was created and the client already knows the URL; 201/Location belong to creation only. Caught by the test suite (expects 200), which is exactly the failure mode `test.sh` was built to catch.
- **Recompile before re-testing:** post-fix test run still showed 201 — the running server was the old binary. `go run`/binary must be restarted after every change; a "failing" test against a stale process is testing the past.
- **Error/success returns stay separate:** first version of the new `service.UpdateUser` ended `return response, err` — a populated struct alongside a possibly non-nil error. Go contract: non-nil error ⇒ other returns are zero values, never half-meaningful data. Fixed: `Update`'s error gets its own early return of `(UserResponse{}, err)`; `response` is built only on the success path, ending `return response, nil`.

**`HandlePatchUser` error handling — how the ladder works, and two lessons (2026-07-05):**

The finished pipeline: parse id (400) → decode (400) → validate (400) → map to `UpdateUserInput` → `UpdateUser(id, input)` → error ladder → success response (item 8, pending). All three 400s run before anything expensive, and mapping happens *after* validation — untrusted data passes the checkpoint before it crosses into a domain type.

The error ladder is the handler translating domain sentinels into HTTP, and it's the *only* layer allowed to speak status codes:
- `errors.Is(err, domain.ErrUserNotFound)` → **404** — produced by two different repo lines (`Read` on a missing row, or `Update`'s `rows == 0`), and the handler neither knows nor cares which; the sentinel is the contract.
- `errors.Is(err, domain.ErrUserAlreadyExists)` → **409** — duplicate email. Rung exists but is dead code until item 7 (repo doesn't translate 1062 yet; today a duplicate email falls through to 500).
- anything else → **500**, message says "Server Error" only — internals never leak to the client.
Every rung ends in `return` — that's not decoration, it's what the swallowed-error bug was made of.

**Lesson — the swallowed error (2026-07-05):** first wiring of item 6 was `if err != nil { fmt.Println(...) }` with no `http.Error`, no `return`: every failure printed server-side and fell off the end of the function → client got 200 for a failed PATCH. A Go handler that returns without writing sends an implicit 200 — the same silent-200 that the skeleton produced, now hiding real errors. Rule reinforced: an error you print but don't act on is still swallowed.

**Discovery — MySQL counts rows *changed*, not rows *matched* (2026-07-05, found by experiment):** ran the valid PATCH on user 4 twice; second run sends values identical to what's stored → `RowsAffected() == 0` → repo `Update` returned `ErrUserNotFound` for a user that provably exists (the service's `Read` had just succeeded). go-sql-driver's default reports *changed* rows, so `rows == 0` cannot distinguish "row missing" from "row already had these values". The check was found honestly (it's how `Delete` detects missing rows, where it *is* valid — DELETE's affected-rows means matched) but in `Update` it's wrong. Open decision on the checklist: what should replace it, given the service's `Read` already owns the existence question?

**Resolution — `ClientFoundRows = true` fixes the false 404 (2026-07-06):** two honest options were on the table: (1) flip the driver to report rows *matched* instead of rows *changed*, or (2) drop the `rows == 0` check in repo `Update` and rely on the service's preceding `Read` for existence. Option 2 has a race: a row deleted between the `Read` and the `Update` would silently return success for a write that touched nothing. Chose **option 1** — one field on the `mysql.Config` already being built in `main.go`: `cfg.ClientFoundRows = true`. Effects:
- No-op PATCH (values identical to stored) → matched 1, changed 0 → `RowsAffected() == 1` → success. False 404 gone; a no-op PATCH must succeed — PATCH is about desired state, not about forcing a diff.
- Row deleted between service `Read` and repo `Update` → matched 0 → `ErrUserNotFound` → 404. The repo's check is now *truthful* on its own, not dependent on the service having checked earlier — and the race window is closed.
- **Caveat — connection-level flag:** it changes `RowsAffected` semantics for *every* statement on the pool, not just this query. Audited the other uses: `Delete` unaffected (DELETE's affected-rows always means matched-and-removed), `UpdatePassword` ignores the count. Nothing currently depends on "changed" semantics — but remember this flag exists if a future query ever cares about "did anything actually change."
- Repo `Update` needs **no code change** for this item — the `rows == 0 → ErrUserNotFound` line simply means the right thing now.

**`DELETE /users/{id}` — DONE (2026-07-06, committed `adcf17d` + pushed; note: message says "wip: delete" but the endpoint is complete and verified — next milestone commit should get a milestone message):**

- **Success response: 204 No Content, chosen deliberately.** The deciding question ("what would the body even say?") had no answer — the resource is gone; a `{"message":"deleted"}` body would duplicate what the status line already asserts. Success write is a single bare `WriteHeader(http.StatusNoContent)`.
- **Lesson — headers must match the status's promises:** first version set `Content-Type: application/json` above the 204 (a PATCH copy-paste remnant). Content-Type describes the *body*; 204 promises *no body* — the header labeled the format of content guaranteed not to exist. Harmless on the wire, contradictory in the contract. Removed.
- **Idempotency question — answered (RFC 9110):** double-delete gives 204 then 404, and that does NOT violate DELETE's idempotency. Idempotency is a promise about *server state* (effect of N identical requests = effect of one), not about *responses* — after every call, user is equally gone; the differing status codes are irrelevant to the promise. Contrast POST: replaying a create makes a second user or burns an id on the constraint — effect of N ≠ effect of 1. Practical payoff: idempotent methods are safely retryable after a timeout. (Some APIs return 204 for missing-on-delete — defensible "tolerant" style; 404 is equally correct and more informative. Neither is "required by idempotency".)
- **Responses are receipts, not hopes:** a client needs ONE call — the 204 is authoritative because the handler writes it only *after* the SQL succeeded. GET-after-DELETE verification belongs in the test suite (proving the unproven handler keeps its promises), never in the client workflow — if the client can't trust the 204 it can't trust the verification GET either; the protocol only works because responses are authoritative.
- **`scripts/test_delete.sh` — self-contained suite pattern (5 checks, all green):** setup POSTs a disposable user (unique email per run: `delete-test-$$-$RANDOM@`) and extracts the id from the 201 body — repeatable forever, no reseeding, never touches seed user 4 that the PATCH suite depends on. The lifecycle triple encodes the concepts: delete → 204 (receipt), GET → 404 (state really changed), delete again → 404 (idempotent state, informative response). Plus rejection branches: garbage id → 400, unknown id → 404. The API tests itself — POST's response body is what makes DELETE testable.
- Error ladder correctly *shorter* than PATCH's: no 409 rung — a delete can't produce a duplicate-email conflict. Reasoned, not copy-pasted.

**Original in-progress notes (2026-07-06):**

- **Lesson — 405 reads as "path known, method not bound" (found by experiment):** first DELETE curl returned **405 Method Not Allowed**. The instinct is to suspect the handler — but the handler was never the problem: the route was missing from `RegisterRoutes`. The diagnostic value of 405 is precise: Go 1.22's `ServeMux` answers it *automatically* when the path matches some registered pattern but no pattern with that method — a 404 would have meant "path unknown", 405 means "I know `/users/{id}`, just not for DELETE". The mux even sends an `Allow` header listing the methods it does accept (visible with `curl -i`). Status codes aren't just outputs to map correctly — they're *inputs* when debugging; the code alone located the bug in one step, no print statements needed.
- **Corollary:** the route comment block at the top of `handler/user.go` is the endpoint spec — the bug was the gap between the comment and the `RegisterRoutes` body. When adding an endpoint, the route registration is step zero, and 405 is the symptom of skipping it.
- Open (answer in flight): the double-delete idempotency question — first call succeeds, second returns 404; does that violate "DELETE must be idempotent"?

**`GET /health` — added 2026-07-06, deliberately shallow (liveness only):** returns 200 + `{"status":"ok"}` if the process is serving HTTP; makes NO claim about MySQL — decided (not defaulted) to keep it that way for now. Deferred to revisit (likely at deploy time / Phase 2+): (1) readiness variant that pings the DB — can't live on `UserHandler` (holds only `UserService`; health would need a pinger — smallest-interface question); (2) currently a method on `UserHandler` that never uses its receiver — should be a plain func or own handler; (3) consistency slips vs. own conventions: `Encode` error discarded, 200 implicit; (4) Phase 2: `/health` must be excluded from auth middleware (LBs can't log in).

**Backlog — dev tooling (added 2026-07-06):** watch-rebuild-restart for the dev loop (user-requested after hitting the stale-binary trap twice; scheduled after Phase 1 endpoints are done). Go has no literal hot reload (compiled binary, nothing to swap in-process) — tools watch files, rebuild, restart. Candidate: `air` (air-verse/air, de facto standard; `air init` → `.air.toml`, exclude `bin/`/`scripts/`/`.claude/`), likely as a `make dev` target. Dev-only — never a production concern.

**Status:** ✅ `POST /users` done and verified (committed: `6b4ed40`) — ✅ `PATCH /users/{id}` **DONE**: complete, verified (lint clean, seven `test.sh` checks green), committed `57159f2` and pushed 2026-07-06. Full pipeline: three-state contract → pointer bridge → service merge → truthful repo checks via `ClientFoundRows` → 400/404/409/500 ladder → deliberate 200+body. Sentinel renamed to `ErrMailAlreadyExists`. ✅ `DELETE /users/{id}` **DONE** (204 deliberate, `adcf17d`). ✅ `GET /users` + `GET /health` **DONE** (`e48d0ab` "handlers done", merged to master): list endpoint one-rung ladder (empty table = valid state), `[]`-never-`null` held, no-password contract verified over the full table; health = shallow liveness by decision. ✅ Verification bundle (`d6dbf05`, branch `feat/test-api`): seed migration `002_seed_users.sql`, PATCH suite refactored self-contained, new `test_get_users.sh` — **all three suites green: 15/15**.

# 🏁 PHASE 1 — CORE CRUD: CLOSED (2026-07-06)

Five endpoints + health, each with a deliberate status contract and executable proof:

| Endpoint | Success | Suite |
|---|---|---|
| `POST /users` | 201 + Location + body | covered via suite setups |
| `GET /users/{id}` | 200 + body | exercised in delete lifecycle |
| `PATCH /users/{id}` | 200 + post-merge body | `test.sh` 7/7 |
| `DELETE /users/{id}` | 204, no body | `test_delete.sh` 5/5 |
| `GET /users` | 200 + `[]`-never-`null`, no password | `test_get_users.sh` 3/3 |
| `GET /health` | 200 (liveness only) | — |

**Lesson — the test-fixture incident (2026-07-06):** after closing DELETE, the PATCH suite went 5/7 red — 404s on user 4, deleted hours earlier by a manual smoke-test curl. Not a code regression: a *fixture* regression. Root cause: the suite depended on pre-existing shared mutable state (seed user 4), while the DELETE suite — which creates its own disposable user — sailed through green. Fix was twofold: (1) `migrations/002_seed_users.sql` — the seed data existed only in this doc, never as a runnable file ("state that exists only because someone once typed it is state you can't recreate"); (2) PATCH suite refactored to the disposable-user pattern. Rule: **suites create what they need; seed data is for dev convenience, never a test dependency.**

**Deferred polish (optional, noted 2026-07-06):** rename `test.sh` → `test_patch.sh` (name predates its siblings; use `git mv` to keep history); `make db-reset` (pipe 001+002) and `make test-api` (run all `test_*.sh`; make's fail-fast composes the suites' nonzero exits for free).

**Next up (in discussed order):** `air` hot-reload (backlog entry above) → password-change endpoint (design questions logged above — resource vs action, PUT/POST, 400/401/403 mapping) → **Phase 2: auth**. **Deferred by decision 2026-07-06:** the password-change endpoint (service `ChangePassword` flow exists but has no route — gap spotted 2026-07-06). Design questions on the table: resource vs action (`/users/{id}/password`?), PUT-vs-POST given the body mixes new state with proof (oldPassword), status for wrong-old-password (400/401/403) and same-password (400 vs 422), success 204?. Build it after GET/DELETE close Phase 1 — it's the natural bridge into Phase 2 auth.

---

## Phase 2 — Auth: kickoff (2026-07-06, afternoon)

**Release first:** `v0.1.0` published — annotated tag on `d6dbf05` (first attempt was a tag named "releases"; deleted and redone). Learned: a tag names ONE commit forever (tags don't move, branches do — no "save branch" needed, a branch can sprout from a tag any time); semver `0.x` = "contracts may still change", `1.0.0` is a stability promise we can't make before auth; GitHub Release = presentation wrapper over a git tag; Go modules read `vX.Y.Z` tags.

**Learning plan agreed:** no Phase 1 redo now. After Phase 2: solo checkpoint project (small CRUD API, different resource, from `git init`, no mentor, no peeking — same standards incl. curl suites; gaps found = the findings). Final exam: the audio pipeline, built mostly solo.

**Concepts covered:**
- The stateless problem: request N has no memory of N−1 → something must carry identity. Sessions = server-side state (breaks/complicates behind an ALB: sticky sessions or shared Redis). Tokens = self-contained proof, any instance verifies without asking anyone → JWT.
- JWT = `header.payload.signature`. Payload (claims) is base64-encoded JSON — **encoded, not encrypted**; anyone holding the token reads it (jwt.io). Signature = HMAC over the first two parts with the server secret: only the server can mint, anyone can't tamper. **Metaphor that landed: the claims are a note I write to my future self; the client is the courier.** Secret = crown jewels, lives in config, never git.
- Claims membership test (two gates): (1) courier reads the note → only public-safe data; (2) note exists to serve future requests → only what's needed every request. `sub` + `exp` are in; password hash obviously out.

**Q1 ANSWERED (with honors): failed login → 401, always — and unknown email returns the SAME 401.** Distinguishing "no such email" from "wrong password" = **user enumeration** oracle (OWASP). Three layers of "same": status, body, and *timing* — bcrypt only runs when the email exists, so the missing-email path is ~100ms faster; standard fix is a dummy bcrypt compare on that path (planted for when the service method is written). 401 vs 403 nailed: 401 = "I don't know who you are" (bad login, missing/invalid token); 403 = "I know who you are, and no" (valid token, forbidden action). Consequence for the error ladder: login's service method must COLLAPSE `ErrUserNotFound` + wrong-password into one new sentinel (~`ErrInvalidCredentials`) — first time hiding information is the design goal; repo stays truthful, service does the hiding. (RFC nicety: 401 should carry `WWW-Authenticate: Bearer`.)

**~~Still open~~ — all three resolved 2026-07-13, see "contract closed" section below:**
- **Q2:** claims — do `email`/`name` pass the two gates? → answered: no, `sub` + `exp` only.
- **Q3:** token lifetime + reasoning → answered: 15 min.
- Sub-question parked: login logic on `UserService` or a separate `AuthService`? → answered: separate `AuthService`.

**Phase 2 map & build order:** `handler/auth.go` (own `AuthHandler` — don't bolt onto UserHandler, cf. health-check lesson) → service owns lookup+bcrypt+mint → `middleware/` package is NEW (wraps handlers, verifies token, identity into context, 401 without handler running) → repo gap: no read-by-email yet, likely the only repo addition. Order: contract (Q1–3) → repo → service → handler → middleware → protect routes. Branch: `feat/auth`.

**Mentoring style recalibrated (2026-07-06):** explanations now follow the confirmed rubber-duck template (physical-first, first-person roles, one fact per step, metaphor, conclusion falls out, one check question) — captured in mentor memory; density was the failure mode.

---

## Phase 2 — Auth: contract closed & build plan (2026-07-13)

**Contract (Q1–Q3) DONE:**
- **Q1:** failed login → **401 always**, identical status + body + timing for unknown-email and wrong-password (user enumeration defense). Timing fix: dummy bcrypt compare on the missing-email path. Service collapses `ErrUserNotFound` + wrong-password into one new sentinel `ErrInvalidCredentials` — repo stays truthful, service hides.
- **Q2:** claims = **`sub` (user id) + `exp` only**. Email/name rejected: fetchable via id on any request, and mutable — a PATCHed email would go stale inside a frozen token. Rule: only immutable, needed-every-request facts go in claims.
- **Q3:** lifetime = **15 min** (production-grade short window; stolen-token exposure over re-login friction). Refresh tokens deliberately NOT in Phase 2 — token dies, log in again. Lifetime + JWT secret live in config/`.env`, secret never in git.

**Service split decision (answered by me, confirmed):** `Login` gets its own **`AuthService`** — it's a different task/contract ("verify identity, mint proof") than `UserService`'s CRUD ("manage the users resource"). Split on the *contract*, not the table — same muscle as the health-check-on-UserHandler lesson. The split is service-layer only: there is still ONE `UserRepo` (one users table); `AuthService` is just its second consumer. No `AuthRepo`.

**Boundary rule locked in: repo fetches, service compares.** The repo answers exactly one question — "give me the user with this email" — returning the row *including the password hash*, or `ErrUserNotFound`. It never sees the plaintext password. The service holds both pieces (stored hash + submitted plaintext) and runs the bcrypt comparison itself. If the repo "checks passwords," data access and business logic have blurred.

**`AuthService` dependencies (= what `main.go` hands the constructor):** (1) the user repo, (2) the JWT secret, (3) the token lifetime. Minting is a library call — `golang-jwt` is the standard choice.

**Build steps (agreed order, step 1 in progress):**
1. **Repo — read-by-email** (in `repository/user.go`): method takes email, returns user **including password-hash column** (the one query allowed to select it — check the scan-target struct has a field for it; DTOs already strip it from responses). `SELECT` with `?` placeholder, `QueryRow`, scan; `sql.ErrNoRows` → `ErrUserNotFound`; other errors pass through unchanged. ~~takes `ctx` + `QueryRowContext`~~ — decided 2026-07-14: NO ctx yet, match the existing methods (see backlog entry below).
2. **Service — `AuthService`:** new file; struct holds repo + secret + lifetime. `Login(ctx, email, password)`: fetch by email → bcrypt-compare → either failure returns `ErrInvalidCredentials` (dummy bcrypt on not-found path) → success mints JWT (`sub`, `exp`) and returns the token string.
3. **Handler — `handler/auth.go`:** `POST /auth/login`, decode `{email, password}`, call `Login`; map success → 200 + token JSON, `ErrInvalidCredentials` → 401, else → 500. (RFC nicety: 401 carries `WWW-Authenticate: Bearer`.)
4. **Middleware — new package:** verify token, put user id into context, 401 without the handler running. Then protect routes; `/health` stays public.

**Backlog — `context.Context` in the repository (decided 2026-07-14):** none of the repo methods take a `context.Context` today. Decision: the new read-by-email method matches the existing style (plain `QueryRow`, no ctx) — consistency now, one change later. In a future version, add `ctx` to ALL repo methods in a single pass (`QueryRowContext`/`ExecContext`/`QueryContext`), threading it from `r.Context()` in the handlers down through the service. Concept learned 2026-07-14: context carries a cancel signal + deadline so work that no longer matters (client gone, request too slow) can stop early; it can also carry request-scoped values — that part arrives with the auth middleware (user id into context), which does NOT wait for this backlog item.

---

## Phase 2 — build progress (2026-07-14)

**Housekeeping:** pending tasks now live in `.claude/BACKLOG.md` (created today) — one line per task, grouped Now / Next / Refactors / Dev tooling / Later. New deferred work goes there, not buried in this file's paragraphs.

**Concepts learned today:**
- **`context.Context`** = a value that travels with one request through all layers, carrying a cancel signal + a deadline, so work that no longer matters (client gone, request too slow) can stop early. `QueryRow` vs `QueryRowContext` = same query, the second can be stopped. Third use (carrying values, e.g. user id from middleware) comes with Phase 2 middleware.
- **JWT vocabulary fixed:** the JWT **is** the token — one string `xxxxx.yyyyy.zzzzz`; the id (`sub`) is written *inside* the middle chunk. Not "an id and a token" — one thing, id inside.
- **Why read-by-email exists at all:** at the login moment the client has ONLY email + password — no token exists yet (the request's purpose is to create the first one). Email finds the row → row gives id + hash → password proves identity → id goes into the token. Every later request carries the id in the token; find-by-email is login-only. Every successful login mints a fresh token (not just the "first" — 15-min expiry means re-login is routine).

**Step 1 DONE — `ReadByEmail` in `repository/user.go`:** clone of `Read` with `WHERE email = ?`; selects the password hash (auth needs it); `sql.ErrNoRows` → `ErrUserNotFound`; no ctx (per today's decision); build + vet clean. Nit noted: wrap prefix `"Read by Email:"` breaks the grep-to-function convention (`"ReadByEmail:"`).
- **Placement lesson:** a `repository/login.go` (or `auth.go`) file was considered and rejected — the method answers "give me the user with this email", it knows nothing about login; repo files are named for the DATA they access, not the feature that calls them. A stray `repository/auth.go` (empty package clause) was created anyway and deleted after review. New files belong one level up: `service/auth.go`, `handler/auth.go`.

**Step 2 IN PROGRESS — `AuthSvc` in `service/auth.go`:** struct (`authRepo`, `jwtSecret`, `tokenLifetime`) + `NewAuthService` constructor DONE, builds clean. `Login` method NOT started.
- **`tokenLifetime` is `time.Duration`, not `string`:** minting writes `exp` = now + lifetime — clock arithmetic needs a duration. Config parses the env string ONCE at startup (`time.ParseDuration`), fails fast if broken; the service receives a ready-to-use value. (First draft had `tl string` — both name and type corrected.)
- **`AuthRepository` interface (consumer-owned, ONE method):** declared in `service/auth.go` above its consumer, contains only `ReadByEmail` — a shopping list of what auth needs, NOT a new warehouse; `*UserRepo` stays the only concrete repo and satisfies it implicitly, zero changes in the repository package. `main.go` will hand the same `*UserRepo` to both services; each sees only the slice its interface names.
- **Lesson — an interface is a claim:** `ReadByEmail` was also added to `UserRepository` (both options done at once); removed after review. `UserSvc` never calls it, so the claim was false — and every future test fake for `UserRepository` would be forced to implement a method no test exercises. Smallest interface, same muscle as the health-check lesson.

**`Login(email, password) (string, error)` — WRITTEN (2026-07-14, compiles + vets clean; three review fixes pending, see below):** body = `ReadByEmail` → bcrypt compare → build `jwt.RegisteredClaims` (`Subject` = `strconv.Itoa(u.ID)`, `ExpiresAt` = `jwt.NewNumericDate(now + lifetime)`) → `jwt.NewWithClaims(HS256, claims)` → `token.SignedString([]byte(secret))` → return the string.

Design points settled while writing it:
- **Both client-fault failures collapse into `ErrInvalidCredentials`** (unknown email, wrong password) — deliberately indistinguishable. Everything else wraps with `fmt.Errorf("Login: %w", err)` and bubbles up → handler fallback 500. The whole policy in one sentence: the sentinel appears exactly twice; every other error is a server problem and must arrive intact.
- **Bcrypt error split answered:** `ErrMismatchedHashAndPassword` → `ErrInvalidCredentials`; any other bcrypt error (e.g. malformed stored hash) is a SERVER problem → wrap, 500, never 401.
- **Signing error is NOT `ErrInvalidCredentials`:** it fires after both checks passed — the client did everything right. Rule for choosing an error: *what is this error's true story?* `ErrInvalidCredentials` tells exactly one story ("your email or password is wrong"); a signing failure tells a different one → wrap it.
- **Detour reverted — `domain.ErrInternalServer` (created, then deleted):** replacing unknown errors with a generic sentinel (1) destroys information — the original "connection refused" is gone from every future log; (2) "internal server error" is HTTP language (the name of 500) — domain sentinels speak business language; the handler is the only translator. Sentinel test reconfirmed: no caller branches on "unknown failure" — that's the fallback rung, it needs no name.
- **Claims: the data vs the container.** Claims = the data (`sub`, `exp`). Container = `jwt.MapClaims` (loose map, typo-prone) or `jwt.RegisteredClaims` (typed struct, compiler checks names) — chose `RegisteredClaims`. No custom claims type: only needed for non-standard claims (embed `RegisteredClaims` + extra field); our contract is sub+exp only. The claims value is LOCAL inside `Login` — born and dies in the function; test: does anyone outside see it? No → it lives inside.
- **Signing mechanics:** `SignedString` encodes header + claims, computes the HMAC signature with the secret, glues `xxxxx.yyyyy.zzzzz`. HS256 key must be `[]byte` — a plain string compiles but fails at RUNTIME ("key is of invalid type").
- **Lesson — docs examples teach shape, not text:** the golang-jwt example (custom `MyCustomClaims`, "johndoe", hardcoded subject and 15min, `IssuedAt`) got pasted into `main.go` → syntax error + wrong file + wrong content. Claims are built inside `Login`; every value comes from a variable already in scope (`u.ID`, `a.tokenLifetime`); the contract (sub+exp) decides the fields, not the example.
- **`Login` returns ONLY the token string** — the `domain.User` from `ReadByEmail` is used for two fields (hash to compare, id for `sub`) and dies inside the function; it contains the password hash and must never travel toward HTTP. Repo returns complete data (full `User`), service uses what it needs and exposes only the product. A function's return is its contract, not its work history.
- **Config values reach `Login` through the struct, not the config package:** `main.go` reads config ONCE and injects via `NewAuthService`; the method reads its own fields (`a.jwtSecret`, `a.tokenLifetime`). Service never imports config / calls `os.Getenv` — same rule as the repo not opening its own DB connection. Payoff: a test builds an `AuthSvc` with any secret/lifetime in one line.

**The timing leak, explained (why the TODO marker and where it goes):** count the work on the two failure paths. Path A (email exists, wrong password): `ReadByEmail` finds the row, bcrypt runs — bcrypt is *deliberately slow* (~100ms; that's its anti-brute-force job) → 401. Path B (email unknown): `ReadByEmail` returns not-found, bcrypt never runs, ~2ms → 401. Status and body are identical — but an attacker can *time* the responses: 100ms → email exists; 2ms → it doesn't. The stopwatch answers exactly the question the identical 401s refuse to answer — user enumeration through a side door. Fix (planned): on path B run a bcrypt compare against a dummy hash and throw the result away, purely to burn the same ~100ms — both paths then take equal time and the stopwatch learns nothing. The `// TODO: dummy bcrypt compare (timing)` marker sits on the exact line where the dummy compare will go (inside the `ErrUserNotFound` branch, before its return) — the marker lives IN the hole it marks, so anyone reading the fast path sees it's a known gap, not an accident. Close before the endpoint ships.

**Login review fixes — ALL APPLIED (2026-07-14, later):** `a.tokenLifetime` in `ExpiresAt`, var renamed `tokenString`, TODO marker sits in the not-found branch. Build + vet clean. `Login` done; only the TODO itself stays open (deliberately, until just before the endpoint ships).

**Config wiring — DONE (2026-07-14):** `.env` got `JWT_SECRET` + `TOKEN_LIFETIME=15m`; `config.go` loads both, validates (secret ≥ 32 chars, lifetime > 0), `time.ParseDuration` once at startup; `main.go` wires `service.NewAuthService(r, c.JwtSecret, c.TokenLifetime)`. Lessons collected on the way:

- **What makes a good JWT secret:** never "encrypted" — just unguessable. Attacker with one valid token can brute-force OFFLINE: compute the HMAC with guessed secrets until the signature matches; then they mint tokens for any user id. 5 chars falls in seconds; 32+ random bytes from a tool (`openssl rand -base64 32`) is practically unguessable. Config consequence: `Validate()` gets its first *quality* check (not just presence) — secret shorter than 32 chars = refuse to start; a server with a guessable secret is worse than no server.
- **The secret is generated OUTSIDE the app, once.** If the app generated it at startup: every restart = new secret = all existing tokens rejected = everyone logged out; and multiple instances behind a load balancer would each mint with a different secret — instance B rejects tokens minted by A, killing JWT's whole point ("any instance verifies alone"). Same secret across restarts and instances ⇒ it's CONFIG, like `DB_PASSWORD`: created once by a human/ops, injected by the environment. Rotation is an ops action outside the app.
- **`.env` WAS TRACKED BY GIT** — found because "double-check before the secret exists on disk" was actually done. `.gitignore` never contained `.env`; the DB password sat in pushed history since the early commits. Fix: `.gitignore` line + `git rm --cached .env` (untrack, keep on disk) + commit — the JWT secret was still uncommitted and never entered history. Old password in history = treat as burned (low stakes here: local dev DB, private repo — no rewrite needed). THE lesson: *a secret that has ever touched git is not a secret anymore*; and habits don't switch on when stakes rise — you ship the habits you trained. Pending nicety: commit a `.env.example` with placeholders.
- **`time.Time` vs `time.Duration`:** a *point* on the calendar vs an *amount* of time. `"15m"` is an amount → `time.ParseDuration` → `time.Duration`. They meet in `Login`: `time.Now().Add(a.tokenLifetime)` — point + amount = new point (the expiry). Config parses the string ONCE; the string is never seen again after startup.
- **Config review fixes (3 rounds):** (1) the 32-char length check first landed on `AppEnv` — "development" is 11 chars, the app would never have started; moved to `JwtSecret`, merged with the empty check (one problem = one message). (2) `string(c.TokenLifetime)` — the `string(65)`→`"A"` trap AGAIN, on a `time.Duration` (a number); meaningful zero-check is `== 0`. (3) `ParseDuration`'s error was first ignored (compile error: declared and not used), then replaced with a fixed sentence that lost the cause; final form wraps the original (`%w`) and appends it to the collected error list — collect-all philosophy holds, and the wrapped error still says WHY ("unknown unit x in 15x"). Bonus trap avoided: `fmt.Errorf(variable)` = non-constant format string, vet flags it.
- **Secrets never go into logs:** the scaffolding `fmt.Println("main - newauthsvc:", l)` prints the `AuthSvc` struct — and `fmt` prints UNEXPORTED fields too, so the JWT secret lands on stdout at every startup. Stdout is the log stream in production; logs get stored and shipped. All four wiring debug prints must go (backlog).
- **Implicit interfaces, seen live in `main.go`:** the same `r` (`*UserRepo`) feeds both `NewUserSvc(r)` (needs the 7-method `UserRepository`) and `NewAuthService(r, ...)` (needs the 1-method `AuthRepository`). An interface parameter means "anything with these methods"; the compiler checks method sets, no declarations anywhere. One person, two ID cards — each door checks only its own card. Deletion test (answered wrong first, corrected): deleting `ReadByEmail` from the repo breaks ONLY the `NewAuthService` line — a method's deletion breaks exactly the consumers whose interface names it; false claims in interfaces make changes look more dangerous than they are.

**Next — `handler/auth.go` (`POST /auth/login`):** the two warm-up questions on the table: (1) login request struct — fields + validation tags? (2) does login's error ladder need a 404 rung? (Careful: it must NOT — `ErrUserNotFound` never escapes `Login`; 404 on login would be the enumeration leak.) Also first: delete the four debug prints from `main.go`.

---

## Phase 2 — build progress (2026-07-15): login endpoint DONE and verified live

**`AuthService` interface — the handler stops knowing the struct:** first draft of `handler/auth.go` had the field typed `service.AuthSvc` — the concrete struct, with the secret and the repo inside. But the handler uses exactly ONE thing: `Login(email, password) → (token, error)`. So: one-method `AuthService` interface, declared in `service/auth.go` next to `UserService` (same placement as the existing pattern). Bonus it fixed for free: `NewAuthService` returns `*AuthSvc` (a pointer), and the pointer is what has the `Login` method — with a struct-typed field `main.go` wouldn't even have compiled cleanly; the interface field accepts the pointer directly. Payoff named: a test can hand the handler any fake with a `Login` method. Same move as `AuthRepository`, one layer up — **each layer names only what it needs from the layer below.** Naming note (left as-is, his call): `NewAuthService` returns the struct while `AuthService` is now the interface; `NewUserSvc` reads differently.

**`HandlePostLogin` — three review rounds, all fixed:**
1. **Route registered nothing:** `main.go` had `handler.NewAuthHandler(a)` — built the handler and threw it away; `RegisterRoutes` was written but never called; `POST /auth/login` was a 404. The 405 lesson again from a new angle: *route registration is step zero, and a constructor's return value has to land in a variable to be usable.* Fixed — and the wiring variables got real names on the way (`authHandler`, `userHandler`, `authSvc` instead of `a`, `u`, `s`).
2. **409 for invalid credentials → 401 + `WWW-Authenticate: Bearer`.** Say the code out loud: 409 = "your request fights the current state of the resource — fix and retry" (right for duplicate email on create; nothing to do with identity). 401 = "I don't know who you are"; its header tells the client HOW to authenticate. The contract he himself wrote (Q1, 2026-07-06) said 401-always — the first draft contradicted his own design doc.
3. **201 for success → 200.** 201 = "I created a resource and it has an address" — a token has no address (no `GET /tokens/{id}`). 200 = "here is your answer."
4. **Bare string body → `AuthResponse{Token string `json:"token"`}`.** `Encode(token)` produced `"eyJhb..."` — a legal JSON string as the whole body. Clients want an object they can pick fields from, and an object can grow without breaking anyone — same reason `UserResponse` exists. Placement: next to `LoginRequest` in the handler file — it's HTTP contract, not domain.

**`LoginRequest` validation — `required` only (his call, correct):** password rules (min 8 / max 72) are rules about *creating* passwords; at login the DB hash is the truth — a 3-char guess simply fails bcrypt → 401. A `min=8` tag at login would answer 400 to the same fact: two different answers for one situation, and free information for an attacker probing the endpoint. Reused the package-level `validate` var from `user.go` (same package — no second validator instance).

**Verified live end-to-end (curl, 2026-07-15):** create 201 → good login 200 + `{"token":"..."}` → wrong password 401 + `Www-Authenticate: Bearer` → unknown email the IDENTICAL 401 (no enumeration) → missing field 400. Decoded the token's middle chunk (base64): `{"sub":"9","exp":...}` — the whole Q2 contract physically visible, nothing else in the claims. Every line of the designed contract answers correctly on the wire.

**Incident — `.env` deleted from disk, recovered:** git had the file only up to the commit BEFORE it was untracked (`e1e58f4^`) — the 8 pre-JWT keys. `JWT_SECRET`/`TOKEN_LIFETIME` were added AFTER untracking, so they were in no commit anywhere: the old secret is gone forever — **which is the system working** (never leaked into history). Recovery: restore the 8 keys from git, regenerate the secret (`openssl rand -base64 32`), re-add `TOKEN_LIFETIME=15m`. Rotation cost: zero — the only thing the old secret could do is validate old tokens, and those die within 15 min anyway. Trap met on the way: the committed file had no trailing newline, so a naive append glued `JWT_SECRET=` onto the `NET_PROT` line. Lesson: **`.env.example` is the committed map of which keys must exist** — after this incident it's no longer a nicety (backlog, still open).

**Discovery — `GET /health` never existed:** every doc said "health done 2026-07-06 (e48d0ab)"; the code says it was ONLY ever a comment line in `handler/user.go` — no handler function, no route, answers 404, in every commit including e48d0ab. Docs corrected; implementation moved to backlog Refactors. Lesson: *notes can lie; the code is the record.*

**Discovery — `master` == `feat/auth` (`abb4414`):** wip auth commits are on master. Open question for him (backlog "Decisions pending"): intended, or should master sit at the last finished work?

**Remaining before middleware (in order):** (1) `scripts/test_login.sh` — self-contained curl suite; (2) close the timing TODO — dummy bcrypt on the unknown-email path (the unknown-email 401 currently answers visibly faster than the wrong-password 401); (3) `.env.example`. Then middleware — verify token, user id into context, protect routes.

**2026-07-16 — `test_login.sh` in progress:** first draft reviewed twice. Fixed so far: URL bug (`/login` → `/auth/login`), setup locked to 201 with abort-on-failure (`exit 1` — every later check depends on the disposable user), unique email per run. Still open: capture `USER_ID` + cleanup DELETE, unknown-email 401 test, missing-field 400 test, `WWW-Authenticate` header check. **Decision:** the draft's `GET /me` tests asserted a route that was never designed — a red test is a to-do only when it encodes an AGREED contract, otherwise it's a guess smuggled in through a test file. `/me` moved to backlog "Later / ideas" (design after middleware); its tests come out of this suite.

**2026-07-16 — rule change + suite green:** CLAUDE.md amended by his decision: Claude MAY write `.sh` files (test suites, tooling), always asking first; Go code stays fully under the no-code rule. The change went into the doc deliberately (Option B) instead of breaking the rule on request — the rule was written to hold even against direct asks, so the doc had to change first. Claude then finished `test_login.sh`: /me tests removed, `USER_ID` captured from the register body (bare number, no quotes — different sed pattern than the quoted token), wrong-password 401 now also checks `WWW-Authenticate` via `curl -D -` header dump, new unknown-email-identical-401 and missing-field-400 tests, cleanup DELETE → 204. **Run against the live server: 10/10 green.** Note for the solo checkpoint: he has now written zero curl suites solo this phase — the checkpoint project still requires them from scratch.

**2026-07-16 — timing TODO SKIPPED (his call, scope not ignorance):** full walkthrough done first — the two 401 paths cost different time (not-found returns in ms; wrong-password pays bcrypt's deliberate ~100ms), so a stopwatch distinguishes them even though body+status are identical: the clock leaks what the words hide (guest-book guard metaphor). Fix understood: dummy `CompareHashAndPassword` against a REAL constant hash on the not-found path, result discarded — same work = same time by construction, which is why `time.Sleep` is the wrong tool (a guess that goes stale across hardware/cost factors). Decision: skip for this learning project; moved to backlog "Later / ideas"; `// TODO` stays in `Login`. Also today, sidebars: seed users' tutorial hash had unknown plaintext → lesson "a hash is a fingerprint, not a container" + plan to reseed with a hash minted via his own API (donor-user trick); heredoc scratch-comments after `[[ $FAIL -eq 0 ]]` in test.sh silently forced exit 0 → lesson "a script's exit code is its LAST command's code".

**2026-07-16 — git day + `VerifyToken` built:** master moved back to `d6dbf05`/`v.0.1.0` (`git branch -f` + force-with-lease; safe: feat/auth names every commit) — master = finished work only; idle `dev` deleted local+remote; `feat/middleware` branched off `feat/auth` (127a4a9 "login works"). Flagged: next release should probably be `v0.2.0` not `v1.0.0` (1.0.0 = stability promise), and tag style `v.0.1.0` has a stray dot — his calls. Middleware concept landed: handler-in-handler-out signature (he derived it), `http.HandlerFunc` = type-conversion costume, closure holds `next`. **`VerifyToken` built in AuthSvc after real struggle** — misconceptions worked through, in order: (1) "compare tokenString with the subject field" → no lookup exists, verification is recomputing the signature; (2) "compare with WHAT?" → the token with ITSELF: carried chunk-3 vs freshly recomputed chunk-3 under the secret; (3) keyfunc first draft MINTED (`t.SignedString`) instead of returning `[]byte(secret)`; (4) copy-paste smells: bcrypt sentinel + `"Login:"` prefixes in VerifyToken; (5) duplicate `VerifyToken` in the `AuthService` interface (build error he then fixed). Detailed plain-language writeup added to DOCS.md §12 at his request ("this function was hard to me"). Feedback logged: THIRD length reprimand — replies must be SHORT. Open design q pending: should `VerifyToken` sit in the handler's `AuthService` interface, or in a middleware-owned interface? — RESOLVED: middleware owns its one-method `TokenVerifier`; handler's `AuthService` keeps only `Login` (consumer-owned interfaces, third instance of the pattern). NEXT: middleware body (6-step prose plan given), then wiring, then protected-routes suite.

**2026-07-16 — `middleware.Auth` body built (three review rounds):**
1. `if header == " "` — a single SPACE; a missing header is the EMPTY string `""`, so every tokenless request walked past the guard. Fixed.
2. The missing-header branch set `WWW-Authenticate` and returned — no status written → Go sends an implicit 200: *the guard forgot to say no*. Every deny needs an explicit `http.Error`. Fixed.
3. `CutPrefix(header, "Bearer")` without the trailing space → token arrives as `" eyJ..."` and verification fails for VALID tokens. The prefix is `"Bearer "`, word + one space. Fixed.
4. Token was cut and thrown away (`_`) — captured; fed to `t.VerifyToken(token)`; `err != nil` → 401.
5. Three 401 branches must be identical twins (same header, same neutral `"Unauthorized"` body — no hints, login's policy again) → extracted unexported helper `unauthorized(w)`. **Leftover open:** the `WWW-Authenticate` line stayed OUTSIDE the helper (only branch 1 sends it) — move it inside.
6. `fmt.Println(id)` placeholder keeps the compiler quiet until the context step — flagged (debug-print lesson, second appearance); dies when `context.WithValue` lands.
Design note: any `VerifyToken` error → 401 (no ladder); strict version would `errors.Is(ErrInvalidToken)` → 401 / else 500 — deferred, only our server mints tokens.

**SESSION CLOSED 2026-07-16.** Final state: `WWW-Authenticate` moved INTO `unauthorized(w)` (first line, before `http.Error` — headers set after a status write are ignored); build OK; all committed on `feat/middleware` (`ebc61f5` "update auth function"). `middleware.Auth` is complete except the `fmt.Println(id)` placeholder.

~~**START HERE TOMORROW — remaining Phase 2, in order:**~~ (all five done 2026-07-17, next entry)
1. ~~**`context` lesson**~~ 2. ~~**Wire in `main.go`**~~ 3. ~~**Verify live**~~ 4. ~~**Protected-routes curl suite**~~ 5. ~~**Phase 2 close-out: merge + release**~~

## Phase 2 — session 2026-07-17: context + wiring + suites → 🏁 PHASE 2 CLOSED, `v0.2.0` RELEASED

**Context lesson (the last new concept) — landed well:** built from the physical fact that `next.ServeHTTP(w, r)` passes exactly two things, so the id must travel INSIDE `r`. Concept ladder: context = per-request bag, already there (`r.Context()`), two jobs (stop signal — what the repo's future `ctx` params listen for — and request-scoped values); airport-tray metaphor. Theory question from HIM ("what is context from a design-systems perspective?") → the request-scope answer: globals are the wrong scope (shared across concurrent requests), parameters don't survive change (every new fact = every signature edited), so servers need a third scope — request scope — and context is that scope as a value. His summaries, corrected: "they are stateless" → statelessness is the neighbor idea (server keeps nothing BETWEEN requests; context dies WITH its request — which is exactly why it's allowed); "context isolates each request's process" → goroutine isolates the WORK, context isolates the DATA. Key question ("why is a string key risky?") — his guess "exposes information" → no, client never sees it; the real answer is COLLISION (keys compare by value AND type; two packages picking `"userID"` = same key; private `type ctxKey int` makes collision unwritable; the const's value is irrelevant, `0`). Flagged for later: private key ⇒ outside packages can't read either ⇒ exported `UserIDFromContext(ctx)` helper needed for `/me`/ownership (backlog).

**Code — HIS, clean first try:** `type ctxKey int` + `const userIDKey ctxKey = 0`, then `c := context.WithValue(r.Context(), userIDKey, id)` / `r = r.WithContext(c)` — Println gone, `fmt` import gone, build + vet clean. Bag is extended-never-edited (WithValue returns a NEW wrapping context; WithContext a NEW request) — he asked "do I create the context inside Auth?" → not create, EXTEND.

**Wiring — his instinct + the trap:** his plan "middleware before handler functions" → right shape; his second answer "separate the routes in two groups: protected and public" → exactly right. The trap walked through first: wrapping the WHOLE mux locks `POST /users` and `/auth/login` — a token needed to get a token; the door locked with the key inside. Final shape (he built it after a step-by-step request — direct-mode, per the 07-13 rule): `RegisterRoutes(mux *http.ServeMux, t middleware.TokenVerifier)`; four protected routes switch `HandleFunc` → `mux.Handle(pattern, middleware.Auth(http.HandlerFunc(method), t))` (`Handle` because Auth returns a handler; `HandlerFunc` = the costume, second appearance); `POST /users` untouched; `main.go` passes `authSvc` — satisfies `TokenVerifier` implicitly, fourth consumer-owned-interface payoff.

**Live verification:** no token → 401 + `WWW-Authenticate: Bearer`; garbage token → 401; register 201 (public proven) → login → token → `GET /users` 200. All as designed.

**Suites — the guard broke the old ones, and that WAS the lesson:** `test_login.sh` cleanup DELETE + all of `test.sh` / `test_get_users.sh` / `test_delete.sh` called guarded routes tokenless → red. Named live: the API's contract changed, every existing client broke = **what "breaking change" means** — and why v1.0.0 is a promise made AFTER the contract stops moving. Claude wrote `test_middleware.sh` (asked first, OK'd): 12 checks — both public routes, the 401 family (missing header / `Basic` scheme / bare token / garbage / tampered = real token with last 4 sig chars chopped), `WWW-Authenticate` presence, 200s with the real token, DELETE-without-token 401, token-authorized cleanup 204. NOT covered: expired token (15m lifetime; frozen-token or config-override needed — backlog). Claude then patched the four broken suites (asked, OK'd): login-in-setup as the disposable user + `Bearer` header on guarded calls; `test_get_users.sh` also gained its own disposable user + cleanup (it had none). **All five suites green: 38 checks.**

**Close-out — merge + release (git ops run by Claude at his request, NO attribution in messages — his explicit rule, logged):** commit `15a066d` on `feat/middleware`; merge chain shorter than planned — `feat/middleware` grew on `feat/auth`'s tip so it CONTAINED the auth history: single fast-forward `d6dbf05..15a066d` to master, no chain. **Version call HIS: `v0.2.0`** — "password part is not done" = contract still moving = no 1.0.0 stability promise; the broken-suites experience made the argument physical. Annotated tag (`-a`: a release is a statement — author/date/message) "login system and middleware"; tag style corrected (`v0.2.0`, not `v.0.2.0`); pushed master + tag. Housekeeping: all `.claude/*.md` docs moved by him into `.claude/docs/`; all four updated after the release.

**PHASE 2 CLOSED 2026-07-17.** Login + JWT + middleware + context + protected routes: designed, built by him, tested (38 checks), released.

**START HERE NEXT SESSION — options, no order locked:**
1. **The authorization question — still on the table, good opener:** user 12, valid token, `GET /users/10` → today 200. Authn ≠ authz; the token says WHO, nothing says MAY. (Design conversation, then possibly ownership checks via `UserIDFromContext`.)
2. **Password-change endpoint** — design questions logged (resource vs action, PUT/POST, statuses).
3. **`GET /health`** — still doesn't exist (the notes-can-lie discovery); shallow liveness, own handler.
4. Smaller: `UserIDFromContext` helper; expired-token test case; dev tooling (`air`, make targets).
5. On the horizon: the solo checkpoint project (note: he has still written zero curl suites solo — flagged gap).

---

## Questions to Answer in Next Phase

1. ~~How do you connect to MySQL from Go?~~ — answered: `database/sql` + `go-sql-driver/mysql`, `mysql.NewConfig()` + `mysql.NewConnector()` + `sql.OpenDB()` + `db.Ping()`
2. What does error handling look like in the service and repository layers?
3. How do you structure the database access to make it testable without a real database?
4. What HTTP status codes are appropriate for each operation?
5. How do JWT tokens actually work, and how do you validate them in middleware?
6. ~~When should repository functions accept a `User` struct vs. flat parameters?~~ — answered: flat params or dedicated params struct (`UpdateUserParams`); full struct only when most fields are needed
7. How does `context.Context` work — how do you store and retrieve values, and when should you use it?
8. How do you write unit tests for the service layer without hitting the database?
