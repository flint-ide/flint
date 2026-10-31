package docker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const psFixture = `{"ID":"abc123","Names":"shop-db-1","Image":"postgres:16","State":"running","Status":"Up 2 hours (healthy)","Ports":"0.0.0.0:5432->5432/tcp, [::]:5432->5432/tcp","Labels":"com.docker.compose.project=shop,com.docker.compose.service=db,com.docker.compose.project.config_files=/p/shop/compose.yaml,/p/shop/compose.override.yaml,com.docker.compose.project.working_dir=/p/shop","CreatedAt":"2026-10-30 10:00:00 +0100 CET"}
{"ID":"def456","Names":"lonely","Image":"alpine","State":"exited","Status":"Exited (0) 5 minutes ago","Ports":"","Labels":"","CreatedAt":"2026-10-30 09:00:00 +0100 CET"}
`

func TestParsePS(t *testing.T) {
	cs, err := parsePS([]byte(psFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("containers %+v", cs)
	}
	db := cs[0]
	if db.Project != "shop" || db.Service != "db" || db.WorkingDir != "/p/shop" || db.ConfigFiles != "/p/shop/compose.yaml,/p/shop/compose.override.yaml" {
		t.Fatalf("labels %+v", db)
	}
	if len(db.Ports) != 1 || db.Ports[0].HostPort != 5432 || db.Ports[0].Container != "5432/tcp" {
		t.Fatalf("ports %+v", db.Ports)
	}
	if cs[1].Project != "" || len(cs[1].Ports) != 0 || cs[1].State != "exited" {
		t.Fatalf("plain container %+v", cs[1])
	}
}

// fakeDocker puts a docker script on PATH that records its arguments.
func fakeDocker(t *testing.T, stdout string, exit int) (*Docker, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "args.log")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\ncat <<'EOF'\n" + stdout + "EOF\n"
	if exit != 0 {
		script += "echo 'Cannot connect to the Docker daemon at unix:///var/run/docker.sock' >&2\nexit 1\n"
	}
	bin := filepath.Join(dir, "docker")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return &Docker{bin: bin}, log
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	b, _ := os.ReadFile(log)
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestActAndComposeBuildExactCommands(t *testing.T) {
	d, log := fakeDocker(t, "", 0)
	ctx := context.Background()
	if err := d.Act(ctx, "shop-db-1", "restart"); err != nil {
		t.Fatal(err)
	}
	proj := ComposeProject{Name: "shop", Dir: t.TempDir(), Files: []string{"/p/shop/compose.yaml"}}
	if err := d.Compose(ctx, proj, "up"); err != nil {
		t.Fatal(err)
	}
	got := calls(t, log)
	want := []string{"restart shop-db-1", "compose -p shop -f /p/shop/compose.yaml up -d"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %q, want %q", got, want)
	}
}

func TestRejectsUnsafeInput(t *testing.T) {
	d, log := fakeDocker(t, "", 0)
	ctx := context.Background()
	for _, err := range []error{
		d.Act(ctx, "--privileged", "rm"),
		d.Act(ctx, "x", "exec"),
		d.Compose(ctx, ComposeProject{Name: "a", Dir: "/", Files: []string{"rel.yaml"}}, "up"),
		d.Compose(ctx, ComposeProject{Name: "-x", Dir: "/", Files: []string{"/a.yaml"}}, "up"),
		d.Compose(ctx, ComposeProject{Name: "a", Dir: "/", Files: []string{"/a.yaml"}}, "rm -f"),
	} {
		if err == nil {
			t.Error("unsafe input accepted")
		}
	}
	if b, _ := os.ReadFile(log); len(b) > 0 {
		t.Fatalf("docker was called: %s", b)
	}
}

func TestDaemonDownIsExplained(t *testing.T) {
	d, _ := fakeDocker(t, "", 1)
	if _, err := d.List(context.Background()); err == nil || !strings.Contains(err.Error(), "Docker isn't running") {
		t.Fatalf("err = %v", err)
	}
}

func TestFindComposeFiles(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"compose.yaml", "api/docker-compose.yml", "node_modules/x/compose.yaml", ".hidden/compose.yaml", "deep/a/compose.yaml"} {
		full := filepath.Join(root, p)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte("services: {}\n"), 0o644)
	}
	got := FindComposeFiles([]string{root})
	if len(got) != 2 || got[0].Dir != root || got[1].Dir != filepath.Join(root, "api") {
		t.Fatalf("found %+v", got)
	}
}

func TestWatchDebouncesAndSkipsExecNoise(t *testing.T) {
	d, _ := fakeDocker(t, "exec_start: sh\nexec_create: sh\nstart\nstop\n", 0)
	ch := make(chan struct{}, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = d.Watch(ctx, 20*time.Millisecond, func() { ch <- struct{}{} })
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no change reported")
	}
	select {
	case <-ch:
		t.Fatal("start+stop were not debounced into one change")
	case <-time.After(100 * time.Millisecond):
	}
}
