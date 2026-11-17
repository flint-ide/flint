package terminal

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// scanChunks feeds chunks to a new scanner and returns the states it reported.
func scanChunks(chunks ...string) []string {
	var p oscScanner
	var got []string
	for _, c := range chunks {
		p.Feed([]byte(c), func(s string) { got = append(got, s) })
	}
	return got
}

func osc(body string) string { return "\x1b]" + body + "\x07" }

func TestScannerRecognises(t *testing.T) {
	long := strings.Repeat("x", 600)
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"working, BEL", osc("6977;state=working"), []string{"working"}},
		{"working, ST", "\x1b]6977;state=working\x1b\\", []string{"working"}},
		{"waiting", osc("6977;state=waiting"), []string{"waiting"}},
		{"done", osc("6977;state=done"), []string{"done"}},
		{"idle", osc("6977;state=idle"), []string{"idle"}},
		{"several in order", osc("6977;state=working") + osc("6977;state=done"), []string{"working", "done"}},
		{"both terminators mixed", osc("6977;state=working") + "\x1b]6977;state=idle\x1b\\", []string{"working", "idle"}},
		{"interleaved output", "hello\r\n\x1b[31mred\x1b[0m" + osc("6977;state=done") + "tail \x1b[2K\r\n", []string{"done"}},
		{"between other sequences", "\x1b]0;title\x07" + osc("6977;state=waiting") + "\x1b]8;;http://x\x07", []string{"waiting"}},
		{"OSC 9 text", osc("9;Claude needs your permission"), []string{"waiting"}},
		{"OSC 9 text starting with a digit", osc("9;3 files changed"), []string{"waiting"}},
		{"OSC 9 progress is ConEmu's", osc("9;4;1;50"), nil},
		{"OSC 9 progress without values", osc("9;4"), nil},
		{"OSC 9 cwd is ConEmu's", osc("9;9;/home/me"), nil},
		{"OSC 9 empty", osc("9;"), nil},
		{"OSC 9 alone", osc("9"), nil},
		{"OSC 777 notify", osc("777;notify;Claude;waiting for input"), []string{"waiting"}},
		{"OSC 777 notify without text", osc("777;notify"), []string{"waiting"}},
		{"OSC 777 other verb", osc("777;precmd"), nil},
		{"OSC 777 verb that only starts with notify", osc("777;notifying;x"), nil},
		{"another number", osc("1337;SetMark"), nil},
		{"OSC 99 (kitty) is not ours", osc("99;i=1;hello"), nil},
		{"unknown state", osc("6977;state=sleeping"), nil},
		{"empty state", osc("6977;state="), nil},
		{"upper case state", osc("6977;state=WORKING"), nil},
		{"state with trailing text", osc("6977;state=working;x"), nil},
		{"state with trailing space", osc("6977;state=working "), nil},
		{"other key", osc("6977;mode=working"), nil},
		{"no payload", osc("6977"), nil},
		{"number prefix only", osc("69770;state=working"), nil},
		{"CSI is not OSC", "\x1b[6977;state=working\x07", nil},
		{"unterminated", "\x1b]6977;state=working", nil},
		{"CAN cancels", "\x1b]6977;state=wor\x18king\x07", nil},
		{"SUB cancels", "\x1b]6977;state=wor\x1aking\x07", nil},
		{"cut off by another sequence", "\x1b]6977;state=wor\x1b[31m" + osc("6977;state=done"), []string{"done"}},
		{"restarted by a new OSC", "\x1b]6977;state=wor\x1b]6977;state=idle\x07", []string{"idle"}},
		{"ESC ESC ]", "\x1b\x1b]6977;state=done\x07", []string{"done"}},
		{"plain text", "no escapes at all\r\n", nil},
		{"binary noise", "\x00\x01\xff\xfe\x1b\x1b\x1b", nil},
		// 512 bytes is the limit of a body: "9;" and 510 more is the last one read.
		{"body of 512 bytes", osc("9;" + strings.Repeat("x", 510)), []string{"waiting"}},
		{"body of 513 bytes", osc("9;" + strings.Repeat("x", 511)), nil},
		{"oversize, BEL", osc("9;" + long), nil},
		{"oversize, ST, then a sequence", "\x1b]9;" + long + "\x1b\\" + osc("6977;state=done"), []string{"done"}},
		{"oversize then output then a sequence", osc("777;notify;"+long) + "text" + osc("6977;state=working"), []string{"working"}},
		{"oversize cut off by a new OSC", "\x1b]9;" + long + osc("6977;state=idle"), []string{"idle"}},
		{"oversize, ESC inside", "\x1b]9;" + long + "\x1b[1m" + "\x07" + osc("6977;state=done"), []string{"done"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scanChunks(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("whole: %q -> %v, want %v", tt.in, got, tt.want)
			}
			// The same result however the output is cut into chunks.
			for i := 0; i <= len(tt.in); i++ {
				if got := scanChunks(tt.in[:i], tt.in[i:]); !slices.Equal(got, tt.want) {
					t.Errorf("split at %d: %v, want %v", i, got, tt.want)
				}
			}
			var bytewise []string
			var p oscScanner
			for i := 0; i < len(tt.in); i++ {
				p.Feed([]byte{tt.in[i]}, func(s string) { bytewise = append(bytewise, s) })
			}
			if !slices.Equal(bytewise, tt.want) {
				t.Errorf("byte by byte: %v, want %v", bytewise, tt.want)
			}
		})
	}
}

func TestScannerNeverBuffersMoreThanTheLimit(t *testing.T) {
	var p oscScanner
	p.Feed([]byte("\x1b]"), func(string) {})
	for i := 0; i < 100; i++ {
		p.Feed([]byte(strings.Repeat("a", 1000)), func(string) { t.Fatal("reported") })
		if p.n > maxOSC {
			t.Fatalf("buffered %d bytes", p.n)
		}
	}
	// It still finds the next sequence after the flood.
	var got []string
	p.Feed([]byte("\x07"+osc("6977;state=done")), func(s string) { got = append(got, s) })
	if !slices.Equal(got, []string{"done"}) {
		t.Errorf("after a flood: %v", got)
	}
}

func TestScannerHotPathDoesNotAllocate(t *testing.T) {
	var p oscScanner
	n := 0
	emit := func(string) { n++ }
	plain := []byte(strings.Repeat("some ordinary output, with colours \x1b[32mgreen\x1b[0m and more\r\n", 500))
	if allocs := testing.AllocsPerRun(100, func() { p.Feed(plain, emit) }); allocs != 0 {
		t.Errorf("%v allocations per chunk of output with CSI sequences", allocs)
	}
	noEsc := []byte(strings.Repeat("a plain line\r\n", 2000))
	if allocs := testing.AllocsPerRun(100, func() { p.Feed(noEsc, emit) }); allocs != 0 {
		t.Errorf("%v allocations per chunk without ESC", allocs)
	}
	seq := []byte(osc("6977;state=working"))
	if allocs := testing.AllocsPerRun(100, func() { p.Feed(seq, emit) }); allocs != 0 {
		t.Errorf("%v allocations per sequence", allocs)
	}
	if n == 0 {
		t.Error("the sequence was not seen")
	}
}

func FuzzOSCScanner(f *testing.F) {
	for _, s := range []string{
		osc("6977;state=working"), "\x1b]6977;state=idle\x1b\\", osc("9;hi"), osc("9;4;1;2"), osc("777;notify;a;b"),
		"\x1b]9;" + strings.Repeat("x", 600), "\x1b\x1b]\x1b\\\x18\x1a", "plain", "",
	} {
		f.Add(s, uint8(3))
	}
	valid := map[string]bool{AgentWorking: true, AgentWaiting: true, AgentDone: true, AgentIdle: true}
	f.Fuzz(func(t *testing.T, in string, cut uint8) {
		var whole, split, bytewise []string
		var a, b, c oscScanner
		a.Feed([]byte(in), func(s string) { whole = append(whole, s) })
		at := int(cut) % (len(in) + 1)
		b.Feed([]byte(in[:at]), func(s string) { split = append(split, s) })
		if b.n > maxOSC {
			t.Fatalf("buffered %d bytes", b.n)
		}
		b.Feed([]byte(in[at:]), func(s string) { split = append(split, s) })
		for i := 0; i < len(in); i++ {
			c.Feed([]byte{in[i]}, func(s string) { bytewise = append(bytewise, s) })
		}
		if !slices.Equal(whole, split) || !slices.Equal(whole, bytewise) {
			t.Fatalf("chunking changed the result for %q: whole %v, split at %d %v, bytewise %v", in, whole, at, split, bytewise)
		}
		for _, s := range whole {
			if !valid[s] {
				t.Fatalf("reported %q", s)
			}
		}
		if a.n > maxOSC || c.n > maxOSC {
			t.Fatal("buffer over its limit")
		}
	})
}

// nextState waits for a reported agent state.
func nextState(t *testing.T, r *recorder, want string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case s := <-r.states:
			if s == want {
				return
			}
		case <-deadline:
			t.Fatalf("no %q state; output so far: %q", want, r.output())
		}
	}
}

func TestTerminalReportsAgentStateAndPassesTheSequenceThrough(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	if err := m.Start("t1", Options{Cwd: t.TempDir(), Shell: "/bin/sh"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Write("t1", []byte(`printf '\033]6977;state=working\007hello\033]6977;state=waiting\033\\'`+"\n")); err != nil {
		t.Fatal(err)
	}
	nextState(t, rec, "working")
	nextState(t, rec, "waiting")
	// The screen gets exactly what the program printed, sequences included.
	deadline := time.Now().Add(5 * time.Second)
	want := "\x1b]6977;state=working\x07hello\x1b]6977;state=waiting\x1b\\"
	for !strings.Contains(rec.output(), want) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(rec.output(), want) {
		t.Errorf("output %q lost or changed the sequences", rec.output())
	}
	_ = m.Write("t1", []byte("exit\n"))
	waitExit(t, rec)
}

// A hook is a child of the agent CLI with its output captured, so it cannot
// print its sequence to stdout: it opens the controlling terminal. This runs
// such a command the way Claude Code spawns one (same session and process group,
// stdout and stderr piped) and checks the scanner sees what it wrote to /dev/tty.
func TestHookWritesToTheControllingTerminal(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	if err := m.Start("t1", Options{Cwd: t.TempDir(), Shell: "/bin/sh"}); err != nil {
		t.Fatal(err)
	}
	defer m.Close("t1")
	hook := `sh -c 'printf "\033]6977;state=waiting\007" > /dev/tty' </dev/null 2>&1 | cat > /dev/null`
	if err := m.Write("t1", []byte(hook+"\n")); err != nil {
		t.Fatal(err)
	}
	nextState(t, rec, "waiting")
	if strings.Contains(rec.output(), "no such device") || strings.Contains(rec.output(), "Device not configured") {
		t.Errorf("the hook could not open /dev/tty: %q", rec.output())
	}
}

func TestAgentGoesIdleWhenTheProgramLeaves(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	if err := m.Start("t1", Options{Cwd: t.TempDir(), Shell: "/bin/sh", Args: []string{"-i"}}); err != nil {
		t.Fatal(err)
	}
	defer m.Close("t1")
	nextTitle(t, rec, func(i Info) bool { return i.Command == "" })
	// The "agent" reports working, then runs on for a while and exits.
	if err := m.Write("t1", []byte(`printf '\033]6977;state=working\007'; sleep 1`+"\n")); err != nil {
		t.Fatal(err)
	}
	nextState(t, rec, "working")
	nextTitle(t, rec, func(i Info) bool { return i.Command != "" })
	nextState(t, rec, "idle")
}

func TestTerminalWithoutAnAgentReportsNothing(t *testing.T) {
	rec := newRecorder()
	m := NewManager(rec)
	if err := m.Start("t1", Options{Cwd: t.TempDir(), Shell: "/bin/sh", Args: []string{"-i"}}); err != nil {
		t.Fatal(err)
	}
	defer m.Close("t1")
	nextTitle(t, rec, func(i Info) bool { return i.Command == "" })
	if err := m.Write("t1", []byte("sleep 1\n")); err != nil {
		t.Fatal(err)
	}
	nextTitle(t, rec, func(i Info) bool { return i.Command != "" })
	nextTitle(t, rec, func(i Info) bool { return i.Command == "" })
	select {
	case s := <-rec.states:
		t.Errorf("a terminal that runs no agent reported %q", s)
	default:
	}
}
