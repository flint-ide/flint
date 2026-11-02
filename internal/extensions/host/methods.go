package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"flint/internal/extensions/manifest"
	"flint/internal/extensions/rpc"
)

// What an extension may ask of Flint (docs/design/extensions.md, "Extension →
// Flint"). Every request is checked against the manifest's permissions and
// contributions; a missing permission is error -32001. Parameters are
// untrusted: sizes are bounded and text is cleaned before it reaches the UI.

// Limits on what an extension sends.
const (
	maxStatusText  = 40
	maxMessage     = 1000
	maxPrompt      = 20000
	maxAttachments = 20
	// messageBurst toasts per messageWindow; more are dropped (logged).
	messageBurst  = 5
	messageWindow = 10 * time.Second
)

// StatusText is a status item's current content (status/update).
type StatusText struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Tooltip  string `json:"tooltip,omitempty"`
	Command  string `json:"command,omitempty"`
	Severity string `json:"severity,omitempty"` // "", info, warning, error
}

// StatusEvent is EventStatus's data.
type StatusEvent struct {
	Extension string     `json:"extension"`
	Item      StatusText `json:"item"`
}

// RefreshEvent is EventRefresh's data: resolve the view (or the item) again.
type RefreshEvent struct {
	Extension string `json:"extension"`
	View      string `json:"view"`
	Item      string `json:"item,omitempty"`
}

// MessageEvent is EventMessage's data: a toast.
type MessageEvent struct {
	Extension string `json:"extension"`
	Name      string `json:"name"`
	Type      string `json:"type"` // info, warning, error
	Message   string `json:"message"`
}

// OpenFileEvent is EventOpenFile's data.
type OpenFileEvent struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

// AskEvent is EventAsk's data: a chat draft for the user to send.
type AskEvent struct {
	Extension   string   `json:"extension"`
	Prompt      string   `json:"prompt"`
	Attachments []string `json:"attachments,omitempty"`
}

func invalid(format string, args ...any) error {
	return rpc.Errorf(rpc.CodeInvalidParams, format, args...)
}

func decode(params json.RawMessage, v any) error {
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	if err := json.Unmarshal(params, v); err != nil {
		return invalid("invalid params: %v", err)
	}
	return nil
}

// need fails unless the manifest declares perm.
func (e *ext) need(perm string) error {
	if !e.m.HasPermission(perm) {
		return rpc.Errorf(rpc.CodePermission, "%s needs the %q permission in its manifest", e.id, perm)
	}
	return nil
}

// notified handles a notification from the extension (in order, quickly).
func (e *ext) notified(method string, params json.RawMessage) {
	var err error
	switch method {
	case "status/update":
		err = e.statusUpdate(params)
	case "view/refresh":
		err = e.viewRefresh(params)
	case "window/showMessage":
		err = e.showMessage(params)
	case "log":
		var p struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		}
		if err = decode(params, &p); err == nil {
			e.log.add(cleanText(p.Level, 10), cleanText(p.Message, maxLabel))
		}
	default:
		err = fmt.Errorf("unknown notification %q", method)
	}
	if err != nil {
		e.log.add("flint", method+": "+err.Error())
	}
}

// handle answers a request from the extension.
func (e *ext) handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	e.begin()
	defer e.end()
	switch method {
	case "window/showMessageRequest":
		return e.showMessageRequest(params)
	case "window/openFile":
		return nil, e.openFile(params)
	case "window/openUrl":
		return nil, e.openURL(params)
	case "workspace/readFile":
		return e.readFile(params)
	case "secrets/get", "secrets/set", "secrets/delete":
		return e.secret(strings.TrimPrefix(method, "secrets/"), params)
	case "agent/ask":
		return nil, e.askAgent(params)
	}
	return nil, rpc.Errorf(rpc.CodeMethodNotFound, "method not found: %s", method)
}

func (e *ext) statusUpdate(params json.RawMessage) error {
	var s StatusText
	if err := decode(params, &s); err != nil {
		return err
	}
	if !e.m.HasStatusItem(s.ID) {
		return invalid("no status item %q in the manifest", s.ID)
	}
	s.Text = cleanText(s.Text, maxStatusText)
	s.Tooltip = cleanText(s.Tooltip, maxMessage)
	if _, ok := e.m.Command(s.Command); !ok {
		s.Command = ""
	}
	if s.Severity != "info" && s.Severity != "warning" && s.Severity != "error" {
		s.Severity = ""
	}
	e.mu.Lock()
	e.status[s.ID] = s
	e.mu.Unlock()
	e.h.o.App.Emit(EventStatus, StatusEvent{Extension: e.id, Item: s})
	return nil
}

func (e *ext) viewRefresh(params json.RawMessage) error {
	var p RefreshEvent
	if err := decode(params, &p); err != nil {
		return err
	}
	if !e.m.HasPanel(p.View) {
		return invalid("no panel %q in the manifest", p.View)
	}
	e.h.o.App.Emit(EventRefresh, RefreshEvent{Extension: e.id, View: p.View, Item: cleanText(p.Item, maxItemID)})
	return nil
}

func (e *ext) showMessage(params json.RawMessage) error {
	var p struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := decode(params, &p); err != nil {
		return err
	}
	if !e.allowMessage(time.Now()) {
		return fmt.Errorf("dropped (more than %d messages in %s): %s", messageBurst, messageWindow, cleanText(p.Message, 200))
	}
	e.h.o.App.Emit(EventMessage, MessageEvent{Extension: e.id, Name: e.m.Name, Type: messageType(p.Type), Message: cleanText(p.Message, maxMessage)})
	return nil
}

func messageType(t string) string {
	if t == "warning" || t == "error" {
		return t
	}
	return "info"
}

// allowMessage rate-limits toasts per extension.
func (e *ext) allowMessage(now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	recent := e.messages[:0]
	for _, t := range e.messages {
		if now.Sub(t) < messageWindow {
			recent = append(recent, t)
		}
	}
	e.messages = recent
	if len(recent) >= messageBurst {
		return false
	}
	e.messages = append(e.messages, now)
	return true
}

func (e *ext) showMessageRequest(params json.RawMessage) (any, error) {
	var p struct {
		Type    string   `json:"type"`
		Message string   `json:"message"`
		Actions []string `json:"actions"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	if len(p.Actions) == 0 || len(p.Actions) > 3 {
		return nil, invalid("give 1 to 3 actions")
	}
	actions := make([]string, len(p.Actions))
	for i, a := range p.Actions {
		if actions[i] = cleanText(a, 30); actions[i] == "" {
			return nil, invalid("an action needs a title")
		}
	}
	choice := e.h.o.App.Choose(e.m.Name, cleanText(p.Message, maxMessage), actions)
	if choice == "" {
		return map[string]any{}, nil
	}
	return map[string]string{"action": choice}, nil
}

func (e *ext) openFile(params json.RawMessage) error {
	if err := e.need(manifest.PermWorkspaceRead); err != nil {
		return err
	}
	var p OpenFileEvent
	if err := decode(params, &p); err != nil {
		return err
	}
	path, err := e.workspaceFile(p.Path)
	if err != nil {
		return err
	}
	e.h.o.App.Emit(EventOpenFile, OpenFileEvent{Path: path, Line: max(p.Line, 0)})
	return nil
}

// openURL opens an http(s) link after the user allowed links to its host
// from this extension (once per session).
func (e *ext) openURL(params json.RawMessage) error {
	var p struct {
		URL string `json:"url"`
	}
	if err := decode(params, &p); err != nil {
		return err
	}
	u, err := url.Parse(p.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(p.URL) > 4096 {
		return invalid("only http and https links can be opened")
	}
	origin := u.Scheme + "://" + u.Host
	e.mu.Lock()
	allowed := e.origins[origin]
	e.mu.Unlock()
	if !allowed {
		msg := fmt.Sprintf("%s wants to open %s in your browser.\n\nLinks to %s from this extension open without asking again until Flint quits.", e.m.Name, cleanText(p.URL, 300), origin)
		if !e.h.o.App.Confirm("Open link?", msg, "Open") {
			return rpc.Errorf(rpc.CodeCancelled, "the user did not open the link")
		}
		e.mu.Lock()
		e.origins[origin] = true
		e.mu.Unlock()
	}
	return e.h.o.App.OpenURL(u.String())
}

func (e *ext) askAgent(params json.RawMessage) error {
	if err := e.need(manifest.PermAgentAsk); err != nil {
		return err
	}
	var p struct {
		Prompt      string `json:"prompt"`
		Attachments []struct {
			Path string `json:"path"`
		} `json:"attachments"`
	}
	if err := decode(params, &p); err != nil {
		return err
	}
	if strings.TrimSpace(p.Prompt) == "" || len(p.Prompt) > maxPrompt {
		return invalid("the prompt must be 1 to %d characters", maxPrompt)
	}
	if len(p.Attachments) > maxAttachments {
		return invalid("at most %d attachments", maxAttachments)
	}
	ev := AskEvent{Extension: e.id, Prompt: strings.ToValidUTF8(p.Prompt, "�")}
	for _, a := range p.Attachments {
		path, err := e.workspaceFile(a.Path)
		if err != nil {
			return err
		}
		ev.Attachments = append(ev.Attachments, path)
	}
	e.h.o.App.Emit(EventAsk, ev)
	return nil
}
