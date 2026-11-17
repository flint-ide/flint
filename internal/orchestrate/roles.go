package orchestrate

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"flint/internal/prompts"
)

// Roles are Markdown files in the prompt-file format (internal/prompts.Parse:
// front matter `description` and `agent`, then the body) kept in the user's
// config directory (personal) and in the project's .flint/roles folder. A
// role's body is put before the task an agent is given, so "reviewer" can mean
// the same thing in every chat that starts one.

// Limits on role files. A project's roles come from its repository, so like
// prompts they are bounded; they are not settings.
const (
	// MaxRoleBytes is the longest body a role may have: it is prepended to
	// every task that names the role.
	MaxRoleBytes = 16 << 10
	// MaxRoles is how many roles one folder may offer.
	MaxRoles = 50
)

// ErrUnknownRole is returned for a role name that no usable role has.
var ErrUnknownRole = errors.New("orchestrate: unknown role")

// Role is one role template.
type Role struct {
	// Name is the file name without .md.
	Name        string
	Description string
	// Agent, when set, is the only agent the role may run in.
	Agent string
	// Body is the text put before the task.
	Body   string
	Source prompts.Source
	Path   string
}

// PersonalRolesDir is the user's own role folder in Flint's config directory.
func PersonalRolesDir(configDir string) string { return filepath.Join(configDir, "roles") }

// ProjectRolesDir is a project's role folder.
func ProjectRolesDir(root string) string { return filepath.Join(root, ".flint", "roles") }

// LoadRoles reads the personal roles and, when trusted, the project's. A role
// in both places is the personal one: a repository must not redefine what the
// user's own role says. An untrusted (Restricted Mode) project is skipped
// entirely: its roles would put text from the repository into a prompt.
// Files that cannot be used are left out and described in problems; missing
// folders are no problem. The result is sorted by name and never nil.
// trusted is a parameter so this stays free of any notion of workspace trust.
func LoadRoles(configDir, projectRoot string, trusted bool) (roles []Role, problems []string) {
	roles = []Role{}
	personal, p := loadRoleDir(PersonalRolesDir(configDir), "", prompts.SourcePersonal)
	problems = append(problems, p...)
	byName := map[string]Role{}
	for _, r := range personal {
		byName[roleKey(r.Name)] = r
	}
	if trusted && projectRoot != "" {
		project, p := loadRoleDir(ProjectRolesDir(projectRoot), projectRoot, prompts.SourceProject)
		problems = append(problems, p...)
		for _, r := range project {
			if _, taken := byName[roleKey(r.Name)]; !taken {
				byName[roleKey(r.Name)] = r
			}
		}
	}
	for _, r := range byName {
		roles = append(roles, r)
	}
	slices.SortFunc(roles, func(a, b Role) int { return strings.Compare(roleKey(a.Name), roleKey(b.Name)) })
	return roles, problems
}

// loadRoleDir reads one folder through prompts.LoadDir, which checks names,
// the file size and that a project's folder and files stay inside it, then
// applies the limits particular to roles.
func loadRoleDir(dir, within string, source prompts.Source) ([]Role, []string) {
	list, problems := prompts.LoadDir(dir, within, source)
	var roles []Role
	for _, p := range list {
		switch {
		case len(p.Body) > MaxRoleBytes:
			problems = append(problems, fmt.Sprintf("%s.md: role text is larger than %d KB", p.Name, MaxRoleBytes>>10))
		case len(roles) == MaxRoles:
			problems = append(problems, fmt.Sprintf("%s: more than %d roles; %s and the rest are not loaded", dir, MaxRoles, p.Name))
			return roles, problems
		default:
			roles = append(roles, Role{Name: p.Name, Description: p.Description, Agent: p.Agent, Body: p.Body, Source: p.Source, Path: p.Path})
		}
	}
	return roles, problems
}

// ResolveRole finds the role called name (case does not matter). The name
// comes from an agent or the web view, so it is checked against the strict
// prompt-name pattern before anything is read.
func ResolveRole(configDir, projectRoot string, trusted bool, name string) (Role, error) {
	if !prompts.ValidName(name) {
		return Role{}, fmt.Errorf("invalid role name %q: use letters, digits, - and _", name)
	}
	roles, _ := LoadRoles(configDir, projectRoot, trusted)
	for _, r := range roles {
		if roleKey(r.Name) == roleKey(name) {
			return r, nil
		}
	}
	return Role{}, fmt.Errorf("%w %q", ErrUnknownRole, name)
}

// Apply puts the role's body before prompt. With no prompt there is nothing
// to prepend to and it returns "": a role alone does not start a turn.
func (r Role) Apply(prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return ""
	}
	return r.Body + "\n\n" + prompt
}

func roleKey(name string) string { return strings.ToLower(name) }
