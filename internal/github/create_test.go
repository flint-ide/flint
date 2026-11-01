package github

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestCreatePullRequest(t *testing.T) {
	f := newFake(t)
	f.on("pr create", "Creating pull request for agent/x into main\n\nhttps://github.com/acme/flint/pull/12\n")
	c := f.client()
	url, err := c.CreatePullRequest(context.Background(), t.TempDir(), NewPullRequest{
		Base: "main", Head: "agent/x", Title: "--fill Add a button", Body: "Body with @file and --flags",
	})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/acme/flint/pull/12" {
		t.Errorf("url = %q", url)
	}
	calls := f.calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %v", calls)
	}
	want := []string{"pr", "create", "--base=main", "--head=agent/x", "--title=--fill Add a button", "--body-file=-"}
	if !slices.Equal(calls[0].args, want) {
		t.Errorf("args = %q\nwant %q", calls[0].args, want)
	}
	if calls[0].stdin != "Body with @file and --flags" {
		t.Errorf("body on stdin = %q", calls[0].stdin)
	}
}

func TestCreatePullRequestValidates(t *testing.T) {
	c := newFake(t).client()
	for _, pr := range []NewPullRequest{
		{Base: "", Head: "x", Title: "t"},
		{Base: "main", Head: "-x", Title: "t"},
		{Base: "main", Head: "x", Title: " "},
		{Base: "main", Head: "x", Title: "a\nb"},
		{Base: "main", Head: "x", Title: strings.Repeat("t", 300)},
	} {
		if _, err := c.CreatePullRequest(context.Background(), t.TempDir(), pr); err == nil {
			t.Errorf("%+v accepted", pr)
		}
	}
}

func TestCreatePullRequestError(t *testing.T) {
	f := newFake(t)
	f.onExit("pr create", "", "a pull request for branch \"agent/x\" into branch \"main\" already exists:\nhttps://github.com/acme/flint/pull/3", 1)
	_, err := f.client().CreatePullRequest(context.Background(), t.TempDir(), NewPullRequest{Base: "main", Head: "agent/x", Title: "t"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v", err)
	}
}
