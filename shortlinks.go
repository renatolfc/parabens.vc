package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type shortlinkStore struct {
	mu     sync.Mutex
	loaded bool
	byCode map[string]string
	byPath map[string]string
}

var shortlinks = shortlinkStore{
	byCode: map[string]string{},
	byPath: map[string]string{},
}

var shortlinkLimiter = &rateLimiter{
	hits:   map[string][]time.Time{},
	window: shortlinkRateWindow,
	max:    shortlinkRateLimit,
}

func shortlinkResponse(code, path string) ShortLinkResponse {
	cleanPath := strings.TrimPrefix(strings.TrimSpace(path), "/")
	base := strings.TrimRight(publicBaseURL(), "/")
	shortURL := base + "/s/" + code
	destPath := encodePathSegment(cleanPath)
	destination := base + "/" + destPath
	return ShortLinkResponse{
		Code:        code,
		ShortURL:    shortURL,
		Path:        cleanPath,
		Destination: destination,
	}
}

func ensureShortlinksLoaded() error {
	shortlinks.mu.Lock()
	if shortlinks.loaded {
		shortlinks.mu.Unlock()
		return nil
	}
	shortlinks.mu.Unlock()

	path := shortlinkDBPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			shortlinks.mu.Lock()
			shortlinks.loaded = true
			shortlinks.mu.Unlock()
			return nil
		}
		return err
	}

	var entries map[string]string
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	shortlinks.mu.Lock()
	defer shortlinks.mu.Unlock()
	if !shortlinks.loaded {
		shortlinks.byCode = entries
		shortlinks.byPath = make(map[string]string)
		for code, path := range entries {
			shortlinks.byPath[path] = code
		}
		shortlinks.loaded = true
	}
	return nil
}

func persistShortlinksLocked() error {
	path := shortlinkDBPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(shortlinks.byCode, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func shortlinkDBPath() string {
	if value := os.Getenv("SHORTLINK_DB"); value != "" {
		return value
	}
	return "data/shortlinks.json"
}

func generateCode(length int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}
