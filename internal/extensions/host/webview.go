package host

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"flint/internal/extensions/rpc"
)

// Webviews (docs/design/extensions.md, "Webviews"): Flint loads a view's HTML
// for the frontend's sandboxed frame and relays its bridge calls to the
// process (webview/request); the process talks back with webview/post and
// opens tabs with window/openTab.

// Events for webviews.
const (
	// EventWebview carries a WebviewEvent: a message for a webview.
	EventWebview = "extensions:webview"
	// EventOpenTab carries an OpenTabEvent.
	EventOpenTab = "extensions:openTab"
)

// Limits on webview traffic (the transport caps a message at 4 MiB too).
const (
	maxWebviewMessage = 4 << 20
	maxTabKey         = 256
	maxTabData        = 64 << 10
	maxBridgeMethod   = 128
)

// WebviewEvent is EventWebview's data. Key "" addresses every tab of the view
// (and the panel).
type WebviewEvent struct {
	Extension string          `json:"extension"`
	View      string          `json:"view"`
	Key       string          `json:"key,omitempty"`
	Message   json.RawMessage `json:"message"`
}

// OpenTabEvent is EventOpenTab's data: open or focus the tab of view and key.
type OpenTabEvent struct {
	Extension string          `json:"extension"`
	View      string          `json:"view"`
	Key       string          `json:"key"`
	Title     string          `json:"title"`
	Data      json.RawMessage `json:"data,omitempty"`
}

var bridgeMethod = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_./:-]*$`)

// WebviewHTML returns a webview's HTML file (the frontend adds the policy and
// the bridge, and shows it in a sandboxed frame).
func (h *Host) WebviewHTML(id, view string) (string, error) {
	e, err := h.get(id)
	if err != nil {
		return "", err
	}
	if err := e.usable(); err != nil {
		return "", err
	}
	return e.m.ReadWebview(e.dir, view)
}

// WebviewRequest relays a webview's flint.request to the process, starting it
// first, and returns its answer.
func (h *Host) WebviewRequest(ctx context.Context, id, view, key, method string, params json.RawMessage) (json.RawMessage, error) {
	e, err := h.get(id)
	if err != nil {
		return nil, err
	}
	if e.m == nil {
		return nil, e.usable()
	}
	if _, ok := e.m.WebviewOf(view); !ok {
		return nil, fmt.Errorf("%s has no webview %q", e.name(), view)
	}
	if len(method) > maxBridgeMethod || !bridgeMethod.MatchString(method) {
		return nil, fmt.Errorf("invalid webview request method %q", cleanText(method, 40))
	}
	if len(params) > maxWebviewMessage || len(key) > maxTabKey {
		return nil, fmt.Errorf("webview request is too large")
	}
	if len(params) == 0 {
		params = json.RawMessage("null")
	}
	p := map[string]any{"view": view, "method": method, "params": params}
	if key != "" {
		p["key"] = key
	}
	var out json.RawMessage
	if err := e.call(ctx, h.o.CallTimeout, "webview/request", p, &out); err != nil {
		return nil, err
	}
	if len(out) > maxWebviewMessage {
		return nil, fmt.Errorf("%s answered with more than %d MiB", e.name(), maxWebviewMessage>>20)
	}
	if len(out) == 0 {
		out = json.RawMessage("null")
	}
	return out, nil
}

// webviewPost forwards a process's message to its webview.
func (e *ext) webviewPost(params json.RawMessage) error {
	var p WebviewEvent
	if err := decode(params, &p); err != nil {
		return err
	}
	if _, ok := e.m.WebviewOf(p.View); !ok {
		return invalid("no webview %q in the manifest", p.View)
	}
	if len(p.Message) > maxWebviewMessage || len(p.Key) > maxTabKey {
		return invalid("webview message is too large")
	}
	if len(p.Message) == 0 {
		p.Message = json.RawMessage("null")
	}
	e.h.o.App.Emit(EventWebview, WebviewEvent{Extension: e.id, View: p.View, Key: cleanText(p.Key, maxTabKey), Message: p.Message})
	return nil
}

// openTab opens one of the extension's tab views (window/openTab).
func (e *ext) openTab(params json.RawMessage) error {
	var p OpenTabEvent
	if err := decode(params, &p); err != nil {
		return err
	}
	if _, ok := e.m.TabView(p.View); !ok {
		return rpc.Errorf(rpc.CodeInvalidParams, "no tab view %q in the manifest", p.View)
	}
	key := cleanText(p.Key, maxTabKey)
	if key == "" {
		return invalid("a tab needs a key")
	}
	if len(p.Data) > maxTabData {
		return invalid("tab data is larger than %d KiB", maxTabData>>10)
	}
	title := cleanText(p.Title, 100)
	if title == "" {
		tv, _ := e.m.TabView(p.View)
		title = tv.Title
	}
	e.h.o.App.Emit(EventOpenTab, OpenTabEvent{Extension: e.id, View: p.View, Key: key, Title: title, Data: p.Data})
	return nil
}
