package httpfile

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Environment files live next to the .http file, as in IntelliJ. The private one
// holds secrets and belongs in .gitignore; its values win over the shared file's.
const (
	EnvFile        = "http-client.env.json"
	PrivateEnvFile = "http-client.private.env.json"
	// sharedEnv holds variables every environment gets.
	sharedEnv = "$shared"
)

// maxEnvFile bounds what is read from an environment file.
const maxEnvFile = 4 << 20

// envDoc is an environment file: environment name → variable → value.
type envDoc map[string]map[string]string

// Environments lists the environments defined next to the .http file at path, sorted.
func Environments(path string) ([]string, error) {
	docs, err := loadEnvDocs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, doc := range docs {
		for name := range doc {
			if name != sharedEnv && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	return append([]string{}, names...), nil
}

// loadEnv returns the variables of env ("" = only "$shared") from the files in dir.
func loadEnv(dir, env string) (map[string]string, error) {
	docs, err := loadEnvDocs(dir)
	if err != nil {
		return nil, err
	}
	vars := map[string]string{}
	found := env == ""
	// Shared values first, the selected environment over them; the private file wins.
	for _, key := range []string{sharedEnv, env} {
		for _, doc := range docs {
			values, ok := doc[key]
			if key == env && ok {
				found = true
			}
			for k, v := range values {
				vars[k] = v
			}
		}
		if env == "" {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("environment %q is not defined in %s or %s", env, EnvFile, PrivateEnvFile)
	}
	return vars, nil
}

// loadEnvDocs reads the public then the private environment file; missing files are skipped.
func loadEnvDocs(dir string) ([]envDoc, error) {
	var docs []envDoc
	for _, name := range []string{EnvFile, PrivateEnvFile} {
		doc, err := readEnvFile(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func readEnvFile(path string) (envDoc, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	if info.Size() > maxEnvFile {
		return nil, fmt.Errorf("%s is larger than %d MB", name, maxEnvFile>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseEnv(name, data)
}

// parseEnv decodes {"env": {"var": value}}. Numbers and booleans become their JSON
// text, nested values compact JSON. Errors name the file and line, never a value.
func parseEnv(name string, data []byte) (envDoc, error) {
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			return nil, fmt.Errorf("%s:%d: invalid JSON: %v", name, lineAt(data, syntax.Offset), syntax)
		}
		return nil, fmt.Errorf(`%s: expected {"environment": {"variable": "value"}}`, name)
	}
	doc := envDoc{}
	for env, values := range raw {
		vars := map[string]string{}
		for k, v := range values {
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				vars[k] = s
				continue
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, v); err != nil {
				return nil, fmt.Errorf("%s: environment %q, variable %q: invalid value", name, env, k)
			}
			if compact.String() != "null" {
				vars[k] = compact.String()
			} else {
				vars[k] = ""
			}
		}
		doc[env] = vars
	}
	return doc, nil
}

func lineAt(data []byte, offset int64) int {
	offset = min(max(offset, 0), int64(len(data)))
	return bytes.Count(data[:offset], []byte("\n")) + 1
}

// maxDepth bounds variables that refer to variables.
const maxDepth = 16

// resolver substitutes {{name}} for one request: in-file variables defined above
// it (the latest definition wins), then the environment, then dynamic ones ($uuid,
// $timestamp, $randomInt).
type resolver struct {
	file map[string]Var
	env  map[string]string
	now  func() time.Time
	// envName is the selected environment, for error hints.
	envName string
}

func newResolver(vars []Var, before int, env map[string]string, envName string, now func() time.Time) *resolver {
	file := map[string]Var{}
	for _, v := range vars {
		if v.Line < before {
			file[v.Name] = v
		}
	}
	return &resolver{file: file, env: env, now: now, envName: envName}
}

// expand substitutes every {{…}} in s; line is where s starts (for errors).
func (r *resolver) expand(s string, line int) (string, error) {
	return r.expandDepth(s, line, nil)
}

func (r *resolver) expandDepth(s string, line int, stack []string) (string, error) {
	if !strings.Contains(s, "{{") {
		return s, nil
	}
	var out strings.Builder
	rest := s
	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			out.WriteString(rest)
			return out.String(), nil
		}
		at := line + strings.Count(s[:len(s)-len(rest)+open], "\n")
		closing := strings.Index(rest[open:], "}}")
		if closing < 0 {
			return "", Error{Line: at, Message: "unclosed {{"}
		}
		out.WriteString(rest[:open])
		name := strings.TrimSpace(rest[open+2 : open+closing])
		value, err := r.lookup(name, at, stack)
		if err != nil {
			return "", err
		}
		out.WriteString(value)
		rest = rest[open+closing+2:]
	}
}

func (r *resolver) lookup(name string, line int, stack []string) (string, error) {
	if strings.HasPrefix(name, "$") {
		return r.dynamic(name, line)
	}
	if slices.Contains(stack, name) {
		return "", Error{Line: line, Message: fmt.Sprintf("variable %q refers to itself (%s)", name, strings.Join(append(slices.Clip(stack), name), " → "))}
	}
	if len(stack) >= maxDepth {
		return "", Error{Line: line, Message: fmt.Sprintf("variables nested deeper than %d levels", maxDepth)}
	}
	if v, ok := r.file[name]; ok {
		return r.expandDepth(v.Value, v.Line, append(slices.Clip(stack), name))
	}
	if v, ok := r.env[name]; ok {
		// Environment values may use other variables too; errors point at the use.
		value, err := r.expandDepth(v, line, append(slices.Clip(stack), name))
		var e Error
		if errors.As(err, &e) {
			e.Line = line
			return "", e
		}
		return value, err
	}
	hint := " (select an HTTP environment that defines it)"
	if r.envName != "" {
		hint = fmt.Sprintf(" in environment %q", r.envName)
	}
	return "", Error{Line: line, Message: fmt.Sprintf("undefined variable %q%s", name, hint)}
}

func (r *resolver) dynamic(name string, line int) (string, error) {
	switch name {
	case "$uuid", "$random.uuid":
		return newUUID(), nil
	case "$timestamp":
		return strconv.FormatInt(r.now().Unix(), 10), nil
	case "$isoTimestamp":
		return r.now().UTC().Format(time.RFC3339), nil
	case "$randomInt":
		n, err := rand.Int(rand.Reader, big.NewInt(1001)) // 0–1000, as in IntelliJ
		if err != nil {
			return "", err
		}
		return n.String(), nil
	}
	return "", Error{Line: line, Message: fmt.Sprintf("unknown dynamic variable %q (supported: $uuid, $timestamp, $isoTimestamp, $randomInt)", name)}
}

// newUUID returns a random (version 4) UUID.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // never fails (crypto/rand panics instead)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
