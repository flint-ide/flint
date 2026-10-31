package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"flint/internal/docker"
	"flint/internal/shellenv"
)

// eventDockerChanged tells the Docker panel to refresh (containers started,
// stopped, created or removed).
const eventDockerChanged = "docker:changed"

// dockerDebounce merges a burst of daemon events (compose up starting ten
// containers) into one refresh.
const dockerDebounce = 300 * time.Millisecond

// DockerOverview is everything the Docker panel shows.
type DockerOverview struct {
	Containers []docker.Container `json:"containers"`
	// Projects: Compose projects with containers, plus compose files found in
	// the workspace that have none (yet).
	Projects []docker.ComposeProject `json:"projects"`
}

// DockerAPI lists and controls containers through the docker CLI.
type DockerAPI struct {
	app *App

	mu       sync.Mutex
	watchers int                // visible Docker panels
	stop     context.CancelFunc // the running `docker events`
}

func (d *DockerAPI) docker() (*docker.Docker, error) {
	shellenv.Wait()
	return docker.New()
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
	return DockerOverview{Containers: cs, Projects: projects(cs, docker.FindComposeFiles(d.app.projectRoots()))}, nil
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

// Watch starts (on) or stops following the daemon's events; panels call it
// when shown and hidden, so nothing runs while no Docker panel is visible.
func (d *DockerAPI) Watch(on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if on {
		d.watchers++
	} else if d.watchers > 0 {
		d.watchers--
	}
	switch {
	case d.watchers > 0 && d.stop == nil:
		dk, err := d.docker()
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(d.app.ctx)
		d.stop = cancel
		go func() {
			_ = dk.Watch(ctx, dockerDebounce, func() { d.app.emit(eventDockerChanged) })
			d.mu.Lock()
			if ctx.Err() == nil { // the stream died on its own (daemon gone)
				d.stop = nil
				d.watchers = 0
			}
			d.mu.Unlock()
		}()
	case d.watchers == 0 && d.stop != nil:
		d.stop()
		d.stop = nil
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
}
