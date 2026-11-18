package projects

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCloneSource(t *testing.T) {
	for _, tc := range []struct {
		url, name, protocol string
	}{
		{"https://github.com/acme/api.git", "api", "https"},
		{"https://github.com/acme/api", "api", "https"},
		{"https://gitlab.example.com:8443/group/sub/web.git/", "web", "https"},
		{"https://github.com/acme/my.site.git", "my.site", "https"},
		{"ssh://git@github.com:22/acme/api", "api", "ssh"},
		{"ssh://github.com/~me/tools_x", "tools_x", "ssh"},
		{"git@github.com:acme/api.git", "api", "ssh"},
		{"github.com:acme/web", "web", "ssh"},
		{"me.name@host.example:/srv/git/api.git", "api", "ssh"},
	} {
		src, err := CloneSource(tc.url)
		if err != nil || src.Name != tc.name || !slices.Equal(src.Protocols, []string{tc.protocol}) || src.URL != tc.url {
			t.Errorf("%s: %+v %v", tc.url, src, err)
		}
	}
	for _, tc := range []struct{ url, want string }{
		{"", "empty"},
		{"ext::sh -c touch% /tmp/pwned", "spaces"},
		{"ext::ssh-c", "only https://"},
		{"fd::17", "only https://"},
		{"file:///srv/git/api.git", "only https://"},
		{"/srv/git/api.git", "only https://"},
		{"./api", "only https://"},
		{"../api", "only https://"},
		{"~/api", "only https://"},
		{`C:\repos\api`, "only https://"},
		{"C:/repos/api", `host "C"`},
		{"C:repos", `host "C"`},
		{"http://github.com/acme/api.git", "only https://"},
		{"git://github.com/acme/api.git", "only https://"},
		{"git+ssh://github.com/acme/api.git", "only https://"},
		{"HTTPS://github.com/acme/api.git", "only https://"},
		{"-oProxyCommand=touch@host:x", "starts with -"},
		{"--upload-pack=touch", "starts with -"},
		{"-u@host:path", "starts with -"},
		{"git@-oProxyCommand=x:path", `host "-oProxyCommand=x"`},
		{"a;b@host:path", `user name "a;b"`},
		{"-x@host:path", "starts with -"},
		{"git@host:-path", "starts with -"},
		{"git@host:", "starts with -"},
		{"ssh://-oProxyCommand=x/acme/api", `host "-oProxyCommand=x"`},
		{"ssh://-u@host/acme/api", `user name "-u"`},
		{"ssh://git@host:22x/acme/api", "can't be read"},
		{"https://user:token@github.com/acme/api.git", "password"},
		// A token pasted as the user would end up in the clone's config.
		{"https://ghp_abc123@github.com/acme/api.git", "holds a user name"},
		{"https://git@github.com/acme/my.site.git", "holds a user name"},
		{"https://@github.com/acme/api.git", "holds a user name"},
		{"https://github.com/acme/api.git?x=1", "query or a fragment"},
		{"https://github.com/acme/api.git#main", "query or a fragment"},
		{"https://[::1]/acme/api", "only https://"},
		{"https://github.com", "no repository path"},
		{"https://github.com/acme/../api", `path "/acme/../api"`},
		{"https://github.com/acme/..", `path "/acme/.."`},
		{"git@github.com:acme/..", `path "acme/.."`},
		{"git@github.com:..", `path ".."`},
		{"https://github.com/acme/.git", `last part ""`},
		{"https://github.com/acme/.hidden", `last part ".hidden"`},
		{"https://github.com/acme/CON.git", `last part "CON"`},
		{"https://github.com/acme/com1", `last part "com1"`},
		{"https://github.com/acme/a%2F..%2Fb", `path`},
		{"https://github.com/acme/a%0Ab", `path`},
		{"https://github.com/acme/ä", "non-ASCII"},
		{"https://github.com/acme/" + strings.Repeat("x", maxCloneName+1), "last part"},
		{"https://x/acme/api", `host "x"`},
	} {
		_, err := CloneSource(tc.url)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.url, err, tc.want)
		}
	}
}

// bareRepo makes a bare git repository with one commit (no network).
func bareRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	base := tempDir(t)
	work := filepath.Join(base, "work")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write(t, filepath.Join(work, "README"), "hi\n")
	run(base, "init", "-q", work)
	run(work, "add", "README")
	run(work, "commit", "-q", "-m", "first")
	bare := filepath.Join(base, "api.git")
	run(base, "clone", "-q", "--bare", work, bare)
	return bare
}

// cloneFixture clones through a test seam: CloneSource still checks the
// project's URL, then the local bare repository is cloned over file.
func cloneFixture(t *testing.T) (*fixture, string, string) {
	t.Helper()
	f := newFixture(t, nil)
	f.open()
	bare := bareRepo(t)
	f.api.clones.source = func(s *cloneSource) { s.URL, s.Protocols = bare, []string{"file"} }
	parent := filepath.Join(f.base, "src")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	return f, parent, filepath.Join(parent, "web")
}

func (f *fixture) cloneEvents(name string) []CloneEvent {
	var out []CloneEvent
	for _, e := range f.h.Events() {
		if e.Name == name {
			out = append(out, e.Data[0].(CloneEvent))
		}
	}
	return out
}

func leftovers(t *testing.T, dir string) []string {
	t.Helper()
	names, _ := filepath.Glob(filepath.Join(dir, ".flint-clone-*"))
	return names
}

func TestClone(t *testing.T) {
	f, parent, dest := cloneFixture(t)
	f.h.Answer = true
	f.h.SetRepository(dest, "https://github.com/acme/web.git") // the fake host's view of the clone
	v, err := f.api.Clone(f.plan, "web", parent)
	if err != nil {
		t.Fatal(err)
	}
	if v.Folder != dest || !v.Located || v.Trusted || v.InWorkspace || v.Problem != "" {
		t.Errorf("view %+v", v)
	}
	if data, err := os.ReadFile(filepath.Join(dest, "README")); err != nil || string(data) != "hi\n" {
		t.Fatalf("README %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Fatal(err)
	}
	if checkoutOf(f.api.checkouts(), "github.com/acme/web") != dest {
		t.Errorf("checkouts %+v", f.api.checkouts())
	}
	asked := f.h.Asked()
	if len(asked) != 1 || !strings.Contains(asked[0], "URL: https://github.com/acme/web.git\nInto: "+dest) || !strings.Contains(asked[0], "Restricted Mode until you trust it") {
		t.Errorf("asked %q", asked)
	}
	if done := f.cloneEvents(EventCloneDone); len(done) != 1 || done[0].Error != "" || done[0].Cancelled || done[0].Destination != dest || done[0].Repo != "web" || done[0].Project != f.plan {
		t.Errorf("done %+v", done)
	}
	if len(leftovers(t, parent)) != 0 || f.load().Clone != nil {
		t.Error("left a clone behind")
	}
	// The repository is located now: its view shows the clone.
	if r := f.load().Repositories[1]; r.Folder != dest || !r.Located {
		t.Errorf("repository %+v", r)
	}
	// Into the same folder again: it is not empty.
	_, err = f.api.Clone(f.plan, "web", parent)
	wantErr(t, err, "is not empty")
}

func TestCloneIntoAnEmptyFolder(t *testing.T) {
	f, parent, dest := cloneFixture(t)
	f.h.Answer = true
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := f.api.Clone(f.plan, "web", parent); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README")); err != nil {
		t.Fatal(err)
	}
}

func TestCloneRefusals(t *testing.T) {
	f, parent, dest := cloneFixture(t)
	f.h.Answer = true
	file := filepath.Join(f.base, "file")
	write(t, file, "x")
	for _, tc := range []struct{ folder, repo, parent, want string }{
		{f.base, "web", parent, "not an open project"},
		{f.plan, "db", parent, `no repository "db"`},
		{f.plan, "web", "", "choose a folder"},
		{f.plan, "web", "src", "choose a folder"},
		{f.plan, "web", filepath.Join(f.base, "missing"), "no such file"},
		{f.plan, "web", file, "not a folder"},
	} {
		_, err := f.api.Clone(tc.folder, tc.repo, tc.parent)
		wantErr(t, err, tc.want)
	}
	// The destination: a file, a link, a folder with something in it.
	write(t, dest, "x")
	_, err := f.api.Clone(f.plan, "web", parent)
	wantErr(t, err, "exists already")
	os.Remove(dest)
	if err := os.Symlink(f.base, dest); err != nil {
		t.Fatal(err)
	}
	_, err = f.api.Clone(f.plan, "web", parent)
	wantErr(t, err, "exists already")
	os.Remove(dest)
	write(t, filepath.Join(dest, "x"), "x")
	_, err = f.api.Clone(f.plan, "web", parent)
	wantErr(t, err, "is not empty")
	if len(f.h.Asked()) != 0 {
		t.Errorf("asked before refusing: %q", f.h.Asked())
	}
	// A URL Clone refuses stays refused with the test seam in place.
	other := filepath.Join(f.base, "other")
	write(t, filepath.Join(other, ConfigFile), `{"repositories":[{"name":"x","url":"file:///srv/git/x.git"},{"name":"y","url":"/srv/git/y.git"}]}`)
	f.h.SetTrusted(other)
	if _, err := f.api.Open(other); err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{"x", "y"} {
		_, err = f.api.Clone(other, repo, parent)
		wantErr(t, err, "can't be cloned: only https://")
	}
	if len(f.h.Asked()) != 0 || len(leftovers(t, parent)) != 0 {
		t.Error("a refused URL got further")
	}
}

func TestCloneDeclined(t *testing.T) {
	f, parent, dest := cloneFixture(t)
	f.h.Answer = false
	_, err := f.api.Clone(f.plan, "web", parent)
	wantErr(t, err, "the clone was cancelled")
	if _, err := os.Stat(dest); err == nil || len(leftovers(t, parent)) != 0 {
		t.Error("a declined clone made something")
	}
	if len(f.cloneEvents(EventCloneDone)) != 0 || f.load().Clone != nil {
		t.Error("a declined clone ran")
	}
}

func TestCloneFailureRemovesWhatItMade(t *testing.T) {
	f, parent, dest := cloneFixture(t)
	f.h.Answer = true
	f.api.clones.source = func(s *cloneSource) { s.URL, s.Protocols = filepath.Join(f.base, "missing.git"), []string{"file"} }
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := f.api.Clone(f.plan, "web", parent)
	wantErr(t, err, "does not exist")
	if entries, err := os.ReadDir(dest); err != nil || len(entries) != 0 {
		t.Errorf("the empty destination changed: %v %v", entries, err)
	}
	if len(leftovers(t, parent)) != 0 {
		t.Errorf("left %v", leftovers(t, parent))
	}
	if done := f.cloneEvents(EventCloneDone); len(done) != 1 || !strings.Contains(done[0].Error, "does not exist") || done[0].Cancelled {
		t.Errorf("done %+v", done)
	}
	if checkoutOf(f.api.checkouts(), "github.com/acme/web") != "" {
		t.Error("a failed clone was located")
	}
	// The real transports refuse a local source: without the seam's file
	// transport the same clone fails before it reads anything.
	f.api.clones.source = func(s *cloneSource) { s.URL = bareRepo(t) }
	os.Remove(dest)
	_, err = f.api.Clone(f.plan, "web", parent)
	wantErr(t, err, "transport 'file' not allowed")
}

// One clone per project at a time; it can be cancelled (also by Close), and
// then removes what it made.
func TestCloneCancelAndOneAtATime(t *testing.T) {
	for _, how := range []string{"cancel", "close"} {
		f, parent, dest := cloneFixture(t)
		f.h.Answer = true
		bare := bareRepo(t)
		started, proceed := make(chan struct{}), make(chan struct{})
		f.api.clones.source = func(s *cloneSource) {
			s.URL, s.Protocols = bare, []string{"file"}
			close(started)
			<-proceed
		}
		errc := make(chan error, 1)
		go func() {
			_, err := f.api.Clone(f.plan, "web", parent)
			errc <- err
		}()
		<-started
		if c := f.load().Clone; c == nil || c.Repo != "web" || c.Destination != dest || c.URL != "https://github.com/acme/web.git" {
			t.Errorf("clone view %+v", c)
		}
		_, err := f.api.Clone(f.plan, "api", parent)
		wantErr(t, err, "web is being cloned for this project")
		switch how {
		case "cancel":
			if err := f.api.CancelClone(f.plan); err != nil {
				t.Fatal(err)
			}
		case "close":
			if err := f.api.Close(f.plan); err != nil {
				t.Fatal(err)
			}
		}
		close(proceed)
		err = <-errc
		wantErr(t, err, "the clone was cancelled")
		if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) || len(leftovers(t, parent)) != 0 {
			t.Errorf("%s: left %v %v", how, err, leftovers(t, parent))
		}
		if done := f.cloneEvents(EventCloneDone); len(done) != 1 || !done[0].Cancelled {
			t.Errorf("%s: done %+v", how, done)
		}
		if how == "cancel" {
			wantErr(t, f.api.CancelClone(f.plan), "no clone is running")
		}
	}
}

// Quitting Flint cancels a clone and waits for its cleanup.
func TestCloseAllClones(t *testing.T) {
	f, parent, dest := cloneFixture(t)
	f.h.Answer = true
	bare := bareRepo(t)
	started := make(chan struct{})
	f.api.clones.source = func(s *cloneSource) {
		s.URL, s.Protocols = bare, []string{"file"}
		close(started)
	}
	errc := make(chan error, 1)
	go func() {
		_, err := f.api.Clone(f.plan, "web", parent)
		errc <- err
	}()
	<-started
	f.api.clones.closeAll()
	<-errc
	if _, err := os.Stat(dest); err == nil {
		// The clone may have finished before closeAll: then it is complete.
		if _, err := os.Stat(filepath.Join(dest, "README")); err != nil {
			t.Error("a partial clone was left")
		}
	}
	if len(leftovers(t, parent)) != 0 {
		t.Errorf("left %v", leftovers(t, parent))
	}
}

// Trust covers everything under a folder, so a clone into a trusted folder
// is trusted at once. For a project whose folder is not trusted that is
// refused; for a trusted one the confirmation says so, and the answer shows
// the clone as trusted (reviewer's finding: ~/code trusted, an untrusted
// project's URL cloned into it).
func TestCloneIntoATrustedFolder(t *testing.T) {
	f, parent, dest := cloneFixture(t)
	f.h.Answer = true
	f.h.SetRepository(dest, "https://github.com/acme/web.git")
	f.h.SetTrusted(parent)

	f.h.SetUntrusted(f.plan)
	_, err := f.api.Clone(f.plan, "web", parent)
	wantErr(t, err, "choose a folder that is not inside a trusted folder: a clone there would be trusted at once")
	if len(f.h.Asked()) != 0 || len(leftovers(t, parent)) != 0 {
		t.Fatal("a refused clone got further")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("a refused clone made the folder")
	}

	f.h.SetTrusted(f.plan)
	v, err := f.api.Clone(f.plan, "web", parent)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Trusted || v.Folder != dest {
		t.Errorf("view %+v: the clone is trusted", v)
	}
	asked := f.h.Asked()
	if len(asked) != 1 || !strings.Contains(asked[0], "The new folder WILL BE TRUSTED at once, because it is inside the trusted folder "+parent) ||
		strings.Contains(asked[0], "Restricted Mode until you trust it") {
		t.Errorf("asked %q", asked)
	}
}

// The trust is looked at again after the question: a parent trusted while it
// was open still refuses the clone of an untrusted project.
func TestCloneTrustCheckedAfterTheQuestion(t *testing.T) {
	f, parent, dest := cloneFixture(t)
	f.h.SetUntrusted(f.plan)
	f.h.Answer = true
	f.h.OnConfirm = func() { f.h.SetTrusted(parent) }
	_, err := f.api.Clone(f.plan, "web", parent)
	wantErr(t, err, "not inside a trusted folder")
	if _, err := os.Stat(dest); err == nil || len(leftovers(t, parent)) != 0 {
		t.Error("the clone ran")
	}
	if asked := f.h.Asked(); len(asked) != 1 || !strings.Contains(asked[0], "Restricted Mode until you trust it") || !strings.Contains(asked[0], "check that you expect this URL") {
		t.Errorf("asked %q", asked)
	}
}
