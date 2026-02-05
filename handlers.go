package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

func handleTrack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	if !trackLimiter.allow(clientIP(r)) {
		http.Error(w, "", http.StatusTooManyRequests)
		return
	}
	body, err := readLimitedBody(r, maxTrackBodyBytes)
	if err != nil {
		http.Error(w, "", statusFromError(err))
		return
	}

	var evt TrackEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	ip := clientIP(r)
	slog.Info("track_event",
		"event", evt.Event,
		"path", evt.Path,
		"query", evt.Query,
		"referrer", evt.Referrer,
		"timezone", evt.Timezone,
		"screen", evt.Screen,
		"viewport", evt.Viewport,
		"ip", ip,
		"user_agent", r.UserAgent(),
		"referer", r.Referer(),
		"accept_language", r.Header.Get("Accept-Language"),
	)
	w.WriteHeader(http.StatusNoContent)
}

func handleShortlinkCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	if !shortlinkLimiter.allow(clientIP(r)) {
		http.Error(w, "", http.StatusTooManyRequests)
		return
	}

	if err := ensureShortlinksLoaded(); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	body, err := readLimitedBody(r, maxShortlinkBodyBytes)
	if err != nil {
		http.Error(w, "", statusFromError(err))
		return
	}

	var req ShortLinkRequest
	if err := json.Unmarshal(body, &req); err != nil || strings.TrimSpace(req.Path) == "" {
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	rawPath := strings.TrimPrefix(strings.TrimSpace(req.Path), "/")
	path := decodePath(rawPath)
	if path == "" {
		http.Error(w, "", http.StatusBadRequest)
		return
	}
	if isBlockedMessage(path) {
		http.Error(w, "", http.StatusForbidden)
		return
	}

	shortlinks.mu.Lock()
	if code, ok := shortlinks.byPath[path]; ok {
		resp := shortlinkResponse(code, path)
		shortlinks.mu.Unlock()
		writeJSON(w, http.StatusOK, resp)
		return
	}

	var code string
	for i := 0; i < 10; i++ {
		code = generateCode(shortCodeLen)
		if _, exists := shortlinks.byCode[code]; !exists {
			break
		}
	}
	if code == "" || shortlinks.byCode[code] != "" {
		shortlinks.mu.Unlock()
		http.Error(w, "", http.StatusServiceUnavailable)
		return
	}

	shortlinks.byCode[code] = path
	shortlinks.byPath[path] = code
	if err := persistShortlinksLocked(); err != nil {
		delete(shortlinks.byCode, code)
		delete(shortlinks.byPath, path)
		shortlinks.mu.Unlock()
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	resp := shortlinkResponse(code, path)
	shortlinks.mu.Unlock()
	writeJSON(w, http.StatusCreated, resp)
}

func handleShortlinkRedirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	if err := ensureShortlinksLoaded(); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	code := strings.TrimPrefix(r.URL.Path, "/s/")
	if code == "" {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	shortlinks.mu.Lock()
	path, ok := shortlinks.byCode[code]
	shortlinks.mu.Unlock()
	if !ok {
		http.Error(w, "", http.StatusNotFound)
		return
	}

	encoded := encodePathSegment(path)
	if encoded == "" {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	http.Redirect(w, r, "/"+encoded, http.StatusFound)
}

func handlePage(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Path) > maxPathLen {
		writeHTML(w, http.StatusRequestURITooLong, errorPage("A mensagem é muito longa. Encurte o texto e tente novamente."))
		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}

	switch r.URL.Path {
	case "/":
		serveIndex(w, r, "")
		return
	case "/privacy":
		serveEmbedded(w, r, "public/privacy.html", "text/html; charset=utf-8", "")
		return
	case "/styles.css":
		serveEmbedded(w, r, "public/styles.css", "text/css; charset=utf-8", "public, max-age=300")
		return
	case "/app.js":
		serveEmbedded(w, r, "public/app.js", "application/javascript; charset=utf-8", "public, max-age=300")
		return
	case "/favicon.svg":
		serveEmbedded(w, r, "public/favicon.svg", "image/svg+xml", "public, max-age=86400")
		return
	case "/og-image.svg":
		serveEmbedded(w, r, "public/og-image.svg", "image/svg+xml", "public, max-age=86400")
		return
	case "/og-image.png":
		handleOgImage(w, r)
		return
	default:
		serveIndex(w, r, r.URL.Path)
		return
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request, path string) {
	tpl, _ := embeddedFiles.ReadFile("public/index.html")
	message := decodePath(strings.TrimPrefix(path, "/"))
	if looksLikePath(message) {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	if isBlockedMessage(message) {
		writeHTML(w, http.StatusForbidden, errorPage("Esta mensagem não está disponível."))
		return
	}
	rendered := renderIndexHTML(string(tpl), path)
	writeHTML(w, http.StatusOK, rendered)
}

func serveEmbedded(w http.ResponseWriter, r *http.Request, name, contentType, cacheControl string) {
	data, err := embeddedFiles.ReadFile(name)
	if err != nil {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if cacheControl != "" {
		w.Header().Set("Cache-Control", cacheControl)
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		return
	}
	_, _ = w.Write(data)
}

func handleOgImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "", http.StatusMethodNotAllowed)
		return
	}
	text := ogImageTextPrefix(r.URL.Query().Get("text"))
	if text == "" || looksLikePath(text) || isBlockedMessage(text) {
		serveEmbedded(w, r, "public/og-image.png", "image/png", "public, max-age=86400")
		return
	}
	key := ogCacheKey(text)
	cachePath := ogCachePath(key)
	if ok, err := fileExists(cachePath); ok && err == nil {
		writePngFile(w, r, cachePath)
		return
	}
	if err := ogQueue.render(key, text); err != nil {
		slog.Error("og-image render failed", "error", err)
		serveEmbedded(w, r, "public/og-image.png", "image/png", "public, max-age=86400")
		return
	}
	writePngFile(w, r, cachePath)
}

func writePngFile(w http.ResponseWriter, r *http.Request, path string) {
	file, err := os.Open(path)
	if err != nil {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Length", fmt.Sprint(info.Size()))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, file)
}
