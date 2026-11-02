package docker

import (
	"fmt"
	"strings"

	"flint/internal/settings"
)

// Settings is the "docker" key of settings.json: how the Docker panel reaches
// the engine.
type Settings struct {
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
	MinLogTailLines = 1
	MaxLogTailLines = 10000
	maxHostLength   = 512
)

// Section registers the settings with the settings package (Module).
var Section = &settings.Section[Settings]{
	Key:       "docker",
	Default:   defaultSettings,
	Normalize: normalize,
	Check:     Check,
}

func defaultSettings() Settings { return Settings{LogTailLines: 500} }

// Check reports the first value that isn't valid.
func Check(d Settings) error {
	if len(d.Host) > maxHostLength || strings.ContainsAny(d.Host, "\x00\n\r ") {
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
	if d.LogTailLines < MinLogTailLines || d.LogTailLines > MaxLogTailLines {
		return fmt.Errorf("Docker log lines %d out of range %d–%d", d.LogTailLines, MinLogTailLines, MaxLogTailLines)
	}
	return nil
}

// normalize trims the host and clamps the tail (0 or missing is the default).
func normalize(d Settings) Settings {
	d.Host = strings.TrimSpace(d.Host)
	if d.LogTailLines == 0 {
		d.LogTailLines = defaultSettings().LogTailLines
	}
	d.LogTailLines = max(MinLogTailLines, min(MaxLogTailLines, d.LogTailLines))
	return d
}
