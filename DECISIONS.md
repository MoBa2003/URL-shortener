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
        Get(code string) (string, error)
        GetMetadatafromCode(code string) (MetaData, error)
    }
    ```
  - **Rationale**: Decouples the HTTP handlers from concrete storage implementations (`URLStore`). This allows seamless substitution of persistent storage engines (e.g., SQL/GORM or File-based store in Part 4) or test doubles (`FakeStore`) without altering a single line of HTTP handler logic.

- **Domain Sentinel Errors & Error Mapping Strategy**:
  - **Sentinel Errors**: Defined `ErrNotFound` and `ErrInvalidURL` at the store/domain layer.
  - **Error Inspection**: Handlers inspect underlying errors using `errors.Is(err, ErrNotFound)` and `errors.Is(err, ErrInvalidURL)` after error wrapping (`%w`).
  - **HTTP Status Mapping**:
    - `ErrInvalidURL` \\(\rightarrow\\) **HTTP 400 Bad Request** with structured JSON error response.
    - `ErrNotFound` \\(\rightarrow\\) **HTTP 404 Not Found** with structured JSON error response.
    - Unhandled internal errors \\(\rightarrow\\) **HTTP 500 Internal Server Error**.

- **Isolated HTTP Testing via `FakeStore`**:
  - **Rationale**: The concrete `URLStore` relies on non-deterministic code generation (`crypto/rand`) and cannot easily simulate unexpected storage failures (such as database read timeouts or disk I/O errors).
  - **Implementation**: Created `FakeStore` implementing the `Store` interface. It includes error injection fields (`ShortenErr`, `GetErr`) and direct state setup helpers (`SetMetadata`).
  - **Outcome**: Allows 100% deterministic unit tests for all HTTP status codes (200, 201, 302, 400, 404, 500) while keeping HTTP layer tests isolated from concrete storage logic.

- **Preservation of Idempotency**:
  - Idempotency logic (`urlToCode` lookup) is preserved within the store implementations behind the `Store` interface. Re-submitting an identical normalized URL consistently returns the original short code and metadata across all API callers.
