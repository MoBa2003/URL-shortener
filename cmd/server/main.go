package main

import (
	"flag"
	"log"
	"net/http"
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

	flag.Parse()

	var store shortener.Store
	var err error

	switch *store_type {
	case "postgres":
		log.Println("Initializing Postgres DB ...")
		store, err = shortener.NewPostgresStore(*dsn)
		if err != nil {
			log.Fatalf("Failed to Initialize Postgres : %v", err)
		}
	case "memory":
		log.Println("Initializing in_memory store...")
		store = shortener.NewURLStore()
	default:
		log.Fatalf("Unknown store type , please Enter the Store type (postgres or memory)")
	}

	handler := shortener.NewHandler(store, *baseurl)

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

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("Server Listening on %s (base URL: %s)", *addr, *baseurl)

		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("Server Failed to Start: %v", err)
		}
	}()

	<-quit
	if pgStore, ok := store.(*shortener.PostgresStore); ok {
		if err := pgStore.Close(); err != nil {
			log.Printf("Failed to close database connection")
		} else {
			log.Printf("Database connection closed")
		}
	}

}
