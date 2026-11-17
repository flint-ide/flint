package remote

import (
	"errors"
	"fmt"
	"strings"

	"flint/internal/settings"
)

// A remote terminal's id is "<machineId>:<sessionId>". Machine ids cannot
// contain ':' (settings.ValidMachineID) and every local terminal id is a UUID,
// a hex string or an "agent-"/task id, none of which has one, so the first ':'
// decides: no ':' is local, a ':' is remote and must name a configured machine.
// An id with a ':' is never handed to a local manager.

// idSeparator joins a machine id and a session id.
const idSeparator = ":"

// ErrNotRemoteID: the id has no machine prefix.
var ErrNotRemoteID = errors.New("not a remote terminal id")

// IsRemoteID reports whether id addresses a remote terminal (it has a
// machine prefix, valid or not). Whether the machine exists is the Hub's call.
func IsRemoteID(id string) bool { return strings.Contains(id, idSeparator) }

// JoinID makes the id of session on machine.
func JoinID(machine, session string) string { return machine + idSeparator + session }

// SplitID splits a remote id into its machine and the session id the remote
// host knows. It is a syntax check only: the machine must look like a machine
// id and the session must be a single non-empty piece without ':'.
func SplitID(id string) (machine, session string, err error) {
	machine, session, ok := strings.Cut(id, idSeparator)
	switch {
	case !ok:
		return "", "", ErrNotRemoteID
	case !settings.ValidMachineID(machine):
		return "", "", fmt.Errorf("terminal id has no valid machine prefix %q", clipID(machine))
	case session == "" || strings.Contains(session, idSeparator):
		return "", "", fmt.Errorf("terminal id %q has no valid session part", clipID(id))
	}
	return machine, session, nil
}

func clipID(s string) string {
	if len(s) > 40 {
		return strings.ToValidUTF8(s[:40], "") + "…"
	}
	return s
}
