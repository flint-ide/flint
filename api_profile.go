package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"flint/internal/profiling"
	"flint/internal/runconfig"
	"flint/internal/testrun"
)

// profiles holds the state of profiling shared by ProfileAPI and shutdown:
// temporary directories and the recordings in progress.
type profiles struct {
	dirs *profiling.Dirs

	mu         sync.Mutex
	collecting map[string]context.CancelFunc
}

func newProfiles() *profiles {
	return &profiles{dirs: profiling.NewDirs(""), collecting: map[string]context.CancelFunc{}}
}

// closeAll cancels recordings and removes every temporary profile (Flint quits).
func (p *profiles) closeAll() {
	p.mu.Lock()
	for _, cancel := range p.collecting {
		cancel()
	}
	p.mu.Unlock()
	p.dirs.RemoveAll()
}

// ProfileAPI records pprof profiles (Go tests, or a running configuration's
// net/http/pprof endpoint) and prepares them for the flame graph tab.
type ProfileAPI struct {
	app *App
}

// ProfiledTests is a started profiling test run.
type ProfiledTests struct {
	Title string `json:"title"`
	// Dir is the temporary directory to Discard once the profile is closed.
	Dir   string           `json:"dir"`
	Files testrun.Profiles `json:"files"`
}

// StartTests runs Go tests of one package under id (like RunAPI.StartTests,
// results stream as RunEvent.Tests) with -cpuprofile and, with mem,
// -memprofile. The files exist once the run exited.
func (p *ProfileAPI) StartTests(id string, req testrun.Request, mem bool) (ProfiledTests, error) {
	dirs := p.app.profiles.dirs
	dir, err := dirs.New()
	if err != nil {
		return ProfiledTests{}, err
	}
	plan, files, err := testrun.PlanProfile(req, dir, mem)
	if err == nil {
		err = p.app.startTestPlan(id, plan)
	}
	if err != nil {
		_ = dirs.Remove(dir)
		return ProfiledTests{}, err
	}
	return ProfiledTests{Title: plan.Title, Dir: dir, Files: files}, nil
}

// Collected is a profile recorded from a running program.
type Collected struct {
	Dir  string `json:"dir"`
	Path string `json:"path"`
	// Seconds the profile recorded for (0: a snapshot such as /heap).
	Seconds int `json:"seconds"`
}

// Collect records a profile from cfg's profileURL while the run runID is
// alive, waiting for the program to start listening first. id names the
// recording for Cancel.
func (p *ProfileAPI) Collect(id, runID string, cfg runconfig.Config) (Collected, error) {
	raw := strings.TrimSpace(cfg.ProfileURL)
	if raw == "" {
		return Collected{}, fmt.Errorf("%q has no profileURL: add the program's net/http/pprof endpoint, e.g. http://localhost:6060/debug/pprof/profile", cfg.Name)
	}
	if err := runconfig.ValidateProfileURL(raw); err != nil {
		return Collected{}, err
	}
	endpoint, recording, err := profiling.Endpoint(raw)
	if err != nil {
		return Collected{}, err
	}
	dir, err := p.app.profiles.dirs.New()
	if err != nil {
		return Collected{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.app.profiles.mu.Lock()
	p.app.profiles.collecting[id] = cancel
	p.app.profiles.mu.Unlock()
	defer func() {
		p.app.profiles.mu.Lock()
		delete(p.app.profiles.collecting, id)
		p.app.profiles.mu.Unlock()
		cancel()
	}()

	file := filepath.Join(dir, profileName(endpoint))
	err = profiling.Fetch(ctx, endpoint, recording, file, func() bool { return p.app.runs.Running(runID) })
	if err != nil {
		_ = p.app.profiles.dirs.Remove(dir)
		if errors.Is(err, context.Canceled) {
			return Collected{}, errors.New("profiling cancelled")
		}
		return Collected{}, err
	}
	return Collected{Dir: dir, Path: file, Seconds: int(recording.Seconds())}, nil
}

// profileName names the file after the endpoint: cpu.pprof for /profile, heap.pprof for /heap.
func profileName(endpoint string) string {
	name := "cpu"
	if u, err := url.Parse(endpoint); err == nil {
		if base := path.Base(u.Path); base != "profile" && base != "/" && base != "." {
			name = base
		}
	}
	return name + ".pprof"
}

// Cancel stops a recording started by Collect.
func (p *ProfileAPI) Cancel(id string) {
	p.app.profiles.mu.Lock()
	defer p.app.profiles.mu.Unlock()
	if cancel := p.app.profiles.collecting[id]; cancel != nil {
		cancel()
	}
}

// Open builds the flame graph report of a profile file for sampleType ("" =
// the profile's default).
func (p *ProfileAPI) Open(path, sampleType string) (profiling.Report, error) {
	if !filepath.IsAbs(path) {
		return profiling.Report{}, fmt.Errorf("profile path %q is not absolute", path)
	}
	return profiling.Load(path, sampleType)
}

// Discard removes a temporary profile directory (its tab closed). Directories
// Flint did not create are left alone.
func (p *ProfileAPI) Discard(dir string) error {
	return p.app.profiles.dirs.Remove(dir)
}

// ChooseFile shows the native file picker for a profile. Returns "" when cancelled.
func (p *ProfileAPI) ChooseFile() (string, error) {
	return runtime.OpenFileDialog(p.app.ctx, runtime.OpenDialogOptions{
		Title: "Open Profile",
		Filters: []runtime.FileFilter{
			{DisplayName: "pprof profiles (*.pprof, *.prof, *.pb.gz, *.out)", Pattern: "*.pprof;*.prof;*.pb.gz;*.out"},
			{DisplayName: "All files", Pattern: "*"},
		},
	})
}

// Reveal selects a profile file in Finder (temporary profiles live outside
// the workspace, so WorkspaceAPI.RevealInFinder doesn't apply).
func (p *ProfileAPI) Reveal(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(path) || !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a profile file", path)
	}
	if out, err := exec.Command("open", "-R", path).CombinedOutput(); err != nil {
		return fmt.Errorf("reveal %s: %s", filepath.Base(path), strings.TrimSpace(string(out)))
	}
	return nil
}
