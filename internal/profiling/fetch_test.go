package profiling

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEndpoint(t *testing.T) {
	cases := []struct {
		in, want string
		seconds  time.Duration
	}{
		{"http://localhost:6060/debug/pprof/profile", "http://localhost:6060/debug/pprof/profile?seconds=10", 10 * time.Second},
		{"http://localhost:6060/debug/pprof/profile?seconds=3", "http://localhost:6060/debug/pprof/profile?seconds=3", 3 * time.Second},
		{"http://127.0.0.1:6060/debug/pprof/heap", "http://127.0.0.1:6060/debug/pprof/heap", 0},
	}
	for _, c := range cases {
		got, secs, err := Endpoint(c.in)
		if err != nil || got != c.want || secs != c.seconds {
			t.Errorf("Endpoint(%s) = %s, %s, %v", c.in, got, secs, err)
		}
	}
	if _, _, err := Endpoint("http://localhost/debug/pprof/profile?seconds=x"); err == nil {
		t.Error("invalid seconds accepted")
	}
}

func profileBytes(t *testing.T) []byte {
	t.Helper()
	b := newBuilder("samples", "cpu")
	b.sample("main;work", 3, 300)
	var buf bytes.Buffer
	if err := b.p.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFetchWaitsForTheProgram(t *testing.T) {
	data := profileBytes(t)
	// Reserve a port, then start listening on it only after Fetch began retrying.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	var gotSeconds string
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSeconds = r.URL.Query().Get("seconds")
		_, _ = w.Write(data)
	})}
	go func() {
		time.Sleep(400 * time.Millisecond)
		l2, err := net.Listen("tcp", addr)
		if err != nil {
			return
		}
		_ = srv.Serve(l2)
	}()
	defer srv.Close()

	endpoint, rec, err := Endpoint("http://" + addr + "/debug/pprof/profile?seconds=1")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cpu.pprof")
	if err := Fetch(context.Background(), endpoint, rec, path, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if gotSeconds != "1" {
		t.Fatalf("seconds=%q", gotSeconds)
	}
	r, err := Load(path, "")
	if err != nil || r.Total != 300 {
		t.Fatalf("load: %v total %d", err, r.Total)
	}
}

func TestFetchGivesUpWhenTheProgramEnds(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	err = Fetch(context.Background(), "http://"+addr+"/debug/pprof/heap", 0, filepath.Join(t.TempDir(), "x"), func() bool { return false })
	if err == nil || !strings.Contains(err.Error(), "ended") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/notfound" {
			http.Error(w, "no pprof here", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("<html>not a profile</html>"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	for _, p := range []string{"/notfound", "/html"} {
		path := filepath.Join(dir, "p")
		if err := Fetch(context.Background(), srv.URL+p, 0, path, func() bool { return true }); err == nil {
			t.Errorf("%s: no error", p)
		}
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s: file written", p)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Fetch(ctx, srv.URL+"/x", 0, filepath.Join(dir, "c"), func() bool { return true }); err == nil {
		t.Error("cancelled fetch succeeded")
	}
}

func TestDirs(t *testing.T) {
	base := t.TempDir()
	stale := filepath.Join(base, dirPrefix+"old")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(stale, old, old)
	other := filepath.Join(base, "unrelated")
	_ = os.Mkdir(other, 0o700)

	d := NewDirs(base)
	a, err := d.New()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := d.New()
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale directory kept")
	}
	if !d.Owns(a) || d.Owns(other) {
		t.Fatal("ownership")
	}
	if err := d.Remove(other); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("a directory Flint did not create was removed")
	}
	if err := d.Remove(a); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatal("removed directory still exists")
	}
	d.RemoveAll()
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Fatal("RemoveAll kept a directory")
	}
}
