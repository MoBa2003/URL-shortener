package shortener

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *responseRecorder) WriteHeader(statusCode int) {
	rec.statusCode = statusCode
	rec.ResponseWriter.WriteHeader(statusCode)
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		cleanURL := *r.URL
		cleanURL.RawQuery = ""

		rec := &responseRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // پیش‌فرض
		}

		next.ServeHTTP(rec, r)

		duration := time.Since(start)

		slog.Info("HTTP Request",
			slog.String("method", r.Method),
			slog.String("path", cleanURL.Path),
			slog.Int("status", rec.statusCode),
			slog.String("duration", duration.String()),
			slog.String("ip", strings.Split(r.RemoteAddr, ":")[0]),
		)
	})
}

var (
	visitors = make(map[string]*rate.Limiter)
	mu       sync.Mutex
)

func getVisitor(ip string) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()

	limiter, exists := visitors[ip]
	if !exists {
		limiter = rate.NewLimiter(rate.Limit(5), 10)
		visitors[ip] = limiter
	}
	return limiter
}

func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := strings.Split(r.RemoteAddr, ":")[0]
		limiter := getVisitor(ip)

		if !limiter.Allow() {
			http.Error(w, `{"error":"too many requests"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
