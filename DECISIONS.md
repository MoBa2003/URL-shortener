# Design Decisions

## Phase 1

- **URL Normalization**:
  Two URLs are considered "the same" if their normalized form matches. Normalization consists of trimming whitespace, converting scheme and host to lowercase, and stripping trailing slashes (except for root path `/`).

- **Idempotency Storage**:
  To guarantee that the same long URL returns the exact same code, `URLStore` maintains two synchronized maps in memory:
  1. `codeToMetadata map[string]MetaData` (updated in Part 2)
  2. `urlToCode map[string]string`
  Before generating a new code, `urlToCode` is checked. If the normalized URL exists, the existing code is immediately returned with status 201 Created.

- **Code Generation & Collision Handling**:
  New codes are 6-character URL-safe strings generated randomly using `crypto/rand` from the character set `[a-zA-Z0-9]`. When a new URL is processed, the system checks if the generated code already exists in `codeToMetadata`. If a collision occurs, it retries up to 10 times to generate a unique code.

- **Locking Choice**:
  `sync.RWMutex` is chosen for `URLStore`. Reads (`GetMetadatafromCode` and checking `urlToCode`) acquire `RLock()`, allowing high concurrent throughput for redirects and metadata queries. Writes (`Shorten` insertions) acquire `Lock()`. Double-checking is applied after acquiring the write lock to prevent race conditions during concurrent requests for the same URL.

- **Package Layout**:
  - `cmd/server/main.go`: Thin entrypoint parsing `-addr` and `-base` flags and starting `http.Server`.
  - `internal/shortener/`: Core business logic, store, handlers, and unit/integration tests.

---

## Phase 2

- **Metadata Endpoint (`GET /api/v1/links/{code}`)**:
  - **Purpose**: Exposes link creation metadata (`url` and `created_at`) via JSON with HTTP 200 OK for valid codes and HTTP 404 Not Found for missing codes.
  - **Data Model Change**: Refactored the internal storage map from `map[string]string` to `map[string]MetaData`.
  - **Type Choice for `created_at`**: The `MetaData` struct defines `CreatedAt` as `time.Time`. Using standard `time.Time` allows Go's `encoding/json` marshaler to automatically format timestamps according to the **RFC3339** standard (`2026-01-15T12:00:00Z`) without manual string parsing.

- **Store Interface at Consumer Layer**:
  - **Location**: The `Store` interface is defined in the consumer package (`internal/shortener/handler.go`), following the idiomatic Go rule *"Accept interfaces, return structs"*.
  - **Interface Definition**:
    ```go
    type Store interface {
        Shorten(rawURL string) (string, error)
        GetMetadatafromCode(code string) (MetaData, error)
    }
    ```
  - **Rationale**: Decouples the HTTP handlers from concrete storage implementations (`URLStore`). This allows seamless substitution of persistent storage engines (e.g., SQL/GORM or File-based store in Part 4) or test doubles (`FakeStore`) without altering a single line of HTTP handler logic.

- **Domain Sentinel Errors & Error Mapping Strategy**:
  - **Sentinel Errors**: Defined `NotFoundErr` and `InvalidURLErr` at the internal/shortenerr/errors.go file.
  - **Error Inspection**: Handlers inspect underlying errors using `errors.Is(err, NotFoundErr)` and `errors.Is(err, InvalidURLErr)` after error wrapping (`%w`).
  - **HTTP Status Mapping**:
    - `InvalidURLErr` \\(\rightarrow\\) **HTTP 400 BadRequest** with structured JSON error response.
    - `NotFoundErr` \\(\rightarrow\\) **HTTP 404 NotFound** with structured JSON error response.
    - Unhandled internal errors \\(\rightarrow\\) **HTTP 500 InternalServerError**.

- **Isolated HTTP Testing via `FakeStore`**:
  - **Rationale**: The concrete `URLStore` relies on non-deterministic code generation (`crypto/rand`) and cannot easily simulate unexpected storage failures (such as database read timeouts or disk I/O errors).
  - **Implementation**: Created `FakeStore` implementing the `Store` interface. It includes error injection fields (`ShortenErr`, `GetErr`) and direct state setup helpers (`SetMetadata`).
  - **Outcome**: Allows 100% deterministic unit tests for all HTTP status codes (200, 201, 302, 400, 404, 500) while keeping HTTP layer tests isolated from concrete storage logic.

- **Preservation of Idempotency**:
  - Idempotency logic (`urlToCode` lookup) is preserved within the store implementations behind the `Store` interface. Re-submitting an identical normalized URL consistently returns the original short code and metadata across all API callers.


---

## Phase 3

- **Locking Choice**:
  - **Choice**: `sync.RWMutex`
  - **Rationale**: In a typical URL shortener service, read operations (redirects and metadata lookups via `GET`) vastly outnumber write operations (creating new short URLs via `POST`). `sync.RWMutex` allows multiple concurrent readers to acquire `RLock()` simultaneously without blocking each other, ensuring high throughput for redirect operations while protecting the map from data races. Exclusive `Lock()` is only acquired during new link insertions.

- **Timeout Values & Slow-Client Protection**:
  - `ReadHeaderTimeout`: Set to `2s`. Mitigates **Slowloris attacks** by requiring HTTP request headers to be read promptly.
  - `ReadTimeout`: Set to `5s`. Caps the total duration allowed to read the entire request payload.
  - `WriteTimeout`: Set to `10s`. Ensures connections hung on slow network clients are released cleanly.
  - `IdleTimeout`: Set to `120s`. Controls keep-alive connection reuse efficiency while freeing idle file descriptors.

- **Eviction Cap**:
  - **Decision**: No in-memory memory cap or LRU eviction strategy was introduced in this phase.
  - **Rationale**: For an in-memory storage layer in single-binary scope, simple map synchronization offers maximum performance. Memory eviction boundaries and TTL features are deferred to persistent database or external cache layers (e.g., Redis) in advanced tiers.


---

## Phase 4

- **Storage Choice**:
  - **Selected Engine**: GORM + PostgreSQL (`gorm.io/driver/postgres`).
  - **Rationale**: PostgreSQL provides robust transactional guarantees, high performance, and reliable crash safety for production workloads. GORM abstracts database interactions cleanly while allowing native PostgreSQL indexing features.

- **Schema, Models & Migrations**:
  - **Model**: `URLModel` struct defined with GORM tags:
    - `Code`: Primary Key (`type:varchar(10)`).
    - `LongURL`: Unique Index (`type:text`, `not null`).
    - `CreatedAt`: Timestamp (`not null`).
  - **Migrations**: Handled automatically on application startup via `db.AutoMigrate(&URLModel{})`.

- **Crash Safety & Atomicity**:
  - Insert operations directly issue `db.Create()`, taking advantage of PostgreSQL's ACID transaction boundaries.
  - Links are fully committed to durable storage **prior to returning the HTTP 201 Created response**, guaranteeing no link loss upon sudden server crashes.

- **How `created_at` is Stored**:
  - Stored as standard `time.Time` mapped to PostgreSQL's `TIMESTAMPTZ` / `TIMESTAMP` types in UTC.
  - Serializes transparently to RFC3339 standard JSON strings during API responses.

- **Idempotency + Persistence**:
  - Idempotency is preserved across server restarts by querying the unique index on `long_url` before generating new short codes.
  - Submitting an identical normalized long URL after a database or server restart retrieves the original short code record from PostgreSQL, maintaining 100% consistency with Part 1 domain rules.




## Phase 5 — Millions of Requests (Scale-out Architecture & Redis Cache)

## Dependencies
*   **github.com/redis/go-redis/v9**: Official Go client library for Redis.
    *   Manages TCP connection pooling, thread-safe Redis command execution, automatic reconnections, context timeouts, and TTL key expirations for the caching layer.
    *   **Rationale**: The Go standard library (`net/http`, `os`) does not provide a native client for the Redis Serialization Protocol (RESP). Using `go-redis` ensures production-ready connection management and efficient caching operations.

---

*   **Architecture Overview (Stateless Replicas & Shared Persistence)**:
    *   **Stateless Application Nodes**: The Go web application (`cmd/server`) holds no in-memory state required for request correctness when using persistent storage. Multiple application replicas (\\(N\\) instances) run concurrently behind a Layer 7 Load Balancer (e.g., NGINX / HAProxy / AWS ALB) using Round-Robin routing.
    *   **Shared Storage & Distributed Cache Layer**: All application replicas connect to a centralized PostgreSQL database cluster (primary for writes, read-replicas for scaling queries) and a shared Redis cluster for distributed caching.

*   **Redis Cache Layer Implementation (Bonus Code Architecture)**:
    *   **Decorator Pattern Choice**: Implemented `RedisStore` struct which wraps any underlying implementation of the domain `Store` interface (`PostgresStore` or `URLStore`). This keeps HTTP handlers completely decoupled from caching mechanisms while dynamically adding caching capabilities.
    *   **Key Schemas & Data Mapping**:
        *   `url:{normalized_url}` \\(\rightarrow\\) `code`: Maps normalized long URLs to generated short codes to serve idempotency lookups on `Shorten()` directly from memory.
        *   `meta:{code}` \\(\rightarrow\\) JSON-serialized `MetaData`: Stores link metadata (`url` and `created_at`) to instantly fulfill both `GET /api/v1/links/{code}` queries and `GET /{code}` HTTP 302 redirects.
    *   **Read Path (Cache Hit vs. Cache Miss)**:
        1.  **Cache Hit**: Incoming redirect/metadata requests check Redis `meta:{code}`. On a hit, data is deserialized and returned in \\(O(1)\\) time (<2ms) without hitting PostgreSQL.
        2.  **Cache Miss**: On a miss, `RedisStore` delegates to `underlying.GetMetadatafromCode(code)` (PostgreSQL), serializes the result, populates Redis asynchronously with configured TTL (`-redis-ttl`), and returns the record.
    *   **Write Path & Idempotency Caching**:
        *   When `Shorten()` is invoked, Redis key `url:{normalized_url}` is queried first. If cached, the existing short code is returned immediately, maintaining 100% idempotency.
        *   If not cached, the write is executed on the primary database via `underlying.Shorten()`, and the resulting mappings (`url:` and `meta:`) are populated into Redis with TTL.
    *   **Cache Invalidation & TTL Policy**:
        *   Configurable CLI flag `-redis-ttl` (default: `24h`) controls key expiration.
        *   **Tradeoff Analysis**: Since shortened links and metadata are immutable in this application domain (a created short code never changes its target URL), complex cache invalidation is unnecessary. Keys expire gracefully via Redis memory eviction policies (`allkeys-lru`) when RAM limits are reached.

*   **CDN / Edge Caching Strategy for Redirects**:
    *   **Strategy**: Placing an Edge CDN (e.g., Cloudflare or CloudFront) in front of the Load Balancer to cache `GET /{code}` HTTP 302 responses.
    *   **HTTP Response Headers**: The origin server sets `Cache-Control: public, max-age=86400` on redirect responses.
    *   **Tradeoffs**:
        *   *Pros*: Offloads over 95% of read/redirect traffic away from application nodes directly to edge locations worldwide, yielding ultra-low response latencies (<15ms).
        *   *Cons & Stale Redirects*: If link deletion or click analytics tracking were required, edge caching would cause stale redirects until TTL expiration or manual cache purge. Given link immutability, `302 Found` edge caching offers an optimal cost-to-performance ratio.

*   **Write-Path Scaling Strategy**:
    *   **Pre-Generated Code Pool**: To eliminate DB lock contention and random code generation collision retries during high-throughput POST bursts, background worker goroutines pre-generate unique 6-character codes into a Redis Set/Queue. The `Shorten()` method pops a guaranteed unique code in \\(O(1)\\) time.
    *   **Rate Limiting**: Protects `POST /api/shorten` from abuse via a Redis-backed Sliding Window Middleware per client IP/API token.

*   **Sharding & Database Partitioning Strategy**:
    *   **Read/Redirect Partitioning**: When a single PostgreSQL or Redis node reaches memory/disk limits, keys are sharded across database nodes using consistent hashing on the short `code`.
    *   **Redis Cluster Sharding**: Uses Redis Cluster hash slots (\\(16,384\\) slots based on `CRC16(code)`). Related keys use hash tags (e.g., `{code}:meta`) to guarantee placement on the same cluster node for pipeline efficiency.
    *   **Global Idempotency Index**: Long URL lookup for idempotency across shards is routed via a distributed hash table or dedicated secondary index mapping `Hash(long_url) -> Shard_ID`.


    ---
    ## Phase 6 — Production Habits

## Dependencies
- `golang.org/x/time/rate`: Used to implement the Token Bucket rate-limiting algorithm efficiently for the POST `/api/shorten` endpoint to prevent spam and abuse.

- **Graceful Shutdown**:
  - Implemented using `signal.Notify` to trap `SIGINT` and `SIGTERM`.
  - Upon receiving the signal, `srv.Shutdown(ctx)` is called with a **10-second timeout context**. 
  - **Drain Strategy:** The server stops accepting new connections immediately but allows currently in-flight requests (such as DB writes or Redis caching) up to 10 seconds to finish successfully before terminating the process.

- **Rate Limiting**:
  - A per-IP Token Bucket rate limiter was implemented exclusively for the write path (`POST /api/shorten`).
  - **Parameters:** Each client IP is limited to 5 requests per second with a burst capacity (bucket size) of 10. Exceeding this limit returns HTTP 429 Too Many Requests.

- **Domain Policy (Blocklist)**:
  - Added a strict blocklist check before URL generation.
  - **Rules:** The domains `phishing.com`, `malware.org`, `localhost`, and `127.0.0.1` are permanently blocked. Blocking localhost/127.0.0.1 provides a fundamental layer of protection against internal SSRF (Server-Side Request Forgery) attacks. Blocked URLs return HTTP 403 Forbidden.

- **Observability & Safe Logging (Bonus Claim)**:
  - **Structured Logging:** Adopted Go 1.21's `log/slog` to output structured JSON logs to `stdout`, replacing the standard text logger for better machine readability in production environments.
  - **What is logged:** HTTP Method, sanitized Path, Status Code, IP address, and Request Duration.
  - **What is NEVER logged:** Full URLs and raw query strings (`?token=secret`). The `LoggingMiddleware` explicitly clears `r.URL.RawQuery` before logging the request to guarantee that secrets, API tokens, or user session parameters passed via URL are never persisted in server logs.
  - **pprof Profiling:** The standard `net/http/pprof` endpoints are conditionally registered behind a `-pprof` CLI flag, allowing operators to profile CPU and memory on demand without exposing debug endpoints by default.