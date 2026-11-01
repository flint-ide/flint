package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"flint/internal/docker"
	"flint/internal/mcptools"
	"flint/internal/shellenv"
	"flint/internal/terminal"
)

// eventDockerChanged tells the Docker panel to refresh (containers started,
// stopped, created or removed).
const eventDockerChanged = "docker:changed"

// dockerDebounce merges a burst of daemon events (compose up starting ten
// containers) into one refresh.
const dockerDebounce = 300 * time.Millisecond

// Reconnect delays of the event stream when the engine goes away while a
// panel is shown (a restart of Docker Desktop).
const (
	dockerReconnectMin = 2 * time.Second
	dockerReconnectMax = 30 * time.Second
)

// DockerOverview is everything the Docker panel shows.
type DockerOverview struct {
	Containers []docker.Container `json:"containers"`
	// Projects: Compose projects with containers, plus compose files found in
	// the workspace that have none (yet).
	Projects []docker.ComposeProject `json:"projects"`
	// ComposeAvailable: the docker CLI is installed, which Compose up / down /
	// pull need. Everything else works through the Engine API.
	ComposeAvailable bool `json:"composeAvailable"`
	// Endpoint is where the engine was found ("unix:///var/run/docker.sock").
	Endpoint string `json:"endpoint"`
	// Warning is set when the connection is not protected (plain tcp://).
	Warning string `json:"warning"`
}

// DockerAPI lists and controls containers through the Docker Engine API.
type DockerAPI struct {
	app *App

	mu       sync.Mutex
	client   *docker.Docker
	key      string
	watchers int                // visible Docker panels
	stop     context.CancelFunc // the running event stream
}

// docker returns the client for the configured engine, creating it again when
// the host setting, DOCKER_HOST or the docker context changed.
func (d *DockerAPI) docker() (*docker.Docker, error) {
	shellenv.Wait()
	opts := docker.Options{Host: d.app.currentSettings().Docker.Host}
	ep, err := docker.Resolve(opts)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client != nil && d.key == ep.Key() {
		return d.client, nil
	}
	c, err := docker.New(opts)
	if err != nil {
		return nil, err
	}
	if d.client != nil {
		d.client.Close()
	}
	d.client, d.key = c, ep.Key()
	return c, nil
}

// Overview lists containers and the Compose projects they belong to.
func (d *DockerAPI) Overview() (DockerOverview, error) {
	dk, err := d.docker()
	if err != nil {
		return DockerOverview{}, err
	}
	cs, err := dk.List(d.app.ctx)
	if err != nil {
		return DockerOverview{}, err
	}
	ep := dk.Endpoint()
	return DockerOverview{
		Containers:       cs,
		Projects:         projects(cs, docker.FindComposeFiles(d.app.projectRoots())),
		ComposeAvailable: docker.ComposeAvailable(),
		Endpoint:         ep.Host,
		Warning:          ep.Warning,
	}, nil
}

// projects merges running Compose projects (from container labels) with the
// compose files found in the workspace, one entry per directory.
func projects(cs []docker.Container, found []docker.ComposeProject) []docker.ComposeProject {
	out := []docker.ComposeProject{}
	byDir := map[string]int{}
	for _, c := range cs {
		if c.Project == "" || c.WorkingDir == "" {
			continue
		}
		if _, ok := byDir[c.WorkingDir]; ok {
			continue
		}
		byDir[c.WorkingDir] = len(out)
		out = append(out, docker.ComposeProject{Name: c.Project, Dir: c.WorkingDir, Files: strings.Split(c.ConfigFiles, ",")})
	}
	for _, p := range found {
		if _, ok := byDir[p.Dir]; !ok {
			byDir[p.Dir] = len(out)
			out = append(out, p)
		}
	}
	return out
}

// Act runs start / stop / restart / rm on a container.
func (d *DockerAPI) Act(id, action string) error {
	dk, err := d.docker()
	if err != nil {
		return err
	}
	return dk.Act(d.app.ctx, id, action)
}

// Compose runs up / down / restart / pull / stop on a Compose project.
func (d *DockerAPI) Compose(project docker.ComposeProject, action string) error {
	dk, err := d.docker()
	if err != nil {
		return err
	}
	return dk.Compose(d.app.ctx, project, action)
}

// OpenLogs follows a container's log in the terminal tab termID (a read-only
// stream session, the frontend picked the id).
func (d *DockerAPI) OpenLogs(termID, container string) error {
	dk, err := d.docker()
	if err != nil {
		return err
	}
	rc, err := dk.Logs(d.app.ctx, container, d.app.currentSettings().Docker.LogTailLines)
	if err != nil {
		return err
	}
	return d.startStream(termID, "docker logs "+container, terminal.Stream{Reader: rc, Close: func() { _ = rc.Close() }}, func() { _ = rc.Close() })
}

// OpenShell starts a shell inside a running container in the terminal tab
// termID: bash when the container has it, else sh.
func (d *DockerAPI) OpenShell(termID, container string, cols, rows int) error {
	dk, err := d.docker()
	if err != nil {
		return err
	}
	sh, err := dk.OpenShell(d.app.ctx, container, cols, rows)
	if err != nil {
		return err
	}
	st := terminal.Stream{
		Reader: sh, Writer: sh,
		Resize:   func(c, r int) error { return sh.Resize(d.app.ctx, c, r) },
		Close:    sh.Close,
		ExitCode: func() int { return sh.ExitCode(d.app.ctx) },
	}
	return d.startStream(termID, "docker exec "+container, st, sh.Close)
}

// startStream registers the console for agents (terminal tools) and starts the
// session; cleanup runs when it can't start.
func (d *DockerAPI) startStream(termID, title string, st terminal.Stream, cleanup func()) error {
	if termID == "" {
		cleanup()
		return errors.New("missing terminal id")
	}
	opened := d.app.mcpSources.Terminals.OpenNew(termID, mcptools.KindTerminal, title, "")
	if err := d.app.terminals.StartStream(termID, st); err != nil {
		cleanup()
		if opened {
			d.app.mcpSources.Terminals.Close(termID)
		}
		return fmt.Errorf("start %s: %w", title, err)
	}
	return nil
}

// Watch starts (on) or stops following the engine's events; panels call it
// when shown and hidden, so nothing runs while no Docker panel is visible.
func (d *DockerAPI) Watch(on bool) {
	d.mu.Lock()
	if on {
		d.watchers++
	} else if d.watchers > 0 {
		d.watchers--
	}
	start := d.watchers > 0 && d.stop == nil
	if d.watchers == 0 && d.stop != nil {
		d.stop()
		d.stop = nil
	}
	var ctx context.Context
	if start {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(d.app.ctx)
		d.stop = cancel
	}
	d.mu.Unlock()
	if start {
		go d.follow(ctx)
	}
}

// follow keeps an event stream open until ctx ends, reconnecting with a
// growing delay when the engine is gone; each reconnect asks the panel to
// refresh, since events were missed.
func (d *DockerAPI) follow(ctx context.Context) {
	delay := dockerReconnectMin
	for ctx.Err() == nil {
		dk, err := d.docker()
		if err == nil {
			started := time.Now()
			err = dk.Watch(ctx, dockerDebounce, func() { d.app.emit(eventDockerChanged) })
			if time.Since(started) > dockerReconnectMax {
				delay = dockerReconnectMin // it ran for a while: this is a fresh failure
			}
		}
		if ctx.Err() != nil {
			return
		}
		_ = err // the panel shows the engine's state itself on refresh
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, dockerReconnectMax)
		d.app.emit(eventDockerChanged)
	}
}

// close stops the event stream (app shutdown).
func (d *DockerAPI) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop != nil {
		d.stop()
		d.stop = nil
	}
	d.watchers = 0
}
