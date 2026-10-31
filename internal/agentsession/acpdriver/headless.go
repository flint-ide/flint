package acpdriver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"flint/internal/acp"
	as "flint/internal/agentsession"
	"flint/internal/redact"
)

// Headless runs one prompt in a fresh session in cwd and returns the agent's
// answer: the text after its last tool call (what it said before is
// commentary such as "let me look at the diff"). A one-shot run may look at
// the project but never change it: every permission request is declined,
// writes through Flint are refused and reads stay inside cwd. The agent
// process is stopped before Headless returns, also when ctx ends.
func Headless(ctx context.Context, opts Options, cwd, prompt string) (string, error) {
	if err := opts.CheckTrust(cwd); err != nil {
		return "", err
	}
	h := &headlessHandler{root: cwd}
	aopts, err := opts.acpOptions(ctx, cwd)
	if err != nil {
		return "", headlessError(ctx, opts.Name, err)
	}
	client, err := acp.Start(ctx, aopts, h)
	if err != nil {
		return "", headlessError(ctx, opts.Name, err)
	}
	defer client.Close()
	// A cancelled Prompt waits for the agent to end its turn; a timeout must
	// not wait on an agent that hangs.
	stopWatch := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stopWatch()

	res, err := client.NewSession(ctx, cwd, nil)
	if err != nil {
		return "", headlessError(ctx, opts.Name, err)
	}
	h.setSession(res.SessionID)
	stop, err := client.Prompt(ctx, res.SessionID, []acp.ContentBlock{acp.TextBlock(prompt)})
	if err != nil {
		return "", headlessError(ctx, opts.Name, err)
	}
	answer := h.answer()
	if answer == "" {
		return "", fmt.Errorf("%s gave no answer (stop reason %q)", opts.Name, stop)
	}
	return answer, nil
}

func headlessError(ctx context.Context, name string, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s did not answer in time%s", name, stderrNote(err))
	}
	return fmt.Errorf("%s: %w", name, err)
}

// stderrShown is how much of an agent's stderr a timeout error quotes.
const stderrShown = 800

// stderrNote quotes the end of what an agent that failed its handshake
// printed (often the reason: a login prompt, a question on the terminal),
// with credentials and the home directory taken out; "" when it printed
// nothing.
func stderrNote(err error) string {
	var hs *acp.HandshakeError
	if !errors.As(err, &hs) {
		return ""
	}
	tail := strings.TrimSpace(redact.String(hs.Stderr))
	if tail == "" {
		return ""
	}
	if len(tail) > stderrShown {
		tail = "…" + strings.ToValidUTF8(tail[len(tail)-stderrShown:], "")
	}
	return "; it printed: " + tail
}

// AgentInfo is what an agent reports about itself in `initialize`.
type AgentInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
	// AuthMethods are the names of the ways the agent can log in; an agent
	// that needs a login reports its own error on the first session.
	AuthMethods []string `json:"authMethods"`
}

// Probe starts the agent in dir, runs the `initialize` handshake and stops
// it again, to tell the user whether the command speaks ACP. No session is
// opened, so nothing reaches a model. It returns within moments of ctx's
// deadline: an agent that misses the handshake is killed at once.
func Probe(ctx context.Context, opts Options, dir string) (AgentInfo, error) {
	client, err := startProbe(ctx, opts, dir)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return AgentInfo{}, fmt.Errorf("%s did not answer the ACP handshake in time: check that %q starts its ACP mode and that the agent runs in a terminal%s",
				opts.Name, strings.TrimSpace(opts.Command+" "+strings.Join(opts.Args, " ")), stderrNote(err))
		}
		return AgentInfo{}, err
	}
	defer client.Close()
	init := client.Info()
	out := AgentInfo{AuthMethods: []string{}}
	if init.AgentInfo != nil {
		out.Name, out.Title, out.Version = init.AgentInfo.Name, init.AgentInfo.Title, init.AgentInfo.Version
	}
	for _, m := range init.AuthMethods {
		out.AuthMethods = append(out.AuthMethods, m.Name)
	}
	return out, nil
}

func startProbe(ctx context.Context, opts Options, dir string) (*acp.Client, error) {
	aopts, err := opts.acpOptions(ctx, dir)
	if err != nil {
		return nil, err
	}
	return acp.Start(ctx, aopts, &headlessHandler{root: dir})
}

// headlessHandler serves a session nobody watches: it collects the answer
// and refuses everything that would change something.
type headlessHandler struct {
	root string

	mu      sync.Mutex
	session string
	text    strings.Builder
}

func (h *headlessHandler) setSession(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.session = id
}

func (h *headlessHandler) answer() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.TrimSpace(h.text.String())
}

func (h *headlessHandler) SessionUpdate(_ string, update json.RawMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range translate(update) {
		switch e.Kind {
		case as.KindMessageChunk:
			h.text.WriteString(e.Text)
		case as.KindToolCall:
			h.text.Reset()
		}
	}
}

// RequestPermission declines: nobody is there to approve, and nothing a
// one-shot answer needs requires approval.
func (h *headlessHandler) RequestPermission(_ context.Context, req acp.PermissionRequest) (acp.PermissionOutcome, error) {
	for _, kind := range []string{acp.PermissionRejectOnce, acp.PermissionRejectAlways} {
		for _, o := range req.Options {
			if o.Kind == kind {
				return acp.Selected(o.OptionID), nil
			}
		}
	}
	return acp.Cancelled(), nil
}

func (h *headlessHandler) ReadTextFile(_ context.Context, req acp.ReadTextFileRequest) (string, error) {
	h.mu.Lock()
	session := h.session
	h.mu.Unlock()
	if session == "" || req.SessionID != session {
		return "", fmt.Errorf("unknown session %q", req.SessionID)
	}
	return readConfined(h.root, req)
}

func (h *headlessHandler) WriteTextFile(context.Context, acp.WriteTextFileRequest) error {
	return errors.New("this run may not change files")
}
