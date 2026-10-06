package main

import (
	"flag"
	"log"
	"net/http"
	"urlshortener/internal/shortener"
)

func main() {

	addr := flag.String("addr", ":8080", "HTTP server listen address")
	baseurl := flag.String("base", "http://localhost:8080", "Base URL for short links")

	flag.Parse()

	store := shortener.NewURLStore()
	handler := shortener.NewHandler(store, *baseurl)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shorten", handler.Shorten)
	mux.HandleFunc("GET /{code}", handler.Redirect)
	mux.HandleFunc("GET /api/v1/links/{code}", handler.GetMetaData)

	log.Printf("Server Listening on %s (base URL: %s)", *addr, *baseurl)

	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("Server Failed to Start: %v", err)
	}

}
