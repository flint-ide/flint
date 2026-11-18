package projects

import (
	"net/url"
	"path"
	"strings"
)

// maxURL bounds a repository URL in flint-project.json.
const maxURL = 1000

// RepoKey is how a git remote URL identifies a repository on every machine:
// "host/path" without scheme, user, port, trailing ".git" and slashes, with
// the host in lower case ("github.com/acme/api"), or "file:" and the cleaned
// path for a repository on disk. The path keeps its case: only some servers
// ignore it. ok is false for something that is not a remote URL.
//
// The port is dropped because one repository is reached on different ports
// over ssh and https (ssh://git@host:7999/x/y and https://host/x/y).
func RepoKey(raw string) (key string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxURL || strings.ContainsFunc(raw, func(r rune) bool { return r < ' ' || r == 0x7f || r == ' ' }) {
		return "", false
	}
	var host, p string
	switch {
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil {
			return "", false
		}
		switch strings.ToLower(u.Scheme) {
		case "https", "http", "git", "ssh", "git+ssh", "ssh+git":
			host, p = u.Hostname(), u.Path
		case "file":
			if u.Host != "" && u.Host != "localhost" {
				return "", false
			}
			return fileKey(u.Path)
		default:
			return "", false
		}
		if host == "" {
			return "", false
		}
	case strings.HasPrefix(raw, "/"):
		return fileKey(raw)
	default:
		// scp-like: [user@]host:path. A colon after a slash is a relative
		// path, not a host.
		at, rest, found := strings.Cut(raw, ":")
		if !found || at == "" || strings.Contains(at, "/") {
			return "", false
		}
		host, p = at[strings.LastIndexByte(at, '@')+1:], rest
		if host == "" || strings.ContainsAny(host, "[]") {
			return "", false
		}
	}
	p = trimRepoPath(p)
	if p == "" || badSegment(p) {
		return "", false
	}
	return strings.ToLower(host) + "/" + p, true
}

// badSegment reports a "." or ".." in a path.
func badSegment(p string) bool {
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "." || seg == ".." {
			return true
		}
	}
	return false
}

func fileKey(p string) (string, bool) {
	if !strings.HasPrefix(p, "/") {
		return "", false
	}
	p = trimRepoPath(path.Clean(p))
	if p == "" {
		return "", false
	}
	return "file:/" + p, true
}

// trimRepoPath drops slashes around the path, then a ".git" suffix, then the
// slashes that may have preceded it ("x/y.git/" → "x/y"), and collapses
// repeated slashes.
func trimRepoPath(p string) string {
	p = strings.Trim(p, "/")
	p = strings.TrimSuffix(p, ".git")
	p = strings.Trim(p, "/")
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}
