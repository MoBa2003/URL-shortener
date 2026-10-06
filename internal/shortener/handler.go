package shortener

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type ShortenRequest struct {
	URL string `json:"url"`
}

type ShortenResponse struct {
	Code     string `json:"code"`
	ShortURL string `json:"short_url"`
}

type Handler struct {
	store   *URLStore
	baseURL string
}

func NewHandler(store *URLStore, baseURL string) *Handler {
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
	if code == "" {
		http.Error(w, `{"error":"missing code"}`, http.StatusBadRequest)
		return
	}

	longurl, err := handler.store.GetURLfromCode(code)
	if err != nil {
		if errors.Is(err, NotFoundErr) {
			http.Error(w, `{"error":"code not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, longurl, http.StatusFound)
}
