package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"flint/internal/extensions/manifest"
	"flint/internal/extensions/rpc"
)

// maxReadFile bounds workspace/readFile.
const maxReadFile = 4 << 20

// secretFiles are file names workspace/readFile never returns, whatever
// the permission: credentials an extension has no business reading.
var secretFiles = regexp.MustCompile(`^(\.env(\..*)?|\.envrc|\.netrc|\.npmrc|\.pypirc|\.pgpass|id_(rsa|dsa|ecdsa|ed25519)(\.pub)?|.*\.(pem|key|p12|pfx|keystore|jks))$`)

// workspaceFile resolves path (absolute, existing) and checks that it is
// inside a trusted workspace folder, symbolic links included.
func (e *ext) workspaceFile(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return "", invalid("give an absolute path")
	}
	real, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", invalid("%s: %v", path, errors.Unwrap(err))
	}
	for _, r := range e.roots() {
		root, err := filepath.EvalSymlinks(r.Path)
		if err == nil && (real == root || strings.HasPrefix(real, root+string(filepath.Separator))) {
			return real, nil
		}
	}
	return "", rpc.Errorf(rpc.CodePermission, "%s is not inside an open, trusted project folder", path)
}

func (e *ext) readFile(params json.RawMessage) (any, error) {
	if err := e.need(manifest.PermWorkspaceRead); err != nil {
		return nil, err
	}
	var p struct {
		Path string `json:"path"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	path, err := e.workspaceFile(p.Path)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{filepath.Clean(p.Path), path} {
		if err := readable(name); err != nil {
			return nil, err
		}
	}
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return nil, invalid("%v", err)
	case !info.Mode().IsRegular():
		return nil, invalid("%s is not a file", p.Path)
	case info.Size() > maxReadFile:
		return nil, invalid("%s is larger than %d MiB", p.Path, maxReadFile>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, invalid("%v", err)
	}
	if !utf8.Valid(data) {
		return nil, invalid("%s is not UTF-8 text", p.Path)
	}
	return map[string]string{"text": string(data)}, nil
}

// readable refuses .git internals and credential files.
func readable(path string) error {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".git" {
			return rpc.Errorf(rpc.CodePermission, "files inside .git are not readable by extensions")
		}
	}
	if secretFiles.MatchString(filepath.Base(path)) {
		return rpc.Errorf(rpc.CodePermission, "%s may hold credentials and is not readable by extensions", filepath.Base(path))
	}
	return nil
}

// errNotExist is what a Secrets implementation returns for a missing key.
var errNotExist = fs.ErrNotExist

// Secrets is the OS keyring: Get returns fs.ErrNotExist for a missing entry.
type Secrets interface {
	Get(service, account string) (string, error)
	Set(service, account, value string) error
	Delete(service, account string) error
}

// secretService is the keyring service of every extension's secrets; the
// account is "<id>:<key>", so one extension can't reach another's.
const secretService = "flint-extensions"

const maxSecret = 64 << 10

var secretKey = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func (e *ext) secret(op string, params json.RawMessage) (any, error) {
	if err := e.need(manifest.PermSecrets); err != nil {
		return nil, err
	}
	if e.h.o.Secrets == nil {
		return nil, rpc.Errorf(rpc.CodeInternal, "no keyring")
	}
	var p struct {
		Key   string  `json:"key"`
		Value *string `json:"value"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	if !secretKey.MatchString(p.Key) {
		return nil, invalid("a key is 1 to 128 letters, digits, dots, dashes or underscores")
	}
	account := e.id + ":" + p.Key
	switch op {
	case "get":
		v, err := e.h.o.Secrets.Get(secretService, account)
		if errors.Is(err, errNotExist) {
			return map[string]any{}, nil
		}
		if err != nil {
			return nil, fmt.Errorf("keyring: %w", err)
		}
		return map[string]string{"value": v}, nil
	case "set":
		if p.Value == nil || len(*p.Value) > maxSecret || *p.Value == "" {
			return nil, invalid("a value is 1 byte to %d KiB", maxSecret>>10)
		}
		if err := e.h.o.Secrets.Set(secretService, account, *p.Value); err != nil {
			return nil, fmt.Errorf("keyring: %w", err)
		}
		return map[string]any{}, nil
	default:
		if err := e.h.o.Secrets.Delete(secretService, account); err != nil && !errors.Is(err, errNotExist) {
			return nil, fmt.Errorf("keyring: %w", err)
		}
		return map[string]any{}, nil
	}
}
