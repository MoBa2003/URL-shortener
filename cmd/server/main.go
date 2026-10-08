package main

import (
	"flag"
	"log"
	"net/http"
	"time"
	"urlshortener/internal/shortener"
)

var postgres_dbconfig = shortener.NewDBConfig("localhost", "postgres", "postgres", "urlshortener", "5432", "disable")

func main() {

	addr := flag.String("addr", ":8080", "HTTP server listen address")
	baseurl := flag.String("base", "http://localhost:8080", "Base URL for short links")
	store_type := flag.String("store", "memory", "store type: 'memory' or 'postgres'")
	dsn := flag.String("dsn", postgres_dbconfig.GetFormattedString(), "postgres DSN string")
	redisAddr := flag.String("redis-addr", "", "Redis server address (e.g., localhost:6379). If provided, enables Redis Caching.")
	redisPass := flag.String("redis-pass", "", "Redis password")
	redisTTL := flag.Duration("redis-ttl", 24*time.Hour, "Cache TTL duration for Redis")

	flag.Parse()

	var baseStore shortener.Store
	var err error

	switch *store_type {
	case "postgres":
		log.Println("Initializing Postgres DB ...")
		baseStore, err = shortener.NewPostgresStore(*dsn)
		if err != nil {
			log.Fatalf("Failed to Initialize Postgres : %v", err)
		}
	case "memory":
		log.Println("Initializing in_memory store...")
		baseStore = shortener.NewURLStore()
	default:
		log.Fatalf("Unknown store type , please Enter the Store type (postgres or memory)")
	}

	finalStore := baseStore

	if *redisAddr != "" {
		log.Printf("Wrapping store with Redis Caching Layer (addr: %s, ttl: %v)...", *redisAddr, *redisTTL)
		redisStore, err := shortener.NewRedisStore(*redisAddr, *redisPass, 0, baseStore, *redisTTL)
		if err != nil {
			log.Fatalf("Failed to initialize Redis store: %v", err)
		}
		finalStore = redisStore
	}

	handler := shortener.NewHandler(finalStore, *baseurl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shorten", handler.Shorten)
	mux.HandleFunc("GET /{code}", handler.Redirect)
	mux.HandleFunc("GET /api/v1/links/{code}", handler.GetMetaData)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("Server Listening on %s (base URL: %s)", *addr, *baseurl)

	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server Failed to Start: %v", err)
	}

}
