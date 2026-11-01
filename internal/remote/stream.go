package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	as "flint/internal/agentsession"
)

// Stream messages (JSON text frames, server to client):
//
//	{"type":"hello","chats":[…]}             the open chats, first
//	{"type":"gap","gap":{sessionId,from,to}} events no longer kept
//	{"type":"agent","event":{…}}              an agent event (replayed, then live)
//	{"type":"replayed"}                       replay done; what follows is live
//	{"type":"chat","chat":{…}}                a chat opened or changed state
//	{"type":"chatClosed","id":"…"}            a chat closed
//	{"type":"tasks"}                          the task list changed: GET /api/tasks
//
// The client remembers the last Seq per chat and reconnects with
// ?since=<chatId>:<seq>,… to continue without duplicates.
type StreamMessage struct {
	Type  string    `json:"type"`
	Chats []Chat    `json:"chats,omitempty"`
	Chat  *Chat     `json:"chat,omitempty"`
	ID    string    `json:"id,omitempty"`
	Gap   *as.Gap   `json:"gap,omitempty"`
	Event *as.Event `json:"event,omitempty"`
}

// stream is one open WebSocket of a device.
type stream struct {
	device string
	host   string // the Host it was opened with (hostOnly)
	cancel context.CancelCauseFunc
}

var (
	errRevoked     = errors.New("device revoked")
	errStopping    = errors.New("server stopping")
	errHostRemoved = errors.New("host no longer allowed")
)

func (st *stream) end(cause error) { st.cancel(cause) }

// closeFor sends the close frame that tells the client why its stream ended.
func closeFor(ws *wsConn, cause error) {
	switch cause {
	case errRevoked:
		ws.sendClose(closePolicy, "device revoked")
	case errHostRemoved:
		ws.sendClose(closePolicy, "host no longer allowed")
	default:
		ws.sendClose(closeGoingAway, "Flint Remote stopped")
	}
}

// parseSince reads since=<chatId>:<seq> values (repeated or comma-separated).
func parseSince(values []string) (map[string]uint64, error) {
	out := map[string]uint64{}
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part == "" {
				continue
			}
			i := strings.LastIndexByte(part, ':')
			if i <= 0 || i > 256 || strings.ContainsFunc(part[:i], unicode.IsControl) {
				return nil, fmt.Errorf("invalid since %q", part)
			}
			seq, err := strconv.ParseUint(part[i+1:], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid since %q", part)
			}
			out[part[:i]] = seq
			if len(out) > maxSince {
				return nil, fmt.Errorf("since names more than %d chats", maxSince)
			}
		}
	}
	return out, nil
}

// register adds a stream unless the server is stopping, the device was
// revoked or its host removed after the request was authenticated, or the
// device (or the server) has too many. Revoke removes the device before it
// cuts streams under s.mu, so a stream registered here is either seen by
// that cut or refused by the check below.
func (s *Server) register(device, host string) (*stream, context.Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv == nil || s.ctx.Err() != nil {
		return nil, nil, errStopping
	}
	if !s.opts.Devices.Has(device) {
		return nil, nil, errRevoked
	}
	if !s.hosts[host] {
		return nil, nil, errHostRemoved
	}
	n := 0
	for st := range s.streams {
		if st.device == device {
			n++
		}
	}
	if n >= maxStreamsDevice || len(s.streams) >= maxStreams {
		return nil, nil, errors.New("too many open streams")
	}
	ctx, cancel := context.WithCancelCause(s.ctx)
	st := &stream{device: device, host: host, cancel: cancel}
	s.streams[st] = struct{}{}
	s.wg.Add(1)
	return st, ctx, nil
}

func (s *Server) unregister(st *stream) {
	s.mu.Lock()
	delete(s.streams, st)
	s.mu.Unlock()
	st.cancel(nil)
	s.wg.Done()
}

func (s *Server) handleStream(w *statusRecorder, r *http.Request, dev Device) {
	since, err := parseSince(r.URL.Query()["since"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, status := checkHandshake(r); status != 0 {
		_, _ = upgrade(w, r) // writes the refusal
		return
	}
	st, ctx, err := s.register(dev.ID, hostOnly(r.Host))
	switch {
	case errors.Is(err, errStopping):
		http.Error(w, "stopping", http.StatusServiceUnavailable)
		return
	case errors.Is(err, errRevoked):
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	case errors.Is(err, errHostRemoved):
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	case err != nil:
		tooMany(w)
		return
	}
	defer s.unregister(st)
	// Subscribe before reading the chats and the logs, so nothing emitted
	// in between is missed (duplicates are dropped by Seq).
	sub := s.hub.subscribe()
	defer s.hub.unsubscribe(sub)
	ws, err := upgrade(w, r)
	if err != nil {
		return
	}
	w.status = http.StatusSwitchingProtocols
	s.opts.Logf("remote: device=%s op=stream.open", deviceLabel(dev))
	s.runStream(ctx, ws, sub, since)
}

// streamState tracks what one stream has sent.
type streamState struct {
	ctx   context.Context
	ws    *wsConn
	sent  map[string]uint64 // chat → last Seq sent
	calls *secretCalls      // which tool calls touch credentials files
}

func (st *streamState) send(m StreamMessage) error {
	if err := st.ctx.Err(); err != nil {
		return err // revoked or stopped: send nothing more
	}
	p, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(p) > maxOutMessage && m.Event != nil {
		small := shrinkEvent(*m.Event)
		m.Event = &small
		if p, err = json.Marshal(m); err != nil {
			return err
		}
		if len(p) > maxOutMessage {
			m.Event = &as.Event{SessionID: small.SessionID, Seq: small.Seq, Kind: small.Kind, Text: "[event too large: see the laptop]"}
			p, _ = json.Marshal(m)
		}
	}
	return st.ws.writeText(p)
}

func (st *streamState) agent(e as.Event) error {
	// Observed even when not sent: a call the client already has may get
	// its output (from a credentials file) in a later update.
	secret := st.calls.observe(e)
	if e.Seq <= st.sent[e.SessionID] {
		return nil // already replayed, or the client had it (since)
	}
	st.sent[e.SessionID] = e.Seq
	re := redactEvent(e, secret)
	return st.send(StreamMessage{Type: "agent", Event: &re})
}

func (s *Server) runStream(ctx context.Context, ws *wsConn, sub *subscriber, since map[string]uint64) {
	readDone := make(chan error, 1)
	go func() { readDone <- ws.readLoop(func([]byte) {}) }() // phase 1: client messages are dropped
	readEnded := false
	defer func() {
		if !readEnded && !ws.broken() { // give the client a moment to answer our close frame
			select {
			case <-readDone:
				readEnded = true
			case <-time.After(closeWait):
			}
		}
		_ = ws.close()
		if !readEnded {
			<-readDone
		}
	}()

	// Once the stream is cut, a write blocked on a client that stopped
	// reading must not hold Stop or Revoke: cap it at closeWait.
	stopHurry := context.AfterFunc(ctx, ws.hurry)
	defer stopHurry()

	st := &streamState{ctx: ctx, ws: ws, sent: map[string]uint64{}, calls: newSecretCalls()}
	if err := s.replay(st, since); err != nil {
		if ctx.Err() != nil {
			closeFor(ws, context.Cause(ctx))
		} else {
			ws.sendClose(closeInternal, "")
		}
		return
	}
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		// A cut stream ends now: select picks at random among ready cases, so
		// without this a full queue could keep it writing after Stop or Revoke.
		if ctx.Err() != nil {
			closeFor(ws, context.Cause(ctx))
			return
		}
		select {
		case <-sub.lost: // before anything queued: the queue is incomplete
			ws.sendClose(closeTryAgainLater, "fell behind: reconnect with since")
			return
		default:
		}
		var err error
		select {
		case <-ctx.Done():
			closeFor(ws, context.Cause(ctx))
			return
		case <-readDone:
			readEnded = true
			return
		case <-sub.lost:
			ws.sendClose(closeTryAgainLater, "fell behind: reconnect with since")
			return
		case m := <-sub.ch:
			err = s.deliver(st, m)
		case <-ping.C:
			err = ws.ping()
		}
		if err != nil {
			if ctx.Err() != nil {
				closeFor(ws, context.Cause(ctx))
			}
			return
		}
	}
}

// replay sends the chats, then each chat's kept events after since.
func (s *Server) replay(st *streamState, since map[string]uint64) error {
	chats := s.opts.Chats()
	out := make([]Chat, len(chats))
	for i, c := range chats {
		out[i] = redactChat(c)
	}
	if err := st.send(StreamMessage{Type: "hello", Chats: out}); err != nil {
		return err
	}
	for _, c := range chats {
		after := since[c.ID]
		st.sent[c.ID] = after
		// From the start of the log: the events the client already has are
		// observed (which calls touch credentials files), not sent.
		events, gap := s.opts.Replay(c.ID, 0)
		if gap != nil && gap.To > after {
			g := *gap
			g.From = max(g.From, after+1)
			if err := st.send(StreamMessage{Type: "gap", Gap: &g}); err != nil {
				return err
			}
		}
		for _, e := range events {
			if err := st.agent(e); err != nil {
				return err
			}
		}
	}
	return st.send(StreamMessage{Type: "replayed"})
}

func (s *Server) deliver(st *streamState, m hubMsg) error {
	switch m.kind {
	case msgAgent:
		return st.agent(m.event)
	case msgTasks:
		return st.send(StreamMessage{Type: "tasks"})
	case msgChat:
		for _, c := range s.opts.Chats() {
			if c.ID == m.id {
				rc := redactChat(c)
				return st.send(StreamMessage{Type: "chat", Chat: &rc})
			}
		}
		delete(st.sent, m.id)
		return st.send(StreamMessage{Type: "chatClosed", ID: m.id})
	}
	return nil
}
