package shortener

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

var blockedDomains = map[string]bool{
	"phishing.com": true,
	"malware.org":  true,
	"localhost":    true,
	"127.0.0.1":    true,
}

func isBlocked(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	return blockedDomains[strings.ToLower(parsed.Hostname())]
}

type Store interface {
	Shorten(rawurl string) (string, error)
	GetMetadatafromCode(code string) (MetaData, error)
}

type ShortenRequest struct {
	URL string `json:"url"`
}

type ShortenResponse struct {
	Code     string `json:"code"`
	ShortURL string `json:"short_url"`
}

type Handler struct {
	store   Store
	baseURL string
}

func NewHandler(store Store, baseURL string) *Handler {
	baseURL = strings.TrimSuffix(baseURL, "/")
	return &Handler{
		store:   store,
		baseURL: baseURL,
	}
}

func (handler *Handler) Shorten(w http.ResponseWriter, r *http.Request) {
	var req ShortenRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(&req)
	if err != nil || req.URL == "" {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}

	if isBlocked(req.URL) {
		http.Error(w, `{"error":"domain is not allowed"}`, http.StatusForbidden)
		return
	}

	code, err := handler.store.Shorten(req.URL)
	if err != nil {
		if errors.Is(err, InvalidURLErr) {
			http.Error(w, `{"error":"invalid URL: missing scheme or invalid format"}`, http.StatusBadRequest)
			return
		}
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return

	}
	resp := ShortenResponse{
		Code:     code,
		ShortURL: handler.baseURL + "/" + code,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

func (handler *Handler) Redirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	metadata, err := handler.store.GetMetadatafromCode(code)
	if err != nil {
		if errors.Is(err, NotFoundErr) {
			http.Error(w, `{"error":"code not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, metadata.Longurl, http.StatusFound)
}

func (handler *Handler) GetMetaData(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	metadata, err := handler.store.GetMetadatafromCode(code)
	if err != nil {
		if errors.Is(err, NotFoundErr) {
			http.Error(w, `{"error":"code not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(metadata)
}
