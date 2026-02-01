package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxTrackBodyBytes     = 16 * 1024
	maxPathLen            = 512
	maxShortlinkBodyBytes = 8 * 1024
	shortCodeLen          = 7
	shortlinkRateLimit    = 20
	shortlinkRateWindow   = time.Minute
	trackRateLimit        = 120
	trackRateWindow       = time.Minute
	ogImageWidth          = 600
	ogImageHeight         = 315
	ogImageTextLimit      = 48
	ogRenderTimeout       = 5 * time.Second
	siteDomain            = "parabens.vc"
)

//go:embed public/index.html public/privacy.html public/styles.css public/app.js public/favicon.svg public/og-image.svg public/og-image.png public/og-template.svg public/blocked-words.txt
var embeddedFiles embed.FS

type TrackEvent struct {
	Path           string      `json:"path,omitempty"`
	UserAgent      string      `json:"user_agent,omitempty"`
	Timestamp      string      `json:"timestamp,omitempty"`
	Event          string      `json:"event,omitempty"`
	Query          string      `json:"query,omitempty"`
	Referrer       string      `json:"referrer,omitempty"`
	AcceptLanguage string      `json:"accept_language,omitempty"`
	Timezone       string      `json:"timezone,omitempty"`
	Screen         interface{} `json:"screen,omitempty"`
	Viewport       interface{} `json:"viewport,omitempty"`
}

type ShortLinkRequest struct {
	Path string `json:"path"`
}

type ShortLinkResponse struct {
	Code        string `json:"code"`
	ShortURL    string `json:"short_url"`
	Path        string `json:"path"`
	Destination string `json:"destination"`
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

type shortlinkStore struct {
	mu     sync.Mutex
	loaded bool
	byCode map[string]string
	byPath map[string]string
}

type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	window time.Duration
	max    int
}

type ogImageJob struct {
	key  string
	text string
	done chan error
}

type ogImageQueue struct {
	jobs chan ogImageJob
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/track", handleTrack)
	mux.HandleFunc("/s", handleShortlinkCreate)
	mux.HandleFunc("/s/", handleShortlinkRedirect)
	mux.HandleFunc("/og-image.png", handleOgImage)
	mux.HandleFunc("/", handlePage)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           withSecurityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	log.Printf("Listening on http://0.0.0.0:%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Printf("server error: %v", err)
	}
}

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
	log.Printf("track_event: %+v meta={ip:%q ua:%q ref:%q lang:%q}", evt, ip, r.UserAgent(), r.Referer(), r.Header.Get("Accept-Language"))
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
	if text == "" || isBlockedMessage(text) {
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
		log.Printf("og-image render failed: %v", err)
		serveEmbedded(w, r, "public/og-image.png", "image/png", "public, max-age=86400")
		return
	}
	writePngFile(w, r, cachePath)
}

func renderIndexHTML(tpl string, path string) string {
	raw := strings.TrimPrefix(path, "/")
	message := decodePath(raw)
	name := buildDisplayName(message)
	punct := "!"
	if hasFinalPunctuation(message) || hasEncodedFinalPunctuation(raw) {
		punct = ""
	}
	title := "Parabéns!"
	if message != "" {
		title = fmt.Sprintf("Parabéns, %s%s", message, punct)
	}
	baseURL := publicBaseURL()
	ogURL := baseURL
	if raw != "" {
		ogURL = strings.TrimRight(baseURL, "/") + "/" + raw
	}
	ogImage := ogImageURL(baseURL, message)

	return strings.NewReplacer(
		"__TITLE__", escapeHTML(title),
		"__OG_TITLE__", escapeHTML(title),
		"__OG_DESC__", escapeHTML(message),
		"__OG_URL__", escapeHTML(ogURL),
		"__OG_IMAGE__", escapeHTML(ogImage),
		"__NAME__", escapeHTML(name),
		"__PUNCT__", punct,
	).Replace(tpl)
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

func fileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
}

func decodePath(raw string) string {
	if raw == "" {
		return ""
	}
	decoded, err := urlPathUnescape(raw)
	if err != nil {
		return raw
	}
	decoded = strings.ReplaceAll(decoded, "_", " ")
	return strings.TrimSpace(decoded)
}

func encodePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, " ", "_")
	return url.PathEscape(value)
}

func urlPathUnescape(s string) (string, error) {
	return url.PathUnescape(s)
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

func buildDisplayName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "você é um(a) amigo(a)"
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "voce ") || strings.HasPrefix(lower, "você ") || strings.HasPrefix(lower, "vc ") {
		return value
	}
	if startsWithProperName(value) {
		return value
	}
	return "você " + value
}

func startsWithProperName(value string) bool {
	tokens := tokenizeWords(value)
	if len(tokens) == 0 {
		return false
	}
	if !isCapitalized(tokens[0]) {
		return false
	}
	particles := map[string]bool{"da": true, "de": true, "do": true, "das": true, "dos": true}
	for i := 1; i < len(tokens); {
		lower := strings.ToLower(tokens[i])
		if particles[lower] {
			if i+1 < len(tokens) && isCapitalized(tokens[i+1]) {
				i += 2
				continue
			}
			break
		}
		if isCapitalized(tokens[i]) {
			i++
			continue
		}
		break
	}
	return true
}

func tokenizeWords(value string) []string {
	var tokens []string
	var buf bytes.Buffer
	for _, ch := range value {
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= 'À' && ch <= 'ÿ') || ch == '\'' || ch == '’' {
			buf.WriteRune(ch)
		} else if buf.Len() > 0 {
			tokens = append(tokens, buf.String())
			buf.Reset()
		}
	}
	if buf.Len() > 0 {
		tokens = append(tokens, buf.String())
	}
	return tokens
}

func isCapitalized(token string) bool {
	if token == "" {
		return false
	}
	r := []rune(token)
	return strings.ToUpper(string(r[0])) == string(r[0]) && strings.ToLower(string(r[0])) != string(r[0])
}

func hasFinalPunctuation(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	var last rune
	for _, r := range value {
		last = r
	}
	switch last {
	case '!', '?', '.', '…':
		return true
	default:
		return false
	}
}

func hasEncodedFinalPunctuation(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	if hasFinalPunctuation(raw) {
		return true
	}
	lower := strings.ToLower(raw)
	return strings.HasSuffix(lower, "%21") ||
		strings.HasSuffix(lower, "%3f") ||
		strings.HasSuffix(lower, "%2e") ||
		strings.HasSuffix(lower, "%e2%80%a6")
}

func publicBaseURL() string {
	base := os.Getenv("PUBLIC_BASE_URL")
	if base == "" {
		return "https://" + siteDomain
	}
	return base
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	data, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func errorPage(message string) string {
	return fmt.Sprintf("<!DOCTYPE html><html lang=\"pt-BR\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Erro</title><style>body{font-family:system-ui,Arial,sans-serif;background:#0f172a;color:#f8fafc;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0}.card{max-width:520px;padding:24px;border:1px solid rgba(148,163,184,.3);border-radius:16px;background:rgba(15,23,42,.85);text-align:center}</style></head><body><div class=\"card\"><h1>Ops!</h1><p>%s</p><a href=\"/\" style=\"color:#93c5fd\">Voltar</a></div></body></html>", escapeHTML(message))
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; base-uri 'self'; frame-ancestors 'none'")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func readLimitedBody(r *http.Request, max int64) ([]byte, error) {
	if r.ContentLength > max {
		return nil, errTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errTooLarge
	}
	return data, nil
}

var errTooLarge = fmt.Errorf("payload too large")

func statusFromError(err error) int {
	if err == errTooLarge {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		parts := strings.Split(ip, ",")
		return strings.TrimSpace(parts[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return ip
	}
	return r.RemoteAddr
}

var shortlinks = shortlinkStore{
	byCode: map[string]string{},
	byPath: map[string]string{},
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

var shortlinkLimiter = &rateLimiter{
	hits:   map[string][]time.Time{},
	window: shortlinkRateWindow,
	max:    shortlinkRateLimit,
}

var trackLimiter = &rateLimiter{
	hits:   map[string][]time.Time{},
	window: trackRateWindow,
	max:    trackRateLimit,
}

var ogQueue = newOgImageQueue()

var (
	blockedOnce  sync.Once
	blockedTerms []string
)

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.window)
	list := rl.hits[key]
	filtered := list[:0]
	for _, ts := range list {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	if len(filtered) >= rl.max {
		rl.hits[key] = filtered
		return false
	}
	rl.hits[key] = append(filtered, time.Now())
	return true
}

func generateCode(length int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}

func escapeHTML(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(value)
}

func escapeXML(value string) string {
	return escapeHTML(value)
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

func newOgImageQueue() *ogImageQueue {
	q := &ogImageQueue{jobs: make(chan ogImageJob, 32)}
	go q.run()
	return q
}

var renderOgImageToFileFunc = renderOgImageToFile

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

func isBlockedMessage(message string) bool {
	blockedOnce.Do(loadBlockedTerms)
	if len(blockedTerms) == 0 {
		return false
	}
	normalized := normalizeForBlock(message)
	if normalized == "" {
		return false
	}
	for _, term := range blockedTerms {
		if term != "" && strings.Contains(normalized, term) {
			return true
		}
	}
	return false
}

func loadBlockedTerms() {
	data, err := embeddedFiles.ReadFile("public/blocked-words.txt")
	if err != nil {
		blockedTerms = nil
		return
	}
	lines := strings.Split(string(data), "\n")
	blockedTerms = make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		term := normalizeForBlock(line)
		if term != "" {
			blockedTerms = append(blockedTerms, term)
		}
	}
}

func normalizeForBlock(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "_", " ")
	value = strings.ReplaceAll(value, "-", " ")
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r >= 'À' && r <= 'ÿ':
			return r
		case r == ' ':
			return r
		default:
			return ' '
		}
	}, value)
	return strings.Join(strings.Fields(value), " ")
}
