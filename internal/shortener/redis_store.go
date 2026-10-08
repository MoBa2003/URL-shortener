package shortener

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client     *redis.Client
	underlying Store
	ttl        time.Duration
}

func NewRedisStore(redisAddr string, password string, db int, underlying Store, ttl time.Duration) (*RedisStore, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis at %s: %w", redisAddr, err)
	}

	return &RedisStore{
		client:     rdb,
		underlying: underlying,
		ttl:        ttl,
	}, nil
}

func (r *RedisStore) Shorten(rawURL string) (string, error) {
	normURL, err := NormalizeURL(rawURL)
	if err != nil {
		return "", err
	}

	ctx := context.Background()
	urlKey := "url:" + normURL

	if cachedCode, err := r.client.Get(ctx, urlKey).Result(); err == nil && cachedCode != "" {
		// panic(cachedCode)
		return cachedCode, nil
	}

	code, err := r.underlying.Shorten(normURL)
	if err != nil {
		return "", err
	}

	meta, err := r.underlying.GetMetadatafromCode(code)
	if err == nil {
		metaBytes, _ := json.Marshal(meta)
		_ = r.client.Set(ctx, "code:"+code, normURL, r.ttl).Err()
		_ = r.client.Set(ctx, "meta:"+code, metaBytes, r.ttl).Err()
		_ = r.client.Set(ctx, urlKey, code, r.ttl).Err()
	}

	return code, nil
}

func (r *RedisStore) GetMetadatafromCode(code string) (MetaData, error) {
	ctx := context.Background()
	metaKey := "meta:" + code

	val, err := r.client.Get(ctx, metaKey).Result()
	if err == nil && val != "" {
		var meta MetaData
		if json.Unmarshal([]byte(val), &meta) == nil {
			return meta, nil
		}
	}

	meta, err := r.underlying.GetMetadatafromCode(code)
	if err != nil {
		return MetaData{}, err
	}

	metaBytes, _ := json.Marshal(meta)
	_ = r.client.Set(ctx, metaKey, metaBytes, r.ttl).Err()

	return meta, nil
}
