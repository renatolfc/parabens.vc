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
