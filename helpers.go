package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	window time.Duration
	max    int
}

var trackLimiter = &rateLimiter{
	hits:   map[string][]time.Time{},
	window: trackRateWindow,
	max:    trackRateLimit,
}

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

// Valid theme names
var validThemes = map[string]bool{
	"":        true, // default
	"light":   true,
	"warm":    true,
	"elegant": true,
	"pixel":   true,
}

func themeClass(theme string) string {
	theme = strings.ToLower(strings.TrimSpace(theme))
	if !validThemes[theme] || theme == "" {
		return ""
	}
	return "theme-" + theme
}

// Occasion defines a celebration type with its display properties
type Occasion struct {
	Prefix   string // URL prefix (e.g., "aniversario")
	Greeting string // Greeting text (e.g., "Feliz Aniversário")
	Subtitle string // Subtitle text
	Emoji    string // Emoji for subtitle
}

var defaultOccasion = Occasion{
	Prefix:   "",
	Greeting: "Parabéns",
	Subtitle: "Celebrando com balões e confetes",
	Emoji:    "🎉",
}

var occasions = map[string]Occasion{
	"aniversario": {
		Prefix:   "aniversario",
		Greeting: "Feliz Aniversário",
		Subtitle: "Celebrando mais um ano de vida",
		Emoji:    "🎂",
	},
	"formatura": {
		Prefix:   "formatura",
		Greeting: "Parabéns pela formatura",
		Subtitle: "Uma conquista para celebrar",
		Emoji:    "🎓",
	},
	"promocao": {
		Prefix:   "promocao",
		Greeting: "Parabéns pela promoção",
		Subtitle: "Seu esforço foi reconhecido",
		Emoji:    "🏆",
	},
	"casamento": {
		Prefix:   "casamento",
		Greeting: "Felicidades",
		Subtitle: "Celebrando o amor",
		Emoji:    "💒",
	},
	"boas-vindas": {
		Prefix:   "boas-vindas",
		Greeting: "Boas-vindas",
		Subtitle: "É um prazer ter você aqui",
		Emoji:    "👋",
	},
}

// parseOccasionFromPath extracts occasion prefix and remaining message from path
// e.g., "/aniversario/João" → (Occasion{...}, "João")
// e.g., "/João" → (defaultOccasion, "João")
func parseOccasionFromPath(path string) (Occasion, string) {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return defaultOccasion, ""
	}

	// Check if path starts with a known occasion prefix
	parts := strings.SplitN(path, "/", 2)
	if len(parts) >= 1 {
		if occ, ok := occasions[strings.ToLower(parts[0])]; ok {
			message := ""
			if len(parts) == 2 {
				message = parts[1]
			}
			return occ, message
		}
	}

	return defaultOccasion, path
}

func renderIndexHTML(tpl string, path string, theme string) string {
	occasion, rawMessage := parseOccasionFromPath(path)
	message := decodePath(rawMessage)
	displayMessage := buildDisplayMessage(message)
	punct := "!"
	if hasFinalPunctuation(message) || hasEncodedFinalPunctuation(rawMessage) {
		punct = ""
	}

	// Build title using occasion greeting + display message
	title := fmt.Sprintf("%s, %s%s", occasion.Greeting, displayMessage, punct)

	// Build OG URL
	baseURL := publicBaseURL()
	ogURL := baseURL
	if path != "" && path != "/" {
		ogURL = strings.TrimRight(baseURL, "/") + path
	}

	// OG image uses the occasion greeting + message
	ogImageText := message
	if message != "" && occasion.Greeting != "Parabéns" {
		ogImageText = occasion.Greeting + ", " + message
	}
	ogImage := ogImageURL(baseURL, ogImageText)

	subtitle := occasion.Subtitle + " " + occasion.Emoji

	// Determine if we should show the composer form
	showComposer := "false"
	if message == "" {
		showComposer = "true"
	}

	return strings.NewReplacer(
		"__TITLE__", escapeHTML(title),
		"__OG_TITLE__", escapeHTML(title),
		"__OG_DESC__", escapeHTML(occasion.Subtitle+" "+occasion.Emoji),
		"__OG_URL__", escapeHTML(ogURL),
		"__OG_IMAGE__", escapeHTML(ogImage),
		"__GREETING__", escapeHTML(occasion.Greeting),
		"__MESSAGE__", escapeHTML(displayMessage),
		"__PUNCT__", punct,
		"__SUBTITLE__", escapeHTML(subtitle),
		"__THEME_CLASS__", themeClass(theme),
		"__SHOW_COMPOSER__", showComposer,
	).Replace(tpl)
}

func buildDisplayMessage(value string) string {
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
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= 'À' && ch <= 'ÿ') || ch == '\'' || ch == 0x2019 {
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
