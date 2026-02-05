package main

import (
	"strings"
	"sync"
)

var (
	blockedOnce  sync.Once
	blockedTerms []string
)

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

// Suspicious file extensions commonly used in exploit attempts
var suspiciousExtensions = []string{
	".php", ".asp", ".aspx", ".jsp", ".cgi", ".sql", ".bak",
	".env", ".xml", ".json", ".yml", ".yaml", ".ini",
	".conf", ".htaccess", ".htpasswd", ".log", ".tar", ".gz",
	".zip", ".rar", ".exe", ".sh", ".bat", ".ps1",
}

// Path prefixes commonly used in CMS exploit attempts (must include separator)
var suspiciousPathPrefixes = []string{
	"wp-admin/", "wp-admin\\", "wp-content/", "wp-content\\", "wp-includes/", "wp-includes\\",
	"wordpress/", "wordpress\\", "xmlrpc", "phpmyadmin/", "phpmyadmin\\",
	"cgi-bin/", "cgi-bin\\", "admin/", "admin\\", ".well-known/", ".well-known\\",
	"api/", "api\\", ".git/", ".git\\", "etc/passwd", "etc/shadow", "etc\\passwd", "etc\\shadow",
}

// looksLikePath returns true if the input looks like a file path or URL
// rather than a person's name. Used to reject bot exploit attempts early.
func looksLikePath(path string) bool {
	if path == "" {
		return false
	}
	lower := strings.ToLower(path)

	// Check for directory traversal
	if strings.Contains(lower, "../") || strings.Contains(lower, "..\\") {
		return true
	}

	// Check for URL schemes
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "ftp://") {
		return true
	}

	// Check for suspicious file extensions
	for _, ext := range suspiciousExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}

	// Check for suspicious path prefixes
	for _, prefix := range suspiciousPathPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}

	return false
}
