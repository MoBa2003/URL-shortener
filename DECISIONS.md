# Design Decisions

## Part 1

- **URL Normalization**:
  Two URLs are considered "the same" if their normalized form matches. Normalization consists of trimming whitespace, converting scheme and host to lowercase, and stripping trailing slashes (except for root path `/`).

- **Idempotency Storage**:
  To guarantee that the same long URL returns the exact same code, `URLStore` maintains two synchronized maps in memory:
  1. `codeToURL map[string]string`
  2. `urlToCode map[string]string`
  Before generating a new code, `urlToCode` is checked. If the normalized URL exists, the existing code is immediately returned with status 201 Created.

- **Code Generation & Collision Handling**:
  New codes are 6-character URL-safe strings generated randomly using `crypto/rand` from the character set `[a-zA-Z0-9]`. When a new URL is processed, the system checks if the generated code already exists in `codeToURL`. If a collision occurs, it retries up to 10 times to generate a unique code.

- **Locking Choice**:
  `sync.RWMutex` is chosen for `URLStore`. Reads (`Get` and checking `urlToCode`) acquire `RLock()`, allowing high concurrent throughput for redirects. Writes (`Shorten` insertions) acquire `Lock()`. Double-checking is applied after acquiring the write lock to prevent race conditions during concurrent requests for the same URL.

- **Package Layout**:
  - `cmd/server/main.go`: Thin entrypoint parsing `-addr` and `-base` flags and starting `http.Server`.
  - `internal/shortener/`: Core business logic, store, handlers, and unit/integration tests.
