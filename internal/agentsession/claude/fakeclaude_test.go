package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary doubles as a fake `claude`: re-executed with fakeEnv=1 it
// replays the transcripts captured from the real CLI (testdata/<name>.out.jsonl)
// and validates what the driver writes against the captured host side
// (testdata/<name>.in.jsonl). It works on raw JSON, not this package's types,
// so wire-shape mistakes in the driver are not mirrored here.
const (
	fakeEnv         = "FLINT_CLAUDE_FAKE"
	fakeScenarioEnv = "FLINT_CLAUDE_FAKE_SCENARIO"
	fakeDirEnv      = "FLINT_CLAUDE_FAKE_DIR"
	fakeLogEnv      = "FLINT_CLAUDE_FAKE_LOG"

	fixtureCwd = "/FIXTURE_CWD"
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) == "1" {
		os.Exit(runFake())
	}
	os.Exit(m.Run())
}

// hostMsg is one line the driver wrote to the fake's stdin.
type hostMsg struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Request   struct {
		Subtype string `json:"subtype"`
	} `json:"request"`
	Response struct {
		Subtype   string          `json:"subtype"`
		RequestID string          `json:"request_id"`
		Response  json.RawMessage `json:"response"`
	} `json:"response"`
	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

type fakeCLI struct {
	in      chan hostMsg
	backlog []hostMsg // control_requests not consumed yet
	wmu     sync.Mutex
	logf    *os.File
	dir     string
	cwd     string
	sid     string
	// recorded host lines of the scenario, by request_id
	recordedReq  map[string]string          // control_request id -> subtype
	recordedResp map[string]json.RawMessage // can_use_tool id -> permission answer
}

func fakeFail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fake claude: "+format+"\n", args...)
	os.Exit(7)
}

func runFake() int {
	args := os.Args[1:]
	f := &fakeCLI{in: make(chan hostMsg, 64), dir: os.Getenv(fakeDirEnv)}
	f.cwd = os.Getenv("PWD")
	if f.cwd == "" {
		f.cwd, _ = os.Getwd()
	}
	if p := os.Getenv(fakeLogEnv); p != "" {
		lf, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			fakeFail("open log: %v", err)
		}
		f.logf = lf
		f.log(map[string]any{"args": args, "todoEnv": os.Getenv("CLAUDE_CODE_ENABLE_TODO_TOOLS"),
			"pwd": os.Getenv("PWD"), "claudecode": os.Getenv("CLAUDECODE")})
	}
	f.checkArgs(args)
	if os.Getenv(fakeScenarioEnv) == "closestdin" {
		return runClosingStdin(f)
	}
	go f.readStdin()

	switch scenario := os.Getenv(fakeScenarioEnv); scenario {
	case "badinit":
		fmt.Fprintln(os.Stderr, "Invalid API key · Please run /login")
		return 1
	case "crash":
		f.waitUser()
		f.emitRaw(`{"type":"system","subtype":"init","model":"claude-haiku-5-5","session_id":"x"}`)
		f.emitRaw(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_crash"}},"parent_tool_use_id":null}`)
		f.emitRaw(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}},"parent_tool_use_id":null}`)
		fmt.Fprintln(os.Stderr, "fatal: boom (simulated crash)")
		return 3
	case "hang":
		f.waitUser()
		f.emitRaw(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"working"}},"parent_tool_use_id":null,"api_message_id":"msg_hang"}`)
		for {
			f.next() // ignores interrupts; exits on stdin EOF or SIGTERM
		}
	case "exitidle":
		go func() {
			time.Sleep(300 * time.Millisecond)
			fmt.Fprintln(os.Stderr, "idle crash")
			os.Exit(5)
		}()
		for {
			f.next()
		}
	default:
		if raw, ok := strings.CutPrefix(scenario, "raw:"); ok {
			f.replayRaw(raw)
		} else {
			f.replay(scenario)
		}
		for {
			f.next() // serve late control requests until stdin closes
		}
	}
}

// runClosingStdin answers initialize, then closes stdin while staying alive,
// so the driver's next write fails.
func runClosingStdin(f *fakeCLI) int {
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadBytes('\n')
	if err != nil {
		fakeFail("read initialize: %v", err)
	}
	var m hostMsg
	_ = json.Unmarshal(line, &m)
	data, _ := os.ReadFile(filepath.Join(f.dir, "initialize.json"))
	var v any
	_ = json.Unmarshal(data, &v)
	compact, _ := json.Marshal(v)
	f.emitRaw(strings.Replace(string(compact), `"request_id":"REQ"`, `"request_id":"`+m.RequestID+`"`, 1))
	_ = os.Stdin.Close()
	time.Sleep(time.Hour) // until SIGTERM
	return 0
}

// replayRaw emits a synthetic transcript turn by turn without any
// synchronisation beyond waiting for each user message.
func (f *fakeCLI) replayRaw(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fakeFail("read %s: %v", path, err)
	}
	text := strings.ReplaceAll(string(data), fixtureCwd, f.cwd)
	for _, seg := range segments(strings.Split(strings.TrimSpace(text), "\n")) {
		f.waitUser()
		for _, line := range seg {
			f.emitRaw(line)
		}
	}
}

func (f *fakeCLI) checkArgs(args []string) {
	want := [][]string{
		{"-p"}, {"--input-format", "stream-json"}, {"--output-format", "stream-json"}, {"--verbose"},
		{"--include-partial-messages"}, {"--permission-prompt-tool", "stdio"},
	}
	for _, w := range want {
		i := slices.Index(args, w[0])
		if i < 0 || (len(w) == 2 && (i+1 >= len(args) || args[i+1] != w[1])) {
			fakeFail("missing flag %v in %v", w, args)
		}
	}
	i := slices.Index(args, "--session-id")
	if i < 0 || i+1 >= len(args) || !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(args[i+1]) {
		fakeFail("bad --session-id in %v", args)
	}
	f.sid = args[i+1]
}

func (f *fakeCLI) log(v any) {
	if f.logf == nil {
		return
	}
	data, _ := json.Marshal(v)
	f.wmu.Lock()
	defer f.wmu.Unlock()
	_, _ = f.logf.Write(append(data, '\n'))
}

func (f *fakeCLI) readStdin() {
	defer close(f.in)
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if f.logf != nil {
			f.wmu.Lock()
			_, _ = f.logf.Write(append(append([]byte(`{"stdin":`), line...), '}', '\n'))
			f.wmu.Unlock()
		}
		var m hostMsg
		if err := json.Unmarshal(line, &m); err != nil {
			fakeFail("bad json on stdin: %v: %s", err, line)
		}
		f.in <- m
	}
}

func (f *fakeCLI) emitRaw(line string) {
	f.wmu.Lock()
	defer f.wmu.Unlock()
	_, _ = os.Stdout.WriteString(line + "\n")
}

// next returns the next host message, answering initialize inline. Stdin EOF
// ends the process like the real CLI.
func (f *fakeCLI) next() hostMsg {
	for {
		m, ok := <-f.in
		if !ok {
			os.Exit(0)
		}
		if m.Type == "control_request" && m.Request.Subtype == "initialize" {
			data, err := os.ReadFile(filepath.Join(f.dir, "initialize.json"))
			if err != nil {
				fakeFail("read initialize.json: %v", err)
			}
			var v any
			_ = json.Unmarshal(data, &v)
			compact, _ := json.Marshal(v)
			f.emitRaw(strings.Replace(string(compact), `"request_id":"REQ"`, `"request_id":"`+m.RequestID+`"`, 1))
			continue
		}
		return m
	}
}

// waitUser waits for a user message, keeping control requests for later.
func (f *fakeCLI) waitUser() hostMsg {
	for {
		m := f.next()
		switch m.Type {
		case "user":
			if m.Message.Role != "user" || len(m.Message.Content) == 0 {
				fakeFail("bad user message: %+v", m)
			}
			for _, c := range m.Message.Content {
				if c.Type != "text" || c.Text == "" {
					fakeFail("bad content block: %+v", c)
				}
			}
			return m
		case "control_request":
			f.backlog = append(f.backlog, m)
		case "control_response":
			// answers to CLI requests are only logged
		default:
			fakeFail("unexpected %s while idle", m.Type)
		}
	}
}

// waitControlRequest returns the next host control_request of subtype.
func (f *fakeCLI) waitControlRequest(subtype string) hostMsg {
	for {
		for i, m := range f.backlog {
			if m.Request.Subtype == subtype {
				f.backlog = slices.Delete(f.backlog, i, i+1)
				return m
			}
		}
		m := f.next()
		if m.Type != "control_request" {
			fakeFail("expected control_request %s, got %s", subtype, m.Type)
		}
		f.backlog = append(f.backlog, m)
	}
}

// waitAnyControlRequest blocks until a host control_request is pending.
func (f *fakeCLI) waitAnyControlRequest() {
	for len(f.backlog) == 0 {
		m := f.next()
		if m.Type != "control_request" {
			fakeFail("expected a control_request, got %s", m.Type)
		}
		f.backlog = append(f.backlog, m)
	}
}

// waitPermissionAnswer returns the host's control_response for id.
func (f *fakeCLI) waitPermissionAnswer(id string) json.RawMessage {
	for {
		m := f.next()
		switch {
		case m.Type == "control_response" && m.Response.RequestID == id:
			if m.Response.Subtype != "success" {
				fakeFail("permission answer subtype %q", m.Response.Subtype)
			}
			return m.Response.Response
		case m.Type == "control_request":
			f.backlog = append(f.backlog, m)
		case m.Type == "control_response":
			// an answer to another CLI request (logged on stdin)
		default:
			fakeFail("expected control_response for %s, got %+v", id, m)
		}
	}
}

func (f *fakeCLI) loadRecordedHost(name string) {
	f.recordedReq = map[string]string{}
	f.recordedResp = map[string]json.RawMessage{}
	data, err := os.ReadFile(filepath.Join(f.dir, name+".in.jsonl"))
	if err != nil {
		fakeFail("read fixture: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var m hostMsg
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		switch m.Type {
		case "control_request":
			f.recordedReq[m.RequestID] = m.Request.Subtype
		case "control_response":
			f.recordedResp[m.Response.RequestID] = m.Response.Response
		}
	}
}

// segments splits a transcript into turns: each ends after its result line
// plus the trailing lines before the next system/init.
func segments(lines []string) [][]string {
	var out [][]string
	var cur []string
	afterResult := false
	for _, l := range lines {
		var head struct{ Type, Subtype string }
		_ = json.Unmarshal([]byte(l), &head)
		if afterResult && head.Type == "system" && head.Subtype == "init" {
			out = append(out, cur)
			cur, afterResult = nil, false
		}
		cur = append(cur, l)
		if head.Type == "result" {
			afterResult = true
		}
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

var recordedSessionID = regexp.MustCompile(`"session_id":"[0-9a-f-]{36}"`)

func (f *fakeCLI) replay(name string) {
	data, err := os.ReadFile(filepath.Join(f.dir, name+".out.jsonl"))
	if err != nil {
		fakeFail("read fixture: %v", err)
	}
	f.loadRecordedHost(name)
	text := strings.ReplaceAll(string(data), fixtureCwd, f.cwd)
	text = recordedSessionID.ReplaceAllString(text, `"session_id":"`+f.sid+`"`)
	for _, seg := range segments(strings.Split(strings.TrimSpace(text), "\n")) {
		f.waitUser()
		for _, line := range seg {
			f.replayLine(line)
		}
	}
}

func (f *fakeCLI) replayLine(line string) {
	var head struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
		Response struct {
			RequestID string `json:"request_id"`
		} `json:"response"`
	}
	_ = json.Unmarshal([]byte(line), &head)
	switch {
	case head.Type == "control_response":
		// The CLI's answer to a host request: wait for the driver's request.
		rid := head.Response.RequestID
		subtype := f.recordedReq[rid]
		if subtype == "" {
			fakeFail("no recorded host request %s", rid)
		}
		host := f.waitControlRequest(subtype)
		f.emitRaw(strings.Replace(line, `"request_id":"`+rid+`"`, `"request_id":"`+host.RequestID+`"`, 1))
	case head.Type == "control_request" && head.Request.Subtype == "can_use_tool":
		f.emitRaw(line)
		rec, answered := f.recordedResp[head.RequestID]
		if !answered {
			// The recorded host interrupted instead of answering.
			f.waitAnyControlRequest()
			return
		}
		got := f.waitPermissionAnswer(head.RequestID)
		f.log(map[string]any{"permissionAnswer": got})
		checkPermissionAnswer(got, rec)
	default:
		f.emitRaw(line)
	}
}

func checkPermissionAnswer(got, recorded json.RawMessage) {
	type answer struct {
		Behavior           string            `json:"behavior"`
		UpdatedInput       json.RawMessage   `json:"updatedInput"`
		UpdatedPermissions []json.RawMessage `json:"updatedPermissions"`
		Message            string            `json:"message"`
	}
	var g, r answer
	if json.Unmarshal(got, &g) != nil || json.Unmarshal(recorded, &r) != nil {
		fakeFail("undecodable permission answer %s", got)
	}
	if g.Behavior != r.Behavior {
		fakeFail("permission behavior %q, recorded %q", g.Behavior, r.Behavior)
	}
	if g.Behavior == "allow" && len(g.UpdatedInput) == 0 {
		fakeFail("allow without updatedInput")
	}
	if g.Behavior == "deny" && g.Message == "" {
		fakeFail("deny without message")
	}
	if (len(g.UpdatedPermissions) > 0) != (len(r.UpdatedPermissions) > 0) {
		fakeFail("updatedPermissions %s, recorded %s", got, recorded)
	}
}
