package shortener

import (
	"errors"
	"sync"
	"testing"
)

func getTestDSN() string {
	return "host=localhost user=postgres password=postgres dbname=urlshortener_test port=5432 sslmode=disable"
}

func setupTestStore(t *testing.T) *PostgresStore {
	t.Helper()

	dsn := getTestDSN()
	store, err := NewPostgresStore(dsn)
	if err != nil {
		t.Skipf("Skipping PostgreSQL tests: failed to connect to test database (%v)", err)
	}

	if err := store.db.Exec("TRUNCATE TABLE url_models").Error; err != nil {
		t.Fatalf("failed to truncate url_models table: %v", err)
	}

	return store
}

func TestPostgresStore_RestartSimulation(t *testing.T) {
	dsn := getTestDSN()

	store1 := setupTestStore(t)

	targetURL := "https://example.com/persistent-path"

	code1, err := store1.Shorten(targetURL)
	if err != nil {
		t.Fatalf("failed to shorten URL in store1: %v", err)
	}

	store2, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("failed to re-connect to database in store2: %v", err)
	}

	retrievedData, err := store2.GetMetadatafromCode(code1)
	if err != nil {
		t.Fatalf("failed to get long URL from store2 after restart: %v", err)
	}
	retrievedURL := retrievedData.Longurl
	if retrievedURL != "https://example.com/persistent-path" {
		t.Errorf("expected URL https://example.com/persistent-path, got %s", retrievedURL)
	}

	code2, err := store2.Shorten("https://example.com/persistent-path")
	if err != nil {
		t.Fatalf("failed to shorten identical URL in store2: %v", err)
	}

	if code1 != code2 {
		t.Errorf("idempotency violated across restarts: expected code %s, got %s", code1, code2)
	}
}

func TestPostgresStore_ValidationAndErrors(t *testing.T) {
	store := setupTestStore(t)

	_, errMeta := store.GetMetadatafromCode("nonexistent")
	if !errors.Is(errMeta, NotFoundErr) {
		t.Errorf("expected ErrNotFound for metadata, got %v", errMeta)
	}

	_, errInvalid := store.Shorten("invalid-scheme-url")
	if !errors.Is(errInvalid, InvalidURLErr) {
		t.Errorf("expected ErrInvalidURL, got %v", errInvalid)
	}
}
func TestPostgresStore_ConcurrentShorten(t *testing.T) {
	store := setupTestStore(t)

	const numGoroutines = 20
	const targetURL = "https://example.com/concurrent-db-test"

	var wg sync.WaitGroup
	codes := make(chan string, numGoroutines)
	errorsChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, err := store.Shorten(targetURL)
			if err != nil {
				errorsChan <- err
				return
			}
			codes <- code
		}()
	}

	wg.Wait()
	close(codes)
	close(errorsChan)

	for err := range errorsChan {
		t.Fatalf("unexpected error during concurrent shorten: %v", err)
	}

	var firstCode string
	for code := range codes {
		if firstCode == "" {
			firstCode = code
		} else if code != firstCode {
			t.Fatalf("concurrent idempotency broken in database: expected %s, got %s", firstCode, code)
		}
	}
}
