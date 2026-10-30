package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"flint/internal/httpfile"
)

// HTTPAPI runs requests from .http / .rest files (like IntelliJ's HTTP Client).
// Requests are only ever sent by Send, i.e. an explicit user action. Nothing here
// logs headers, bodies or environment values.
type HTTPAPI struct {
	app *App
}

// SendOptions are the response tab's toggles.
type SendOptions struct {
	FollowRedirects bool `json:"followRedirects"`
	// TimeoutMs overrides the 30 s default (0 = default); "# @timeout" wins over it.
	TimeoutMs int `json:"timeoutMs"`
}

// Outline lists the requests and parse errors of a file's text (gutter ▶ and error marks).
func (h *HTTPAPI) Outline(text string) httpfile.Outline {
	return httpfile.OutlineOf(text)
}

// Environments lists the environments of http-client(.private).env.json next to path.
func (h *HTTPAPI) Environments(path string) ([]string, error) {
	return httpfile.Environments(path)
}

// Send sends the request at target.Line under id (for Cancel) and waits for the response.
func (h *HTTPAPI) Send(id string, target httpfile.Target, opts SendOptions) (*httpfile.Response, error) {
	p, err := httpfile.Prepare(target, time.Now)
	if err != nil {
		return nil, err
	}
	return h.app.http.Send(h.app.ctx, id, target.Path, p, httpfile.Options{
		FollowRedirects: opts.FollowRedirects,
		Timeout:         time.Duration(opts.TimeoutMs) * time.Millisecond,
	})
}

// Cancel aborts a running Send.
func (h *HTTPAPI) Cancel(id string) {
	h.app.http.Cancel(id)
}

// History lists the kept responses of a file (newest first, without bodies).
func (h *HTTPAPI) History(path string) []httpfile.Response {
	return h.app.http.History(path)
}

// Response returns a kept response with its body.
func (h *HTTPAPI) Response(id string) (*httpfile.Response, error) {
	r, ok := h.app.http.Response(id)
	if !ok {
		return nil, errResponseGone
	}
	return r, nil
}

// CurlOf returns the request behind a kept response as a curl command.
func (h *HTTPAPI) CurlOf(id string) (string, error) {
	curl, ok := h.app.http.CurlOf(id)
	if !ok {
		return "", errResponseGone
	}
	return curl, nil
}

// Curl renders the request at target.Line as a curl command without sending it.
func (h *HTTPAPI) Curl(target httpfile.Target, opts SendOptions) (string, error) {
	p, err := httpfile.Prepare(target, time.Now)
	if err != nil {
		return "", err
	}
	return httpfile.Curl(p, opts.FollowRedirects && !p.NoRedirect), nil
}

// ClearCookies forgets the cookies collected for a file.
func (h *HTTPAPI) ClearCookies(path string) {
	h.app.http.ClearCookies(path)
}

// SaveBody asks where to save a kept response's whole body and writes it there.
// Returns the chosen path ("" when the dialog was cancelled).
func (h *HTTPAPI) SaveBody(id, suggestedName string) (string, error) {
	body, ok := h.app.http.Body(id)
	if !ok {
		return "", errResponseGone
	}
	path, err := runtime.SaveFileDialog(h.app.ctx, runtime.SaveDialogOptions{
		Title:                "Save response body",
		DefaultFilename:      filepath.Base(suggestedName),
		CanCreateDirectories: true,
	})
	if err != nil || path == "" {
		return "", err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", fmt.Errorf("save response body: %w", err)
	}
	return path, nil
}

var errResponseGone = errors.New("this response is no longer kept in memory; send the request again")
