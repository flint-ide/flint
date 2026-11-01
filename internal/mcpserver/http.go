package mcpserver

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// ServeHTTP implements the Streamable HTTP transport's POST side. GET (a
// server-to-client event stream) and DELETE (ending a session) are not
// offered: the server is stateless and never sends requests of its own.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.localRequest(r) {
		// DNS rebinding: a web page that resolved its own name to 127.0.0.1
		// sends its own Host (and Origin).
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.URL.Path != endpointPath {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root, ok := s.rootFor(bearer(r.Header.Get("Authorization")))
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="flint"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.opts.Allow != nil && !s.opts.Allow(root) {
		http.Error(w, "Flint's MCP server is off for this project", http.StatusForbidden)
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		http.Error(w, "expected application/json", http.StatusUnsupportedMediaType)
		return
	}
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !slices.Contains(supportedVersions, v) {
		http.Error(w, "unsupported MCP-Protocol-Version "+strconv.Quote(v), http.StatusBadRequest)
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
	reply, status := s.handle(r.Context(), root, body)
	if reply == nil {
		w.WriteHeader(status) // a notification or a response: 202, no body
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(reply)
}

// localRequest accepts only the loopback names of this server's own port,
// and no Origin but its own (browsers send one; agent CLIs don't).
func (s *Server) localRequest(r *http.Request) bool {
	port := strconv.Itoa(s.listenPort())
	allowed := []string{"127.0.0.1:" + port, "localhost:" + port}
	if !slices.Contains(allowed, strings.ToLower(r.Host)) {
		return false
	}
	origin := r.Header.Get("Origin")
	return origin == "" || slices.Contains(allowed, strings.TrimPrefix(strings.ToLower(origin), "http://"))
}

// bearer returns the token of an "Authorization: Bearer <token>" header.
func bearer(h string) string {
	scheme, token, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}
