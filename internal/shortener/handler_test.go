package shortener

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestShortenAndRedirect_Integration(t *testing.T) {
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

func TestIdempotency_Integration(t *testing.T) {
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

func TestTableErrors_Integration(t *testing.T) {
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

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
		})
	}
}

func TestConcurrentShorten_Integration(t *testing.T) {
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

// Test Handler by isolating the Store (Specifically to Test Handler Behaviour During All Kinds of Errors)
func TestShortenHandler_AllCases(t *testing.T) {
	tests := []struct {
		name           string
		reqBody        string
		setupFake      func(f *FakeStore)
		expectedStatus int
		checkResponse  func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:    "OkPath - Success 201 Created",
			reqBody: `{"url":"https://example.com/test-path"}`,
			setupFake: func(f *FakeStore) {

			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var resp ShortenResponse
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode response JSON: %v", err)
				}
				if resp.Code == "" {
					t.Error("expected non-empty code")
				}
				if resp.ShortURL != "http://localhost:8080/"+resp.Code {
					t.Errorf("unexpected short_url: got %s", resp.ShortURL)
				}
			},
		},
		{
			name:           "Invalid Body JSON - 400 Bad Request",
			reqBody:        `{invalid-json}`,
			setupFake:      func(f *FakeStore) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse:  func(t *testing.T, rec *httptest.ResponseRecorder) {},
		},
		{
			name:           "Invalid URL Scheme - 400 Bad Request",
			reqBody:        `{"url":"ftp://invalid-scheme.com"}`,
			setupFake:      func(f *FakeStore) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse:  func(t *testing.T, rec *httptest.ResponseRecorder) {},
		},
		{
			name:    "Internal Store Error - 500 Internal Server Error",
			reqBody: `{"url":"https://example.com/ok"}`,
			setupFake: func(f *FakeStore) {
				f.ShortenErr = errors.New("unexpected database crash")
			},
			expectedStatus: http.StatusInternalServerError,
			checkResponse:  func(t *testing.T, rec *httptest.ResponseRecorder) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := NewFakeStore()
			tt.setupFake(fake)

			handler := NewHandler(fake, "http://localhost:8080")

			req := httptest.NewRequest("POST", "/api/shorten", bytes.NewBufferString(tt.reqBody))
			rec := httptest.NewRecorder()

			handler.Shorten(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
			tt.checkResponse(t, rec)
		})
	}
}

func TestRedirectHandler_AllCases(t *testing.T) {
	tests := []struct {
		name           string
		codeParam      string
		setupFake      func(f *FakeStore)
		expectedStatus int
		expectedLoc    string
	}{
		{
			name:      "OkPath - Success 302 Found",
			codeParam: "valid123",
			setupFake: func(f *FakeStore) {
				f.SetMetadata("valid123", MetaData{
					Longurl:   "https://example.com/target",
					CreatedAt: time.Now().UTC(),
				})
			},
			expectedStatus: http.StatusFound,
			expectedLoc:    "https://example.com/target",
		},
		{
			name:           "Code Not Found - 404 Not Found",
			codeParam:      "nonexistent",
			setupFake:      func(f *FakeStore) {},
			expectedStatus: http.StatusNotFound,
			expectedLoc:    "",
		},
		{
			name:      "Internal Store Error - 500 Internal Server Error",
			codeParam: "valid123",
			setupFake: func(f *FakeStore) {
				f.GetErr = errors.New("disk read error")
			},
			expectedStatus: http.StatusInternalServerError,
			expectedLoc:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := NewFakeStore()
			tt.setupFake(fake)

			handler := NewHandler(fake, "http://localhost:8080")

			req := httptest.NewRequest("GET", "/"+tt.codeParam, nil)
			if tt.codeParam != "" {
				req.SetPathValue("code", tt.codeParam)
			}
			rec := httptest.NewRecorder()

			handler.Redirect(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			if tt.expectedLoc != "" {
				loc := rec.Header().Get("Location")
				if loc != tt.expectedLoc {
					t.Errorf("expected Location header %s, got %s", tt.expectedLoc, loc)
				}
			}
		})
	}
}

func TestGetMetaDataHandler_AllCases(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name           string
		codeParam      string
		setupFake      func(f *FakeStore)
		expectedStatus int
		checkResponse  func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:      "OkPath - Success 200 OK",
			codeParam: "meta123",
			setupFake: func(f *FakeStore) {
				f.SetMetadata("meta123", MetaData{
					Longurl:   "https://golang.org/doc",
					CreatedAt: now,
				})
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				var meta MetaData
				if err := json.NewDecoder(rec.Body).Decode(&meta); err != nil {
					t.Fatalf("failed to decode metadata JSON: %v", err)
				}
				if meta.Longurl != "https://golang.org/doc" {
					t.Errorf("expected URL https://golang.org/doc, got %s", meta.Longurl)
				}
				if !meta.CreatedAt.Equal(now) {
					t.Errorf("expected CreatedAt %v, got %v", now, meta.CreatedAt)
				}
			},
		},
		{
			name:           "Code Not Found - 404 Not Found",
			codeParam:      "unknown_code",
			setupFake:      func(f *FakeStore) {},
			expectedStatus: http.StatusNotFound,
			checkResponse:  func(t *testing.T, rec *httptest.ResponseRecorder) {},
		},
		{
			name:      "Internal Store Error - 500 Internal Server Error",
			codeParam: "meta123",
			setupFake: func(f *FakeStore) {
				f.GetErr = errors.New("database connection timeout")
			},
			expectedStatus: http.StatusInternalServerError,
			checkResponse:  func(t *testing.T, rec *httptest.ResponseRecorder) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := NewFakeStore()
			tt.setupFake(fake)

			handler := NewHandler(fake, "http://localhost:8080")

			req := httptest.NewRequest("GET", "/api/v1/links/"+tt.codeParam, nil)
			if tt.codeParam != "" {
				req.SetPathValue("code", tt.codeParam)
			}
			rec := httptest.NewRecorder()

			handler.GetMetaData(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}
			tt.checkResponse(t, rec)
		})
	}
}
