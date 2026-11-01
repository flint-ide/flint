package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"

	as "flint/internal/agentsession"
	"flint/internal/config"
	"flint/internal/qr"
	"flint/internal/remote"
	"flint/internal/settings"
)

// Flint Remote (internal/remote): a read-only API on 127.0.0.1 that a paired
// phone reaches through the user's own tunnel. Off by default; the server
// runs only while Settings ▸ Remote has it on. Paired devices are kept in
// state.json, a hash of each device's token in the Keychain.

const (
	remoteKeyService = "flint-remote"
	remoteBindHost   = "127.0.0.1"
	// eventRemoteChanged tells Settings that the devices or the server changed.
	eventRemoteChanged = "remote:changed"
	// remotePairScheme is the link the mobile app opens from the QR code.
	remotePairScheme = "flint-remote://pair"
)

// remoteKeychain keeps device token hashes in the Keychain (tests use a fake).
type remoteKeychain struct{}

func (remoteKeychain) Get(account string) (string, error) {
	v, err := keyring.Get(remoteKeyService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", nil
	}
	return v, err
}

func (remoteKeychain) Set(account, value string) error {
	return keyring.Set(remoteKeyService, account, value)
}

func (remoteKeychain) Delete(account string) error {
	err := keyring.Delete(remoteKeyService, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// remoteState is the server's last start error, for Settings.
type remoteState struct {
	mu  sync.Mutex
	err string
}

// newRemote builds the (stopped) server over the App's chats and tasks.
func newRemote(a *App, secrets remote.Secrets) (*remote.Server, *remote.Devices) {
	a.stateMu.Lock()
	known := slices.Clone(a.state.RemoteDevices)
	a.stateMu.Unlock()
	devices := remote.NewDevices(known, secrets, a.saveRemoteDevices)
	srv := remote.New(remote.Options{
		Devices: devices,
		Chats:   a.remoteChats,
		Tasks:   a.remoteTasks,
		Replay:  a.agents.Replay,
		Logf:    a.logf,
	})
	a.agents.SetStateHook(srv.ChatChanged)
	return srv, devices
}

func (a *App) saveRemoteDevices(list []remote.Device) error {
	a.stateMu.Lock()
	next := a.state
	next.RemoteDevices = list
	err := config.SaveJSON(a.statePath(), next)
	if err == nil {
		a.state = next
	}
	a.stateMu.Unlock()
	a.emit(eventRemoteChanged)
	return err
}

// remoteFanout hands the events a phone follows to Flint Remote (called by
// emit; never blocks).
func (a *App) remoteFanout(name string, data []any) {
	if a.remote == nil {
		return
	}
	switch name {
	case eventAgentEvent:
		if len(data) == 1 {
			if e, ok := data[0].(as.Event); ok {
				a.remote.AgentEvent(e)
			}
		}
	case eventTasksChanged:
		a.remote.TasksChanged()
	}
}

// remoteChats lists the open chats; chats in untrusted folders are read-only.
func (a *App) remoteChats() []remote.Chat {
	sessions := a.agents.Sessions()
	out := make([]remote.Chat, 0, len(sessions))
	for _, s := range sessions {
		state := remote.ChatState(s)
		out = append(out, remote.Chat{
			ID: s.ID, Agent: s.Agent, Project: s.Cwd, State: state, WaitingForApproval: state == remote.ChatWaiting,
			Pending: s.Pending, ReadOnly: !a.isTrusted(s.Cwd), LastSeq: s.LastSeq,
		})
	}
	slices.SortFunc(out, func(x, y remote.Chat) int { return strings.Compare(x.ID, y.ID) })
	return out
}

// remoteTasks lists the agent tasks of the open folders with their diff stat.
func (a *App) remoteTasks(ctx context.Context) ([]remote.Task, error) {
	tasks := &TaskAPI{app: a, git: &GitAPI{app: a}}
	views, err := tasks.ListAll(a.projectRoots())
	if err != nil {
		return nil, err
	}
	out := make([]remote.Task, 0, len(views))
	for _, v := range views {
		t := remote.Task{
			ID: v.ID, Title: v.Title, Agent: v.Agent, Branch: v.Branch, Base: v.Base, State: v.State, Error: v.Error,
			Stale: v.Stale, Ahead: v.Ahead, Merged: v.Merged, PullRequest: v.PullRequest, CreatedAt: v.CreatedAt,
		}
		if !v.Stale && ctx.Err() == nil {
			if ds, err := tasks.Changes(v.ID); err == nil {
				t.Changes = &remote.DiffSummary{Files: ds.Count, Added: ds.Added, Deleted: ds.Deleted, Truncated: ds.Truncated}
			}
		}
		out = append(out, t)
	}
	return out, ctx.Err()
}

// applyRemote starts, reconfigures or stops the server for r, and sizes the
// agents' event log (kept only while the server is on).
func (a *App) applyRemote(r settings.Remote) {
	if a.remote == nil {
		return
	}
	if !r.Enabled {
		a.remote.Stop()
		a.remoteDevices.CancelPairing()
		a.agents.SetLogLimits(as.LogLimits{})
		a.setRemoteError("")
		a.emit(eventRemoteChanged)
		return
	}
	a.agents.SetLogLimits(as.LogLimits{Events: r.ReplayEvents, Bytes: r.ReplayKB << 10})
	err := a.remote.Start(remote.Config{
		Host: remoteBindHost, Port: r.Port, AllowedHosts: r.AllowedHosts, RequestsPerMinute: r.RequestsPerMinute,
	})
	if err != nil {
		a.logf("%v", err)
		a.setRemoteError(err.Error())
	} else {
		a.setRemoteError("")
	}
	a.emit(eventRemoteChanged)
}

func (a *App) remoteSettingsChanged(prev, next settings.Remote) {
	if prev.Enabled || next.Enabled {
		a.applyRemote(next)
	}
}

func (a *App) setRemoteError(msg string) {
	a.remoteErr.mu.Lock()
	a.remoteErr.err = msg
	a.remoteErr.mu.Unlock()
}

// RemoteAPI is Settings ▸ Remote: the server's state, pairing and the
// paired devices.
type RemoteAPI struct {
	app *App
}

// RemoteStatus is what Settings shows.
type RemoteStatus struct {
	Enabled   bool            `json:"enabled"`
	Listening bool            `json:"listening"`
	Address   string          `json:"address"`
	Error     string          `json:"error"`
	Devices   []remote.Device `json:"devices"`
	Streams   int             `json:"streams"`
}

// Status reports the server and the paired devices.
func (r *RemoteAPI) Status() RemoteStatus {
	a := r.app
	a.remoteErr.mu.Lock()
	errText := a.remoteErr.err
	a.remoteErr.mu.Unlock()
	addr := a.remote.Addr()
	return RemoteStatus{
		Enabled: a.currentSettings().Remote.Enabled, Listening: addr != "", Address: addr, Error: errText,
		Devices: a.remoteDevices.List(), Streams: a.remote.Streams(),
	}
}

// PairingView is an open pairing: the secret (valid for one device, two
// minutes) and the link the mobile app reads from the QR code.
type PairingView struct {
	// Secret in groups of four, for typing.
	Secret string `json:"secret"`
	// URL is the address the phone uses.
	URL string `json:"url"`
	// Link is flint-remote://pair?url=…&secret=… (what the QR code holds).
	Link    string `json:"link"`
	Expires int64  `json:"expires"` // Unix ms
	// QR draws the link: an SVG path for a viewBox of -4 -4 QRSize+8 QRSize+8.
	QR     string `json:"qr"`
	QRSize int    `json:"qrSize"`
}

// Pair opens a pairing for one device. host picks the address in the link:
// "" for the loopback address (ssh -L), or one of the allowed hosts
// (tailscale serve, HTTPS).
func (r *RemoteAPI) Pair(host string) (PairingView, error) {
	a := r.app
	addr := a.remote.Addr()
	if addr == "" {
		return PairingView{}, errors.New("Flint Remote is off: switch it on first")
	}
	base := "http://" + addr
	if host != "" {
		if !slices.Contains(a.currentSettings().Remote.AllowedHosts, host) {
			return PairingView{}, fmt.Errorf("%q is not an allowed host", host)
		}
		base = "https://" + host
	}
	p, err := a.remoteDevices.BeginPairing()
	if err != nil {
		return PairingView{}, err
	}
	link := remotePairScheme + "?" + url.Values{"url": {base}, "secret": {p.Secret}}.Encode()
	code, err := qr.Encode(link)
	if err != nil {
		a.remoteDevices.CancelPairing()
		return PairingView{}, err
	}
	a.logf("remote: pairing opened (valid until %s)", p.Expires.Format("15:04:05"))
	return PairingView{
		Secret: groupedSecret(p.Secret), URL: base, Link: link, Expires: p.Expires.UnixMilli(),
		QR: code.Path(), QRSize: code.Size,
	}, nil
}

// groupedSecret shows a secret as ABCD-EFGH-…; the server accepts either form.
func groupedSecret(s string) string {
	var parts []string
	for len(s) > 4 {
		parts, s = append(parts, s[:4]), s[4:]
	}
	return strings.Join(append(parts, s), "-")
}

// CancelPairing closes the open pairing.
func (r *RemoteAPI) CancelPairing() { r.app.remoteDevices.CancelPairing() }

// Revoke forgets a paired device; its open streams end at once.
func (r *RemoteAPI) Revoke(id string) error {
	if len(id) != 16 || strings.Trim(id, "0123456789abcdef") != "" {
		return errors.New("invalid device id")
	}
	return r.app.remote.Revoke(id)
}

// RevokeAll forgets every paired device.
func (r *RemoteAPI) RevokeAll() error { return r.app.remote.RevokeAll() }
