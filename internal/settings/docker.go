package settings

import (
	"fmt"
	"strings"
)

// Docker configures how the Docker panel reaches the engine.
type Docker struct {
	// Host overrides where the Docker Engine is: "unix:///path/docker.sock",
	// "tcp://host:2376" (TLS from DOCKER_CERT_PATH when DOCKER_TLS_VERIFY is
	// set) or "https://host:2376". Empty follows DOCKER_HOST, the current
	// docker context and the default sockets, like the docker CLI.
	Host string `json:"host"`
	// LogTailLines is how many past lines a container's log tab starts with.
	LogTailLines int `json:"logTailLines"`
}

// Bounds of the Docker settings.
const (
	MinDockerLogTailLines = 1
	MaxDockerLogTailLines = 10000
	maxDockerHostLength   = 512
)

func defaultDocker() Docker { return Docker{LogTailLines: 500} }

// CheckDocker reports the first value that isn't valid.
func CheckDocker(d Docker) error {
	if len(d.Host) > maxDockerHostLength || strings.ContainsAny(d.Host, "\x00\n\r ") {
		return fmt.Errorf("Docker host must be a single address such as unix:///var/run/docker.sock or tcp://host:2376")
	}
	if h := d.Host; h != "" {
		ok := strings.HasPrefix(h, "/")
		for _, p := range []string{"unix://", "tcp://", "http://", "https://"} {
			ok = ok || strings.HasPrefix(h, p)
		}
		if !ok {
			return fmt.Errorf("Docker host %q: use unix:///path, tcp://host:port or https://host:port", h)
		}
	}
	if d.LogTailLines < MinDockerLogTailLines || d.LogTailLines > MaxDockerLogTailLines {
		return fmt.Errorf("Docker log lines %d out of range %d–%d", d.LogTailLines, MinDockerLogTailLines, MaxDockerLogTailLines)
	}
	return nil
}

// normalizeDocker trims the host and clamps the tail (0 or missing is the
// default).
func normalizeDocker(d Docker) Docker {
	d.Host = strings.TrimSpace(d.Host)
	if d.LogTailLines == 0 {
		d.LogTailLines = defaultDocker().LogTailLines
	}
	d.LogTailLines = max(MinDockerLogTailLines, min(MaxDockerLogTailLines, d.LogTailLines))
	return d
}
