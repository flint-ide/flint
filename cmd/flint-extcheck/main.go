// Command flint-extcheck checks extension folders against this Flint's
// extension contract, with the same code Flint runs: the manifest rules, the
// webview files, and a run of the program through the extension host
// (install, initialize, every declarative panel resolved, a file opened for
// every decoration set, agent tools listed, shutdown). It is what the flint-ide/extensions CI runs.
//
//	go run ./cmd/flint-extcheck path/to/extension [more folders…]
//
// Build each extension's program first (run.command must exist). Exit status
// 1 means a folder broke the contract; with GITHUB_ACTIONS set, problems are
// printed as workflow annotations.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"flint/internal/extensions/host"
	"flint/internal/extensions/manifest"
	"flint/internal/extensions/rpc"
)

// checkApp is the Flint side of the run: one trusted project folder, every
// question answered yes, default settings, events recorded.
type checkApp struct {
	root   string
	mu     sync.Mutex
	events []string
}

func (a *checkApp) Roots() []string                     { return []string{a.root} }
func (a *checkApp) Trusted(string) bool                 { return true }
func (a *checkApp) OpenURL(string) error                { return nil }
func (a *checkApp) Confirm(string, string, string) bool { return true }
func (a *checkApp) Choose(string, string, []string) string {
	return ""
}
func (a *checkApp) Enabled(string) bool             { return true }
func (a *checkApp) SetEnabled(string, bool) bool    { return true }
func (a *checkApp) Settings(string) json.RawMessage { return nil }
func (a *checkApp) Version() string                 { return "extcheck" }
func (a *checkApp) Emit(event string, data ...any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
}

type report struct {
	dir      string
	problems []string
}

func (r *report) fail(format string, args ...any) {
	r.problems = append(r.problems, fmt.Sprintf(format, args...))
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: flint-extcheck <extension folder>…")
		os.Exit(2)
	}
	failed := false
	for _, dir := range os.Args[1:] {
		r := check(dir)
		if len(r.problems) == 0 {
			fmt.Printf("ok   %s\n", dir)
			continue
		}
		failed = true
		for _, p := range r.problems {
			if os.Getenv("GITHUB_ACTIONS") == "true" {
				fmt.Printf("::error file=%s::%s\n", filepath.ToSlash(filepath.Join(dir, "flint-extension.json")), strings.ReplaceAll(p, "\n", " "))
			} else {
				fmt.Printf("FAIL %s: %s\n", dir, p)
			}
		}
	}
	if failed {
		os.Exit(1)
	}
}

func check(dir string) (r report) {
	r.dir = dir
	m, err := manifest.Load(dir)
	if err != nil {
		r.fail("manifest: %v", err)
		return r
	}
	if len(m.Contributes.Unsupported) > 0 {
		r.fail("contribution points this Flint doesn't offer: %s", strings.Join(m.Contributes.Unsupported, ", "))
	}
	for _, view := range webviews(m) {
		if _, err := m.ReadWebview(dir, view); err != nil {
			r.fail("%v", err)
		}
	}
	if !m.HasProgram() {
		return r // data only: nothing runs
	}
	runChecks(&r, dir, m)
	return r
}

func webviews(m *manifest.Manifest) []string {
	var out []string
	for _, p := range m.Contributes.Panels {
		if p.Webview != nil {
			out = append(out, p.ID)
		}
	}
	for _, t := range m.Contributes.TabViews {
		out = append(out, t.ID)
	}
	return out
}

// runChecks installs the folder into a scratch Flint and drives the program.
func runChecks(r *report, dir string, m *manifest.Manifest) {
	tmp, err := os.MkdirTemp("", "flint-extcheck-")
	if err != nil {
		r.fail("%v", err)
		return
	}
	defer os.RemoveAll(tmp)
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		r.fail("%v", err)
		return
	}
	app := &checkApp{root: project}
	h := host.New(host.Options{Dir: filepath.Join(tmp, "config", "extensions"), App: app, StopTimeout: 2 * time.Second, LanguageOf: languageOf})
	defer h.Close()
	info, err := h.Install(dir)
	if err != nil {
		r.fail("install: %v", err)
		return
	}
	for _, w := range info.Warnings {
		r.fail("warning: %s", w)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Each declarative panel must answer view/resolve (a message view is a
	// fine answer when there is nothing to show; an error is not).
	for _, p := range m.Contributes.Panels {
		if p.Webview != nil {
			if _, err := h.WebviewHTML(m.ID, p.ID); err != nil {
				r.fail("webview %s: %v", p.ID, err)
			}
			// A page's request starts the program; any answer (an error for an
			// unknown method included) shows it initialized and answers webview/request.
			if _, err := h.WebviewRequest(ctx, m.ID, p.ID, "", "flint.extcheck/ping", nil); err != nil && !answered(err) {
				r.fail("webview %s: webview/request: %v", p.ID, err)
			}
			continue
		}
		v, err := h.Resolve(ctx, m.ID, p.ID, "")
		if err != nil {
			r.fail("panel %s: view/resolve: %v", p.ID, err)
			continue
		}
		if v.Kind != p.View && v.Kind != "message" {
			r.fail("panel %s: answered a %q view for a %q panel", p.ID, v.Kind, p.View)
		}
		if v.Kind == "message" && strings.HasPrefix(v.Text, "The extension sent a ") {
			r.fail("panel %s: %s", p.ID, v.Text)
		}
	}
	checkDecorationSets(ctx, r, h, m, project)

	// No panel to resolve: start it through its first command's activation
	// only when it declares onStartup; otherwise a webview or tool start is
	// enough to see it initialize.
	if len(m.Contributes.Panels) == 0 && len(m.Contributes.AgentTools) > 0 {
		h.StartAtLaunch()
	}
	if got := len(h.Tools(func(string) bool { return true })); got != len(m.Contributes.AgentTools) {
		r.fail("agent tools: %d offered, %d declared", got, len(m.Contributes.AgentTools))
	}

	// It should have started (and initialized) for the panels; then quit cleanly.
	h.Close()
	log, _ := h.Log(m.ID)
	for _, bad := range []string{"panic:", "fatal error:", "protocol error", "not a JSON-RPC", "editor/setDecorations: ", "editor/clearDecorations: "} {
		if strings.Contains(log, bad) {
			r.fail("its log has %q:\n%s", bad, tail(log, 2000))
			break
		}
	}
	for _, in := range h.List() {
		if in.ID == m.ID && in.State != host.StateStopped && in.State != host.StateDisabled {
			r.fail("after shutdown it is %q, not stopped", in.State)
		}
	}
}

// checkLanguages are the file extensions of Flint's own languages, for the
// decoration sets that select files by language.
var checkLanguages = map[string]string{"go": ".go", "typescript": ".ts", "python": ".py", "rust": ".rs"}

func languageOf(path string) string {
	for language, ext := range checkLanguages {
		if strings.HasSuffix(path, ext) {
			return language
		}
	}
	return ""
}

// sampleFile is a file name a decoration set is for ("" when its selectors
// give none away: a language of another extension, a glob with a class).
func sampleFile(s manifest.DecorationSet) string {
	var names []string
	for _, e := range s.Extensions {
		names = append(names, "extcheck"+e)
	}
	for _, g := range s.Globs {
		names = append(names, strings.NewReplacer("*", "extcheck", "?", "x").Replace(g))
	}
	for _, l := range s.Languages {
		if ext, ok := checkLanguages[l]; ok {
			names = append(names, "extcheck"+ext)
		}
	}
	for _, name := range names {
		if s.Matches(name, languageOf(name)) && name != "." && name != ".." {
			return name
		}
	}
	return ""
}

// checkDecorationSets opens a file for every decoration set, as an editor
// would: the program must start for it, take editor/opened, editor/changed
// and editor/closed, publish only what the contract allows (a publish Flint
// refuses is in its log, which runChecks reads) and answer editor/action (an
// error for an action it doesn't know is a fine answer).
func checkDecorationSets(ctx context.Context, r *report, h *host.Host, m *manifest.Manifest, project string) {
	for _, s := range m.Contributes.DecorationSets {
		name := sampleFile(s)
		if name == "" {
			continue
		}
		path := filepath.Join(project, name)
		text := "flint-extcheck\nTODO: a second line\n"
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			r.fail("%v", err)
			return
		}
		if !h.EditorInterest(path).Tracked {
			r.fail("decoration set %s: not offered for %s", s.ID, name)
			continue
		}
		if err := h.EditorOpened(path, 1, text, true); err != nil {
			r.fail("decoration set %s: %v", s.ID, err)
			continue
		}
		req := host.ActionRequest{Set: s.ID, Path: path, Version: 1, Action: "flint.extcheck.ping", Line: 1, EndLine: 1}
		if _, err := h.EditorAction(ctx, m.ID, req); err != nil && !answered(err) {
			r.fail("decoration set %s: editor/action: %v", s.ID, err)
		}
		h.EditorChanged(path, 2, text+"more\n", true)
		time.Sleep(200 * time.Millisecond) // what it publishes for the change
		h.EditorClosed(path)
	}
}

// answered tells an error the program sent (it is alive and speaks the
// protocol) from one about the program itself (no answer, not started, crashed).
func answered(err error) bool {
	var rpcErr *rpc.Error
	return errors.As(err, &rpcErr)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
