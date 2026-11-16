package settings

import (
	"strings"
	"testing"
)

func TestPRReviewDefaults(t *testing.T) {
	if d := Default().PRReview; d != (PRReview{MaxThreads: 20, ContextLines: 8}) {
		t.Errorf("defaults = %+v", d)
	}
}

func TestCheckPRReview(t *testing.T) {
	good := []PRReview{
		defaultPRReview(),
		{MaxThreads: MinPRReviewThreads, ContextLines: 0},
		{MaxThreads: MaxPRReviewThreads, ContextLines: MaxPRReviewContextLines},
	}
	for _, p := range good {
		if err := CheckPRReview(p); err != nil {
			t.Errorf("%+v: %v", p, err)
		}
	}
	bad := map[string]PRReview{
		"none":    {MaxThreads: 0, ContextLines: 8},
		"many":    {MaxThreads: MaxPRReviewThreads + 1, ContextLines: 8},
		"context": {MaxThreads: 20, ContextLines: MaxPRReviewContextLines + 1},
		"neg":     {MaxThreads: 20, ContextLines: -1},
	}
	for name, p := range bad {
		if err := CheckPRReview(p); err == nil || !strings.Contains(err.Error(), "review") {
			t.Errorf("%s: %+v accepted (%v)", name, p, err)
		}
	}
	s := Default()
	s.PRReview.MaxThreads = 500
	if Validate(s) == nil {
		t.Error("Validate accepted 500 threads per draft")
	}
}

func TestPRReviewNormalize(t *testing.T) {
	s, err := decode([]byte(`{"prReview":{"maxThreads":999,"contextLines":-3}}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := (PRReview{MaxThreads: MaxPRReviewThreads, ContextLines: 0}); s.PRReview != want {
		t.Errorf("normalized = %+v, want %+v", s.PRReview, want)
	}
	s, _ = decode([]byte(`{"prReview":{"contextLines":0}}`))
	if want := (PRReview{MaxThreads: 20, ContextLines: 0}); s.PRReview != want {
		t.Errorf("missing keys = %+v, want %+v", s.PRReview, want)
	}
	s, _ = decode([]byte(`{}`))
	if s.PRReview != defaultPRReview() {
		t.Errorf("no key = %+v, want defaults", s.PRReview)
	}
}
