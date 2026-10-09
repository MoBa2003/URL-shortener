package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
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
	enablePprof := flag.Bool("pprof", false, "Enable pprof profiling endpoints")

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
	mux.Handle("POST /api/shorten", shortener.RateLimitMiddleware(http.HandlerFunc(handler.Shorten)))
	mux.HandleFunc("GET /{code}", handler.Redirect)
	mux.HandleFunc("GET /api/v1/links/{code}", handler.GetMetaData)

	if *enablePprof {
		slog.Info("pprof profiling enabled at /debug/pprof/")
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	loggedMux := shortener.LoggingMiddleware(mux)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           loggedMux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		slog.Info("Server Listening", "addr", *addr, "base", *baseurl)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server Failed to Start: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down server gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}
	slog.Info("Server stopped cleanly.")

	if pgStore, ok := baseStore.(*shortener.PostgresStore); ok {
		if err := pgStore.Close(); err != nil {
			slog.Error("Failed to close database connection", "error", err)
		} else {
			slog.Info("Database connection closed cleanly.")
		}
	}

	slog.Info("Application exited.")

}
