// Package redact takes credentials and the home directory out of text that
// leaves Flint's control: bug reports, logs, and context sent to AI agents.
package redact

import (
	"os"
	"regexp"
	"strings"
)

// secretPatterns match credentials that may appear in log lines or command
// output; each is replaced by its label.
var secretPatterns = []struct {
	re    *regexp.Regexp
	label string
}{
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), "[private key]"},
	{regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), "[aws key id]"},
	{regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}\b|\bgithub_pat_[A-Za-z0-9_]{20,}\b`), "[github token]"},
	{regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`), "[slack token]"},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`), "[api key]"},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`), "[jwt]"},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`), "$1 [redacted]"},
	{regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^\s/:@]+:[^\s/@]+@`), "$1[credentials]@"},
	{regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|session[_-]?token|private[_-]?key|authorization)(["']?\s*[:=]\s*["']?)[^\s"',;\[][^\s"',;]*`), "$1$2[redacted]"},
}

// String removes credentials and replaces the home directory with ~ (user
// names and project paths are personal).
func String(s string) string {
	s = Secrets(s)
	if home, err := os.UserHomeDir(); err == nil && len(home) > 1 {
		s = strings.ReplaceAll(s, home, "~")
	}
	return s
}

// Secrets removes credentials only (paths stay usable, e.g. in saved sessions).
func Secrets(s string) string {
	for _, p := range secretPatterns {
		s = p.re.ReplaceAllString(s, p.label)
	}
	return s
}
