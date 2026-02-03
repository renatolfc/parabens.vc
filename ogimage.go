package main

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type ogImageJob struct {
	key  string
	text string
	done chan error
}

type ogImageQueue struct {
	jobs chan ogImageJob
}

var ogQueue = newOgImageQueue()

var renderOgImageToFileFunc = renderOgImageToFile

func newOgImageQueue() *ogImageQueue {
	q := &ogImageQueue{jobs: make(chan ogImageJob, 32)}
	go q.run()
	return q
}

func (q *ogImageQueue) run() {
	for job := range q.jobs {
		cachePath := ogCachePath(job.key)
		if ok, err := fileExists(cachePath); ok && err == nil {
			job.done <- nil
			continue
		}
		job.done <- renderOgImageToFileFunc(job.text, cachePath)
	}
}

func (q *ogImageQueue) render(key, text string) error {
	done := make(chan error, 1)
	q.jobs <- ogImageJob{key: key, text: text, done: done}
	return <-done
}

func renderOgImageToFile(text, destPath string) error {
	converter, err := exec.LookPath("rsvg-convert")
	if err != nil {
		return fmt.Errorf("rsvg-convert not found: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	tpl, err := embeddedFiles.ReadFile("public/og-template.svg")
	if err != nil {
		return err
	}
	svg := strings.ReplaceAll(string(tpl), "__TEXT__", escapeXML(text))
	ctx, cancel := context.WithTimeout(context.Background(), ogRenderTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, converter, "-w", strconv.Itoa(ogImageWidth), "-h", strconv.Itoa(ogImageHeight), "-o", destPath)
	cmd.Stdin = strings.NewReader(svg)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = os.Remove(destPath)
		return fmt.Errorf("rsvg-convert failed: %w", err)
	}
	return nil
}

func ogImageURL(baseURL, message string) string {
	base := strings.TrimRight(baseURL, "/")
	prefix := ogImageTextPrefix(message)
	if prefix == "" {
		return base + "/og-image.png"
	}
	return base + "/og-image.png?text=" + url.QueryEscape(prefix)
}

func ogImageTextPrefix(message string) string {
	message = strings.Join(strings.Fields(strings.TrimSpace(message)), " ")
	if message == "" {
		return ""
	}
	runes := []rune(message)
	if len(runes) > ogImageTextLimit {
		return string(runes[:ogImageTextLimit]) + "…"
	}
	return message
}

func ogCacheKey(message string) string {
	prefix := ogImageTextPrefix(message)
	if prefix == "" {
		return "default"
	}
	normalized := strings.ToLower(prefix)
	normalized = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == ' ':
			return '-'
		default:
			return '-'
		}
	}, normalized)
	normalized = strings.Trim(normalized, "-")
	if normalized == "" {
		return "default"
	}
	if len(normalized) > ogImageTextLimit {
		normalized = normalized[:ogImageTextLimit]
	}
	return normalized
}

func ogCachePath(key string) string {
	return filepath.Join(ogCacheDir(), "og", key+".png")
}

func ogCacheDir() string {
	if value := os.Getenv("XDG_CACHE_DIR"); value != "" {
		return filepath.Join(value, siteDomain)
	}
	if value := os.Getenv("XDG_CACHE_HOME"); value != "" {
		return filepath.Join(value, siteDomain)
	}
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, siteDomain)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", siteDomain)
	}
	return filepath.Join(os.TempDir(), siteDomain)
}
