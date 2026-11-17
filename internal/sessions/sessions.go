// Package sessions declares what the app needs from terminal and agent-chat
// sessions, so the in-process managers can later be replaced by a client of a
// background host process without touching the code that uses them.
//
// Only the methods the app calls while sessions run are here. Setup that is
// specific to one implementation (registering drivers, installing the action
// gate, starting stream sessions) stays on the concrete managers.
package sessions

import (
	"context"

	as "flint/internal/agentsession"
	"flint/internal/terminal"
)

// Terminals runs shell sessions by id.
type Terminals interface {
	Start(id string, opts terminal.Options) error
	Write(id string, data []byte) error
	Resize(id string, cols, rows int) error
	Cwd(id string) (string, error)
	Resume(id string) (terminal.Resume, error)
	Close(id string)
	CloseAll()
}

// Agents runs agent chat sessions by id.
type Agents interface {
	Agents() []string
	Start(ctx context.Context, agent, cwd string) (as.SessionInfo, error)
	Resume(ctx context.Context, agent, cwd, sessionID string) (as.SessionInfo, error)
	Prompt(sessionID, text string, attachments []as.Attachment) error
	Busy(sessionID string) bool
	Cancel(sessionID string) error
	Respond(sessionID, requestID, optionID string) error
	Stop(sessionID string) error
	Policy() as.Policy
	SetPolicy(p as.Policy)
	Close() error
}
