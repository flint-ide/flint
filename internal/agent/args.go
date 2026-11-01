package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Config is the user's setup of one agent CLI (settings.json "agents"): where
// it is, which model it uses and extra command-line arguments. The zero value
// is the CLI's defaults on the login PATH.
type Config struct {
	// Path is the executable: empty looks the agent's name up on the PATH, a
	// bare name is looked up too, anything else must be an absolute path.
	Path string
	// Model is passed to the CLI (--model, Codex's thread model); empty keeps
	// the CLI's own default.
	Model string
	// Args are added to every command line Flint starts for the agent.
	Args []string
}

// Limits for Config values.
const (
	MaxArgs     = 32
	MaxArgLen   = 512
	MaxPathLen  = 1024
	maxModelLen = 128
)

// modelPattern: model aliases and full names ("sonnet", "claude-opus-4-1",
// "us.anthropic.claude-sonnet-4-5[1m]", "gpt-5-codex"); never a flag.
var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@\[\]-]*$`)

// reservedFlags are the flags Flint sets itself (or that would undo what it
// sets): the stream protocol, the session, the permission prompt, the
// settings sources of Restricted Mode, the sandbox and approval policy, and
// the model (which has its own setting). A user argument may not name them.
var reservedFlags = map[Kind][]string{
	Claude: {
		"-p", "--print", "--input-format", "--output-format", "--include-partial-messages", "--replay-user-messages",
		"--permission-prompt-tool", "--permission-mode", "--dangerously-skip-permissions", "--allow-dangerously-skip-permissions",
		"--session-id", "-r", "--resume", "-c", "--continue", "--fork-session",
		"--setting-sources", "--settings", "--model",
		// Pre-approved tools would skip Flint's permission prompt.
		"--allowedTools", "--allowed-tools", "--disallowedTools", "--disallowed-tools",
		// Headless runs limit the built-in tools to reading.
		"--tools",
	},
	Codex: {
		"-m", "--model", "-a", "--ask-for-approval", "-s", "--sandbox", "--full-auto",
		"--dangerously-bypass-approvals-and-sandbox", "--yolo", "-C", "--cd", "--listen",
		// A profile can carry its own sandbox and approval settings.
		"-p", "--profile",
	},
}

// reservedCodexConfig are `-c key=value` keys that would change Codex's
// approvals, sandbox or which projects it trusts, or replace Flint's own MCP
// server (mcp_servers.flint; the user's other MCP servers are fine).
var reservedCodexConfig = []string{"approval_policy", "sandbox", "projects", "profile", "dangerously", "mcp_servers.flint"}

// CheckPath validates a Config.Path.
func CheckPath(path string) error {
	switch {
	case path == "":
		return nil
	case len(path) > MaxPathLen:
		return fmt.Errorf("path longer than %d characters", MaxPathLen)
	case path != strings.TrimSpace(path) || strings.ContainsFunc(path, unicode.IsControl):
		return fmt.Errorf("invalid path %q", path)
	case strings.HasPrefix(path, "-"):
		return fmt.Errorf("path %q may not start with -", path)
	// A relative path would be resolved in the project folder and run the
	// project's file.
	case strings.ContainsRune(path, '/') && !filepath.IsAbs(path):
		return fmt.Errorf("path %q must be absolute (or a bare command name)", path)
	}
	return nil
}

// CheckModel validates a Config.Model.
func CheckModel(model string) error {
	if model == "" {
		return nil
	}
	if len(model) > maxModelLen || !modelPattern.MatchString(model) {
		return fmt.Errorf("invalid model %q", model)
	}
	return nil
}

// CheckArgs validates extra arguments for k: a bounded list of single-line
// values that never name a flag Flint sets itself, and never "--", which
// would turn Flint's own flags after it into plain arguments.
func CheckArgs(k Kind, args []string) error {
	if err := CheckPlainArgs(args); err != nil {
		return err
	}
	for i, a := range args {
		if a == "--" {
			return errors.New(`"--" is not allowed: Flint's own flags follow the extra arguments`)
		}
		if flag := reservedFlag(k, a); flag != "" {
			return fmt.Errorf("%s is set by Flint and cannot be changed in the extra arguments", flag)
		}
		if k == Codex {
			if key := reservedConfigKey(configOverride(args, i)); key != "" {
				return fmt.Errorf("-c %s is set by Flint and cannot be changed in the extra arguments", key)
			}
		}
	}
	return nil
}

// CheckPlainArgs validates the arguments of a command Flint adds no flags of
// its own to (a custom ACP agent): a bounded list of non-empty, single-line
// values.
func CheckPlainArgs(args []string) error {
	if len(args) > MaxArgs {
		return fmt.Errorf("too many arguments (max %d)", MaxArgs)
	}
	for _, a := range args {
		switch {
		case a == "":
			return errors.New("empty argument")
		case len(a) > MaxArgLen:
			return fmt.Errorf("argument longer than %d characters", MaxArgLen)
		case strings.ContainsFunc(a, unicode.IsControl):
			return fmt.Errorf("argument %q contains a newline or control character", a)
		}
	}
	return nil
}

// configOverride returns the Codex "key=value" override args[i] sets ("-c kv",
// "--config kv", "-ckv", "--config=kv"), or "".
func configOverride(args []string, i int) string {
	a := args[i]
	switch {
	case a == "-c" || a == "--config":
		if i+1 < len(args) {
			return args[i+1]
		}
	case strings.HasPrefix(a, "--config="):
		return strings.TrimPrefix(a, "--config=")
	case strings.HasPrefix(a, "-c") && !strings.HasPrefix(a, "--"):
		// clap also accepts "-c=key=value".
		return strings.TrimPrefix(a[2:], "=")
	}
	return ""
}

// reservedFlag returns the reserved flag a names, in any spelling the CLIs
// accept: "--flag", "--flag=value", "-x", "-xvalue" or combined short flags.
func reservedFlag(k Kind, a string) string {
	reserved := reservedFlags[k]
	if strings.HasPrefix(a, "--") {
		name, _, _ := strings.Cut(a, "=")
		for _, r := range reserved {
			if r == name {
				return r
			}
		}
		return ""
	}
	if !strings.HasPrefix(a, "-") || len(a) < 2 {
		return ""
	}
	if k == Codex && strings.HasPrefix(a, "-c") {
		return "" // "-ckey=value": checked as a config override
	}
	// Short flags can be combined ("-pv") or carry their value ("-mo3"), so
	// any reserved letter before an "=" counts; stricter than the CLIs, never
	// looser.
	letters, _, _ := strings.Cut(a[1:], "=")
	for _, r := range reserved {
		if len(r) == 2 && strings.ContainsRune(letters, rune(r[1])) {
			return r
		}
	}
	return ""
}

func reservedConfigKey(kv string) string {
	if kv == "" {
		return ""
	}
	key := tomlKeyPath(kv)
	for _, r := range reservedCodexConfig {
		// A key above a reserved one sets it through an inline table
		// (`mcp_servers={flint={...}}`).
		if strings.HasPrefix(key, r) || strings.Contains(r, ".") && strings.HasPrefix(r, key+".") {
			return key
		}
	}
	return ""
}

// tomlKeyPath normalizes the key of a "key=value" override the way TOML
// reads it: the dotted parts, unquoted ("..." or '...') and without the
// spaces around them, joined by dots. `mcp_servers . "flint".url` becomes
// mcp_servers.flint.url.
func tomlKeyPath(kv string) string {
	var parts []string
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(kv); i++ {
		c := kv[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote == '"' && c == '\\' && i+1 < len(kv):
			i++
			cur.WriteByte(kv[i])
		case quote != 0:
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			quote = c
		case c == '.':
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		case c == '=':
			return strings.Join(append(parts, strings.TrimSpace(cur.String())), ".")
		case c == ' ' || c == '\t':
			// spaces around a part are not part of the key
		default:
			cur.WriteByte(c)
		}
	}
	return strings.Join(append(parts, strings.TrimSpace(cur.String())), ".")
}

// ComposeArgs builds a command line from Flint's own arguments and the
// user's extra ones. The extra arguments come first so that Flint's flags,
// parsed later, win wherever a CLI lets a repeated flag override an earlier
// one; CheckArgs already keeps the flags Flint relies on out of them. In
// Restricted Mode (untrusted) the extra arguments are dropped: a flag such as
// --mcp-config or --add-dir with a relative path would load the project's
// files. Invalid extra arguments fail rather than being silently dropped.
func ComposeArgs(k Kind, user, flint []string, untrusted bool) ([]string, error) {
	if untrusted || len(user) == 0 {
		return append([]string{}, flint...), nil
	}
	if err := CheckArgs(k, user); err != nil {
		return nil, fmt.Errorf("%s extra arguments: %w", k, err)
	}
	return append(append(make([]string, 0, len(user)+len(flint)), user...), flint...), nil
}

// Binary is the executable to run for k: the configured path, else its name.
func (c Config) Binary(k Kind) string {
	if c.Path != "" {
		return c.Path
	}
	return string(k)
}
