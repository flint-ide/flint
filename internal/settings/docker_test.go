package settings

import (
	"strings"
	"testing"
)

func TestDockerSettings(t *testing.T) {
	if d := Default().Docker; d.Host != "" || d.LogTailLines != 500 {
		t.Fatalf("defaults %+v", d)
	}
	n := Default()
	n.Docker = Docker{Host: "  unix:///tmp/d.sock ", LogTailLines: 0}
	if got := Normalize(n).Docker; got.Host != "unix:///tmp/d.sock" || got.LogTailLines != 500 {
		t.Fatalf("normalized %+v", got)
	}
	n.Docker = Docker{LogTailLines: 99999}
	if got := Normalize(n).Docker.LogTailLines; got != MaxDockerLogTailLines {
		t.Fatalf("clamp %d", got)
	}
	hosts := map[string]bool{"": true, "unix:///var/run/docker.sock": true, "/var/run/docker.sock": true, "tcp://h:2376": true, "https://h": true, "ssh://h": false, "a b": false, "unix:///x\ny": false}
	for host, ok := range hosts {
		err := CheckDocker(Docker{Host: host, LogTailLines: 10})
		if (err == nil) != ok {
			t.Errorf("host %q: err = %v", host, err)
		}
	}
	if err := CheckDocker(Docker{LogTailLines: -1}); err == nil || !strings.Contains(err.Error(), "log lines") {
		t.Errorf("err = %v", err)
	}
}
