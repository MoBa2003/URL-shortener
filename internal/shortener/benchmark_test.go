package shortener

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func BenchmarkShorten(b *testing.B) {
	store := NewURLStore()
	handler := NewHandler(store, "http://localhost:8080")
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/api/shorten", bytes.NewBufferString(`{"url":"https://example.com/bench"}`))
		rec := httptest.NewRecorder()
		handler.Shorten(rec, req)
	}

}

func BenchmarkRedirect(b *testing.B) {
	store := NewURLStore()
	handler := NewHandler(store, "http://localhost:8080")

	code, err := handler.store.Shorten("http://example.com/redirect")
	if err != nil {
		b.Fatalf("Failed to Shorten : %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/"+code, nil)
		req.SetPathValue("code", code)
		rec := httptest.NewRecorder()
		handler.Redirect(rec, req)
	}
}

func BenchmarkGetMetaData(b *testing.B) {
	store := NewURLStore()
	handler := NewHandler(store, "http://localhost:8080")

	code, err := handler.store.Shorten("https://example.com/getmetadata")
	if err != nil {
		b.Fatalf("faled to shorten: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/api/v1/links/"+code, nil)
		req.SetPathValue("code", code)
		rec := httptest.NewRecorder()
		handler.GetMetaData(rec, req)
	}
}
