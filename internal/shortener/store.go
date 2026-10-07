package shortener

import (
	"crypto/rand"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

type MetaData struct {
	Longurl   string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

type URLStore struct {
	codetoMetaData map[string]MetaData
	urltoCode      map[string]string
	mu             sync.RWMutex
}

func NewURLStore() *URLStore {
	return &URLStore{
		codetoMetaData: make(map[string]MetaData),
		urltoCode:      make(map[string]string),
	}
}

func (store *URLStore) Shorten(rawurl string) (string, error) {
	normalizedurl, err := NormalizeURL(rawurl)
	if err != nil {
		return "", err
	}

	store.mu.RLock()

	if code, exists := store.urltoCode[normalizedurl]; exists {
		return code, nil
	}
	store.mu.RUnlock()

	store.mu.Lock()
	defer store.mu.Unlock()

	if code, exists := store.urltoCode[normalizedurl]; exists {
		return code, nil
	}

	generatedcode := ""
	for attempts := 0; attempts < 10; attempts++ {
		code, err := GenerateCode(6)
		if err != nil {
			return "", err
		}
		if _, exists := store.codetoMetaData[code]; !exists {
			generatedcode = code
			break
		}

	}

	if generatedcode == "" {
		return "", CodeGenerationFailed
	}

	store.urltoCode[normalizedurl] = generatedcode
	store.codetoMetaData[generatedcode] = MetaData{Longurl: normalizedurl, CreatedAt: time.Now().UTC()}
	return generatedcode, nil
}

func (store *URLStore) GetMetadatafromCode(code string) (MetaData, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if metadata, exists := store.codetoMetaData[code]; exists {
		return metadata, nil
	}
	return MetaData{}, NotFoundErr
}

func NormalizeURL(rawurl string) (string, error) {
	trimmed := strings.TrimSpace(rawurl)
	if trimmed == "" {
		return "", InvalidURLErr
	}
	parsed, err := url.Parse(trimmed)

	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", InvalidURLErr
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)

	if parsed.Path != "/" {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}
	return parsed.String(), nil
}

func GenerateCode(length int) (string, error) {
	b := make([]byte, length)
	for i := range b {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		b[i] = charset[num.Int64()]
	}
	return string(b), nil
}
