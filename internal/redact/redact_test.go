package redact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	home, _ := os.UserHomeDir()
	in := strings.Join([]string{
		"open " + filepath.Join(home, "projects", "acme") + " failed",
		"key AKIAIOSFODNN7EXAMPLE and ASIAABCDEFGHIJKLMNOP",
		"token ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"Authorization: Bearer abc.def.ghi-123456",
		`{"password": "hunter2", "api_key":"k-123"}`,
		"postgres://admin:s3cret@db.internal:5432/app",
		"jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.SflKxwRJSMeKKF2QT4fwpM",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----",
	}, "\n")
	out := String(in)
	for _, leak := range []string{home, "AKIAIOSFODNN7EXAMPLE", "ASIAABCDEFGHIJKLMNOP", "ghp_abc", "abc.def.ghi", "hunter2", "k-123", "s3cret", "eyJzdWIi", "MIIEow"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q survived:\n%s", leak, out)
		}
	}
	if got := String("token=ghp_abcdefghijklmnopqrstuvwxyz0123456789"); got != "token=[github token]" {
		t.Errorf("double redaction: %q", got)
	}
	for _, kept := range []string{"~/projects/acme", "db.internal:5432/app", "[aws key id]", "[private key]"} {
		if !strings.Contains(out, kept) {
			t.Errorf("%q missing:\n%s", kept, out)
		}
	}
}

func TestSecretsKeepsPaths(t *testing.T) {
	home, _ := os.UserHomeDir()
	in := filepath.Join(home, "projects") + " token=abc123456"
	if got := Secrets(in); got != filepath.Join(home, "projects")+" token=[redacted]" {
		t.Fatalf("Secrets = %q", got)
	}
}
