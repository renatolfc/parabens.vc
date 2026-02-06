package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
			got := renderIndexHTML(tpl, tc.path, "")
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

// ============================================================================
// Path Handling & URL Encoding Tests
// ============================================================================

func TestDecodePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"simple", "Joao", "Joao"},
		{"with underscore", "Joao_Silva", "Joao Silva"},
		{"multiple underscores", "Joao___Silva", "Joao   Silva"},
		{"url encoded", "Jo%C3%A3o", "João"},
		{"underscore and encoded", "Jo%C3%A3o_Silva", "João Silva"},
		{"leading/trailing spaces", "  Joao  ", "Joao"},
		{"emoji", "Joao%20%F0%9F%8E%89", "Joao 🎉"},
		{"special chars", "Test%21%3F%2E", "Test!?."},
		{"invalid encoding keeps original", "Test%ZZ", "Test%ZZ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodePath(tt.input)
			if got != tt.want {
				t.Errorf("decodePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEncodePathSegment(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"spaces only", "   ", ""},
		{"simple", "Joao", "Joao"},
		{"with space", "Joao Silva", "Joao_Silva"},
		{"multiple spaces", "Joao   Silva", "Joao___Silva"},
		{"special chars", "João & José", "Jo%C3%A3o_&_Jos%C3%A9"},
		{"leading/trailing", "  test  ", "test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodePathSegment(tt.input)
			if got != tt.want {
				t.Errorf("encodePathSegment(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	tests := []string{
		"Simple Name",
		"João & José",
		"Test with émojis 🎉",
		"Multiple   spaces",
		"Special!@#$%^&*()",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			encoded := encodePathSegment(input)
			decoded := decodePath(encoded)
			trimmed := strings.TrimSpace(input)
			if decoded != trimmed {
				t.Errorf("round trip failed: %q -> %q -> %q", input, encoded, decoded)
			}
		})
	}
}

// ============================================================================
// Display Name Logic Tests
// ============================================================================

func TestBuildDisplayMessage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "você é um(a) amigo(a)"},
		{"  ", "você é um(a) amigo(a)"},
		{"Renato", "Renato"},
		{"renato", "você renato"},
		{"João Silva", "João Silva"},
		{"joão silva", "você joão silva"},
		{"José da Silva", "José da Silva"},
		{"maria de Paula", "você maria de Paula"},
		{"você é legal", "você é legal"},
		{"Você é legal", "Você é legal"},
		{"voce tem razão", "voce tem razão"},
		{"vc está certo", "vc está certo"},
		{"Ana Maria", "Ana Maria"},
		{"pedro", "você pedro"},
		{"Pedro Paulo", "Pedro Paulo"},
		{"Carlos Alberto dos Santos", "Carlos Alberto dos Santos"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := buildDisplayMessage(tt.input)
			if got != tt.want {
				t.Errorf("buildDisplayMessage(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStartsWithProperName(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"Renato", true},
		{"renato", false},
		{"João Silva", true},
		{"joão Silva", false},
		{"José da Silva", true},
		{"josé da Silva", false},
		{"Maria de Paula", true},
		{"Ana", true},
		{"ana", false},
		{"", false},
		{"Pedro Paulo dos Santos", true},
		{"pedro Paulo", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := startsWithProperName(tt.input)
			if got != tt.want {
				t.Errorf("startsWithProperName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsCapitalized(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"A", true},
		{"a", false},
		{"Test", true},
		{"test", false},
		{"UPPER", true},
		{"123", false},
		{"Ção", true},
		{"ção", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isCapitalized(tt.input)
			if got != tt.want {
				t.Errorf("isCapitalized(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTokenizeWords(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"Hello", []string{"Hello"}},
		{"Hello World", []string{"Hello", "World"}},
		{"João da Silva", []string{"João", "da", "Silva"}},
		{"test123test", []string{"test", "test"}},
		{"it's", []string{"it's"}},
		{"one-two", []string{"one", "two"}},
		{"  spaces  ", []string{"spaces"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := tokenizeWords(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("tokenizeWords(%q) = %v, want %v", tt.input, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("tokenizeWords(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ============================================================================
// Punctuation Detection Tests
// ============================================================================

func TestHasFinalPunctuation(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"test", false},
		{"test!", true},
		{"test?", true},
		{"test.", true},
		{"test…", true},
		{"test  !", true},
		{"test !more", false},
		{"  ", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := hasFinalPunctuation(tt.input)
			if got != tt.want {
				t.Errorf("hasFinalPunctuation(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHasEncodedFinalPunctuation(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"test", false},
		{"test!", true},
		{"test%21", true},
		{"test%3F", true},
		{"test%3f", true},
		{"test%2E", true},
		{"test%2e", true},
		{"test%E2%80%A6", true},
		{"test%e2%80%a6", true},
		{"test%21more", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := hasEncodedFinalPunctuation(tt.input)
			if got != tt.want {
				t.Errorf("hasEncodedFinalPunctuation(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Content Filtering Tests
// ============================================================================

func TestNormalizeForBlock(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"  ", ""},
		{"Test", "test"},
		{"TEST", "test"},
		{"test_word", "test word"},
		{"test-word", "test word"},
		{"test  multiple   spaces", "test multiple spaces"},
		{"Test!@#$%Word", "test word"},
		{"João", "joão"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeForBlock(tt.input)
			if got != tt.want {
				t.Errorf("normalizeForBlock(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsBlockedMessage(t *testing.T) {
	// Reset blocked terms for testing - must call Do to prevent real load
	blockedOnce = sync.Once{}
	blockedOnce.Do(func() {
		// Set test data instead of loading from file
		blockedTerms = []string{"bad word", "offensive"}
	})

	tests := []struct {
		input   string
		blocked bool
	}{
		{"clean message", false},
		{"this has bad word in it", true},   // Contains "bad word" with space
		{"this is offensive content", true}, // Contains "offensive"
		{"BAD WORD", true},                  // Case insensitive
		{"bad_word", true},                  // Underscore normalized to space
		{"bad-word", true},                  // Hyphen normalized to space
		{"", false},
		{"unrelated text", false},
		{"badword", false}, // No space, so doesn't match "bad word"
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isBlockedMessage(tt.input)
			if got != tt.blocked {
				t.Errorf("isBlockedMessage(%q) = %v, want %v", tt.input, got, tt.blocked)
			}
		})
	}
}

func TestLooksLikePath(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// Should be blocked (looks like exploit attempts)
		{"wp-login.php", true},
		{"wp-admin/index.php", true},
		{"wp-content/uploads", true},
		{"wordpress/readme.html", true},
		{"../etc/passwd", true},
		{"..\\windows\\system32", true},
		{"xmlrpc.php", true},
		{"phpmyadmin/index.php", true},
		{".env", true},
		{".git/config", true},
		{"admin.php", true},
		{"admin/dashboard", true},
		{"backup.sql", true},
		{"config.yml", true},
		{"database.json", true},
		{"http://evil.com/", true},
		{"https://attacker.net/payload", true},
		{"ftp://files.example.com", true},
		{"cgi-bin/script.cgi", true},
		{".htaccess", true},
		{"shell.sh", true},
		{"exploit.exe", true},
		{"api/users", true},
		{"etc/passwd", true},

		// Should NOT be blocked (legitimate names)
		{"João", false},
		{"Maria Silva", false},
		{"J.R. Tolkien", false},
		{"João Paulo", false},
		{"Dr. Smith", false},
		{"Ana Maria", false},
		{"José", false},
		{"Müller", false},
		{"François", false},
		{"", false},
		{"admin", false},
		{"Administração", false},
		{"wordpress", false},
		{"wp-admin", false}, // No slash, allowed
		{"I love wordpress", false},
		{"A / B / C", false},
		{"I love wordpress / drupal", false},
		{"my api key", false},
		{"the admin panel", false},
		{"/admin/dashboard", false}, // Starts with /, message won't have leading /
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := looksLikePath(tt.input)
			if got != tt.expected {
				t.Errorf("looksLikePath(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// ============================================================================
// OG Image Tests
// ============================================================================

func TestOgImageTextPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"  ", ""},
		{"short", "short"},
		{"exactly 39 characters in this text her", "exactly 39 characters in this text her"},
		{"this is a very long message that exceeds the maximum allowed length for og image text", "this is a very long message that exceed…"},
		{"multiple   spaces   collapsed", "multiple spaces collapsed"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ogImageTextPrefix(tt.input)
			if got != tt.want {
				t.Errorf("ogImageTextPrefix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestOgCacheKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "default"},
		{"Test", "test"},
		{"Test Message", "test-message"},
		{"Test!!!Message", "test---message"}, // Multiple punctuation becomes multiple dashes
		{"   ", "default"},
		{"João Silva", "jo-o-silva"}, // Unicode chars outside a-z become dashes
		{"test_underscore", "test-underscore"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ogCacheKey(tt.input)
			if got != tt.want {
				t.Errorf("ogCacheKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestOgImageURL(t *testing.T) {
	baseURL := "https://test.example.com"
	tests := []struct {
		message string
		want    string
	}{
		{"", "https://test.example.com/og-image.png"},
		{"Test", "https://test.example.com/og-image.png?text=Test"},
		{"Test Message", "https://test.example.com/og-image.png?text=Test+Message"},
	}

	for _, tt := range tests {
		t.Run(tt.message, func(t *testing.T) {
			got := ogImageURL(baseURL, tt.message)
			if got != tt.want {
				t.Errorf("ogImageURL(%q, %q) = %q, want %q", baseURL, tt.message, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Rate Limiter Tests
// ============================================================================

func TestRateLimiter(t *testing.T) {
	rl := &rateLimiter{
		hits:   map[string][]time.Time{},
		window: 100 * time.Millisecond,
		max:    3,
	}

	key := "test-key"

	// First 3 requests should succeed
	for i := 0; i < 3; i++ {
		if !rl.allow(key) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 4th request should fail
	if rl.allow(key) {
		t.Error("request 4 should be blocked")
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again
	if !rl.allow(key) {
		t.Error("request after window should be allowed")
	}
}

func TestRateLimiterMultipleKeys(t *testing.T) {
	rl := &rateLimiter{
		hits:   map[string][]time.Time{},
		window: time.Minute,
		max:    2,
	}

	// Different keys should have independent limits
	if !rl.allow("key1") {
		t.Error("key1 request 1 should be allowed")
	}
	if !rl.allow("key2") {
		t.Error("key2 request 1 should be allowed")
	}
	if !rl.allow("key1") {
		t.Error("key1 request 2 should be allowed")
	}
	if !rl.allow("key2") {
		t.Error("key2 request 2 should be allowed")
	}
	if rl.allow("key1") {
		t.Error("key1 request 3 should be blocked")
	}
	if rl.allow("key2") {
		t.Error("key2 request 3 should be blocked")
	}
}

// ============================================================================
// Shortlink Tests
// ============================================================================

func TestGenerateCode(t *testing.T) {
	length := 7
	code := generateCode(length)
	if len(code) != length {
		t.Errorf("generateCode(%d) returned length %d", length, len(code))
	}

	// Check all characters are valid
	for _, ch := range code {
		valid := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
		if !valid {
			t.Errorf("generateCode returned invalid character: %c", ch)
		}
	}
}

func TestGenerateCodeUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	iterations := 1000

	for i := 0; i < iterations; i++ {
		code := generateCode(7)
		if seen[code] {
			t.Logf("collision after %d iterations (expected with random generation)", i)
			return
		}
		seen[code] = true
	}
}

func TestShortlinkResponse(t *testing.T) {
	tests := []struct {
		code string
		path string
	}{
		{"abc1234", "Test Message"},
		{"xyz5678", "João Silva"},
		{"test123", "Simple"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			resp := shortlinkResponse(tt.code, tt.path)
			if resp.Code != tt.code {
				t.Errorf("code = %q, want %q", resp.Code, tt.code)
			}
			if !strings.Contains(resp.ShortURL, tt.code) {
				t.Errorf("short_url %q should contain code %q", resp.ShortURL, tt.code)
			}
			if resp.Path != strings.TrimPrefix(strings.TrimSpace(tt.path), "/") {
				t.Errorf("path = %q, want %q", resp.Path, tt.path)
			}
		})
	}
}

// ============================================================================
// Client IP Detection Tests
// ============================================================================

func TestClientIP(t *testing.T) {
	tests := []struct {
		name          string
		remoteAddr    string
		xForwardedFor string
		xRealIP       string
		expectedIP    string
	}{
		{
			name:       "from RemoteAddr",
			remoteAddr: "192.168.1.1:12345",
			expectedIP: "192.168.1.1",
		},
		{
			name:       "from X-Real-IP",
			remoteAddr: "192.168.1.1:12345",
			xRealIP:    "10.0.0.1",
			expectedIP: "10.0.0.1",
		},
		{
			name:          "from X-Forwarded-For single",
			remoteAddr:    "192.168.1.1:12345",
			xForwardedFor: "10.0.0.1",
			expectedIP:    "10.0.0.1",
		},
		{
			name:          "from X-Forwarded-For multiple",
			remoteAddr:    "192.168.1.1:12345",
			xForwardedFor: "10.0.0.1, 10.0.0.2, 10.0.0.3",
			expectedIP:    "10.0.0.1",
		},
		{
			name:          "X-Forwarded-For priority over X-Real-IP",
			remoteAddr:    "192.168.1.1:12345",
			xForwardedFor: "10.0.0.1",
			xRealIP:       "10.0.0.2",
			expectedIP:    "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{
				RemoteAddr: tt.remoteAddr,
				Header:     http.Header{},
			}
			if tt.xForwardedFor != "" {
				r.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				r.Header.Set("X-Real-IP", tt.xRealIP)
			}

			got := clientIP(r)
			if got != tt.expectedIP {
				t.Errorf("clientIP() = %q, want %q", got, tt.expectedIP)
			}
		})
	}
}

// ============================================================================
// HTML/XML Escaping Tests
// ============================================================================

func TestEscapeHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"simple", "simple"},
		{"<script>", "&lt;script&gt;"},
		{"&", "&amp;"},
		{`"quotes"`, "&quot;quotes&quot;"},
		{"'single'", "&#39;single&#39;"},
		{"<>&\"'", "&lt;&gt;&amp;&quot;&#39;"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeHTML(tt.input)
			if got != tt.want {
				t.Errorf("escapeHTML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ============================================================================
// HTTP Handler Integration Tests
// ============================================================================

func TestHandleTrack(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       string
		wantStatus int
	}{
		{
			name:       "valid POST",
			method:     http.MethodPost,
			body:       `{"path":"/test","event":"view"}`,
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "GET not allowed",
			method:     http.MethodGet,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "invalid JSON",
			method:     http.MethodPost,
			body:       `{invalid}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty body",
			method:     http.MethodPost,
			body:       ``,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/track", strings.NewReader(tt.body))
			req.RemoteAddr = "192.168.1.1:12345"
			w := httptest.NewRecorder()

			handleTrack(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandleShortlinkCreate(t *testing.T) {
	// Setup temporary storage
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "shortlinks.json")
	oldEnv := os.Getenv("SHORTLINK_DB")
	os.Setenv("SHORTLINK_DB", dbPath)
	defer os.Setenv("SHORTLINK_DB", oldEnv)

	// Reset shortlinks state
	shortlinks = shortlinkStore{
		byCode: map[string]string{},
		byPath: map[string]string{},
	}

	tests := []struct {
		name       string
		method     string
		body       string
		wantStatus int
		wantCode   bool
	}{
		{
			name:       "valid creation",
			method:     http.MethodPost,
			body:       `{"path":"Test Message"}`,
			wantStatus: http.StatusCreated,
			wantCode:   true,
		},
		{
			name:       "empty path",
			method:     http.MethodPost,
			body:       `{"path":""}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "whitespace only path",
			method:     http.MethodPost,
			body:       `{"path":"   "}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid JSON",
			method:     http.MethodPost,
			body:       `{invalid}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "GET not allowed",
			method:     http.MethodGet,
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/s", strings.NewReader(tt.body))
			req.RemoteAddr = "192.168.1.1:12345"
			w := httptest.NewRecorder()

			handleShortlinkCreate(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantCode && w.Code == http.StatusCreated {
				var resp ShortLinkResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.Code == "" {
					t.Error("expected non-empty code")
				}
				if resp.ShortURL == "" {
					t.Error("expected non-empty short_url")
				}
			}
		})
	}
}

func TestHandleShortlinkCreateIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "shortlinks.json")
	oldEnv := os.Getenv("SHORTLINK_DB")
	os.Setenv("SHORTLINK_DB", dbPath)
	defer os.Setenv("SHORTLINK_DB", oldEnv)

	shortlinks = shortlinkStore{
		byCode: map[string]string{},
		byPath: map[string]string{},
	}

	path := "Same Path"
	body := fmt.Sprintf(`{"path":"%s"}`, path)

	// First request
	req1 := httptest.NewRequest(http.MethodPost, "/s", strings.NewReader(body))
	req1.RemoteAddr = "192.168.1.1:12345"
	w1 := httptest.NewRecorder()
	handleShortlinkCreate(w1, req1)

	var resp1 ShortLinkResponse
	json.NewDecoder(w1.Body).Decode(&resp1)

	// Second request with same path
	req2 := httptest.NewRequest(http.MethodPost, "/s", strings.NewReader(body))
	req2.RemoteAddr = "192.168.1.2:12345"
	w2 := httptest.NewRecorder()
	handleShortlinkCreate(w2, req2)

	var resp2 ShortLinkResponse
	json.NewDecoder(w2.Body).Decode(&resp2)

	if resp1.Code != resp2.Code {
		t.Errorf("expected same code for same path: %q vs %q", resp1.Code, resp2.Code)
	}
}

func TestHandleShortlinkRedirect(t *testing.T) {
	shortlinks = shortlinkStore{
		byCode: map[string]string{"abc1234": "Test Message"},
		byPath: map[string]string{"Test Message": "abc1234"},
		loaded: true,
	}

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantLoc    string
	}{
		{
			name:       "valid code",
			path:       "/s/abc1234",
			wantStatus: http.StatusFound,
			wantLoc:    "/Test_Message",
		},
		{
			name:       "invalid code",
			path:       "/s/invalid",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "empty code",
			path:       "/s/",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			handleShortlinkRedirect(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantLoc != "" {
				location := w.Header().Get("Location")
				if location != tt.wantLoc {
					t.Errorf("Location = %q, want %q", location, tt.wantLoc)
				}
			}
		})
	}
}

func TestHandlePageStatic(t *testing.T) {
	tests := []struct {
		path       string
		wantStatus int
		wantType   string
	}{
		{"/styles.css", http.StatusOK, "text/css"},
		{"/app.js", http.StatusOK, "application/javascript"},
		{"/favicon.svg", http.StatusOK, "image/svg+xml"},
		{"/privacy", http.StatusOK, "text/html"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			handlePage(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantType != "" {
				ct := w.Header().Get("Content-Type")
				if !strings.Contains(ct, tt.wantType) {
					t.Errorf("Content-Type = %q, should contain %q", ct, tt.wantType)
				}
			}
		})
	}
}

func TestHandlePageMethodNotAllowed(t *testing.T) {
	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/", nil)
			w := httptest.NewRecorder()

			handlePage(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}

func TestHandlePageTooLong(t *testing.T) {
	longPath := "/" + strings.Repeat("a", 600)
	req := httptest.NewRequest(http.MethodGet, longPath, nil)
	w := httptest.NewRecorder()

	handlePage(w, req)

	if w.Code != http.StatusRequestURITooLong {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestURITooLong)
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	headers := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'self'",
	}

	for key, want := range headers {
		got := w.Header().Get(key)
		if !strings.Contains(got, want) {
			t.Errorf("header %q = %q, should contain %q", key, got, want)
		}
	}
}

// ============================================================================
// Body Reading Tests
// ============================================================================

func TestReadLimitedBody(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentLen  int64
		maxBytes    int64
		wantErr     bool
		wantErrType error
	}{
		{
			name:       "within limit",
			body:       "short",
			contentLen: 5,
			maxBytes:   100,
			wantErr:    false,
		},
		{
			name:        "exceeds limit via content-length",
			body:        "x",
			contentLen:  200,
			maxBytes:    100,
			wantErr:     true,
			wantErrType: errTooLarge,
		},
		{
			name:        "exceeds limit via actual body",
			body:        strings.Repeat("x", 200),
			contentLen:  -1,
			maxBytes:    100,
			wantErr:     true,
			wantErrType: errTooLarge,
		},
		{
			name:       "empty body",
			body:       "",
			contentLen: 0,
			maxBytes:   100,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			r.ContentLength = tt.contentLen

			data, err := readLimitedBody(r, tt.maxBytes)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.wantErrType != nil && err != tt.wantErrType {
					t.Errorf("error = %v, want %v", err, tt.wantErrType)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if string(data) != tt.body {
					t.Errorf("data = %q, want %q", string(data), tt.body)
				}
			}
		})
	}
}

// ============================================================================
// Render Index HTML Tests (comprehensive)
// ============================================================================

func TestRenderIndexHTMLComprehensive(t *testing.T) {
	template := "__TITLE__ | __OG_TITLE__ | __OG_DESC__ | __MESSAGE__ | __PUNCT__ | __OG_URL__ | __OG_IMAGE__ | __GREETING__ | __SUBTITLE__ | __THEME_CLASS__"

	tests := []struct {
		name       string
		path       string
		checkTitle bool
		checkPunct bool
		checkName  bool
	}{
		{"empty path", "", true, true, true},
		{"simple name", "/Renato", true, true, true},
		{"lowercase name", "/renato", true, true, true},
		{"with punctuation", "/Renato!", true, true, false},
		{"encoded punctuation", "/Renato%21", true, true, false},
		{"proper name multiple words", "/João Silva", true, true, true},
		{"você prefix", "/você é legal", true, true, true},
		{"special chars", "/João & José", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := renderIndexHTML(template, tt.path, "")
			if result == template {
				t.Error("template was not modified")
			}
			if strings.Contains(result, "__") {
				t.Error("template placeholders not replaced")
			}
		})
	}
}

func TestThemeClass(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"light", "theme-light"},
		{"LIGHT", "theme-light"},
		{"warm", "theme-warm"},
		{"elegant", "theme-elegant"},
		{"pixel", "theme-pixel"},
		{"invalid", ""},
		{"<script>", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := themeClass(tt.input)
			if got != tt.want {
				t.Errorf("themeClass(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseOccasionFromPath(t *testing.T) {
	tests := []struct {
		path        string
		wantGreet   string
		wantMessage string
	}{
		{"/", "Parabéns", ""},
		{"/João", "Parabéns", "João"},
		{"/aniversario/João", "Feliz Aniversário", "João"},
		{"/ANIVERSARIO/Maria", "Feliz Aniversário", "Maria"},
		{"/formatura/Ana", "Parabéns pela formatura", "Ana"},
		{"/casamento/Pedro_e_Ana", "Felicidades", "Pedro_e_Ana"},
		{"/boas-vindas/Novo_Membro", "Boas-vindas", "Novo_Membro"},
		{"/promocao/Carlos", "Parabéns pela promoção", "Carlos"},
		{"/unknown/Test", "Parabéns", "unknown/Test"},
		{"/aniversario/", "Feliz Aniversário", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			occ, msg := parseOccasionFromPath(tt.path)
			if occ.Greeting != tt.wantGreet {
				t.Errorf("parseOccasionFromPath(%q) greeting = %q, want %q", tt.path, occ.Greeting, tt.wantGreet)
			}
			if msg != tt.wantMessage {
				t.Errorf("parseOccasionFromPath(%q) message = %q, want %q", tt.path, msg, tt.wantMessage)
			}
		})
	}
}

// ============================================================================
// Concurrency Tests
// ============================================================================

func TestShortlinkConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "shortlinks.json")
	oldEnv := os.Getenv("SHORTLINK_DB")
	os.Setenv("SHORTLINK_DB", dbPath)
	defer os.Setenv("SHORTLINK_DB", oldEnv)

	shortlinks = shortlinkStore{
		byCode: map[string]string{},
		byPath: map[string]string{},
	}

	var wg sync.WaitGroup
	concurrency := 10

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			path := fmt.Sprintf("Path %d", id)
			body := fmt.Sprintf(`{"path":"%s"}`, path)
			req := httptest.NewRequest(http.MethodPost, "/s", strings.NewReader(body))
			req.RemoteAddr = fmt.Sprintf("192.168.1.%d:12345", id)
			w := httptest.NewRecorder()
			handleShortlinkCreate(w, req)

			if w.Code != http.StatusCreated {
				t.Errorf("goroutine %d: status = %d", id, w.Code)
			}
		}(i)
	}

	wg.Wait()

	shortlinks.mu.Lock()
	count := len(shortlinks.byCode)
	shortlinks.mu.Unlock()

	if count != concurrency {
		t.Errorf("expected %d shortlinks, got %d", concurrency, count)
	}
}

func TestRateLimiterConcurrency(t *testing.T) {
	rl := &rateLimiter{
		hits:   map[string][]time.Time{},
		window: time.Second,
		max:    10,
	}

	var wg sync.WaitGroup
	allowed := 0
	var mu sync.Mutex

	// Send 20 concurrent requests from same IP
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rl.allow("test-ip") {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	mu.Lock()
	final := allowed
	mu.Unlock()

	if final != 10 {
		t.Errorf("expected exactly 10 allowed, got %d", final)
	}
}

// ============================================================================
// Additional Handler and Error Path Tests
// ============================================================================

func TestStatusFromError(t *testing.T) {
	tests := []struct {
		err    error
		status int
	}{
		{errTooLarge, http.StatusRequestEntityTooLarge},
		{fmt.Errorf("other error"), http.StatusBadRequest},
	}

	for _, tt := range tests {
		got := statusFromError(tt.err)
		if got != tt.status {
			t.Errorf("statusFromError(%v) = %d, want %d", tt.err, got, tt.status)
		}
	}
}

func TestEscapeXML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"test", "test"},
		{"<tag>", "&lt;tag&gt;"},
		{"a & b", "a &amp; b"},
	}

	for _, tt := range tests {
		got := escapeXML(tt.input)
		if got != tt.want {
			t.Errorf("escapeXML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestServeIndex(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"root path", "/", http.StatusOK},
		{"simple name", "/Renato", http.StatusOK},
		{"with query", "/Renato?source=test", http.StatusOK},
		{"unicode name", "/João", http.StatusOK},
		{"empty after slash", "/", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			serveIndex(w, req, tt.path)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			// Check that response contains HTML
			if w.Code == http.StatusOK {
				body := w.Body.String()
				if !strings.Contains(body, "<!DOCTYPE html>") && !strings.Contains(body, "<html") {
					t.Error("response should contain HTML")
				}
			}
		})
	}
}

func TestServeIndexBlocked(t *testing.T) {
	// Setup blocked terms
	blockedOnce = sync.Once{}
	blockedOnce.Do(func() {
		blockedTerms = []string{"blocked"}
	})

	req := httptest.NewRequest(http.MethodGet, "/blocked", nil)
	w := httptest.NewRecorder()

	serveIndex(w, req, "/blocked")

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}

	body := w.Body.String()
	if !strings.Contains(body, "não está disponível") {
		t.Error("blocked message should contain appropriate error text")
	}
}

func TestHandleOgImageDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/og-image.png", nil)
	w := httptest.NewRecorder()

	handleOgImage(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "image/png" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/png")
	}
}

func TestHandleOgImageWithText(t *testing.T) {
	// Mock the render function to avoid needing rsvg-convert
	oldRender := renderOgImageToFileFunc
	defer func() { renderOgImageToFileFunc = oldRender }()

	tmpDir := t.TempDir()
	os.Setenv("XDG_CACHE_DIR", tmpDir)
	defer os.Unsetenv("XDG_CACHE_DIR")

	renderOgImageToFileFunc = func(text, destPath string) error {
		// Create a fake PNG file
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(destPath, []byte("fake png data"), 0o644)
	}

	req := httptest.NewRequest(http.MethodGet, "/og-image.png?text=Test", nil)
	w := httptest.NewRecorder()

	handleOgImage(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "image/png" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/png")
	}
}

func TestHandleOgImageBlocked(t *testing.T) {
	blockedOnce = sync.Once{}
	blockedOnce.Do(func() {
		blockedTerms = []string{"blocked"}
	})

	req := httptest.NewRequest(http.MethodGet, "/og-image.png?text=blocked", nil)
	w := httptest.NewRecorder()

	handleOgImage(w, req)

	// Should fall back to default image
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHandleOgImageMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/og-image.png", nil)
	w := httptest.NewRecorder()

	handleOgImage(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestOgCacheDir(t *testing.T) {
	// Test with XDG_CACHE_DIR
	os.Setenv("XDG_CACHE_DIR", "/test/cache")
	defer os.Unsetenv("XDG_CACHE_DIR")

	dir := ogCacheDir()
	if !strings.Contains(dir, "/test/cache") {
		t.Errorf("ogCacheDir() = %q, should contain /test/cache", dir)
	}
}

func TestOgCacheDirXDGHome(t *testing.T) {
	os.Unsetenv("XDG_CACHE_DIR")
	os.Setenv("XDG_CACHE_HOME", "/test/home")
	defer os.Unsetenv("XDG_CACHE_HOME")

	dir := ogCacheDir()
	if !strings.Contains(dir, "/test/home") {
		t.Errorf("ogCacheDir() = %q, should contain /test/home", dir)
	}
}

func TestEnsureShortlinksLoadedError(t *testing.T) {
	// Test with invalid JSON file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "bad.json")
	os.WriteFile(dbPath, []byte("invalid json{"), 0o644)

	oldEnv := os.Getenv("SHORTLINK_DB")
	os.Setenv("SHORTLINK_DB", dbPath)
	defer os.Setenv("SHORTLINK_DB", oldEnv)

	shortlinks = shortlinkStore{
		byCode: map[string]string{},
		byPath: map[string]string{},
		loaded: false,
	}

	err := ensureShortlinksLoaded()
	if err == nil {
		t.Error("expected error loading invalid JSON")
	}
}

func TestPersistShortlinks(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir", "shortlinks.json")

	oldEnv := os.Getenv("SHORTLINK_DB")
	os.Setenv("SHORTLINK_DB", dbPath)
	defer os.Setenv("SHORTLINK_DB", oldEnv)

	shortlinks = shortlinkStore{
		byCode: map[string]string{"test123": "Test Path"},
		byPath: map[string]string{"Test Path": "test123"},
		loaded: true,
	}

	shortlinks.mu.Lock()
	err := persistShortlinksLocked()
	shortlinks.mu.Unlock()

	if err != nil {
		t.Fatalf("persistShortlinksLocked() error = %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("shortlinks file was not created")
	}

	// Verify content
	data, _ := os.ReadFile(dbPath)
	if !strings.Contains(string(data), "test123") {
		t.Error("shortlinks file doesn't contain expected data")
	}
}

func TestShortlinkDBPathDefault(t *testing.T) {
	os.Unsetenv("SHORTLINK_DB")
	path := shortlinkDBPath()
	if path != "data/shortlinks.json" {
		t.Errorf("shortlinkDBPath() = %q, want %q", path, "data/shortlinks.json")
	}
}

func TestShortlinkDBPathCustom(t *testing.T) {
	os.Setenv("SHORTLINK_DB", "/custom/path.json")
	defer os.Unsetenv("SHORTLINK_DB")

	path := shortlinkDBPath()
	if path != "/custom/path.json" {
		t.Errorf("shortlinkDBPath() = %q, want %q", path, "/custom/path.json")
	}
}

func TestPublicBaseURLDefault(t *testing.T) {
	os.Unsetenv("PUBLIC_BASE_URL")
	url := publicBaseURL()
	expected := "https://parabens.vc"
	if url != expected {
		t.Errorf("publicBaseURL() = %q, want %q", url, expected)
	}
}

func TestPublicBaseURLCustom(t *testing.T) {
	os.Setenv("PUBLIC_BASE_URL", "https://custom.example.com")
	defer os.Unsetenv("PUBLIC_BASE_URL")

	url := publicBaseURL()
	if url != "https://custom.example.com" {
		t.Errorf("publicBaseURL() = %q, want %q", url, "https://custom.example.com")
	}
}

func TestHandleTrackRateLimit(t *testing.T) {
	// Create new rate limiter with low limit for testing
	trackLimiter = &rateLimiter{
		hits:   map[string][]time.Time{},
		window: time.Minute,
		max:    2,
	}

	ip := "192.168.1.100"

	// First 2 requests should succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/track", strings.NewReader(`{"event":"test"}`))
		req.RemoteAddr = ip + ":12345"
		w := httptest.NewRecorder()
		handleTrack(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("request %d: status = %d, want %d", i+1, w.Code, http.StatusNoContent)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest(http.MethodPost, "/api/track", strings.NewReader(`{"event":"test"}`))
	req.RemoteAddr = ip + ":12345"
	w := httptest.NewRecorder()
	handleTrack(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	// Reset for other tests
	trackLimiter = &rateLimiter{
		hits:   map[string][]time.Time{},
		window: trackRateWindow,
		max:    trackRateLimit,
	}
}

func TestHandleShortlinkCreateRateLimit(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "shortlinks.json")
	oldEnv := os.Getenv("SHORTLINK_DB")
	os.Setenv("SHORTLINK_DB", dbPath)
	defer os.Setenv("SHORTLINK_DB", oldEnv)

	shortlinks = shortlinkStore{
		byCode: map[string]string{},
		byPath: map[string]string{},
	}

	// Create new rate limiter with low limit
	shortlinkLimiter = &rateLimiter{
		hits:   map[string][]time.Time{},
		window: time.Minute,
		max:    1,
	}

	ip := "192.168.1.200"

	// First request should succeed
	req1 := httptest.NewRequest(http.MethodPost, "/s", strings.NewReader(`{"path":"Test1"}`))
	req1.RemoteAddr = ip + ":12345"
	w1 := httptest.NewRecorder()
	handleShortlinkCreate(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Errorf("first request: status = %d, want %d", w1.Code, http.StatusCreated)
	}

	// Second request should be rate limited
	req2 := httptest.NewRequest(http.MethodPost, "/s", strings.NewReader(`{"path":"Test2"}`))
	req2.RemoteAddr = ip + ":12345"
	w2 := httptest.NewRecorder()
	handleShortlinkCreate(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: status = %d, want %d", w2.Code, http.StatusTooManyRequests)
	}

	// Reset for other tests
	shortlinkLimiter = &rateLimiter{
		hits:   map[string][]time.Time{},
		window: shortlinkRateWindow,
		max:    shortlinkRateLimit,
	}
}

func TestHandleTrackTooLarge(t *testing.T) {
	largeBody := strings.Repeat("x", int(maxTrackBodyBytes)+100)
	req := httptest.NewRequest(http.MethodPost, "/api/track", strings.NewReader(largeBody))
	req.RemoteAddr = "192.168.1.1:12345"
	req.ContentLength = int64(len(largeBody))
	w := httptest.NewRecorder()

	handleTrack(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHandleShortlinkCreateTooLarge(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "shortlinks.json")
	oldEnv := os.Getenv("SHORTLINK_DB")
	os.Setenv("SHORTLINK_DB", dbPath)
	defer os.Setenv("SHORTLINK_DB", oldEnv)

	shortlinks = shortlinkStore{
		byCode: map[string]string{},
		byPath: map[string]string{},
	}

	largeBody := `{"path":"` + strings.Repeat("x", int(maxShortlinkBodyBytes)) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/s", strings.NewReader(largeBody))
	req.RemoteAddr = "192.168.1.1:12345"
	req.ContentLength = int64(len(largeBody))
	w := httptest.NewRecorder()

	handleShortlinkCreate(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Test with existing file
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("test"), 0o644)

	exists, err := fileExists(testFile)
	if err != nil {
		t.Errorf("fileExists() error = %v", err)
	}
	if !exists {
		t.Error("fileExists() should return true for existing file")
	}

	// Test with non-existing file
	exists, err = fileExists(filepath.Join(tmpDir, "nonexistent.txt"))
	if err != nil {
		t.Errorf("fileExists() error = %v", err)
	}
	if exists {
		t.Error("fileExists() should return false for non-existing file")
	}

	// Test with directory (should return false)
	exists, err = fileExists(tmpDir)
	if err != nil {
		t.Errorf("fileExists() error = %v", err)
	}
	if exists {
		t.Error("fileExists() should return false for directory")
	}
}

func TestResponseRecorder(t *testing.T) {
	// Test default status
	rec := httptest.NewRecorder()
	rr := &responseRecorder{ResponseWriter: rec, status: http.StatusOK}

	if rr.status != http.StatusOK {
		t.Errorf("initial status = %d, want %d", rr.status, http.StatusOK)
	}

	// Test WriteHeader
	rr.WriteHeader(http.StatusNotFound)
	if rr.status != http.StatusNotFound {
		t.Errorf("status after WriteHeader = %d, want %d", rr.status, http.StatusNotFound)
	}

	// Test Write
	n, err := rr.Write([]byte("hello"))
	if err != nil {
		t.Errorf("Write() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Write() returned %d, want 5", n)
	}
	if rr.size != 5 {
		t.Errorf("size = %d, want 5", rr.size)
	}

	// Test multiple writes
	rr.Write([]byte(" world"))
	if rr.size != 11 {
		t.Errorf("size after second write = %d, want 11", rr.size)
	}
}

func TestWithRequestLogging(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("test response"))
	})

	logged := withRequestLogging(handler)

	req := httptest.NewRequest(http.MethodGet, "/test-path", nil)
	rec := httptest.NewRecorder()

	logged.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if rec.Body.String() != "test response" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "test response")
	}
}
