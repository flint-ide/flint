package agentsession

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
)

func TestWithFolders(t *testing.T) {
	ctx := context.Background()
	if WithFolders(ctx, nil) != ctx || Folders(ctx) != nil {
		t.Error("no folders changed the context")
	}
	in := []string{"/a", "/b"}
	got := Folders(WithFolders(ctx, in))
	in[0] = "/changed"
	if !slices.Equal(got, []string{"/a", "/b"}) {
		t.Errorf("Folders = %v", got)
	}
	if err := RefuseFolders(ctx, "x"); err != nil {
		t.Errorf("no folders refused: %v", err)
	}
	if err := RefuseFolders(WithFolders(ctx, in), "x"); err == nil {
		t.Error("folders not refused")
	}
}

func TestCheckFolders(t *testing.T) {
	cwd, a, b := t.TempDir(), t.TempDir(), t.TempDir()
	many := make([]string, MaxFolders+1)
	for i := range many {
		many[i] = filepath.Join(a, string(rune('a'+i)))
	}
	trustAllBut := func(p string) func(string) bool { return func(x string) bool { return x == p } }
	for _, c := range []struct {
		name      string
		folders   []string
		untrusted func(string) bool
		ok        bool
	}{
		{"none", nil, nil, true},
		{"two", []string{a, b}, nil, true},
		{"trusted", []string{a, b}, trustAllBut("/elsewhere"), true},
		{"relative", []string{"a"}, nil, false},
		{"unclean", []string{a + "/"}, nil, false},
		{"missing", []string{filepath.Join(a, "missing")}, nil, false},
		{"twice", []string{a, a}, nil, false},
		{"the cwd", []string{cwd}, nil, false},
		{"too many", many, nil, false},
		{"untrusted folder", []string{a, b}, trustAllBut(b), false},
		{"untrusted cwd", []string{a}, trustAllBut(cwd), false},
	} {
		if err := CheckFolders(cwd, c.folders, c.untrusted); (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// Edits and reads in a session's additional folders are auto-approved as in
// its cwd, with the same protected directories; elsewhere they still ask.
func TestPolicyWithFolders(t *testing.T) {
	cwd, other := t.TempDir(), t.TempDir()
	def := DefaultPolicy()
	for _, c := range []struct {
		name string
		tc   ToolCall
		want bool
	}{
		{"edit in cwd", ToolCall{Kind: ToolEdit, Paths: []string{"src/a.go"}}, true},
		{"edit in the folder", ToolCall{Kind: ToolEdit, Paths: []string{filepath.Join(other, "b.go")}}, true},
		{"edits in both", ToolCall{Kind: ToolEdit, Paths: []string{filepath.Join(cwd, "a"), filepath.Join(other, "b")}}, true},
		{"read in the folder", ToolCall{Kind: ToolRead, Paths: []string{filepath.Join(other, "b.go")}}, true},
		{"git hook in the folder", ToolCall{Kind: ToolEdit, Paths: []string{filepath.Join(other, ".git", "hooks", "x")}}, false},
		{"elsewhere", ToolCall{Kind: ToolEdit, Paths: []string{"/etc/hosts"}}, false},
	} {
		if got := def.autoApproves(c.tc, cwd, other); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if def.autoApproves(ToolCall{Kind: ToolEdit, Paths: []string{filepath.Join(other, "b.go")}}, cwd) {
		t.Error("a session without folders auto-approves an edit outside its cwd")
	}
}

// The manager answers a permission request in an additional folder by the
// policy, as one in the cwd: the folders come from the start's context.
func TestManagerAutoApprovesInFolders(t *testing.T) {
	other := t.TempDir()
	m := NewManager((&recorder{}).emit)
	fake := newFake(ToolEdit, filepath.Join(other, "a.go"))
	m.Register("fake", fake)
	info, err := m.Start(WithFolders(context.Background(), []string{other}), "fake", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prompt(info.ID, "hi", nil); err != nil {
		t.Fatal(err)
	}
	if opt := <-fake.answered; opt != "yes" {
		t.Errorf("answered %q", opt)
	}
	close(fake.release)
}
