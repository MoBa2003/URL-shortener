package shortener

import (
	"sync"
	"time"
)

type FakeStore struct {
	mu             sync.RWMutex
	codeToMetadata map[string]MetaData
	urlToCode      map[string]string

	ShortenErr error
	GetErr     error
}

func NewFakeStore() *FakeStore {
	return &FakeStore{
		codeToMetadata: make(map[string]MetaData),
		urlToCode:      make(map[string]string),
	}
}

func (f *FakeStore) Shorten(rawURL string) (string, error) {

	if f.ShortenErr != nil {
		return "", f.ShortenErr
	}

	normURL, err := NormalizeURL(rawURL)
	if err != nil {
		return "", err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if code, exists := f.urlToCode[normURL]; exists {
		return code, nil
	}

	code := "fake" + string(rune('1'+len(f.codeToMetadata)))

	meta := MetaData{
		Longurl:   normURL,
		CreatedAt: time.Now().UTC(),
	}

	f.codeToMetadata[code] = meta
	f.urlToCode[normURL] = code

	return code, nil
}

func (f *FakeStore) GetMetadatafromCode(code string) (MetaData, error) {
	if f.GetErr != nil {
		return MetaData{}, f.GetErr
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	meta, exists := f.codeToMetadata[code]
	if !exists {
		return MetaData{}, NotFoundErr
	}

	return meta, nil
}

func (f *FakeStore) SetMetadata(code string, meta MetaData) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codeToMetadata[code] = meta
	f.urlToCode[meta.Longurl] = code
}
