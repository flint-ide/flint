package remote

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestSplitID(t *testing.T) {
	tests := []struct {
		id               string
		machine, session string
		wantErr          bool
		notRemote        bool
	}{
		{id: "dev-box:0f8e6c1e-1111-4222-8333-444455556666", machine: "dev-box", session: "0f8e6c1e-1111-4222-8333-444455556666"},
		{id: "a:b", machine: "a", session: "b"},
		{id: "0f8e6c1e-1111-4222-8333-444455556666", wantErr: true, notRemote: true},
		{id: "", wantErr: true, notRemote: true},
		{id: ":x", wantErr: true},      // no machine
		{id: "box:", wantErr: true},    // no session
		{id: "box:a:b", wantErr: true}, // two separators: ambiguous
		{id: "Box:x", wantErr: true},   // upper case is not a machine id
		{id: "-box:x", wantErr: true},  // leading dash
		{id: "bo x:y", wantErr: true},  // space
		{id: "box\n:x", wantErr: true}, // control character
		{id: strings.Repeat("a", 33) + ":x", wantErr: true},
		{id: "../etc:x", wantErr: true},
		{id: "docker:abc:def", wantErr: true},
	}
	for _, tt := range tests {
		m, s, err := SplitID(tt.id)
		if (err != nil) != tt.wantErr {
			t.Errorf("SplitID(%q) error = %v, want error %v", tt.id, err, tt.wantErr)
			continue
		}
		if tt.notRemote && !errors.Is(err, ErrNotRemoteID) {
			t.Errorf("SplitID(%q) = %v, want ErrNotRemoteID", tt.id, err)
		}
		if !tt.wantErr && (m != tt.machine || s != tt.session) {
			t.Errorf("SplitID(%q) = %q, %q", tt.id, m, s)
		}
	}
}

func TestJoinThenSplitRoundTrips(t *testing.T) {
	id := JoinID("box", "abc")
	m, s, err := SplitID(id)
	if err != nil || m != "box" || s != "abc" || !IsRemoteID(id) {
		t.Fatalf("got %q %q %v", m, s, err)
	}
}

// Local terminal ids are made by crypto.randomUUID in the web view and by
// randomID / agenttask.NewID in Go: all hex and dashes. None has a ':', so
// none is ever taken for a remote id.
func TestLocalIDsAreNeverRemote(t *testing.T) {
	b := make([]byte, 16)
	for range 500 {
		_, _ = rand.Read(b)
		h := hex.EncodeToString(b)
		uuid := h[:8] + "-" + h[8:12] + "-4" + h[13:16] + "-8" + h[17:20] + "-" + h[20:]
		for _, id := range []string{uuid, h, "agent-" + h, "run-" + uuid} {
			if IsRemoteID(id) {
				t.Fatalf("local id %q looks remote", id)
			}
		}
	}
}
