package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRenderIndexHTMLPunctuation(t *testing.T) {
	tpl := "__PUNCT__"
	cases := []struct {
		name string
		path string
		want string
	}{
		{name: "no punctuation", path: "/joao", want: "!"},
		{name: "plain question mark", path: "/joao?", want: ""},
		{name: "encoded question mark", path: "/joao%3F", want: ""},
		{name: "encoded exclamation", path: "/joao%21", want: ""},
		{name: "encoded ellipsis", path: "/joao%E2%80%A6", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderIndexHTML(tpl, tc.path)
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestEncodeDecodePathSegment(t *testing.T) {
	message := "é SD, mas cadê suas AH?"
	encoded := encodePathSegment(message)
	if !strings.Contains(strings.ToLower(encoded), "%3f") {
		t.Fatalf("expected encoded path to include %%3F, got %q", encoded)
	}
	decoded := decodePath(encoded)
	if decoded != message {
		t.Fatalf("expected decodePath to round-trip. want %q got %q", message, decoded)
	}
}

func TestBlockedMessage(t *testing.T) {
	blockedOnce = sync.Once{}
	blockedOnce.Do(func() {})
	blockedTerms = []string{"muito ruim", "palavra"}

	if !isBlockedMessage("Isso é muito ruim") {
		t.Fatalf("expected message to be blocked")
	}
	if isBlockedMessage("Tudo certo aqui") {
		t.Fatalf("expected message to be allowed")
	}
}

func TestOgImageQueueSerializes(t *testing.T) {
	oldRender := renderOgImageToFileFunc
	defer func() {
		renderOgImageToFileFunc = oldRender
	}()

	tmp := t.TempDir()
	if err := os.Setenv("XDG_CACHE_DIR", tmp); err != nil {
		t.Fatalf("set env: %v", err)
	}

	var mu sync.Mutex
	current := 0
	maxConcurrent := 0

	renderOgImageToFileFunc = func(text, destPath string) error {
		mu.Lock()
		current++
		if current > maxConcurrent {
			maxConcurrent = current
		}
		mu.Unlock()

		time.Sleep(50 * time.Millisecond)
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(destPath, []byte("png"), 0o644); err != nil {
			return err
		}

		mu.Lock()
		current--
		mu.Unlock()
		return nil
	}

	q := newOgImageQueue()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := q.render("first", "primeiro"); err != nil {
			t.Errorf("render first: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := q.render("second", "segundo"); err != nil {
			t.Errorf("render second: %v", err)
		}
	}()
	wg.Wait()

	if maxConcurrent != 1 {
		t.Fatalf("expected serialized rendering, max concurrent=%d", maxConcurrent)
	}
}
