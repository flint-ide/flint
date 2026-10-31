// Package docker drives the docker CLI (Docker Desktop, Colima, OrbStack…) for
// the Docker panel: containers grouped by Compose project, their actions, the
// project's compose files, and the daemon's event stream.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
)

const cmdTimeout = 30 * time.Second

// composeTimeout covers `compose up`/`pull`, which may download images.
const composeTimeout = 10 * time.Minute

// ErrNotInstalled: no docker binary on PATH.
var ErrNotInstalled = errors.New("docker is not installed (or not on the login shell's PATH)")

// Container is one row of `docker ps -a`.
type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`  // running, exited, paused, restarting, created, dead
	Status  string `json:"status"` // "Up 3 hours (healthy)"
	Ports   []Port `json:"ports"`
	Created string `json:"created"`
	// Compose labels, empty for containers not started by Compose.
	Project     string `json:"project"`
	Service     string `json:"service"`
	ConfigFiles string `json:"configFiles"` // comma-separated, as Compose records them
	WorkingDir  string `json:"workingDir"`
}

// Port is a published port.
type Port struct {
	Host      string `json:"host"`
	HostPort  int    `json:"hostPort"`
	Container string `json:"container"` // "80/tcp"
}

// psRow is `docker ps --format '{{json .}}'`.
type psRow struct {
	ID        string `json:"ID"`
	Names     string `json:"Names"`
	Image     string `json:"Image"`
	State     string `json:"State"`
	Status    string `json:"Status"`
	Ports     string `json:"Ports"`
	Labels    string `json:"Labels"`
	CreatedAt string `json:"CreatedAt"`
}

// Docker runs the docker binary found on PATH.
type Docker struct {
	bin string
}

// New finds docker on PATH.
func New() (*Docker, error) {
	bin, err := exec.LookPath("docker")
	if err != nil {
		return nil, ErrNotInstalled
	}
	return &Docker{bin: bin}, nil
}

// run executes docker with args; stderr becomes the error text.
func (d *Docker) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, d.bin, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if strings.Contains(msg, "Cannot connect to the Docker daemon") || strings.Contains(msg, "docker daemon is not running") {
			msg = "Docker isn't running — start Docker Desktop, Colima or OrbStack"
		}
		return nil, fmt.Errorf("docker %s: %s", args[0], msg)
	}
	return out, nil
}

// List returns every container, running or not.
func (d *Docker) List(ctx context.Context) ([]Container, error) {
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	out, err := d.run(ctx, "", "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	return parsePS(out)
}

func parsePS(out []byte) ([]Container, error) {
	cs := []Container{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r psRow
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("unexpected docker ps output: %w", err)
		}
		labels := parseLabels(r.Labels)
		cs = append(cs, Container{
			ID: r.ID, Name: strings.Split(r.Names, ",")[0], Image: r.Image, State: r.State, Status: r.Status,
			Ports: parsePorts(r.Ports), Created: r.CreatedAt,
			Project:     labels["com.docker.compose.project"],
			Service:     labels["com.docker.compose.service"],
			ConfigFiles: labels["com.docker.compose.project.config_files"],
			WorkingDir:  labels["com.docker.compose.project.working_dir"],
		})
	}
	return cs, sc.Err()
}

// parseLabels splits "a=b,c=d"; values may contain commas only in config_files
// lists, which Compose joins with commas too — keep them by attaching
// key-less pieces to the previous value.
func parseLabels(s string) map[string]string {
	labels := map[string]string{}
	last := ""
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok || strings.ContainsAny(k, " /") {
			if last != "" {
				labels[last] += "," + part
			}
			continue
		}
		labels[k] = v
		last = k
	}
	return labels
}

// portRe matches "0.0.0.0:8080->80/tcp" and "[::]:8080->80/tcp".
var portRe = regexp.MustCompile(`^(.*):(\d+)->(\S+)$`)

func parsePorts(s string) []Port {
	ports := []Port{}
	seen := map[int]bool{}
	for _, p := range strings.Split(s, ",") {
		m := portRe.FindStringSubmatch(strings.TrimSpace(p))
		if m == nil {
			continue
		}
		var hp int
		fmt.Sscan(m[2], &hp)
		if seen[hp] { // the same port on IPv4 and IPv6
			continue
		}
		seen[hp] = true
		ports = append(ports, Port{Host: strings.Trim(m[1], "[]"), HostPort: hp, Container: m[3]})
	}
	return ports
}

// containerActions are what Act accepts.
var containerActions = []string{"start", "stop", "restart", "rm"}

// Act runs start / stop / restart / rm on a container (by id).
func (d *Docker) Act(ctx context.Context, id, action string) error {
	if !slices.Contains(containerActions, action) {
		return fmt.Errorf("unknown container action %q", action)
	}
	if !validID(id) {
		return fmt.Errorf("invalid container id %q", id)
	}
	ctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	_, err := d.run(ctx, "", action, id)
	return err
}

var idRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// validID accepts container ids and names; never anything that could read as a flag.
func validID(s string) bool { return idRe.MatchString(s) }

// composeActions map to `docker compose` arguments.
var composeActions = map[string][]string{
	"up":      {"up", "-d"},
	"down":    {"down"},
	"restart": {"restart"},
	"pull":    {"pull"},
	"stop":    {"stop"},
}

// Compose runs an action on a Compose project, from its files and working
// directory (as recorded by Compose, or a compose file found in the workspace).
func (d *Docker) Compose(ctx context.Context, project ComposeProject, action string) error {
	verb, ok := composeActions[action]
	if !ok {
		return fmt.Errorf("unknown compose action %q", action)
	}
	if len(project.Files) == 0 || project.Dir == "" {
		return errors.New("the compose project has no files")
	}
	args := []string{"compose"}
	if project.Name != "" {
		if !validID(project.Name) {
			return fmt.Errorf("invalid project name %q", project.Name)
		}
		args = append(args, "-p", project.Name)
	}
	for _, f := range project.Files {
		if !filepath.IsAbs(f) {
			return fmt.Errorf("compose file %q is not absolute", f)
		}
		args = append(args, "-f", f)
	}
	ctx, cancel := context.WithTimeout(ctx, composeTimeout)
	defer cancel()
	_, err := d.run(ctx, project.Dir, append(args, verb...)...)
	return err
}

// ComposeProject is a Compose application: running (from labels) or only a
// file in the workspace.
type ComposeProject struct {
	Name  string   `json:"name"`
	Dir   string   `json:"dir"`
	Files []string `json:"files"`
}

// composeNames are the file names `docker compose` looks for.
var composeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}

// FindComposeFiles lists compose files in each root and its direct subfolders.
func FindComposeFiles(roots []string) []ComposeProject {
	var out []ComposeProject
	seen := map[string]bool{}
	add := func(dir string) {
		for _, n := range composeNames {
			p := filepath.Join(dir, n)
			if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && !seen[dir] {
				seen[dir] = true
				out = append(out, ComposeProject{Dir: dir, Files: []string{p}})
			}
		}
	}
	for _, root := range roots {
		add(root)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && e.Name() != "node_modules" && e.Name() != "vendor" {
				add(filepath.Join(root, e.Name()))
			}
		}
	}
	return out
}

// Watch runs `docker events` for containers and calls onChange (debounced)
// until ctx ends. It returns when the stream ends; callers restart it if needed.
func (d *Docker) Watch(ctx context.Context, debounce time.Duration, onChange func()) error {
	cmd := exec.CommandContext(ctx, d.bin, "events", "--filter", "type=container", "--format", "{{.Action}}")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var timer *time.Timer
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		action := sc.Text()
		// exec_create/exec_start fire constantly for health checks; they change nothing shown.
		if strings.HasPrefix(action, "exec_") || action == "top" {
			continue
		}
		if timer == nil {
			timer = time.AfterFunc(debounce, onChange)
		} else {
			timer.Reset(debounce)
		}
	}
	if timer != nil && timer.Stop() && ctx.Err() == nil {
		onChange() // the stream ended (daemon restart): report what was pending
	}
	return cmd.Wait()
}
