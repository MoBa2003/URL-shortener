package shortener

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestShortenAndRedirect(t *testing.T) {
	store := NewURLStore()
	handler := NewHandler(store, "http://localhost:8080")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shorten", handler.Shorten)
	mux.HandleFunc("GET /{code}", handler.Redirect)

	reqBody := `{"url":"https://go.dev/doc/"}`
	req := httptest.NewRequest("POST", "/api/shorten", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d", rec.Code)
	}

	var resp ShortenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if resp.Code == "" || resp.ShortURL != "http://localhost:8080/"+resp.Code {
		t.Fatalf("unexpected response structure: %+v", resp)
	}

	redReq := httptest.NewRequest("GET", "/"+resp.Code, nil)
	redRec := httptest.NewRecorder()

	mux.ServeHTTP(redRec, redReq)

	if redRec.Code != http.StatusFound {
		t.Fatalf("expected status 302 Found, got %d", redRec.Code)
	}

	location := redRec.Header().Get("Location")
	if location != "https://go.dev/doc" {
		t.Fatalf("expected Location https://go.dev/doc, got %s", location)
	}
}

func TestIdempotency(t *testing.T) {
	store := NewURLStore()
	handler := NewHandler(store, "http://localhost:8080")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shorten", handler.Shorten)

	targetURL := "https://example.com/idempotent-test"

	req1 := httptest.NewRequest("POST", "/api/shorten", bytes.NewBufferString(`{"url":"`+targetURL+`"}`))
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)

	var resp1 ShortenResponse
	_ = json.Unmarshal(rec1.Body.Bytes(), &resp1)

	req2 := httptest.NewRequest("POST", "/api/shorten", bytes.NewBufferString(`{"url":"`+targetURL+`"}`))
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	var resp2 ShortenResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)

	if rec2.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created on second request, got %d", rec2.Code)
	}

	if resp1.Code != resp2.Code {
		t.Fatalf("idempotency failed: expected same code %s, got %s", resp1.Code, resp2.Code)
	}
}

func TestTableErrors(t *testing.T) {
	store := NewURLStore()
	handler := NewHandler(store, "http://localhost:8080")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shorten", handler.Shorten)
	mux.HandleFunc("GET /{code}", handler.Redirect)

	tests := []struct {
		name           string
		method         string
		path           string
		body           string
		expectedStatus int
	}{
		{
			name:           "Empty URL",
			method:         "POST",
			path:           "/api/shorten",
			body:           `{"url":""}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid scheme (ftp)",
			method:         "POST",
			path:           "/api/shorten",
			body:           `{"url":"ftp://example.com"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid URL format",
			method:         "POST",
			path:           "/api/shorten",
			body:           `{"url":"not-a-url"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Unknown Code Redirect",
			method:         "GET",
			path:           "/nonexist",
			body:           "",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			fmt.Println(tt.name, " ", rec.Code)
			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestConcurrentShorten(t *testing.T) {
	store := NewURLStore()
	handler := NewHandler(store, "http://localhost:8080")

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shorten", handler.Shorten)

	const numRequests = 50
	const exampleURL = "https://example.com/concurrent"

	var wg sync.WaitGroup
	codes := make(chan string, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/api/shorten", bytes.NewBufferString(`{"url":"`+exampleURL+`"}`))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code == http.StatusCreated {
				var resp ShortenResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err == nil {
					codes <- resp.Code
				}
			}
		}()
	}

	wg.Wait()
	close(codes)

	var firstCode string
	for code := range codes {
		if firstCode == "" {
			firstCode = code
		} else if code != firstCode {
			t.Fatalf("concurrent idempotency failed: expected %s, got %s", firstCode, code)
		}
	}
}
