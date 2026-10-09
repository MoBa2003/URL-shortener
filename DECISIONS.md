
# URL Shortener Architecture & Design Decisions

## Phase 1: Core Fundamentals & Baseline Memory Store

When establishing the baseline for the URL shortener service, the primary design focus centers on consistent URL handling, deterministic idempotency, and concurrent thread safety.

### URL Normalization
To ensure that duplicate representations of a web address map accurately, two URLs are treated as identical whenever their normalized forms match. Normalization applies a standardized sequence: trimming leading and trailing whitespace, converting both the scheme and host components to lowercase, and stripping any trailing slashes, with the sole exception of the root path `/`.

### Idempotency & In-Memory Storage Strategy
Guaranteeing that re-submitting an already processed long URL yields the exact same shortened code requires deterministic lookup mechanics. To achieve this, `URLStore` maintains two synchronized in-memory maps:
1. `codeToMetadata map[string]MetaData` (which undergoes model enhancement in Phase 2).
2. `urlToCode map[string]string`.

Whenever a request arrives to shorten a link, the system queries `urlToCode` first. If a matching normalized URL is present, the service immediately returns the existing code accompanied by an `HTTP 201 Created` status code.

### Code Generation & Collision Mitigation
Short codes are generated as 6-character, URL-safe random strings constructed using Go's `crypto/rand` package over the alphanumeric character set `[a-zA-Z0-9]`. Upon generating a candidate code, the application verifies whether it already exists within `codeToMetadata`. In the event of a key collision, the system executes an automated retry loop up to 10 times to secure a unique code before proceeding.

### Concurrency & Locking Choices
For thread-safe operations in `URLStore`, `sync.RWMutex` serves as the core synchronization primitive. Read operations—such as retrieving metadata via `GetMetadatafromCode` or executing idempotency checks against `urlToCode`—acquire a read lock (`RLock()`), enabling high concurrent throughput for redirects and metadata queries. Write operations during link creation acquire an exclusive write lock (`Lock()`). To guard against race conditions when concurrent requests attempt to shorten the same URL simultaneously, double-checking logic is applied immediately after securing the write lock.

### Package Layout
The project follows a clean architectural layout:
* `cmd/server/main.go`: Functions as a lightweight entry point responsible for parsing the `-addr` and `-base` CLI flags before starting the standard `http.Server`.
* `internal/shortener/`: Houses all core business logic, data storage logic, HTTP request handlers, and corresponding unit and integration test suites.

## Phase 2: Metadata Endpoint, Abstractions, & Domain Safety

Phase 2 introduces detailed metadata access, clean domain abstraction layers, and robust error mapping across the HTTP layer.

### Metadata Endpoint (`GET /api/v1/links/{code}`)
The metadata endpoint exposes key creation details—specifically the target `url` and the `created_at` timestamp—using JSON payloads. Valid codes receive an `HTTP 200 OK` response, while nonexistent codes return an `HTTP 404 Not Found`.

To support this capability, the internal storage map transitioned from a simple `map[string]string` structure to `map[string]MetaData`. Within the `MetaData` struct, `CreatedAt` is explicitly typed as `time.Time`. Leveraging standard `time.Time` allows Go's `encoding/json` package to format timestamps automatically according to the RFC3339 standard (e.g., `2026-01-15T12:00:00Z`), eliminating the need for manual string parsing.

### Consumer-Side Store Interface
Following the idiomatic Go principle *"Accept interfaces, return structs"*, the `Store` interface is defined directly in the consumer package (`internal/shortener/handler.go`).

```go
type Store interface {
    Shorten(rawURL string) (string, error)
    GetMetadatafromCode(code string) (MetaData, error)
}
```

Defining the interface at the consumption point decouples HTTP handlers from concrete storage mechanics like `URLStore`. This design allows switching to persistent engines (such as SQL/GORM or file-backed stores in Phase 4) or substituting test doubles (`FakeStore`) without modifying handler logic.

### Domain Sentinel Errors & HTTP Mapping
To keep error handling structured, domain-specific sentinel errors—`NotFoundErr` and `InvalidURLErr`—are established in `internal/shortener/errors.go`.

Handlers evaluate incoming errors by unwrapping them using `errors.Is(err, NotFoundErr)` and `errors.Is(err, InvalidURLErr)`. Errors are mapped directly to HTTP responses:
* `InvalidURLErr` \\(\rightarrow\\) `HTTP 400 Bad Request` with a structured JSON payload.
* `NotFoundErr` \\(\rightarrow\\) `HTTP 404 Not Found` with a structured JSON payload.
* Unhandled internal errors \\(\rightarrow\\) `HTTP 500 Internal Server Error`.

### Isolated Testing with `FakeStore`
Because `URLStore` relies on non-deterministic random code generation (`crypto/rand`), simulating unexpected storage failures (such as database read timeouts or I/O faults) directly is difficult. To solve this, a `FakeStore` implementing the `Store` interface was created. It includes fields for error injection (`ShortenErr`, `GetErr`) alongside helper methods like `SetMetadata` for state setup. This enables 100% deterministic unit testing across all HTTP status codes (200, 201, 302, 400, 404, 500) while isolating the HTTP layer from storage implementations.

### Idempotency Preservation
Idempotency enforcement via `urlToCode` lookups remains preserved behind the `Store` interface abstraction. Submitting an identical normalized URL repeatedly returns the original short code and metadata across all API consumers.

## Phase 3: Concurrency Optimization & Network Resilience

Phase 3 focuses on tuning concurrent synchronization and protecting the server against slow or malicious clients.

### Concurrency Model
`sync.RWMutex` remains the optimal locking choice. In URL shortening workloads, read operations (redirects and metadata requests) vastly outnumber write operations. Using `sync.RWMutex` allows multiple concurrent readers to acquire `RLock()` simultaneously without blocking one another, ensuring high redirect throughput while safeguarding maps against data races. Exclusive write locks (`Lock()`) are restricted strictly to new link insertions.

### Connection Timeouts & Slow-Client Mitigation
To harden the HTTP server against connection exhaustion and Slowloris attacks, explicit timeout parameters are configured:
* `ReadHeaderTimeout` (`2s`): Mitigates Slowloris attacks by requiring request headers to be read promptly.
* `ReadTimeout` (`5s`): Caps the total time allowed for reading the entire request body.
* `WriteTimeout` (`10s`): Ensures connections stuck on slow network clients are cleaned up gracefully.
* `IdleTimeout` (`120s`): Manages keep-alive connection efficiency while freeing idle file descriptors.

### Memory Policy & Eviction Decisions
No in-memory eviction cap or LRU policy was introduced in this phase. For single-binary workloads, direct map synchronization delivers optimal performance. Memory eviction boundaries and TTL mechanisms are intentionally deferred to persistent storage or external caching layers (such as Redis).

## Phase 4: Durable Persistence with PostgreSQL & GORM

Phase 4 moves the storage foundation from volatile RAM to a durable, transactional relational database.

### Storage Engine Selection
The service uses GORM paired with PostgreSQL (`gorm.io/driver/postgres`). PostgreSQL supplies reliable transactional guarantees, high performance, and crash safety for production traffic, while GORM provides clean object-relational mapping alongside native PostgreSQL indexing capabilities.

### Data Model, Schema, & Auto-Migrations
The database structure relies on the `URLModel` struct configured with GORM struct tags:
* `Code`: Primary Key (`type:varchar(10)`).
* `LongURL`: Unique Index (`type:text`, `not null`).
* `CreatedAt`: Timestamp (`not null`).

Schema migrations run automatically at application startup using `db.AutoMigrate(&URLModel{})`.

### Atomicity & Crash Safety
Database inserts execute directly through `db.Create()`, leveraging PostgreSQL's native ACID transaction boundaries. Links are committed to durable disk storage prior to returning the `HTTP 201 Created` response, guaranteeing zero data loss during unexpected server crashes.

### Timestamp Handling
`CreatedAt` is stored as a standard `time.Time` value mapped to PostgreSQL `TIMESTAMPTZ` / `TIMESTAMP` types in UTC. During API serialization, it formats transparently into RFC3339 JSON strings.

### Persistence & Idempotency Rules
Idempotency persists across application restarts by checking the unique index on `long_url` prior to generating new short codes. Re-submitting an existing normalized long URL after a database or application reboot retrieves the original short code record from PostgreSQL, maintaining total consistency with Phase 1 domain rules.

### Close Mechanism for PostgresDB Before Finishing the Program
We run the server inside a separate Goroutine and use a channel in the main Goroutine to intercept OS termination signals. When a shutdown signal is received, we check if the active store is PostgreSQL and explicitly invoke Close() on its connection pool. This prevents resource leaks and lingering socket connections on the database, avoids exhausting PostgreSQL's max_connections limit during service restarts, and ensures active transactions complete cleanly without data corruption.