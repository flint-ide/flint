package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"
)

// The test binary doubles as a fake `codex app-server`: when
// FAKE_CODEX_SCRIPT is set, TestMain replays that transcript instead of
// running tests. Transcript lines are {"dir":"in"|"out","msg":...} as
// captured from the real app-server ("in" = server → client), plus the
// directives {"dir":"exit","code":N,"stderr":"..."} and {"dir":"sleep","ms":N}.
//
// "out" entries are awaited from the driver: requests and notifications are
// matched by method (ids are remapped), responses by id and result. "in"
// entries are written with "$CWD" replaced by the thread/start cwd.
const (
	envFakeScript = "FAKE_CODEX_SCRIPT"
	envFakeLog    = "FAKE_CODEX_LOG"
)

type scriptEntry struct {
	Dir    string          `json:"dir"`
	Msg    json.RawMessage `json:"msg"`
	Code   int             `json:"code"`
	Stderr string          `json:"stderr"`
	Ms     int             `json:"ms"`
}

type wireMsg struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

type fakeServer struct {
	in  *lineReader
	out *bufio.Writer
	log io.Writer
	ids map[string]json.RawMessage // recorded client request id → actual id
	cwd string
}

func runFakeServer(script string) int {
	data, err := os.ReadFile(script)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake: read script:", err)
		return 5
	}
	f := &fakeServer{
		in:  newLineReader(os.Stdin, maxMessageBytes),
		out: bufio.NewWriter(os.Stdout),
		log: io.Discard,
		ids: map[string]json.RawMessage{},
	}
	if p := os.Getenv(envFakeLog); p != "" {
		lf, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			defer lf.Close()
			f.log = lf
		}
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e scriptEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			fmt.Fprintln(os.Stderr, "fake: bad script line:", err)
			return 5
		}
		if code, done := f.step(e); done {
			return code
		}
	}
	// Script done: idle until the driver closes stdin.
	for {
		line, err := f.in.next()
		if err != nil {
			return 0
		}
		f.logLine(line)
	}
}

// step runs one entry; done reports that the server must exit with code.
func (f *fakeServer) step(e scriptEntry) (code int, done bool) {
	switch e.Dir {
	case "in":
		f.write(e.Msg)
	case "out":
		if err := f.expect(e.Msg); err != nil {
			fmt.Fprintln(os.Stderr, "fake:", err)
			return 3, true
		}
	case "sleep":
		time.Sleep(time.Duration(e.Ms) * time.Millisecond)
	case "exit":
		_ = f.out.Flush()
		fmt.Fprint(os.Stderr, e.Stderr)
		return e.Code, true
	}
	return 0, false
}

func (f *fakeServer) logLine(line []byte) {
	_, _ = f.log.Write(append(append([]byte(nil), line...), '\n'))
}

func (f *fakeServer) write(raw json.RawMessage) {
	text := string(raw)
	if f.cwd != "" {
		quoted, _ := json.Marshal(f.cwd)
		text = strings.ReplaceAll(text, "$CWD", string(quoted[1:len(quoted)-1]))
	}
	var m wireMsg
	_ = json.Unmarshal([]byte(text), &m)
	if m.Method == "" && len(m.ID) > 0 {
		if actual, ok := f.ids[string(m.ID)]; ok {
			var obj map[string]json.RawMessage
			_ = json.Unmarshal([]byte(text), &obj)
			obj["id"] = actual
			b, _ := json.Marshal(obj)
			text = string(b)
		}
	}
	_, _ = f.out.WriteString(text + "\n")
	_ = f.out.Flush()
}

// expect reads driver messages until one matches want. Unexpected responses
// (e.g. a "cancel" decision sent while interrupting) are skipped.
func (f *fakeServer) expect(raw json.RawMessage) error {
	var want wireMsg
	if err := json.Unmarshal(raw, &want); err != nil {
		return err
	}
	for {
		line, err := f.in.next()
		if err != nil {
			return fmt.Errorf("waiting for %s: %w", describe(want), err)
		}
		f.logLine(line)
		var got wireMsg
		if err := json.Unmarshal(line, &got); err != nil {
			return fmt.Errorf("bad message %q: %w", line, err)
		}
		if got.Method == "" && want.Method != "" {
			continue
		}
		return f.match(want, got)
	}
}

func (f *fakeServer) match(want, got wireMsg) error {
	if want.Method != "" {
		if got.Method != want.Method {
			return fmt.Errorf("expected %s, got %s", describe(want), describe(got))
		}
		if len(want.ID) > 0 {
			f.ids[string(want.ID)] = got.ID
		}
		if got.Method == methodThreadStart {
			var p struct {
				Cwd string `json:"cwd"`
			}
			_ = json.Unmarshal(got.Params, &p)
			f.cwd = p.Cwd
		}
		return nil
	}
	if string(compact(got.ID)) != string(compact(want.ID)) {
		return fmt.Errorf("expected response to %s, got %s", want.ID, describe(got))
	}
	if want.Error != nil {
		if got.Error == nil || got.Error.Code != want.Error.Code {
			return fmt.Errorf("expected error %d for %s, got %s", want.Error.Code, want.ID, describe(got))
		}
		return nil
	}
	if !sameJSON(want.Result, got.Result) {
		return fmt.Errorf("response %s: expected result %s, got %s", want.ID, want.Result, got.Result)
	}
	return nil
}

func describe(m wireMsg) string {
	if m.Method != "" {
		return m.Method
	}
	b, _ := json.Marshal(m)
	return "response " + string(b)
}

func compact(raw json.RawMessage) json.RawMessage {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	b, _ := json.Marshal(v)
	return b
}

func sameJSON(a, b json.RawMessage) bool {
	var va, vb any
	if json.Unmarshal(a, &va) != nil || json.Unmarshal(b, &vb) != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}
