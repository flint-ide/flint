//go:build darwin

// Command bench measures how fast an editor shows its first window on a
// project and how much memory it uses once it has settled, for Flint and the
// editors people compare it with. It is a development tool for the numbers in
// the README (docs/BENCHMARK.md describes the method); it runs on macOS only.
//
//	go run ./cmd/bench -flint build/bin/flint.app -project "$(go env GOROOT)/src"
//
// Every run starts the editor with a fresh, empty profile directory, so no
// extensions, caches or restored windows of the user's own setup take part.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// editor describes how to start one editor on a project with its own profile.
type editor struct {
	name    string
	binary  string
	command func(binary, profile, project string) (*exec.Cmd, error)
}

func editors(flintApp string) []editor {
	return []editor{
		{
			name:   "Flint",
			binary: filepath.Join(flintApp, "Contents/MacOS/flint"),
			command: func(binary, profile, project string) (*exec.Cmd, error) {
				// Skip the one-time tool check: it is a dialog of the first
				// launch only, and it starts language servers to read versions.
				if err := os.WriteFile(filepath.Join(profile, "state.json"), []byte(`{"toolCheckShown": true}`), 0o600); err != nil {
					return nil, err
				}
				cmd := exec.Command(binary, project)
				cmd.Env = append(os.Environ(), "FLINT_CONFIG_DIR="+profile)
				return cmd, nil
			},
		},
		{
			name:   "Visual Studio Code",
			binary: "/Applications/Visual Studio Code.app/Contents/MacOS/Code",
			command: func(binary, profile, project string) (*exec.Cmd, error) {
				return exec.Command(binary, "--new-window", "--disable-workspace-trust", "--skip-welcome", "--skip-release-notes",
					"--user-data-dir", filepath.Join(profile, "data"), "--extensions-dir", filepath.Join(profile, "extensions"), project), nil
			},
		},
		{
			name:   "Zed",
			binary: firstExisting("/Applications/Zed.app/Contents/MacOS/zed", "/Applications/Zed Preview.app/Contents/MacOS/zed"),
			command: func(binary, profile, project string) (*exec.Cmd, error) {
				return exec.Command(binary, "--user-data-dir", profile, project), nil
			},
		},
		{
			name:   "Cursor",
			binary: "/Applications/Cursor.app/Contents/MacOS/Cursor",
			command: func(binary, profile, project string) (*exec.Cmd, error) {
				return exec.Command(binary, "--new-window", "--disable-workspace-trust", "--skip-welcome",
					"--user-data-dir", filepath.Join(profile, "data"), "--extensions-dir", filepath.Join(profile, "extensions"), project), nil
			},
		},
	}
}

type result struct {
	window time.Duration
	memory int64 // bytes of physical footprint, summed over the process tree
}

func main() {
	flintApp := flag.String("flint", "build/bin/flint.app", "path to the Flint.app to measure")
	project := flag.String("project", "", "project directory to open (default: the Go source tree)")
	runs := flag.Int("runs", 5, "measured runs per editor, after one warm-up run")
	settle := flag.Duration("settle", 30*time.Second, "how long an editor idles before its memory is read")
	timeout := flag.Duration("timeout", 60*time.Second, "how long to wait for the first window")
	only := flag.String("only", "", "comma-separated editor names to measure (default: all installed)")
	flag.Parse()
	if *runs < 1 {
		fatal(errors.New("-runs must be at least 1"))
	}

	if *project == "" {
		out, err := exec.Command("go", "env", "GOROOT").Output()
		if err != nil {
			fatal(fmt.Errorf("find the Go source tree: %w", err))
		}
		*project = filepath.Join(strings.TrimSpace(string(out)), "src")
	}
	files, err := countFiles(*project)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("Project: %s (%d files), %d runs per editor after a warm-up, memory read after %s idle\n\n", *project, files, *runs, *settle)
	fmt.Println("| Editor | Version | First window (median) | Memory after settling (median) |")
	fmt.Println("|---|---|---|---|")
	for _, e := range editors(*flintApp) {
		if *only != "" && !slices.Contains(strings.Split(strings.ToLower(*only), ","), strings.ToLower(e.name)) {
			continue
		}
		if _, err := os.Stat(e.binary); err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: not installed\n", e.name)
			continue
		}
		var all []result
		for i := 0; i <= *runs; i++ {
			r, err := measure(e, *project, *settle, *timeout)
			if err != nil {
				fatal(fmt.Errorf("%s: %w", e.name, err))
			}
			fmt.Fprintf(os.Stderr, "%s run %d: window %s, memory %s\n", e.name, i, r.window.Round(time.Millisecond), mib(r.memory))
			if i > 0 {
				all = append(all, r)
			}
		}
		fmt.Printf("| %s | %s | %s | %s |\n", e.name, version(e.binary), medianWindow(all).Round(10*time.Millisecond), mib(medianMemory(all)))
	}
}

// measure starts the editor once, waits for its first window, lets it settle,
// reads its memory and quits it.
func measure(e editor, project string, settle, timeout time.Duration) (result, error) {
	profile, err := os.MkdirTemp("", "flint-bench-")
	if err != nil {
		return result{}, err
	}
	defer os.RemoveAll(profile)
	cmd, err := e.command(e.binary, profile, project)
	if err != nil {
		return result{}, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	before := webKitProcesses()
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return result{}, err
	}
	defer stop(cmd)

	window, err := waitForWindow(cmd.Process.Pid, start, timeout)
	if err != nil {
		return result{}, err
	}
	time.Sleep(settle)
	// A WKWebView (Flint) renders in WebKit's XPC services, which launchd
	// starts: they are not the editor's children, so count the ones that
	// appeared with it. Nothing else may open a web view during a run.
	pids := processTree(cmd.Process.Pid)
	for p := range webKitProcesses() {
		if !before[p] {
			pids = append(pids, p)
		}
	}
	memory, err := footprint(pids)
	if err != nil {
		return result{}, err
	}
	return result{window: window, memory: memory}, nil
}

func waitForWindow(pid int, start time.Time, timeout time.Duration) (time.Duration, error) {
	for time.Since(start) < timeout {
		for _, p := range processTree(pid) {
			if hasWindow(p) {
				return time.Since(start), nil
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	return 0, errors.New("no window appeared in time")
}

// stop quits the editor and everything it started, then waits for it.
func stop(cmd *exec.Cmd) {
	pids := processTree(cmd.Process.Pid)
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	for _, p := range pids {
		_ = syscall.Kill(p, syscall.SIGTERM)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		for _, p := range pids {
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
		<-done
	}
	time.Sleep(2 * time.Second) // let helpers exit before the next run
}

// webKitProcesses lists the running WebKit XPC services (web content,
// networking, GPU) by pid.
func webKitProcesses() map[int]bool {
	out, err := exec.Command("ps", "-A", "-o", "pid=,comm=").Output()
	if err != nil {
		return nil
	}
	found := map[int]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		pid, comm, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if ok && strings.Contains(comm, "com.apple.WebKit.") {
			if p, err := strconv.Atoi(pid); err == nil {
				found[p] = true
			}
		}
	}
	return found
}

// processTree lists pid and all its descendants.
func processTree(pid int) []int {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return []int{pid}
	}
	children := map[int][]int{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 2 {
			continue
		}
		p, _ := strconv.Atoi(f[0])
		pp, _ := strconv.Atoi(f[1])
		children[pp] = append(children[pp], p)
	}
	tree := []int{pid}
	for i := 0; i < len(tree); i++ {
		tree = append(tree, children[tree[i]]...)
	}
	return tree
}

var footprintLine = regexp.MustCompile(`\[(\d+)\]: .*Footprint: (\d+) B`)

// footprint sums the physical footprint (what Activity Monitor shows as
// Memory) of the processes.
func footprint(pids []int) (int64, error) {
	args := []string{"-f", "bytes"}
	for _, p := range pids {
		args = append(args, strconv.Itoa(p))
	}
	out, err := exec.Command("footprint", args...).CombinedOutput()
	if err != nil && len(out) == 0 {
		return 0, fmt.Errorf("footprint: %w", err)
	}
	var total int64
	seen := map[string]bool{}
	for _, m := range footprintLine.FindAllStringSubmatch(string(out), -1) {
		if seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		n, _ := strconv.ParseInt(m[2], 10, 64)
		total += n
	}
	if total == 0 {
		return 0, fmt.Errorf("footprint gave no numbers: %s", firstLine(out))
	}
	return total, nil
}

func version(binary string) string {
	app := binary
	for !strings.HasSuffix(app, ".app") && app != "/" {
		app = filepath.Dir(app)
	}
	out, err := exec.Command("defaults", "read", filepath.Join(app, "Contents/Info"), "CFBundleShortVersionString").Output()
	if err != nil {
		return "dev build"
	}
	return strings.TrimSpace(string(out))
}

func countFiles(dir string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return err
	})
	return n, err
}

func medianWindow(rs []result) time.Duration {
	v := make([]time.Duration, len(rs))
	for i, r := range rs {
		v[i] = r.window
	}
	slices.Sort(v)
	return v[len(v)/2]
}

func medianMemory(rs []result) int64 {
	v := make([]int64, len(rs))
	for i, r := range rs {
		v[i] = r.memory
	}
	slices.Sort(v)
	return v[len(v)/2]
}

func mib(b int64) string { return fmt.Sprintf("%d MB", (b+(1<<19))>>20) }

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return paths[0]
}

func firstLine(b []byte) string {
	s, _, _ := strings.Cut(string(b), "\n")
	return s
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bench:", err)
	os.Exit(1)
}
