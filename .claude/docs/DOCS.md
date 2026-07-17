# go-learn-api — Study Notes

> Compiled 2026-07-13 from `SESSION_001.md`, the raw working log (2026-06-18 → 2026-07-13).
> This version uses plain vocabulary and explains every idea fully. Read it top to bottom to re-learn the project, or jump to a section to review one topic.
> `SESSION_001.md` stays the source of truth for work in progress.

---

## 1. Timeline — what happened, when

| Date | What happened |
|---|---|
| 2026-06-18 | Project started. Designed the REST endpoints, created the folder structure, decided how configuration works. |
| 2026-06-29 | Sketched the six methods the `UserService` needs. |
| 2026-07-01 | Connected to MySQL. Finished the repository layer. Learned the error-handling rules. |
| 2026-07-03 | `POST /users` finished. Switched to the validator library. Designed how PATCH should behave. |
| 2026-07-04 | Understood why one entity needs several structs. A "bug" in the validator turned out to be correct behavior misread. |
| 2026-07-05 | Rolled back the half-built PATCH code and rebuilt it cleanly. Learned the swallowed-error lesson and discovered how MySQL counts updated rows. |
| 2026-07-06 | Big day: PATCH finished (`57159f2`), DELETE finished (`adcf17d`), GET list + health finished (`e48d0ab`), all test suites green 15/15 (`d6dbf05`). **Phase 1 closed.** Released `v0.1.0`. Started Phase 2 (auth) and answered its first design question. |
| 2026-07-13 | Answered the remaining auth design questions. Decided login gets its own `AuthService`. Locked the build plan. Currently writing step 1. |
| 2026-07-14 | Big auth day: `ReadByEmail` done (no ctx — deferred). `AuthSvc` + one-method `AuthRepository` interface + `Login` DONE (review fixes applied). Config done: `JWT_SECRET` + `TOKEN_LIFETIME` in `.env`, secret quality check (≥32 chars), `ParseDuration` once, `main.go` wires `NewAuthService`. **Found `.env` tracked by git since the early commits — untracked + gitignored.** Learned: what `context.Context` is; the JWT *is* the token; the error's-true-story rule; timing-leak mechanics; secret brute-forcing offline; secrets never in git OR logs (`fmt` prints unexported fields!); implicit interfaces live (one repo, two ID cards). Created `.claude/BACKLOG.md`. Next: `handler/auth.go`. |
| 2026-07-15 | **Login endpoint DONE and verified live.** One-method `AuthService` interface (handler depends on the interface, not the struct). `HandlePostLogin` through three review rounds: 409→401+`WWW-Authenticate: Bearer`, 201→200, bare token string→`AuthResponse{token}` object; route was written but never registered (constructor result thrown away in `main.go`) — fixed. Curl-verified: 200+token / identical 401s for wrong password and unknown email / 400 missing field; decoded the token payload: `{"sub","exp"}` only. **Incident: `.env` deleted from disk — recovered** from git (pre-JWT keys) + regenerated secret (rotation free with 15-min tokens). **Discovered `GET /health` never existed** — only a comment; docs said "done" since 2026-07-06. Discovered `master` == `feat/auth` (wip on master — decision pending). Next: `test_login.sh`, timing TODO, `.env.example`, then middleware. |

---

## 2. What the API looks like

### The endpoints

```
POST   /users           — create an account (public)
POST   /auth/login      — log in (public)
GET    /users/{id}      — read one user (will require login)
PATCH  /users/{id}      — change name/email (will require login)
DELETE /users/{id}      — delete the account (will require login)
GET    /users           — list all users (will require admin)
GET    /health          — "is the server alive?" (public — ⚠ discovered 2026-07-15: never implemented, answers 404; on the backlog)
```

A user has: id, name, email, password. The password never appears in any response, ever.

### What each endpoint promises on success

These were decided one by one, each with a reason — that's what "status contract" means: the endpoint promises a specific status code and body shape, and the tests check the promise is kept.

| Endpoint | Promise | Why |
|---|---|---|
| `POST /users` | 201 + a `Location` header + body | 201 means "created". The Location header tells the client where the new thing lives. |
| `GET /users/{id}` | 200 + the user as JSON | Plain read. |
| `PATCH /users/{id}` | 200 + the user *after* the change | The client learns the final state without a second request. |
| `DELETE /users/{id}` | 204, no body at all | 204 means "done, nothing to say". The resource is gone — there is nothing to show. |
| `GET /users` | 200 + a JSON list that is `[]` when empty, never `null` | An empty list is a normal answer, not a missing one. |
| `GET /health` | 200 | Only says "the process is running". Says nothing about the database — that was a deliberate choice. ⚠ Designed, never built (found 2026-07-15). |
| `POST /auth/login` | 200 + `{"token":"..."}` | 200, not 201 — a token has no address, nothing was "created" that can be visited. Failures: 401 + `WWW-Authenticate: Bearer`, identical for wrong password and unknown email. |

### The database table

Defined in `migrations/001_create_users.sql`, not inside Go code — database structure belongs in files you can re-run.

Points worth remembering:
- `email` has a `UNIQUE` constraint. The **database** enforces that no two users share an email — the Go code doesn't check first, it just tries and handles the failure (explained in §7).
- `password` is `CHAR(60)` because a bcrypt hash is always exactly 60 characters.
- `updated_at ... ON UPDATE CURRENT_TIMESTAMP` means MySQL updates that column by itself on every change.
- Five seed users live in `migrations/002_seed_users.sql` (added 2026-07-06). They exist for convenience while developing. Tests must never depend on them (the story behind that rule is in §10).

### The folders

```
go-learn-api/
├── cmd/main.go          — the starting point: read config, connect DB, wire everything, start server
├── internal/
│   ├── config/          — read and check configuration
│   ├── domain/          — types and errors that several layers share
│   ├── handler/         — the HTTP layer: read requests, write responses
│   ├── service/         — the business rules
│   ├── repository/      — the only code that talks to the database
│   └── middleware/      — (Phase 2) code that runs before handlers, e.g. checking a token
├── migrations/          — SQL files that build and seed the database
├── scripts/             — setup script + curl test suites
└── Makefile             — shortcuts: run, build, clean, lint
```

---

## 3. Configuration (2026-06-18, refined 2026-07-01)

**The idea:** the same program must run on a laptop and in the cloud without changing code. So all the differences (ports, database address, passwords) come in as *environment variables* — values the operating system hands to the process.

- On a laptop, the variables come from a `.env` file that a small library (`godotenv`) loads at startup.
- In production (AWS, Azure, Docker), the platform sets the variables directly. There is no `.env` file, and that's fine.
- The Go code doesn't care which happened: `os.Getenv()` reads the process environment either way.

**A chicken-and-egg problem worth remembering:** the variable `APP_ENV` says which environment we're in — but `APP_ENV` itself might live in the `.env` file. You can't load `.env` to find out whether you should load `.env`. The solution: read `APP_ENV` straight from the operating system first, before any file.
- If it's empty → we must be on a laptop → load `.env`, and stop the program if the file is missing.
- If it's set → we're in production → skip `.env` entirely and trust the platform.

**Two smaller rules that came out of this:**
- When checking the config, collect **all** the missing fields and report them together. Failing on the first one means the user fixes one, runs again, hits the next — a slow, annoying loop.
- The config code only *detects* problems and returns them. It never decides to kill the program — `main.go` does that. A function that finds a problem should report it; the caller decides what to do about it.

---

## 4. How the layers fit together

### Each layer has exactly one job

| Layer | Its job | What it must never do |
|---|---|---|
| handler | read the HTTP request, call the service, write the HTTP response | talk to the database, contain business rules |
| service | the business rules (hashing passwords, merging updates, deciding what's allowed) | know anything about HTTP or SQL |
| repository | run SQL queries, return the results honestly | make decisions about what the data means |
| middleware | (Phase 2) run before the handler — e.g. check the login token | — |
| domain | hold the types and errors that several layers need | become a dumping ground for everything |
| main.go | build all the real objects and connect them | — |

### Why interfaces sit between the layers (2026-07-01)

Picture the naive version first: the handler holds a concrete `*UserSvc`, which holds a concrete `*UserRepo`, which holds the database. Everything is glued to everything. Change something at the bottom and the change ripples all the way up. And you can't test the handler without a real database, because the real repo is welded in.

Now the version we built. Each layer writes down, as a Go interface, only what it *needs* from below:

```
handler  says: "I need something with CreateUser, ReadUser, ..."   → the UserService interface
service  says: "I need something with Create, Read, All, ..."      → the UserRepository interface
repo     says: "I need a *sql.DB"
main.go  is the only file that knows the real, concrete types
```

An interface here is a **contract**: a list of method signatures, nothing more. Go has a nice trick: a type satisfies an interface automatically if it has the right methods. You never write "UserRepo implements UserRepository" — Go just checks the signatures. That means the repo package and the service package don't need to know about each other at all.

`main.go` is where everything meets. It builds the real database connection, hands it to the real repo, hands the repo to the real service, hands the service to the real handler:

```
create *sql.DB
  → NewUserRepo(db)         gives a *UserRepo
    → NewUserSvc(repo)      gives a *UserSvc
      → NewUserHandler(svc) gives a *UserHandler
        → register routes, start the server
```

This whole pattern has a name: **dependency inversion**. The layers at the top don't depend on the concrete layers at the bottom — both sides depend on the small contracts in between. The payoff is testability: in a test you can hand the handler a fake service, no database needed. (Later, 2026-07-05, we learned the architecture-world name for this style: ports and adapters, also called hexagonal or clean architecture. Same idea: dependencies point inward, and the import graph is not the same as the call graph.)

### The domain package — why it exists (2026-07-01)

A concrete problem forced it: the type `UpdateUserParams` started life inside the repository. Then the service needed it too. Two bad options: the service imports the repository (wrong — the service shouldn't know the repo's internals), or the repository imports the service (worse — the repo is the lowest layer, it imports nobody). When neither side should import the other, the shared type moves to a **third, neutral package**: `internal/domain`. Both layers already import domain, so no new arrows appear.

One temptation was rejected on the way: putting `json:"-"` on the repo's user struct to hide the password from JSON. Why rejected: JSON is an HTTP concern, and the repository has nothing to do with HTTP. A struct should serve one purpose; that tag would have welded two layers together (this idea grows into §5).

### Small Go habits collected along the way

- Names starting with a capital letter are visible outside the package; lowercase names are private to it. If code outside doesn't need to touch a field, make it lowercase.
- Every struct that gets wired in `main.go` has a constructor: `NewUserRepo(db)`, `NewUserSvc(repo)`, and so on. The constructor is *where* the dependency gets injected, and it returns a pointer because the struct is meant to be shared, not copied.
- The `internal/` folder is special in Go: packages inside it cannot be imported by other projects. It enforces "this is my app's private code".
- An import written as `_ "some/package"` runs the package's setup code without using it directly — that's how database drivers register themselves. Never use it for a package you actually call.
- Don't write `else` after a `return` — return early and let the code fall through.
- A comment must say something the code cannot: the *why*. Never repeat what the line already says.

---

## 5. One struct per contract — why "User" has so many shapes (2026-07-04)

This confused things until the rule was found, so here it is slowly.

The instinct is: there's a users table, so there should be one `User` struct. But look at the different situations a "user shape" appears in:

- what the database row contains (including the password hash),
- what the API is allowed to send back (definitely NOT the password hash),
- what a client must send to create a user (untrusted input that needs validation),
- what a client may send to update a user (fields are optional here!),
- what the repository should write to the database (definite values, nothing optional).

Each of those is a different **contract** — a different agreement about which fields exist, which are optional, whether the data is trusted, and how it looks on the wire. The rule that fell out:

> **One struct per contract — not one per table, and not one per method.** A new struct is justified only when the fields, the optionality, the trust level, or the wire format actually differ. When two operations share a contract, they share the struct.

The current cast of characters:

| Struct | The contract it represents |
|---|---|
| `domain.User` | the full database row, password hash included — never leaves the service layer |
| `domain.UserResponse` | what the API returns — no password; its JSON tags are the public wire format |
| `handler.CreateUserRequest` | what a client must send to register — untrusted, carries validation tags |
| `handler.UpdateUserRequest` | what a client *may* send to update — optional fields, so pointers + tags |
| `domain.UpdateUserInput` | the handler-to-service hand-off — pointers, but no tags (explained in §9) |
| `domain.UpdateUserParams` | the service-to-repository write order — plain values, matches the SQL exactly |

The strongest argument for the rule: **every bug in this area came from one struct trying to serve two contracts that didn't match.**
- The `json:"-"` idea — a database struct trying to also be a wire format.
- Echoing `CreateUserRequest` back as the response — the *request* struct contains the plaintext password, so the API leaked it.
- Trying to reuse `CreateUserRequest` for PATCH — its `required` tags physically cannot say "this field is optional".

Yes, this means more structs and some copying code between them. That's not waste — it's the price of making mismatches *impossible to compile*. Nothing leaks by accident, validation tags exist only on input types, and the database row can change shape without breaking what clients see.

(The industry name for these request/response structs is **DTOs** — data transfer objects. Also, a naming warning from 2026-07-04: these in-between types are "boundary types", not "middleware" — in this project *middleware* means only the HTTP kind, like the future auth check.)

---

## 6. Error handling — the complete picture

### Each layer speaks its own language (2026-07-01)

Follow one error on its journey. The database says "no rows found" by returning `sql.ErrNoRows`. The handler will eventually need to answer 404. But the handler must not check for `sql.ErrNoRows` itself — that error belongs to the `database/sql` package, and only the repository knows the database is even involved. So the error gets *translated* at each boundary:

```
repository:  sees sql.ErrNoRows  → returns domain.ErrUserNotFound   (database language → domain language)
service:     passes domain.ErrUserNotFound through, untouched       (it's already in the right language)
handler:     sees domain.ErrUserNotFound → writes 404               (domain language → HTTP language)
```

Why translate at the repo and nowhere else: if the service checked `sql.ErrNoRows` directly, then swapping MySQL for something else would force changes in the service — a layer that supposedly knows nothing about databases. The translation happens at the *one* layer that knows the source.

Two supporting rules:
- The shared error values live in `internal/domain`, so the repo (which produces them) and the handler (which checks them) can both see them without importing each other.
- When wrapping an error with context — `fmt.Errorf("Read: %w", err)` — do it at the layer where the work happened. The repo wraps because the SQL ran there. Layers above pass along errors they didn't cause, without re-wrapping.

### Sentinel errors — what they are and when you need one

A **sentinel error** is a named, package-level error value, like:

`var ErrUserNotFound = errors.New("user not found")`

It must be a package-level variable. If you call `errors.New` *inside* a function, you make a brand-new value every call, and no caller can ever compare against it.

**When does an outcome deserve a sentinel? Ask: does the caller at the top need to *act differently* for this specific outcome?**
- `Read` on a missing user → the handler must choose 404 vs 500 → it needs to tell this outcome apart → sentinel.
- `All` on an empty table → the handler returns 200 with `[]` either way → no branch → no sentinel.

That second example carries a distinction worth pausing on: "not found" and "empty list" feel similar but are different kinds of thing. Not-found means *a specific thing you asked for doesn't exist* — that's an event with meaning to the business. An empty list is just *a valid state of the world*. The general rule that fell out: **translate errors that carry business meaning; pass plain failures through untouched.** A handler must never answer 404 because the database *crashed* — only because the thing genuinely isn't there.

One rename to remember (2026-07-06): the duplicate-email sentinel was renamed from `ErrUserAlreadyExists` to **`ErrMailAlreadyExists`**. Reason: name the error after the *rule that was broken* (emails must be unique), and it reads correctly from both POST and PATCH.

### `errors.Is` vs `errors.As` — two tools, two jobs

Since Go 1.13, errors can be wrapped inside other errors (`%w`). A plain `==` comparison can't see through the wrapping, so:

- **`errors.Is(err, ErrUserNotFound)`** — "is this error, or anything wrapped inside it, *this particular value*?" Use it for sentinels. Always use it instead of `==`.
- **`errors.As(err, &target)`** — "is this error, or anything inside it, *this particular type*? If so, hand it to me so I can look at its fields." Use it when you need details — for example MySQL failures arrive as a `*mysql.MySQLError` struct, and you need to read its `.Number` field (1062 means "duplicate entry").

Rule of thumb: known value → `Is`. Typed struct you must inspect → `As`.

### The handler's error ladder (2026-07-05)

The handler is the **only** layer allowed to turn errors into status codes. After calling the service, it climbs a ladder of checks: is it `ErrUserNotFound`? → 404. Is it `ErrMailAlreadyExists`? → 409. Anything else → 500, and the body says only "Server Error" — internal details never reach the client, because error text can leak table names, file paths, driver versions.

**Every rung of the ladder ends with `return`.** That's not style — here is the bug that taught it (2026-07-05): the first version of the PATCH handler did `if err != nil { fmt.Println(err) }` — print, no return, no response written. The function fell off the end. In Go, a handler that finishes without writing anything sends an automatic 200. So every failed PATCH told the client "success". The lesson, worth memorizing word for word: **an error you print but don't act on is still a swallowed error.**

---

## 7. The database layer

### `database/sql` basics (2026-07-01)

- `sql.Open()` doesn't actually connect — it only checks that the driver name and address *look* valid. The real "can I reach the database?" test is `db.Ping()`. Always ping at startup.
- The idiomatic connection path used here: `mysql.NewConfig()` (a config struct) → `mysql.NewConnector()` → `sql.OpenDB()` → `db.Ping()`.
- A `*sql.DB` is not one connection — it's a **pool** of connections that the driver manages for you. That's why it's safe to share one `*sql.DB` across the whole program.
- Three methods cover everything:
  - `db.Exec(...)` for INSERT, UPDATE, DELETE — returns a result you can ask "how many rows did that touch?"
  - `db.QueryRow(...)` for a SELECT expecting one row — call `.Scan()` to copy the columns into variables.
  - `db.Query(...)` for a SELECT expecting many rows — loop with `rows.Next()`, scan each, `defer rows.Close()` right after checking the error, and check `rows.Err()` when the loop ends.
- **Always use `?` placeholders for values. Never build SQL by gluing strings together.** Glued strings are SQL injection: the classic attack where user input becomes part of the query itself.

### The repo tells the truth; the service decides what to show (2026-07-01)

`repo.Read(id)` returns the complete row, password hash included — *always*, no matter who's asking. It's the **service** that converts a `domain.User` into a `domain.UserResponse` (dropping the password) before anything reaches the handler. Why not strip it in the repo? Because another caller needs it: `ChangePassword` must read the stored hash to verify the old password — and soon, login will too. The repo serves complete data; each service method shapes it for its purpose.

### Try it, then handle the failure — don't check first (2026-07-01)

The instinct when creating a user: first query "does this email exist?", then insert. That's wrong, and not just slower — it's a race: between your check and your insert, another request can take the email. The database's `UNIQUE` constraint already enforces the rule *atomically* (in one indivisible step). So: just INSERT, and if the database answers with error 1062 (duplicate key), translate that to `ErrMailAlreadyExists`. Same idea for existence: just `Read` and translate the no-rows error. **Let the database enforce what the database can enforce.**

### Counting affected rows — a real war story (2026-07-05 → 2026-07-06)

Background: an UPDATE or DELETE that matches zero rows is **not an error** in SQL — it "succeeds" quietly. To detect "that user doesn't exist", the repo asks the result for `RowsAffected()` and treats 0 as `ErrUserNotFound`.

The discovery (2026-07-05, found by running a test twice): the PATCH test was run a second time with the *same values*. MySQL matched the row but changed nothing — and the MySQL driver, by default, reports rows **changed**, not rows **matched**. So `RowsAffected()` returned 0, the repo said "user not found", and the API answered 404 *for a user that provably existed*. A no-op update looked like a missing row.

Two honest fixes were on the table:
1. Tell the driver to count *matched* rows instead.
2. Delete the zero-check from `Update` and rely on the existence check the service already does (it reads the user before updating).

Option 2 hides a race: if the row is deleted *between* the service's read and the repo's update, the update silently touches nothing and the client still hears "success". So: **option 1** — one line in `main.go`: `cfg.ClientFoundRows = true`. After it:
- A no-op PATCH → matched 1 → success. Correct: PATCH describes the state you want, it doesn't demand that something change.
- A row deleted in that race window → matched 0 → 404. The repo's check now tells the truth by itself.

One caveat to keep in mind: this flag changes the meaning of `RowsAffected` for **every query in the program**, not just this one. The other uses were audited (DELETE is unaffected — for deletes, matched and removed are the same thing; `UpdatePassword` ignores the count). But if a future query ever needs to know "did the value actually *change*?", remember this flag is on.

### AUTO_INCREMENT has gaps — and that's fine (2026-07-03)

Noticed during testing: after a failed insert (duplicate email → 409), the next successful user skipped an id number. That's by design: InnoDB reserves the id *before* trying the insert, under a very short internal lock. Giving the number back on failure would mean holding that lock until the transaction finishes — which would force all concurrent inserts to wait in line. The database trades pretty numbering for speed. Deletes and rollbacks leave gaps too.

The real lesson: **an id is an identifier, not a counter.** Its only job is to be unique and never change. Nothing in the API may assume ids are consecutive; never compute `MAX(id)+1`. And a note for the security phase later: sequential ids let outsiders guess how many users you have and iterate through them (this shows up in the OWASP API list as BOLA/IDOR) — serious systems often show opaque public ids or UUIDs instead.

---

## 8. The service layer

### The six methods (sketched 2026-06-29)

| Method | Takes | Returns | Notes |
|---|---|---|---|
| `CreateUser` | name, email, password | (id, error) | the id feeds the Location header |
| `ReadUser` | id | (UserResponse, error) | |
| `FetchAllUsers` | — | ([]UserResponse, error) | |
| `UpdateUser` | id, UpdateUserInput | (UserResponse, error) | the merge lives here — see §9 |
| `DeleteUser` | id | error | |
| `ChangePassword` | id, old, new | error | written, but has **no route yet** — deferred |

### Passwords (2026-07-01)

- Passwords are hashed with **bcrypt** in the service, before the repo ever sees them. The repository never touches a plaintext password.
- In `bcrypt.GenerateFromPassword(pw, cost)` the second argument is a **cost factor** — how much *work* the hash takes, not how long the output is. The output is always 60 characters. Higher cost = slower to compute = slower for an attacker to brute-force. Use the named constant `bcrypt.DefaultCost`, never a bare `10` — the name documents itself and tracks the library's recommendation.
- You can never check a password with string comparison — the stored value is a hash, not the password. Use `bcrypt.CompareHashAndPassword(hash, plaintext)`.
- When that comparison fails, check *which way* it failed: `bcrypt.ErrMismatchedHashAndPassword` means "wrong password" — any *other* bcrypt error (like a corrupted hash in the DB) is an infrastructure problem and must not be reported as "wrong password". `errors.Is` makes the distinction.
- The `ChangePassword` flow, in order: read the user (getting the stored hash) → bcrypt-compare the offered old password → if old and new are equal, refuse (`ErrSamePassword`) — and *that* comparison may be plain `==`, because by then both values are plaintexts and bcrypt already proved the old one is real → hash the new password → persist via `UpdatePassword(id, newHash)`.
- Hashing was extracted into a small shared helper (used by create and change-password). It returns an error too — bcrypt refuses input longer than 72 bytes.

### The service never reads from context (2026-07-01)

Later, auth middleware will store "who is logged in" inside the request's `context.Context`. The tempting shortcut is letting the service reach into the context to find the user id. Decided against, firmly: the **handler** pulls the id out of the context and passes it to the service as a **plain, visible parameter**. Two reasons: a function's needs should be readable in its signature, not hidden in a bag it secretly opens; and a service that reads context is annoying to test — every test would have to build a context with magic keys in it.

### Pointer or value for small structs? (2026-07-01)

First instinct was "structs are heavy, pass pointers". Correction: pass a pointer only when (a) the struct is genuinely big, (b) the function must modify the caller's copy, or (c) `nil` carries meaning ("not provided"). `UpdateUserParams` is two small strings, nothing mutates it, nil means nothing → pass it by value.

---

## 9. How each endpoint got built — and what each one taught

### `GET /users/{id}` — the first endpoint

The pipeline, in order:
1. `r.PathValue("id")` pulls the `{id}` piece out of the URL (standard library, Go 1.22+).
2. `strconv.Atoi` turns it into a number. If that fails, the answer is **400** — the client sent garbage; it's their error, not our 500.
3. Call the service.
4. Climb the error ladder: `ErrUserNotFound` → 404, anything else → 500.
5. Set `Content-Type: application/json` **before** writing the body.
6. Encode the user to JSON. If encoding fails *at this point*, the headers are already sent — you can't change the status anymore. Log it and move on; calling `http.Error` now would just corrupt the output.

### `POST /users` — finished 2026-07-03

Flow: decode the JSON body into `CreateUserRequest` → validate it → call the service → duplicate email? 409; other failure? 500 → on success build a `UserResponse`, set `Content-Type` and `Location: /users/{id}`, send **201**, encode the body.

What it taught — each of these was hit for real:

- **Order matters when writing a response: headers first, then status, then body.** The moment `WriteHeader` sends the status line, any header you set afterwards is *silently thrown away* — that's exactly how the Location header vanished for a while. And if you never call `WriteHeader` at all, the first body write quietly sends 200 — but a created resource must announce 201 explicitly.
- `r.Response` is nil on the server side — that field exists for HTTP *clients* following redirects. A server doesn't "read" a Location from anywhere; it *builds* the URL itself from what it knows: `"/users/" + id`.
- `string(65)` is `"A"`, not `"65"` — converting an int with `string()` gives you the character at that code point. The right tool is `strconv.Itoa`. (`go vet` catches this one.)
- **Never send the request struct back as the response.** It happened: echoing `CreateUserRequest` returned the plaintext password to the client. The response must describe what the server *created* — that's what `UserResponse` is for.
- **JSON tags are a public promise.** The response briefly had `"ID"` capitalized next to `"name"` lowercase. Once any client depends on a field name, renaming it breaks them — that's versioning territory. Fix casing before anyone integrates.
- `Header().Set` replaces a header; `Add` appends another copy. Single-value headers like Location want `Set`.

### Validation (2026-07-02, simplified 2026-07-03)

Hand-rolling every rule (email regexes, length checks…) was eating time that should go to learning the pipeline, so: the `validator/v10` library, with rules written as struct tags on the request types. A deliberate, dated decision: **keep validation simple for now.** No password character-class rules, and the validator's raw message goes straight to the client. Making the error responses client-friendly is deferred to the error-contract phase (Phase 3).

Rules today: name 5–100 chars; email valid and ≤255; password 8–72. Why 72: **bcrypt silently ignores everything past 72 bytes** — a 100-character password would quietly lose its tail. The DB column being CHAR(60) is unrelated — that's the *hash's* size, not the input's.

### `PATCH /users/{id}` — the long one (2026-07-03 → 2026-07-06, commit `57159f2`)

**The contract decided first (2026-07-03):** a true partial update. The client sends only the fields it wants changed. A field that isn't sent keeps its old value. A field sent as an *empty string* is rejected with 400 — "please blank out my name" isn't a thing this API allows.

**The problem that shaped everything — absent versus empty.** Decode JSON into a plain Go `string` and you destroy information: if the key is missing, the string is `""`; if the client sent `""`, the string is also `""`. Two different client intentions collapse into one value. The contract needs *three* states (not sent / sent with a value / sent empty) and a `string` can only hold two.

**The solution: pointer fields.** In the request struct, `Name` and `Email` are `*string` — pointer to string. Now:
- key absent → the pointer stays `nil` → keep the old value
- key sent with a value → pointer to that value → validate it, use it
- key sent as `""` → pointer to an empty string → reject, 400

`encoding/json` gives this behavior for free — the decoder simply never touches a field whose key is absent, so it stays nil. The cost: every use needs a nil-check *before* dereferencing, because dereferencing a nil pointer panics.

(The dead end that preceded this is worth remembering: "plain strings, and `""` means bad request." But absent *also* decodes to `""` — so leaving out a field would 400, forcing clients to always send everything, which is PUT behavior wearing a PATCH name. Precisely defining the three states is what forced the pointer design.)

**Where does the merge live?** Someone has to combine "the current user" with "the fields the client sent". Options considered: build the SQL SET clause dynamically (one query, but the logic hides in string-building); do the overlay inside SQL with `COALESCE` (one query, logic hidden in the database); require all fields (that's just PUT); or **read-modify-write in the service** — fetch the current user, lay the provided fields over it, write the result. Chosen: the last one. Two queries instead of one, but the merge is ordinary Go you can read, test, and debug.

**The data flow — follow the pointers until they die (2026-07-04):**

```
handler.UpdateUserRequest    pointers + json/validate tags     (the wire contract, untrusted)
      ↓ the handler copies it over
domain.UpdateUserInput       pointers, NO tags                 ("what the client may have sent")
      ↓ the service merges: read current user, overlay the non-nil fields
domain.UpdateUserParams      plain strings                     ("write exactly these values")
      ↓ the repo runs
UPDATE users SET name=?, email=?                               (definite values only)
```

The pointers **stop at the service**. The merge is the exact moment every "maybe" becomes a definite value — so the repository never sees a pointer, only concrete instructions.

Why is there both `UpdateUserRequest` and `UpdateUserInput` when the fields look identical? Because the service must receive the pointer struct, but the pointer struct lived in `handler` — and `service` importing `handler` is an arrow pointing the wrong way. Same disease as `UpdateUserParams` stuck in the repo (§4), same cure: move the shared type to `domain`. But it's a **split, not a move** — the JSON and validation tags are HTTP concerns that must stay in the handler's copy; domain gets a bare, tagless twin. (The compiler itself enforced part of this on 2026-07-05: Go only lets you declare methods on a type inside the type's own package, so the handler's `validateRequest` method physically couldn't follow the type into domain — and dragging the validator library into domain would poison the core with a transport concern.)

Three details that complete the picture:
- **The existence check is free.** The merge's first step is reading the current user — and that read already returns `ErrUserNotFound` for a missing row, which becomes the 404. "Check if the user exists" never exists as a separate step; it falls out of a read the merge needed anyway.
- **Cheap checks before expensive ones:** parse the id (400) → decode the JSON (400) → validate (400) → only *then* touch the database.
- **`omitempty` on pointer fields:** in the tag chain `omitempty,min=5,...`, "empty" for a pointer means *nil*. Nil → the remaining rules are skipped, so an absent field passes. A pointer to `""` is not nil → present → `min=5` runs against an empty string → 400. Exactly the three-state contract, enforced by tags. And note the division of labor: validation decides whether the *request* is acceptable; the merge decides what the *write* looks like. Two different questions, answered in two different places.

**The rollback (2026-07-05):** the half-wired handler code from the previous day was rolled back to a clean skeleton and rebuilt the same day, step by step, through a 12-item checklist — every item phrased as a question to answer *before* writing the code. All 12 closed by 2026-07-06. (A bug from the first attempt — the repo interface accidentally taking the pointer type — vanished with the rollback and was deliberately not reintroduced.)

**Success response (2026-07-06):** 200 with the user *after* the merge. Cost-free, because the service just computed the merged values — no extra query. The status is written with an explicit `WriteHeader(200)` even though 200 is the default — this handler had already produced two *accidental* status codes, so from now on it states its intentions.

**Three lessons from the final day (2026-07-06):**
- **Copy-paste is a contract smell.** The first success-block was POST's, pasted: it sent 201 and a Location header — on a PATCH, where nothing was created and the client already knows the address. Each endpoint's response must be *reasoned*, not inherited. The test suite caught it, which is exactly what it was built for.
- **Recompile before you re-test.** A fix "didn't work" — because the old binary was still running. A test against a stale process is testing the past. (This happened three times; a file-watching auto-restart tool, `air`, is queued in the backlog.)
- **Never return data and an error together.** A function ended `return response, err` where both could be non-zero. Go's contract: if the error is non-nil, every other return value is a zero value the caller must ignore. Failures return `(UserResponse{}, err)` early; the success value is built only on the success path.

**And one debugging lesson (2026-07-04) — make the system testify.** A test was failing and the theory was "the validator is checking the pointer's *address* instead of its value." Before touching any code, the error was unwrapped properly — `errors.As` into `validator.ValidationErrors`, printing each error's Field, Tag, and Value. The output: `Field: Email, Tag: email, Value: not-an-email`. A real dereferenced value, from the test that *deliberately* sends a broken email and *expects* 400. Everything was working; the flat `err.Error()` string had been misread. Three takeaways: dig into structured errors instead of squinting at one long string; before theorizing about a failure, confirm it isn't correct behavior; and the unwrap loop written for the debugging is the seed of Phase 3's client-friendly error responses. (A real sub-bug did surface: the first unwrap returned nil for errors of any *other* type — a swallowed error. Fixed to pass everything through.)

### `DELETE /users/{id}` — finished 2026-07-06 (commit `adcf17d`)

- **Success is 204 No Content — decided by asking "what would the body even say?"** There was no answer. The thing is gone; a body like `{"message":"deleted"}` would just restate the status line. So: one bare `WriteHeader(204)` and nothing else.
- **Headers must not contradict the status.** A leftover `Content-Type: application/json` (pasted from PATCH) sat above the 204. Content-Type describes a body; 204 *promises there is no body*. Harmless on the wire, contradictory as a contract. Removed.
- **The double-delete question, answered from RFC 9110:** deleting the same user twice returns 204, then 404. Does the 404 break the rule that DELETE must be idempotent? No — and the reason is the definition. Idempotency is a promise about the **state of the server**: doing the request N times leaves things exactly as doing it once. After each call, the user is equally gone — promise kept. The *responses* may differ; the promise says nothing about them. (Contrast POST: replaying a create makes a second user. N times ≠ once.) The practical payoff: when an idempotent request times out, the client can simply retry it, safely.
- **Responses are receipts, not hopes.** The 204 can be trusted because the handler writes it only *after* the SQL succeeded. Should a client GET the user afterwards to "make sure"? No — if the 204 can't be trusted, neither can the verification GET; the protocol only works because responses are authoritative. Verification GETs belong in the *test suite*, whose whole job is proving the unproven handler keeps its promises.
- The error ladder is deliberately **shorter** than PATCH's: no 409 rung, because deleting can't create a duplicate-email conflict. Each ladder is reasoned per endpoint, never pasted.
- **405 found a bug in one step.** The first DELETE request returned 405 Method Not Allowed. Instinct said "handler bug" — but the handler had never run. Go's router (1.22+) answers 405 *by itself* when the path matches a registered route but no route has that method; it even sends an `Allow` header listing what the path does accept. So the code alone said precisely: "the path is known, DELETE isn't registered for it" — the route was missing from `RegisterRoutes`. A 404 would have meant "path unknown" instead. Lesson: **status codes are diagnostic inputs, not just outputs** — 405 pinpointed the bug with no print statements. Corollary: registering the route is step zero of every new endpoint.

### `GET /users` and `GET /health` — 2026-07-06 (commit `e48d0ab`) — ⚠ health correction 2026-07-15: designed, never built (only a comment line in `handler/user.go`; answers 404; implementation is on the backlog)

- The list endpoint's ladder has **one rung**: any error → 500. There's no 404, because an empty table is not an error — it's a perfectly valid answer.
- **`[]`, never `null`:** in Go, a slice you declared but never initialized encodes to JSON as `null`; an initialized empty slice encodes as `[]`. Clients should never need to handle "the list is null" — initialize the slice.
- The repo's `All()` selects only id, name, and email — the password column isn't even in the query, and the no-password promise was verified across the whole table.
- `/health` answers 200 with `{"status":"ok"}` if the process is serving HTTP — and *deliberately* claims nothing about the database. That's a **liveness** check ("the process is up"), chosen over a readiness check ("dependencies work too") for now — decided, not defaulted. Parked for later: a readiness variant needs something that can ping the DB, which doesn't belong on `UserHandler`; the method never uses its receiver (a smell); and in Phase 2, `/health` must stay **outside** the auth wall — load balancers can't log in.

---

## 10. Testing — the curl-suite pattern

Each endpoint has a shell script of `curl` calls, and the scripts check themselves:

- Every test states the status code it expects, prints PASS or FAIL, and shows the response body on failure. The script exits non-zero if anything failed — so `make` can chain them later and stop on the first red suite.
- The expected codes describe the **finished** behavior. Write the suite early and the failures *are* your to-do list; the suite turns green as the handler gets built.

**The fixture incident (2026-07-06) — how a testing rule was earned.** Right after DELETE was finished, the PATCH suite suddenly went 5-of-7 red with 404s. No code had changed. The cause: the PATCH tests targeted seed user 4 — which a manual curl had deleted hours earlier during smoke-testing. Not a code regression: a *fixture* regression (a fixture is the pre-arranged data a test relies on). Meanwhile the DELETE suite — which creates its own throwaway user in its setup — sailed through green. The fix had two parts:

1. The seed data became a real migration file. Until then, the five seed users existed only as SQL pasted in the session notes — "state that exists only because someone once typed it is state you can't recreate."
2. The PATCH suite was rewritten to the **disposable-user pattern**: its setup POSTs a fresh user with a unique email (process id + random suffix), and every test targets that user. Repeatable forever, no reseeding, can't collide with anything.

> **The rule: test suites create the data they need. Seed data is a convenience for humans developing — never something a test depends on.**

A nice detail: the DELETE suite's core is a lifecycle triple that *encodes the concepts it tests* — delete → 204 (the receipt), GET → 404 (the state really changed), delete again → 404 (state idempotent, response informative). And the suites are only possible because POST returns the new user's id — the API's own contract is what makes the API testable.

Phase 1 closed with three suites at **15/15 green** (PATCH 7, DELETE 5, list 3).

(Also filed under testing: a Go test file named `package config` sees the package's private names; named `package config_test` it sees only the public ones — the same view users get, which is the better default when it's enough.)

---

## 11. Phase 1 closed — and the first release (2026-07-06) 🏁

Five endpoints plus health, each with a deliberate status contract and an executable proof. Tagged **`v0.1.0`**.

What the release taught:
- A git **tag** names one commit, forever. Tags don't move; branches do. You never need a "backup branch" of a release — a branch can sprout from a tag at any time.
- Semantic versioning: anything `0.x` announces "the contracts may still change". `1.0.0` is a *stability promise* — one this API can't make before auth exists.
- A GitHub "Release" is a presentation page wrapped around a git tag; the tag is the real thing.
- Go's module system reads version tags in exactly the `vX.Y.Z` shape.

**The learning plan (agreed 2026-07-06):** no redoing Phase 1. After Phase 2 comes a **solo checkpoint**: a small CRUD API, different resource, from `git init`, no mentor, no peeking at this repo — same standards, curl suites included. Whatever gaps show up *are* the result. The final exam is the audio-processing pipeline, built mostly solo with the mentor as reviewer only.

**Parked (deliberately):** renaming `test.sh` → `test_patch.sh` (with `git mv`, keeping history); `make db-reset` and `make test-api` targets; the `air` auto-restart dev loop; and the password-change endpoint — service logic exists but no route, and its design questions are logged: is it a resource or an action (`/users/{id}/password`?), PUT or POST when the body mixes new state with proof (the old password), which status for a wrong old password (400? 401? 403?), which for new-equals-old (400 vs 422), 204 on success?

---

## 12. Phase 2 — Authentication (kickoff 2026-07-06 · contract closed 2026-07-13)

### The problem tokens solve (2026-07-06)

HTTP is stateless: request number N carries no memory of request N−1. Logging in at 9:00 means nothing at 9:01 unless *something* carries the identity along. Two families of solution:
- **Sessions:** the server remembers who's logged in (a session table), and the client carries just a session id. Trouble in the cloud: with several servers behind a load balancer, the one that receives request N might not be the one that remembers you — you need sticky routing or a shared store like Redis.
- **Tokens:** the *client* carries a self-contained, tamper-proof proof of identity. Any server can check it on its own, asking nobody. That's the JWT, and it's why token auth fits this project's cloud goals.

### What a JWT actually is (2026-07-06)

Physically: one string, three base64-encoded chunks separated by dots — `header.payload.signature`.

- The **header** is tiny JSON saying which algorithm signed the token (here HS256 — an HMAC using SHA-256).
- The **payload** holds the **claims** — statements the server asserts: "the subject is user 42", "this expires Friday". The JWT spec (RFC 7519) standardizes short names: `sub` (subject — whose token), `exp` (expiry time), `iat` (issued at). You can add your own claims too.
- The **signature** is computed from the first two chunks *plus a secret only the server knows*. Anyone can read the token; only the server can *produce* a valid signature. Change one character of the payload and the signature no longer matches.

The metaphor that made it click: **the claims are a note I write to my future self, and the client is the courier.** At login I write "this is user 42, trust it until Friday", sign it, and hand it over. Tomorrow the courier shows up with my own note; I check my signature and trust my own words.

Two consequences to never forget:
- The payload is **encoded, not encrypted** — base64 is a format, not a lock. Anyone holding the token can read every claim (paste one into jwt.io). Never put anything sensitive in it.
- The secret is the crown jewels. It lives in config/`.env` and never, ever, in git.

### The three contract questions

**Q1 — what does a failed login return? (2026-07-06)**
**401. Always. And an unknown email returns the *same* 401 as a wrong password.**

Why the sameness matters: if "no such email" and "wrong password" are distinguishable in *any* way, the login endpoint becomes a machine for testing which emails have accounts — **user enumeration** (it's in OWASP's API security list). The sameness must hold on three levels:
1. Same status — 401 for both.
2. Same body — one generic "invalid credentials" for both.
3. **Same timing** — the sneaky one. When the email exists, the server runs bcrypt, which is *deliberately* slow (~100ms). When it doesn't exist, the code returns early — fast. An attacker with a stopwatch tells the difference anyway. The standard fix: run a *dummy* bcrypt comparison on the missing-email path, so both paths take the same time. (To do in step 2 of the build.)

Getting 401 vs 403 straight, once and for all: **401 means "I don't know who you are"** — despite its misleading official name "Unauthorized", it's about *authentication*: bad login, missing token, expired token. **403 means "I know exactly who you are — and no."** Valid token, forbidden action. The middleware will lean on this distinction.

Consequence inside the code: the repo will truthfully report "no user with that email" (`ErrUserNotFound`) — but if that escaped from `Login`, the handler's ladder would map it to 404 and *leak the very fact we're hiding*. So the login service method **collapses** both failure causes — unknown email, wrong password — into one new sentinel, `ErrInvalidCredentials`, and that's all the handler ever sees. Run the sentinel test from §6 on it: does the handler branch on it? Yes — 401 vs 500. It earns its existence. Note what's new here: it's the first time in the project that *hiding* information is the design goal. The repo stays honest; the service does the hiding. (Small spec nicety: a 401 response should carry the header `WWW-Authenticate: Bearer`.)

**Q2 — what goes in the claims? (2026-07-13)**
**`sub` (the user's id) and `exp` (the expiry). Nothing else.**

Every candidate claim must pass two gates: (1) the courier can read the note — so only data safe for the user to see; (2) the note exists to serve future requests — so only what's needed on *every* request. The id passes both: it's the user's own id, and it's the key that unlocks everything else — need the email or a role flag? Look it up by id. So nothing else is *needed*.

And a third argument, the strongest: **the note is frozen the moment it's signed.** Suppose the email were a claim. An hour after login, the user PATCHes their email — this API allows that. Every request until expiry now carries the *old* email; the note to your future self is lying to you. The id can never go stale because it never changes. The rule that falls out: **only facts that cannot change while the token lives belong in the claims.**

**Q3 — how long does a token live? (2026-07-13)**
**15 minutes.**

The trade-off: a stolen token works until it expires — so a short life shrinks the damage window — but every expiry forces a re-login — so a short life adds friction. 15 minutes is the choice serious systems make, not a training-wheels shortcut. The friction has a standard cure called refresh tokens, which are **deliberately out of scope for Phase 2** — when the token dies, log in again. The lifetime and the signing secret both live in config/`.env`.

### Where does login live? `UserService` or a new `AuthService`? (2026-07-13)

Login *touches* users — it reads one by email — so the pull is to hang it on `UserService`. But look at the question each struct answers. Every `UserService` method answers "do something to the users resource" — that's CRUD. Login answers a different question: "is this person who they claim to be? Here's a token." Different task, different contract → **its own `AuthService`**. This is the same test that split health-check off `UserHandler` and the same test behind the DTO rule (§5): **one struct per contract, not per table.**

And the boundary of the split: it exists at the **service layer only**. There's still one users table, so still one `UserRepo` — `AuthService` simply becomes its second consumer. No `AuthRepo`.

### The boundary inside login: the repo fetches, the service compares (2026-07-13)

Walk the roles. I'm the repo: my whole contribution to login is answering one question — "give me the user with this email." I hand back the row **including the password hash**, or `ErrUserNotFound`. I never see the password the client typed. Now I'm the service: I'm the only one holding *both* pieces — the stored hash from the repo and the plaintext from the client — so the bcrypt comparison happens *in me*. If a repo ever "checks passwords", data access and business logic have blurred into each other.

### What `AuthService` needs handed to it (2026-07-13)

Read the work `Login` must do and the dependency list writes itself: it fetches a user by email → it needs **the user repo**. It signs a token → it needs **the JWT secret**. It computes `exp` → it needs **the token lifetime**. Three things, injected by `main.go` through the constructor — same wiring pattern as every other layer. The actual minting is a library call; `golang-jwt` is the standard package.

### The build plan (locked 2026-07-13 — steps 1–2 built 2026-07-14)

1. ✅ **Repository — read by email** (in `repository/user.go`, done 2026-07-14). `ReadByEmail(email)`: returns the user *including the password-hash column* — the one query in the app allowed to select it (responses are already safe because the DTOs strip it). `SELECT` with a `?` placeholder, scan. No rows → `ErrUserNotFound`; any other error wrapped. No `ctx` — see the context section below.
2. ✅ **Service — `AuthSvc`** (`service/auth.go`; DONE 2026-07-14, review fixes applied — only the timing `// TODO` stays open until the endpoint ships). Struct holds the repo, the secret, the lifetime. One method, `Login(email, password)`: fetch by email → bcrypt-compare → *either* failure becomes `ErrInvalidCredentials` → on success, mint the JWT with `sub` and `exp` (`time.Now().Add(a.tokenLifetime)`) and return the token string.
3. ✅ **Config** (DONE 2026-07-14) — `.env` has `JWT_SECRET` + `TOKEN_LIFETIME=15m`; `config.go` loads, validates (secret ≥ 32 chars, lifetime > 0), `time.ParseDuration` once at startup with the parse error wrapped and collected; `main.go` wires `NewAuthService(r, c.JwtSecret, c.TokenLifetime)`. See "The secret" section below for everything learned here.
4. **Handler — `handler/auth.go`**, its own `AuthHandler` (don't bolt login onto `UserHandler` — the health-check lesson). `POST /auth/login`: decode `{email, password}`, call `Login`, map: success → 200 with the token as JSON; `ErrInvalidCredentials` → 401 (plus `WWW-Authenticate: Bearer`); anything else → 500.
5. **Middleware — a new package.** Wraps protected handlers: verify the token's signature and expiry, put the user id into the request context, answer 401 *without the handler ever running* when the token is missing or bad. Then protect the routes — `/health` stays public.

Branch: `feat/auth`.

### What `context.Context` is (2026-07-14)

A context is a value that travels with ONE request, from the handler down through service and repository. It carries two things: a **cancel signal** (the user closed the browser — why keep running a SQL query for nobody?) and a **deadline** ("if this takes more than 5 seconds, give up"). That's why `database/sql` has pairs like `QueryRow` / `QueryRowContext`: same query, but the second one can be stopped when the context says "cancelled" or "too late". A third use — carrying request-scoped values, like the user id the auth middleware will store — arrives in build step 5.

Decision (2026-07-14): none of the existing repo methods take a context, so `ReadByEmail` doesn't either — consistency now, and one future pass adds `ctx` to ALL repo methods together (tracked in `.claude/BACKLOG.md`).

### Vocabulary fixed: the JWT **is** the token (2026-07-14)

Not "an id and a token" — one thing, two names. The token is the string `xxxxx.yyyyy.zzzzz`; the id (`sub`) is written *inside* the middle chunk. And why login needs email+password at all: at the login moment the client has NO token yet — creating the first one is the request's whole purpose. The email finds the row, the row gives the id and the stored hash, the password proves identity, the id goes into the token. Every request after that carries the id inside the token; find-by-email is login-only. Every successful login mints a fresh token — nothing special about the "first" one; 15-minute expiry makes re-login routine.

### How `Login` was built — and its error policy (2026-07-14)

The flow: `ReadByEmail` → bcrypt compare → build `jwt.RegisteredClaims` (`Subject` = the id as a string via `strconv.Itoa`, `ExpiresAt` = `jwt.NewNumericDate(now + lifetime)`) → `jwt.NewWithClaims(HS256, claims)` → `SignedString([]byte(secret))` → return the string. The claims value is local — born and dead inside the function; nobody outside ever sees it. `Login` returns ONLY the token string: the `domain.User` it fetched is used for two fields (hash to compare, id for `sub`) and never travels toward HTTP — it contains the password hash.

The error policy in one sentence: **`ErrInvalidCredentials` appears exactly twice** — unknown email, wrong password, the two client-fault cases, deliberately indistinguishable — **and every other error wraps and bubbles up** to the handler's fallback 500. The rule that decides every case: *what is this error's true story?*
- Wrong password (`bcrypt.ErrMismatchedHashAndPassword`) → "your credentials are wrong" → the sentinel.
- A malformed stored hash → "the server's data is broken" → wrap, 500 — never blame the client.
- A signing failure → fires *after* both checks passed, the client did everything right → wrap, 500.

A detour worth remembering: a `domain.ErrInternalServer` sentinel was created for the unknown cases, then deleted. Two reasons: it *destroys information* (the original "connection refused" disappears from every future log — `fmt.Errorf("Login: %w", err)` keeps it), and its name is HTTP language (it's what 500 is called) inside the domain layer. Unknown failures need no sentinel — no caller branches on them; they're the fallback rung.

Two more lessons from the same session:
- **An interface is a claim.** `ReadByEmail` briefly sat in `UserRepository` too — but `UserSvc` never calls it, so the claim was false, and every future test fake would have been forced to implement a method no test uses. It lives only in `AuthRepository` — a one-method interface declared next to its consumer in `service/auth.go`. `*UserRepo` satisfies it implicitly; there is still exactly one concrete repo.
- **Docs examples teach shape, not text.** The golang-jwt example (custom claims type, `"johndoe"`, hardcoded subject and 15 minutes) got pasted into `main.go` → syntax error + wrong file + wrong content. Take the *calls* from an example; the values come from your own variables (`u.ID`, `a.tokenLifetime`) and the *contract* decides the fields (sub + exp, so plain `jwt.RegisteredClaims`, no custom type). Related trap: the HS256 key must be `[]byte` — a plain string compiles and fails only at runtime.

### The secret — what makes it good, where it lives, where it must never appear (2026-07-14)

Not "encrypted" — the secret is a plain string whose entire power is being **unguessable**. Think like the attacker: I hold one valid token and I know how the signature is made (HMAC over the first two chunks with the secret). At home, offline, I try secrets one by one until my computed signature matches the real one — then I can mint tokens for ANY user id. How long that takes depends only on how hard the secret is to guess: a 5-character or dictionary-word secret falls in seconds; 32+ random bytes from a tool (`openssl rand -base64 32`) is practically unguessable.

Where it comes from: **outside the app, once.** If the app generated its own secret at startup, every restart would invalidate all existing tokens (everyone logged out at 3am deploys), and multiple instances behind a load balancer would each hold a *different* secret — instance B rejects tokens minted by instance A, destroying JWT's whole point ("any instance verifies alone"). Same secret across restarts and instances = it's configuration, exactly like `DB_PASSWORD`: a human/ops creates it once, the environment hands it in. Changing it on purpose ("rotation") is likewise an ops action outside the app.

What config does with it: `Validate()` got its first *quality* check — presence checks ask "is it set?", this one asks "is it any good?" — secret shorter than 32 characters → the app refuses to start. A server running with a guessable secret is worse than a server that doesn't run.

Where it must never appear — two incidents from the same day:
- **Git: `.env` was tracked since the early commits** (`.gitignore` never listed it) — the dev DB password sat in pushed history. Fix: add to `.gitignore`, `git rm --cached .env` (untrack, keep on disk), commit. The JWT secret itself was still uncommitted — caught in time. A value already in history is *burned* (accepted here: local dev DB, private repo). The lesson: **a secret that has ever touched git is not a secret anymore** — and check the ignore rules *before* the secret exists on disk. Habits don't switch on when stakes rise; you ship the habits you trained.
- **Logs: `fmt` prints unexported struct fields.** The scaffolding line `fmt.Println("main - newauthsvc:", l)` printed the whole `AuthSvc` — repo pointer, lifetime, and the JWT secret — to stdout at every startup. In production stdout IS the log stream; logs get stored, shipped, and read. **Secrets never go into logs.** The four wiring debug prints are scheduled for deletion.

Also learned here — `time.Time` vs `time.Duration`: a *point* on the calendar versus an *amount* of time. `"15m"` is an amount → `time.ParseDuration` → `time.Duration`, parsed ONCE at startup; the string is never seen again. The two types meet in `Login`: `time.Now().Add(a.tokenLifetime)` — point + amount = the expiry point. And three config review rounds worth remembering: the length check first landed on the wrong field (`AppEnv` — "development" is 11 chars, the app could never start); `string(c.TokenLifetime)` repeated the `string(65)`→`"A"` trap on a number type (the zero-check for a duration is `== 0`); and the parse error went ignored → replaced-by-fixed-text (cause lost) → properly wrapped with `%w` and appended to the collected list.

### Implicit interfaces, seen live in `main.go` (2026-07-14)

The same `r` (`*UserRepo`) feeds two constructors: `NewUserSvc(r)` asks for `UserRepository` (seven methods), `NewAuthService(r, …)` asks for `AuthRepository` (one method). An interface parameter never means "this exact type" — it means **"anything that has these methods"**, and the compiler checks the method set at the call, no declarations anywhere. One person, two ID cards: the gym card and the library card open different doors, and each door checks only its own card. The payoff shows in the deletion test: remove `ReadByEmail` from the repo and ONLY the `NewAuthService` line stops compiling — a deletion breaks exactly the consumers whose interface names the method. That's why false claims in interfaces (the `ReadByEmail`-in-`UserRepository` episode) are poison: they make changes look more dangerous than they really are.

### The timing leak, concretely (2026-07-14)

Count the work on the two failure paths. Email exists + wrong password: the row is fetched, bcrypt runs — and bcrypt is *deliberately* slow, ~100ms; that slowness is its anti-brute-force job. Email unknown: `ReadByEmail` returns not-found and the function returns immediately, ~2ms. Same 401, same body — but an attacker doesn't only read responses, they *time* them: 100ms means "the email exists", 2ms means "it doesn't". The stopwatch answers exactly the question the identical 401s refuse to answer.

The fix (planned): on the not-found path, run a bcrypt compare against a dummy hash and throw the result away — purely to burn the same ~100ms, so both paths take equal time. The `// TODO: dummy bcrypt compare (timing)` marker sits on the exact line where that dummy compare will go, inside the `ErrUserNotFound` branch — the marker lives IN the hole it marks, so anyone reading the fast path sees a known gap, not a design choice.

**Decision 2026-07-16: skipped on purpose.** This is a learning project with no real attackers; the concept is understood (that was the goal), the fix recipe is written down in the backlog under "Later / ideas", and the `// TODO` stays in `Login` as the honest marker. Important detail preserved for that day: the dummy constant must be a REAL bcrypt hash, not a junk string — `CompareHashAndPassword` rejects a malformed hash instantly, and the timing gap comes right back.

### `VerifyToken` — how token verification actually works (2026-07-16)

This one was hard, so here it is slowly, from the physical thing.

**The question it answers.** A request arrives carrying a token string. Maybe the server minted it; maybe someone edited the payload (`"sub":"9"` → `"sub":"1"`); maybe it's invented from nothing. `VerifyToken` answers: *is this token genuine, and who does it belong to?*

**Compare with what? — with itself.** The first instinct is that verifying means looking something up — like bcrypt comparing a password against a stored hash. Wrong instinct here: **nothing about the token is stored anywhere**. That is the whole point of JWT — the server keeps no session table. The comparison is between two things that both exist in the moment of the request:

- **Thing A:** the signature the token *carries* — its third chunk.
- **Thing B:** the signature the server *recomputes right now* — from that same token's first two chunks plus the secret.

When `Login` minted the token, it took chunks one and two, mixed them with the secret, and got a fingerprint: that fingerprint IS chunk three. Verification simply **repeats the mint step** on the arriving token and checks that the result equals the chunk three it brought along. A == B: only the server could have made this token, and nobody touched it since. A ≠ B: forged or tampered. A forger can write any payload they like — they can't produce the matching third chunk, because making it requires the secret.

Then one more look: is `exp` in the past? Signature genuine + clock happy = valid token.

**Where the compare happens — not in our code.** The jwt library does the A/B comparison and the clock check inside one call: `jwt.ParseWithClaims`. Our code compares nothing. The whole method body is: call parse → if error, sentinel → read `sub` → convert → return. Ten lines.

**The three arguments of `ParseWithClaims`:**

1. *The token string* — the thing under test.
2. *A pointer to an empty claims struct.* `claims := &jwt.RegisteredClaims{}` — an empty box. The library opens the token and writes `sub` and `exp` INTO the box; afterwards the code reads them back out (`claims.GetSubject()`). It must be a pointer for the same reason `Decode(&l)` needed `&` in the handler: the library writes into OUR variable, not into a copy.
3. *The keyfunc* — a small anonymous function the library calls back mid-parse to ask one question: "what key do I use?" The only correct answer is the secret, as `[]byte`: two lines, `return []byte(a.jwtSecret), nil`. Why a function instead of the key directly? Apps with several keys pick the right one per token (the callback receives the token, header already readable); with one key the function just always answers the same.

**Trap met — minting inside the keyfunc:** first draft called `t.SignedString(...)` inside the keyfunc and returned the result as "the key". `SignedString` is the MINT operation; run on a half-parsed token it produces garbage, and verification would fail for every token, real or fake. The keyfunc hands over the key. Nothing else.

**The error policy — one sentinel, no detail:** any error from the parse call (malformed string, forged signature, expired) means the same thing to callers: `domain.ErrInvalidToken`. No inner `errors.Is` laddering — there is nothing to distinguish, the middleware will answer 401 either way. New sentinel, NOT `ErrInvalidCredentials`: credentials are what you present at the door (email+password); a token failure is about the wristband (expired, forged). Same 401 on the wire, different invariant in the code.

**The boundary conversion:** JWT subjects are strings by spec, so `Login` stored the id as `strconv.Itoa(u.ID)` — and `VerifyToken` converts it back with `strconv.Atoi`, returning an **int**. Callers get the id in the domain's type; the "sub is a string" fact is a JWT detail, and JWT details stay inside this file. Signature: `VerifyToken(tokenString string) (int, error)` — the value-and-error pair, exactly like `Login`.

**Why it lives in the service, next to `Login`:** minting and verifying are the two halves of one secret-handling job — both need the secret and the jwt library, and both already live in `AuthSvc`. The middleware stays free of crypto: pull the string out of the header, hand it to the expert, obey the answer.

**Copy-paste smells caught in review (third appearance of the lesson):** a `bcrypt.ErrMismatchedHashAndPassword` check wandered in from `Login` (no bcrypt anywhere near this function), and all the wrap prefixes said `"Login:"` in a function named `VerifyToken`. Copied code carries its old context along — read every pasted line as if it were new.

### The middleware — a guard in code (2026-07-16)

**What it is, physically.** Without middleware, the mux calls the handler: one hop. A middleware is a handler that *holds another handler inside it*. The request reaches the guard first; the guard checks the token and makes one decision — call the inner handler, or write a 401 and never call it. The line `return` after each 401 IS the security: it is the guard not stepping aside. Chain: mux → guard → real handler. The handlers themselves never change; they get wrapped, like a letter inside a security envelope.

**The signature, and why it must be this.** The mux can only serve things shaped like a handler — so from the *outside*, the guard must be an `http.Handler`. But the guard must also know who it guards — and that can't arrive with the request, so it's handed over *once*, at wiring time. Both facts together force the shape: **handler in, handler out** — `Auth(next http.Handler, t TokenVerifier) http.Handler`.

**Two quiet tricks inside the skeleton:**
- `http.HandlerFunc(...)` is NOT a function call — it is a *type conversion*, a costume that makes a plain `func(w, r)` pass the `http.Handler` interface check (the type's `ServeHTTP` just calls itself).
- `next` is used inside the returned function but arrived outside it: the closure remembers it forever, from the one wiring moment in `main.go`. Same trick delivers the verifier.

**The middleware owns its interface.** `TokenVerifier`, one method, declared in the middleware package — not in `service`. The handler's `AuthService` interface keeps only `Login`, because the handler only calls `Login`. Third appearance of the rule: *the consumer declares the interface, listing only what it uses* (`AuthRepository`, `AuthService`, now `TokenVerifier`). `*AuthSvc` satisfies all three without knowing any of them exist.

**The body, as a story.** Read the `Authorization` header. Empty → 401, stop. Cut the `"Bearer "` prefix — **the space is part of the prefix**; `strings.CutPrefix` returns the rest AND a bool saying the prefix was really there — not found → 401, stop. Hand the token to `VerifyToken` — error → 401, stop. Only now: `next.ServeHTTP(w, r)` — the guard steps aside.

**All three denies must be identical twins.** Same status, same `WWW-Authenticate: Bearer` header, same neutral body. If a client can tell "no header" from "bad token" from "expired", so can an attacker probing the door — the same no-hints policy as login's two identical 401s. Three branches saying the same thing = extract one `unauthorized(w)` helper and call it three times.

**Traps met while building it:**
- `if header == " "` — one space. A missing header is the EMPTY string `""`. With the space version, every tokenless request walked straight past the guard. Test the difference: `""` has length 0, `" "` has length 1.
- Setting a response header and returning — without writing a status — makes Go send an implicit **200**. The guard said nothing, and silence means yes. Every deny must write its status explicitly.
- `CutPrefix(header, "Bearer")` without the space leaves the token as `" eyJ..."` — one invisible leading space, and signature verification fails for perfectly VALID tokens. The kind of bug you find by staring at two identical-looking strings.

**Still ahead:** ~~putting the verified id into the request context, wiring the guard, the protected-routes curl suite~~ — all done 2026-07-17, next two sections.

### The request context — how the id travels (2026-07-17)

**The problem, physically.** The middleware ends with `next.ServeHTTP(w, r)`. It hands the next handler exactly two things: `w` and `r`. There is no third argument. So if the verified user id has to reach the handler, it has to travel *inside* `r`.

**What a context is.** Every request that arrives already carries one — `net/http` made it, and `r.Context()` hands it over. It is a small bag of values that travels with the request through every layer. It has two jobs. Job one: carry a stop signal — when the client gives up, the bag says "stop, nobody wants this answer anymore" (this is what the repo's future `ctx` parameters listen for). Job two: carry values that belong to this one request — like the user id, born in the middleware, needed later in a handler.

**The metaphor:** the airport security tray. Your things go in the tray, the tray rides the belt with you, station to station. Each station can look inside. When you're through, the tray is thrown away — it belonged to that one pass, nobody else's.

**Why one request's bag is wrong for another (and it's not "statelessness"):** the values answer questions *about this request*. Request A's bag says "user 12"; request B belongs to user 99 — if B read A's bag, B would act as user 12: a security bug. Statelessness is the neighbor idea: the server remembers nothing *between* requests, and the context respects that by dying with its request. Also fixed a wrong guess: the isolation of the *work* comes from goroutines (each request gets its own worker); the context isolates the *data* (each request gets its own luggage).

**The bag is extended, never edited.** You never put a value into an existing context. `context.WithValue(old, key, value)` returns a NEW context wrapping the old one plus your value; `r.WithContext(ctx)` returns a NEW request carrying it. So the whole context step is three lines where the `Println(id)` placeholder used to be: make the new context, make the new request, hand that request to `next`.

**Why the key must be a private type, not a string.** First guess was "a string key would expose information" — no; the client never sees the context. The real problem is **collision**: the bag is shared by every package that touches the request (yours, a logging library, a tracing library), and keys compare by value AND type. If two packages both pick the obvious string `"userID"`, they have the same key and overwrite each other — nobody did anything wrong. Fix: `type ctxKey int` (private) + one const of it. The number is irrelevant; the TYPE is the uniqueness, and nobody outside the package can make one. Flip side: nobody outside can *read* the value either — so the package will need an exported `UserIDFromContext(ctx)` helper the day `/me` or ownership checks arrive (backlog).

### Wiring the guard, breaking the clients, shipping v0.2.0 (2026-07-17)

**Where the guard attaches.** First instinct: wrap the whole mux — one line, everything protected. Walk it through: a new user tries `POST /users` to register. No account yet, so no token. The guard says 401. Same for `POST /auth/login` — you'd need a token to get a token. *The door is locked with the key inside.* So the guard wraps route by route, at registration: the four protected routes changed from `mux.HandleFunc(pattern, method)` to `mux.Handle(pattern, middleware.Auth(http.HandlerFunc(method), t))`. Two mechanics inside that line: `Handle` not `HandleFunc`, because `Auth` returns an `http.Handler`; and `http.HandlerFunc(...)` — the costume again — turns a plain method into a handler. `RegisterRoutes` gained a `middleware.TokenVerifier` parameter; `main.go` hands in `authSvc`, which fits without knowing the interface exists (fourth time).

**The guard broke the old tests — and that was the lesson.** Four existing curl suites called guarded routes with no token; they all started failing. Nothing in them changed — the API's contract changed underneath them. That is what **breaking change** means, felt live: every existing client broke. The fix taught the other half: the suites now *log in during setup* (create the disposable user, login as it, carry the `Bearer` header on every guarded call) — self-containment now includes identity.

**Verified on the wire:** no token → 401 + `WWW-Authenticate`; wrong scheme, bare token, garbage, tampered signature (real token, last 4 chars chopped) → the identical 401; real token → 200; `POST /users` and `POST /auth/login` still open. `test_middleware.sh`: 12/12. All five suites: 38 checks green. Not covered: expired token (15-minute lifetime can't be waited out in a suite — parked).

**The release.** `feat/middleware` had grown on top of `feat/auth`'s tip, so it already contained the whole story — one fast-forward merge to master, no chain. Then the version call, HIS: **v0.2.0, not v1.0.0** — "the password part is not done"; the contract is still moving, and 1.0.0 is a promise that it stopped. The breaking-change experience above is exactly why that promise matters. Tag: annotated (`git tag -a` — author, date, message: a release is a statement), message "login system and middleware", style fixed to `v0.2.0` (no stray dot). **Phase 2 closed.**

**The question left standing at the door:** the token now proves WHO you are on every protected route — but nothing decides what you MAY do. User 12, valid token, `GET /users/10`: today, allowed. Authentication is done; authorization hasn't started. That's the opener for the next design conversation.

---

## 13. The principles — the whole project in 30 lines

The original seventeen (from Phase 1's design and build):

1. **Design before code** — endpoints, layers, and config were locked before any handler existed.
2. **Separate concerns ruthlessly** — handlers never touch the DB; services never know HTTP; repos only move data.
3. **Validate at the boundaries** — handlers check the input's shape; services check the business rules.
4. **Depend on contracts, not concrete types** — interfaces between every layer.
5. **Configuration is a feature** — one binary runs anywhere; only the environment changes.
6. **Fail fast at startup** — a bad config kills the process before it serves a single request.
7. **Collect all the errors, then report** — not one complaint at a time.
8. **`main.go` is the composition root** — everything is built and connected in one place; nothing finds its own dependencies.
9. **Try, then handle — don't check, then act** — let the database enforce what it can enforce atomically.
10. **Explicit over implicit** — what a function needs is in its signature, never smuggled through a context.
11. **The consumer owns the interface** — the handler declares what it needs; the service satisfies it without knowing.
12. **Authentication is not authorization** — "who are you" is middleware; "may you do this" is the service.
13. **Shared types live in neutral territory** — `internal/domain`, so no import arrow ever points backwards.
14. **Pointer receivers by convention** — consistency now, safety if mutable state arrives later.
15. **Dependencies flow downward through constructors** — service holds repo; repo holds the DB pool.
16. **Translate errors where the source is known** — only the repo knows there's SQL underneath.
17. **Wrap errors where the work happened** — layers above pass along what they didn't cause.

Eight more, each earned by a specific mistake or discovery:

18. **One struct per contract** — never per table, never per method (2026-07-04).
19. **An error you print but don't act on is still swallowed** (2026-07-05).
20. **Status codes are diagnostic inputs** — a 405 located a missing route in one step (2026-07-06).
21. **Suites create the data they need** — shared fixtures rot underneath you (2026-07-06).
22. **Responses are receipts, not hopes** — write the status only after the work truly succeeded (2026-07-06).
23. **Sometimes hiding information is the design** — collapse error causes when telling them apart helps an attacker (2026-07-06).
24. **Only unchangeable, always-needed facts belong in token claims** (2026-07-13).
25. **The repo fetches, the service compares** — trust boundaries hold even inside login (2026-07-13).
26. **Choose an error by its true story** — a signing failure after a correct password is never "invalid credentials"; generic sentinels that eat the original cause are a step backwards (2026-07-14).
27. **An interface is a claim** — list only the methods the consumer actually calls; every extra method is a lie that test fakes must implement forever (2026-07-14).
28. **Docs examples teach shape, not text** — take the calls, never the hardcoded values or the location; your contract decides the fields (2026-07-14).
29. **A secret that touches git or logs is not a secret anymore** — `.gitignore` before the secret exists; `fmt` prints unexported fields (2026-07-14).
30. **The app doesn't invent its own configuration** — secrets and lifetimes are created outside, once, and handed in; an app-generated secret dies on every restart and splits every load-balanced pair (2026-07-14).
31. **Each layer names only what it needs from the layer below** — the handler asks for "anything with `Login`", not for the whole `AuthSvc` with its secret and repo inside (2026-07-15).
32. **Say the status code out loud before using it** — "409: your request fights the resource's state" is not "401: I don't know who you are"; a code chosen by habit contradicted a contract the same person had already written (2026-07-15).
33. **Response bodies are objects, not bare values** — a lone JSON string is legal and useless; a named field can be picked and the object can grow (2026-07-15).
34. **Notes can lie; the code is the record** — "/health done" lived in every document for nine days while the code had only a comment; one curl exposed it (2026-07-15).
35. **Short-lived tokens make secret rotation free** — the regenerated JWT secret cost nothing because every old token dies within 15 minutes anyway (2026-07-15).
36. **Request scope is the third scope** — not global (shared by all requests), not a parameter (unsustainable to thread); the context is per-request luggage: born with the request, extended never edited, dead when it's answered (2026-07-17).
37. **Context keys are types, not names** — a string key is a collision waiting for the second package to pick the same obvious word; a private type makes the collision impossible to even write (2026-07-17).
38. **Guard the routes, not the mux** — a guard around everything locks the registration and login doors too; you'd need a token to get a token (2026-07-17).
39. **A breaking change is measured in broken clients** — four green suites went red the moment the guard turned on, without a line of them changing; that experience is why 1.0.0 waits until the contract stops moving (2026-07-17).

---

## 14. Open questions — the running list

Still to be answered as the phases advance:

- What should a consistent, client-friendly error response look like across all endpoints? (Phase 3 — the validator-unwrapping loop from the PATCH debugging is the starting point.)
- How do you structure database access so it can be tested without a real database?
- ~~How does `context.Context` really work?~~ — ANSWERED in two halves: cancellation 2026-07-14, values 2026-07-17 (§12, "The request context"). Still pending in practice: the repo-wide `ctx` pass in BACKLOG.md.
- How do you unit-test the service layer without touching the database?
- ~~How exactly does middleware validate a JWT?~~ — ANSWERED 2026-07-16/17 (§12: `VerifyToken` recomputes the signature; the guard wraps per-route; the id rides the context).
- The password-change endpoint's design: resource or action? PUT or POST? Which statuses for wrong-old-password and same-password? 204 on success?
- **Authorization** — the new one, posed and unanswered: user 12 holds a valid token and asks `GET /users/10`. Today the answer is 200. Should it be? The token says WHO; nothing yet says MAY.
