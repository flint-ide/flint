package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Requests Flint sends to an extension, each starting it first when needed.

// call sends a request with a timeout, starting the process first.
func (e *ext) call(ctx context.Context, timeout time.Duration, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	p, err := e.session(ctx)
	if err != nil {
		return err
	}
	e.begin()
	defer e.end()
	err = p.conn.Call(ctx, method, params, result)
	if errors.Is(err, context.DeadlineExceeded) {
		e.log.add("flint", fmt.Sprintf("%s: no answer within %s", method, timeout))
		return fmt.Errorf("%s is not responding (%s took longer than %s)", e.name(), method, timeout)
	}
	return err
}

// Execute runs one of the extension's commands; args is JSON (null: none).
func (h *Host) Execute(ctx context.Context, id, command string, args json.RawMessage) error {
	e, err := h.get(id)
	if err != nil {
		return err
	}
	if e.m == nil {
		return e.usable()
	}
	if _, ok := e.m.Command(command); !ok {
		return fmt.Errorf("%s has no command %q", e.name(), command)
	}
	if len(args) > maxArgs {
		return errors.New("command arguments are too large")
	}
	params := map[string]any{"id": command}
	if len(args) > 0 && string(args) != "null" {
		params["args"] = args
	}
	return e.call(ctx, h.o.CallTimeout, "command/execute", params, nil)
}

// Resolve asks for a panel's content; parent expands a tree node.
func (h *Host) Resolve(ctx context.Context, id, view, parent string) (View, error) {
	e, err := h.get(id)
	if err != nil {
		return View{}, err
	}
	if e.m == nil {
		return View{}, e.usable()
	}
	i := -1
	for j, p := range e.m.Contributes.Panels {
		if p.ID == view {
			i = j
		}
	}
	if i < 0 {
		return View{}, fmt.Errorf("%s has no panel %q", e.name(), view)
	}
	params := map[string]any{"view": view}
	if parent != "" {
		params["parent"] = parent
	}
	var v View
	if err := e.call(ctx, h.o.ResolveTimeout, "view/resolve", params, &v); err != nil {
		return View{}, err
	}
	return cleanView(v, e.m.Contributes.Panels[i], e.m), nil
}

// Action sends the choice of an item's action (item "" for a message's).
func (h *Host) Action(ctx context.Context, id, view, item, action string) error {
	e, err := h.get(id)
	if err != nil {
		return err
	}
	if e.m == nil {
		return e.usable()
	}
	if !e.m.HasPanel(view) {
		return fmt.Errorf("%s has no panel %q", e.name(), view)
	}
	return e.call(ctx, h.o.CallTimeout, "view/action", map[string]string{"view": view, "item": item, "action": action}, nil)
}

// SetVisible records that a panel was shown or hidden: while one is on
// screen the extension is not stopped for being idle, and one that crashed
// is started again after its backoff.
func (h *Host) SetVisible(id, view string, visible bool) error {
	e, err := h.get(id)
	if err != nil {
		return err
	}
	if e.m == nil || !e.m.HasPanel(view) {
		return fmt.Errorf("no panel %q", view)
	}
	e.setVisible(view, visible)
	return nil
}
