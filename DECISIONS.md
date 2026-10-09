
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

## Phase 5: High-Scale Architecture & Distributed Caching

To handle millions of requests, Phase 5 introduces a stateless scale-out architecture backed by Redis caching and edge CDN capabilities.

### Dependencies
* `github.com/redis/go-redis/v9`: The official Go client for Redis. It manages TCP connection pooling, thread-safe command execution, auto-reconnections, context timeouts, and TTL key expirations. Because Go's standard library (`net/http`, `os`) lacks native support for the Redis Serialization Protocol (RESP), `go-redis` is required for production connection management.

### Stateless Scale-Out Architecture
* **Stateless Application Nodes**: The Go web server (`cmd/server`) holds no local state required for request correctness. Multiple application replicas (\\(N\\) instances) run concurrently behind a Layer 7 Load Balancer (such as NGINX, HAProxy, or AWS ALB) using Round-Robin routing.
* **Shared Storage & Cache**: All replicas connect to a centralized PostgreSQL database cluster (primary for writes, read-replicas for scaling read queries) and a shared distributed Redis cluster.

### Redis Caching with the Decorator Pattern
The caching layer is implemented via `RedisStore`, which wraps any underlying `Store` implementation (`PostgresStore` or `URLStore`) using the Decorator Pattern. This keeps HTTP handlers decoupled from caching mechanics while dynamically adding caching capabilities.

#### Key Schemas
* `url:{normalized_url}` \\(\rightarrow\\) `code`: Maps normalized long URLs to generated codes to serve idempotency checks on `Shorten()` directly from RAM.
* `meta:{code}` \\(\rightarrow\\) JSON-serialized `MetaData`: Holds link metadata (`url` and `created_at`) to instantly satisfy both `GET /api/v1/links/{code}` requests and `GET /{code}` HTTP 302 redirects.

#### Read Path Flow
1. **Cache Hit**: Incoming redirect or metadata queries check Redis `meta:{code}`. On a hit, data is deserialized and returned in \\(O(1)\\) time (<2ms) without querying PostgreSQL.
2. **Cache Miss**: On a miss, `RedisStore` delegates execution to `underlying.GetMetadatafromCode(code)` in PostgreSQL, serializes the result, asynchronously populates Redis with the configured TTL (`-redis-ttl`), and returns the record.

#### Write Path & Idempotency Flow
When `Shorten()` is called, `url:{normalized_url}` is queried in Redis first. If cached, the existing short code is returned immediately. If uncached, the write executes against the primary database via `underlying.Shorten()`, and the resulting mappings (`url:` and `meta:`) are written to Redis with TTL.

#### Cache Invalidation & TTL Policy
Key expiration is controlled via the configurable CLI flag `-redis-ttl` (defaulting to `24h`). Because shortened links and metadata are immutable in this system (a short code never changes target destination), complex cache invalidation is unnecessary. Keys expire naturally or are evicted under memory pressure via Redis `allkeys-lru` policies.

### Edge CDN Strategy for Redirects
To scale redirects, an Edge CDN (such as Cloudflare or CloudFront) sits in front of the Load Balancer to cache `GET /{code}` HTTP 302 responses. The origin server sets `Cache-Control: public, max-age=86400` on redirect responses.
* **Advantages**: Offloads over 95% of redirect traffic away from application nodes directly to edge locations globally, achieving response latencies under 15ms.
* **Trade-offs**: If link deletion or real-time click analytics were required, edge caching could cause stale redirects until TTL expiration. Given link immutability, HTTP 302 edge caching offers an ideal performance-to-cost ratio.

### Write-Path Scaling & Rate Limiting
* **Pre-Generated Code Pool**: To eliminate database lock contention and collision retries during high-volume POST spikes, background worker goroutines pre-generate unique 6-character codes into a Redis Set/Queue. The `Shorten()` method pops a guaranteed unique code in \\(O(1)\\) time.
* **Rate Limiting**: Protects `POST /api/shorten` against abuse using a Redis-backed Sliding Window Middleware keyed per client IP or API token.

### Database Sharding & Partitioning
* **Consistent Hashing**: When a single PostgreSQL or Redis node reaches resource boundaries, keys are partitioned across database nodes using consistent hashing on the short code.
* **Redis Cluster Sharding**: Utilizes Redis Cluster hash slots (\\(16,384\\) slots derived from `CRC16(code)`). Hash tags (such as `{code}:meta`) ensure related keys reside on the same cluster node for pipeline efficiency.
* **Global Idempotency Index**: Cross-shard long URL lookups for idempotency route through a distributed hash table or dedicated secondary index mapping `Hash(long_url) -> Shard_ID`.

## Phase 6: Production Habits & Operational Safety

Phase 6 incorporates production-grade operational practices around shutdown handling, traffic safety, and observability.

### Dependencies
* `golang.org/x/time/rate`: Used to implement an efficient Token Bucket rate-limiting algorithm for the `POST /api/shorten` endpoint.

### Graceful Shutdown
Graceful shutdown is managed using `signal.Notify` to capture `SIGINT` and `SIGTERM` signals. Upon receiving a signal, `srv.Shutdown(ctx)` is invoked with a 10-second timeout context.
* **Drain Strategy**: The server halts acceptance of new incoming connections immediately while granting in-flight requests (such as database writes or Redis cache operations) up to 10 seconds to complete cleanly before process exit.

### Rate Limiting Controls
A per-IP Token Bucket rate limiter is applied specifically to the write path (`POST /api/shorten`). Each client IP is restricted to 5 requests per second with a burst capacity (bucket size) of 10. Exceeding this quota triggers an `HTTP 429 Too Many Requests` response.

### Domain Blocklist Policy
A domain verification check executes prior to code generation. Domains including `phishing.com`, `malware.org`, `localhost`, and `127.0.0.1` are permanently blocked. Blocking `localhost` and `127.0.0.1` provides protection against internal Server-Side Request Forgery (SSRF) vectors. Blocked attempts yield an `HTTP 403 Forbidden` status.

### Observability, Safe Logging, & Profiling
* **Structured Logging**: Uses Go 1.21's `log/slog` library to output structured JSON logs to `stdout`, replacing text logging for better log ingestion in production.
* **Logged Fields**: HTTP Method, sanitized Path, Status Code, IP address, and Request Duration.
* **Sensitive Data Protection**: Full URLs and raw query strings (such as `?token=secret`) are strictly omitted. `LoggingMiddleware` explicitly clears `r.URL.RawQuery` prior to logging to ensure tokens, secrets, or session parameters are never stored in server logs.
* **`pprof` Profiling**: Standard `net/http/pprof` diagnostic endpoints are registered conditionally behind a `-pprof` CLI flag, enabling on-demand CPU and memory profiling without exposing debug endpoints by default.