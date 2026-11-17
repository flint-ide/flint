//go:build !server

package main

import (
	"strings"
	"testing"
)

func TestNotificationTextMasksSecrets(t *testing.T) {
	tests := []struct {
		name, in string
		hidden   string
	}{
		{"github token", "app: Run curl -H 'Authorization: token ghp_abcdefghijklmnopqrstuvwxyz0123456789'", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"},
		{"assignment", `app: Run API_KEY="s3cr3tvalue123" ./deploy`, "s3cr3tvalue123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notificationText(tt.in)
			if strings.Contains(got, tt.hidden) {
				t.Errorf("notificationText(%q) = %q, still holds the secret", tt.in, got)
			}
		})
	}
	if got := notificationText("app: Run npm test"); got != "app: Run npm test" {
		t.Errorf("plain text changed: %q", got)
	}
}
