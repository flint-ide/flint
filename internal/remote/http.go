package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"
)

// Routes. Everything else is 404 (after authentication, so an unpaired
// client learns nothing about the API).
const (
	pathPair   = "/api/pair"
	pathChats  = "/api/chats"
	pathTasks  = "/api/tasks"
	pathStream = "/api/stream"
)

// statusRecorder remembers the status for the audit line.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(p)
}

// Unwrap lets http.ResponseController hijack the connection (the stream).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// operation names a request in the audit log; never the query or body.
func operation(r *http.Request) string {
	switch r.URL.Path {
	case pathPair:
		return "pair"
	case pathChats:
		return "chats.list"
	case pathTasks:
		return "tasks.list"
	case pathStream:
		return "stream"
	}
	return "unknown"
}

// ServeHTTP checks every request (browser, Host, token, rate, scope) before
// routing it, and writes one audit line when it ends (refused requests that
// prove no device are sampled, so a flood can't fill the log).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := &statusRecorder{ResponseWriter: w}
	start, op, who := time.Now(), operation(r), "-"
	defer func() { s.audit(who, op, rec.status, time.Since(start)) }()
	h := rec.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	if fromBrowser(r) || !s.allowedHost(r) {
		// A web page can reach a loopback port (an <img>, a form, fetch,
		// DNS rebinding with its own Host). Browsers mark their requests;
		// Flint's clients call the API from Go and send no such headers.
		http.Error(rec, "forbidden", http.StatusForbidden)
		return
	}
	if r.URL.Path == pathPair {
		s.handlePair(rec, r, &who)
		return
	}
	token := bearer(r.Header.Get("Authorization"))
	if token == "" {
		// Costs nothing to answer, so it is charged to no budget.
		unauthorized(rec)
		return
	}
	// Charge the device the token names before any work for it (hash
	// compare, a Keychain read); tokens of unknown devices share one budget.
	id, known := s.opts.Devices.TokenDevice(token)
	if !known {
		id = anonKey
	}
	if !s.lim.allow(id) {
		tooMany(rec)
		return
	}
	dev, ok := s.opts.Devices.Authenticate(token)
	if !ok {
		if known && !s.lim.allow(anonKey) { // wrong secrets for a real device count as bad tokens too
			tooMany(rec)
			return
		}
		unauthorized(rec)
		return
	}
	who = deviceLabel(dev)
	if op == "unknown" {
		http.NotFound(rec, r)
		return
	}
	if !dev.has(ScopeRead) {
		http.Error(rec, "this device may not read", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		h.Set("Allow", http.MethodGet)
		http.Error(rec, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch r.URL.Path {
	case pathChats:
		chats := s.opts.Chats()
		out := make([]Chat, len(chats))
		for i, c := range chats {
			out[i] = redactChat(c)
		}
		writeJSON(rec, map[string]any{"chats": out})
	case pathTasks:
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout-5*time.Second)
		defer cancel()
		tasks, err := s.opts.Tasks(ctx)
		if err != nil {
			s.opts.Logf("remote: tasks: %v", err)
			http.Error(rec, "tasks unavailable", http.StatusInternalServerError)
			return
		}
		out := make([]Task, len(tasks))
		for i, t := range tasks {
			out[i] = redactTask(t)
		}
		writeJSON(rec, map[string]any{"tasks": out})
	case pathStream:
		s.handleStream(rec, r, dev)
	}
}

// deviceLabel names a device in the audit log: its id and quoted name, so a
// name can't forge a log line.
func deviceLabel(d Device) string { return d.ID + "(" + quoteName(d.Name) + ")" }

func quoteName(name string) string {
	b, _ := json.Marshal(name)
	return string(b)
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="flint"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// fromBrowser reports a request a web browser made: browsers send Origin on
// cross-origin and non-GET requests and Sec-Fetch-* on every request to a
// secure context (loopback is one). The API is never meant for pages.
func fromBrowser(r *http.Request) bool {
	if _, ok := r.Header["Origin"]; ok {
		return true
	}
	for k := range r.Header {
		if strings.HasPrefix(k, "Sec-Fetch-") {
			return true
		}
	}
	return false
}

// audit writes the audit line of a request. Lines of refused requests from
// no device are limited to auditRefusedPerMinute; the next one written says
// how many were left out.
func (s *Server) audit(who, op string, status int, d time.Duration) {
	if who == "-" && status >= 400 {
		if !s.auditLim.allow("refused") {
			s.auditSkipped.Add(1)
			return
		}
		if n := s.auditSkipped.Swap(0); n > 0 {
			s.opts.Logf("remote: %d refused requests not logged", n)
		}
	}
	s.opts.Logf("remote: device=%s op=%s status=%d duration=%s", who, op, status, d.Round(time.Millisecond))
}

func tooMany(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "10")
	http.Error(w, "too many requests", http.StatusTooManyRequests)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// pairRequest is the body of POST /api/pair.
type pairRequest struct {
	Secret string `json:"secret"`
	Name   string `json:"name"`
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request, who *string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
		return
	}
	// Only real pairing attempts are charged, to a budget of their own.
	if !s.lim.allow(pairKey) {
		tooMany(w)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "read request", http.StatusBadRequest)
		}
		return
	}
	var req pairRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	dev, token, err := s.opts.Devices.Pair(req.Secret, req.Name)
	switch {
	case errors.Is(err, ErrPairing):
		http.Error(w, "pairing secret wrong, used or expired", http.StatusUnauthorized)
		return
	case err != nil:
		var nameErr error = CheckDeviceName(req.Name)
		if nameErr != nil {
			http.Error(w, nameErr.Error(), http.StatusBadRequest)
		} else {
			s.opts.Logf("remote: pair: %v", err)
			http.Error(w, "pairing failed", http.StatusInternalServerError)
		}
		return
	}
	*who = deviceLabel(dev)
	writeJSON(w, map[string]any{"token": token, "device": dev})
}

// allowedHost accepts loopback names and the allowed hosts in Host, on any
// port (an ssh -L tunnel may use another local port): DNS rebinding sends
// the attacker's own name.
func (s *Server) allowedHost(r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hosts[hostOnly(r.Host)]
}

// hostOnly lowercases a host[:port] and drops the port and IPv6 brackets.
func hostOnly(hostport string) string {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	return normalizeHost(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"))
}

func normalizeHost(h string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
}

// bearer returns the token of an "Authorization: Bearer <token>" header.
func bearer(h string) string {
	scheme, token, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
