package shortener

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func getTestRedisAddr() string {
	if addr := os.Getenv("TEST_REDIS_ADDR"); addr != "" {
		return addr
	}
	return "localhost:6379"
}

func setupRedisTestStore(t *testing.T) (*RedisStore, *FakeStore) {
	t.Helper()

	fake := NewFakeStore()
	redisAddr := getTestRedisAddr()

	redisStore, err := NewRedisStore(redisAddr, "", 1, fake, 5*time.Minute)
	if err != nil {
		t.Skipf("Skipping Redis tests: server not reachable at %s (%v)", redisAddr, err)
	}

	_ = redisStore.client.FlushDB(t.Context()).Err()

	return redisStore, fake
}

func TestRedisStore_ExplicitCacheHitProof(t *testing.T) {
	redisStore, fakeStore := setupRedisTestStore(t)
	targetURL, _ := NormalizeURL("https://example.com/redis-proof-test")

	code, err := redisStore.Shorten(targetURL)
	if err != nil {
		t.Fatalf("failed to shorten via redis store: %v", err)
	}

	delete(fakeStore.urlToCode, targetURL)
	delete(fakeStore.codeToMetadata, code)

	retrievedMetadata, err := redisStore.GetMetadatafromCode(code)
	if err != nil {
		t.Fatalf("EXPECTED CACHE HIT: failed to get long URL even though it was cached: %v", err)
	}
	// panic(retrievedMetadata)
	if retrievedMetadata.Longurl != targetURL {
		// panic("s")
		t.Errorf("expected %s from cache, got %s", targetURL, retrievedMetadata.Longurl)
	}

	uncashed_url, _ := NormalizeURL("https://example.com/uncached")
	_ = redisStore.client.FlushDB(t.Context()).Err()
	uncached_code, _ := fakeStore.Shorten(uncashed_url)
	uncachedmeta, err := redisStore.GetMetadatafromCode(uncached_code)
	if err != nil || uncachedmeta.Longurl != uncashed_url {
		t.Fatalf("failed on cache miss fallback to database: %v", err)
	}

	cachedVal, err := redisStore.client.Get(t.Context(), "meta:"+uncached_code).Result()
	var metadata MetaData
	json.Unmarshal([]byte(cachedVal), &metadata)
	if err != nil || metadata.Longurl != uncashed_url {
		t.Errorf("expected redis to be populated after cache miss, got err: %v", cachedVal)
		// log.Println("salam   ", cachedVal)
	}
}
