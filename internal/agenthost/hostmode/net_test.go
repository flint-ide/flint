package hostmode

import (
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestListenMakesAPrivateSocketAndReplacesAStaleOne(t *testing.T) {
	sock := filepath.Join(shortDir(t), "host.sock")

	// A host that died without cleaning up leaves its socket behind.
	old, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	old.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = old.Close()
	if _, err := os.Lstat(sock); err != nil {
		t.Fatalf("no stale socket to test with: %v", err)
	}

	ln, err := listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	fi, err := os.Lstat(sock)
	if err != nil || fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o600 {
		t.Errorf("socket = %v, %v", fi, err)
	}
}

func TestPeerUIDOfARealSocket(t *testing.T) {
	if !peerCredSupported {
		t.Skip("no peer credentials on this platform")
	}
	sock := filepath.Join(shortDir(t), "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan *net.UnixConn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- nil
			return
		}
		done <- c.(*net.UnixConn)
	}()
	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	server := <-done
	if server == nil {
		t.Fatal("accept failed")
	}
	defer server.Close()
	uid, err := peerUID(server)
	if err != nil || uid != uint32(os.Getuid()) {
		t.Errorf("peerUID = %d, %v; want %d", uid, err, os.Getuid())
	}
}

// The accept loop serves a peer of our uid and drops any other without
// reading a byte from it.
func TestAcceptLoopChecksThePeer(t *testing.T) {
	if !peerCredSupported {
		t.Skip("no peer credentials on this platform")
	}
	tests := []struct {
		name   string
		want   uint32
		served bool
	}{
		{"same uid", uint32(os.Getuid()), true},
		{"another uid", uint32(os.Getuid()) + 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sock := filepath.Join(shortDir(t), "s")
			ln, err := net.Listen("unix", sock)
			if err != nil {
				t.Fatal(err)
			}
			served := make(chan struct{}, 1)
			loopDone := make(chan struct{})
			go func() {
				defer close(loopDone)
				acceptLoop(ln, tt.want, 8, func(c net.Conn) {
					served <- struct{}{}
					_ = c.Close()
				})
			}()
			c, err := net.Dial("unix", sock)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			// A refused peer sees its connection closed.
			_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, _ = c.Read(make([]byte, 1))
			select {
			case <-served:
				if !tt.served {
					t.Error("a peer of another uid was served")
				}
			default:
				if tt.served {
					t.Error("a peer of our uid was not served")
				}
			}
			_ = ln.Close()
			<-loopDone
		})
	}
}

func TestAcceptLoopBoundsConnections(t *testing.T) {
	if !peerCredSupported {
		t.Skip("no peer credentials on this platform")
	}
	sock := filepath.Join(shortDir(t), "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var served atomic.Int32
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		acceptLoop(ln, uint32(os.Getuid()), 2, func(c net.Conn) {
			served.Add(1)
			started <- struct{}{}
			<-release
			_ = c.Close()
		})
	}()
	var conns []net.Conn
	for range 3 {
		c, err := net.Dial("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	// The third is closed by the host while the first two are held.
	_ = conns[2].SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := conns[2].Read(make([]byte, 1)); err == nil || os.IsTimeout(err) {
		t.Errorf("third connection read = %v, want it closed by the host", err)
	}
	// The loop counts a connection before its serve goroutine runs, so the
	// third can be refused before the first two were scheduled: wait for them.
	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatalf("served %d connections, want 2", served.Load())
		}
	}
	if n := served.Load(); n != 2 {
		t.Errorf("served %d connections, want 2", n)
	}
	close(release)
	for _, c := range conns {
		_ = c.Close()
	}
	_ = ln.Close()
	<-loopDone
}

func TestIdleWatch(t *testing.T) {
	const d = 80 * time.Millisecond
	var idle atomic.Bool
	fired := make(chan struct{}, 4)
	w := newIdleWatch(d, idle.Load, func() { fired <- struct{}{} })
	defer w.Stop()

	expectNo := func(why string) {
		t.Helper()
		select {
		case <-fired:
			t.Fatalf("fired: %s", why)
		case <-time.After(3 * d):
		}
	}
	expectFire := func(why string) {
		t.Helper()
		select {
		case <-fired:
		case <-time.After(10 * time.Second):
			t.Fatalf("did not fire: %s", why)
		}
	}

	expectNo("the host was busy from the start") // idle is false at creation
	idle.Store(true)
	w.Poke()
	expectFire("nothing left to serve")

	idle.Store(false)
	w.Poke()
	idle.Store(true)
	w.Poke()
	idle.Store(false)
	w.Poke()
	expectNo("a client came back within the time")

	idle.Store(true)
	w.Poke()
	w.Stop()
	expectNo("stopped")
}

// A host that starts with nothing to do exits too: the spawner is expected to
// connect within the time.
func TestIdleWatchArmsAtCreationWhenIdle(t *testing.T) {
	fired := make(chan struct{}, 1)
	w := newIdleWatch(50*time.Millisecond, func() bool { return true }, func() { fired <- struct{}{} })
	defer w.Stop()
	select {
	case <-fired:
	case <-time.After(10 * time.Second):
		t.Fatal("an idle host never exited")
	}
}

// If the host got busy just as the timer fired, it stays.
func TestIdleWatchChecksAgainWhenTheTimerFires(t *testing.T) {
	var idle atomic.Bool
	idle.Store(true)
	fired := make(chan struct{}, 1)
	w := newIdleWatch(50*time.Millisecond, idle.Load, func() { fired <- struct{}{} })
	defer w.Stop()
	idle.Store(false) // no Poke: the timer fires on a stale view
	select {
	case <-fired:
		t.Fatal("fired although the host was busy")
	case <-time.After(300 * time.Millisecond):
	}
}
