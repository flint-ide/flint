package remote

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"flint/internal/agenthost"
)

// ErrNoSession: the remote host does not hold the terminal (it ended there, or
// the host was restarted since the window last saw it).
var ErrNoSession = errors.New("the session no longer exists on the machine")

// TerminalIDs are the terminals in use through this Conn, ids as the remote
// host knows them (without the machine prefix).
func (c *Conn) TerminalIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := keys(c.terms)
	slices.Sort(ids)
	return ids
}

// AttachTerminal shows a terminal the remote host kept running from an
// earlier run of Flint: the host replays what it buffered and live output
// follows through the Conn's handlers. It connects first when needed. It never
// starts a shell: a terminal the host does not hold is ErrNoSession, and from
// then on the connection recovers the attached terminal like any other.
func (c *Conn) AttachTerminal(ctx context.Context, id string) error {
	cl, err := c.acquire(ctx)
	if err != nil {
		return err
	}
	list, err := cl.ListTerminals(ctx)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(list, func(t agenthost.TermStatus) bool { return t.ID == id }) {
		return fmt.Errorf("%w (%s)", ErrNoSession, id)
	}
	c.mu.Lock()
	c.terms[id] = struct{}{}
	delete(c.termPos, id)
	delete(c.pendingClose, id)
	c.mu.Unlock()
	if err := cl.AttachTerminal(id, 0); err != nil {
		c.mu.Lock()
		delete(c.terms, id)
		c.mu.Unlock()
		c.wake()
		return err
	}
	return nil
}
