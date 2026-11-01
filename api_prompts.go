package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"flint/internal/git"
	"flint/internal/prompts"
	"flint/internal/redact"
)

// Bounds on what the web view hands Expand (it is untrusted like any caller).
const (
	maxPromptSelection = 256 << 10
	maxPromptProblems  = 64 << 10
	maxPromptArg       = 4 << 10
	maxPromptPath      = 4096
)

// PromptLibrary is what the agent chat's "/" picker lists.
type PromptLibrary struct {
	// Prompts are merged and sorted by name (see prompts.Merge).
	Prompts []prompts.Prompt `json:"prompts"`
	// Problems are files that could not be used ("name.md: reason").
	Problems []string `json:"problems"`
	// PersonalDir holds the user's own prompts.
	PersonalDir string `json:"personalDir"`
	// ProjectDir holds the project's prompts ("" with no folder).
	ProjectDir string `json:"projectDir"`
	// Trusted is false in Restricted Mode: project prompts are marked and
	// can't replace a built-in or personal prompt.
	Trusted bool `json:"trusted"`
}

// PromptInput is the editor context a prompt's placeholders can use.
type PromptInput struct {
	// Selection is the selected text of the active editor ({{selection}}).
	Selection string `json:"selection"`
	// File is the absolute path of the active editor's file ({{file}}).
	File string `json:"file"`
	// Problems lists the file's diagnostics, one per line ({{problems}}).
	Problems string `json:"problems"`
	// Args are the values of the prompt's own args, by name.
	Args map[string]string `json:"args"`
}

// PromptsAPI is the prompt library. It only ever returns text for the chat
// draft; nothing here sends anything to an agent or runs a project's code.
type PromptsAPI struct {
	app *App
	git *GitAPI
}

// List returns the prompts for root (a workspace folder, or "" for none).
// Files are read on every call, so edits show up the next time the picker opens.
func (p *PromptsAPI) List(root string) (PromptLibrary, error) {
	root, err := p.workspaceRoot(root)
	if err != nil {
		return PromptLibrary{}, err
	}
	return p.library(root), nil
}

func (p *PromptsAPI) library(root string) PromptLibrary {
	lib := PromptLibrary{PersonalDir: prompts.PersonalDir(p.app.configDir), Problems: []string{}}
	var builtin []prompts.Prompt
	if p.app.currentSettings().Prompts.Builtins {
		var problems []string
		builtin, problems = prompts.Builtins()
		lib.Problems = append(lib.Problems, problems...)
	}
	personal, problems := prompts.LoadDir(lib.PersonalDir, "", prompts.SourcePersonal)
	lib.Problems = append(lib.Problems, problems...)
	var project []prompts.Prompt
	if root != "" {
		lib.ProjectDir = prompts.ProjectDir(root)
		lib.Trusted = p.app.isTrusted(root)
		project, problems = prompts.LoadDir(lib.ProjectDir, root, prompts.SourceProject)
		lib.Problems = append(lib.Problems, problems...)
	}
	list, shadowed := prompts.Merge(builtin, personal, project, root == "" || lib.Trusted)
	for _, name := range shadowed {
		lib.Problems = append(lib.Problems, fmt.Sprintf("%s.md: not used in Restricted Mode, where a project prompt can't replace a built-in or personal one", name))
	}
	lib.Prompts = list
	return lib
}

// Expand fills prompt name's placeholders and returns the text for the chat
// draft. {{diff}} and {{branch}} are read from git only when the prompt uses them.
func (p *PromptsAPI) Expand(root, name string, in PromptInput) (string, error) {
	root, err := p.workspaceRoot(root)
	if err != nil {
		return "", err
	}
	list := p.library(root).Prompts
	idx := slices.IndexFunc(list, func(x prompts.Prompt) bool { return strings.EqualFold(x.Name, name) })
	if idx < 0 {
		return "", fmt.Errorf("no prompt named %q", name)
	}
	prompt := list[idx]
	vars, err := promptVars(prompt, in, root)
	if err != nil {
		return "", err
	}
	used := prompts.Placeholders(prompt.Body)
	if slices.Contains(used, "diff") || slices.Contains(used, "branch") {
		vars["diff"], vars["branch"] = p.gitContext(root, slices.Contains(used, "diff"))
	}
	return prompts.Expand(prompt.Body, vars), nil
}

// promptVars checks the editor context and the args and turns them into values.
func promptVars(prompt prompts.Prompt, in PromptInput, root string) (map[string]string, error) {
	switch {
	case len(in.Selection) > maxPromptSelection:
		return nil, fmt.Errorf("the selection is larger than %d KB", maxPromptSelection>>10)
	case len(in.Problems) > maxPromptProblems:
		return nil, fmt.Errorf("the problem list is larger than %d KB", maxPromptProblems>>10)
	case len(in.File) > maxPromptPath || strings.ContainsFunc(in.File, unicode.IsControl):
		return nil, errors.New("invalid file path")
	case in.File != "" && !filepath.IsAbs(in.File):
		return nil, errors.New("the file path must be absolute")
	}
	file := displayPath(in.File, root)
	// Like everything else Flint puts in a draft: credentials redacted, and
	// nothing at all from a file that holds credentials as a whole.
	selection := prompts.Fenced(redact.Secrets(in.Selection), "")
	if in.Selection != "" && redact.SecretFile(in.File) {
		selection = "(selection omitted: " + file + " holds credentials)"
	}
	vars := map[string]string{
		"selection": selection,
		"file":      file,
		"problems":  orElse(redact.Secrets(strings.TrimSpace(in.Problems)), "(no problems reported)"),
	}
	for k, v := range in.Args {
		if !slices.Contains(prompt.Args, k) {
			return nil, fmt.Errorf("prompt %s has no arg %q", prompt.Name, k)
		}
		if len(v) > maxPromptArg {
			return nil, fmt.Errorf("the value of %s is longer than %d KB", k, maxPromptArg>>10)
		}
		vars[k] = v
	}
	for _, a := range prompt.Args {
		if _, ok := vars[a]; !ok {
			vars[a] = ""
		}
	}
	return vars, nil
}

// displayPath is file relative to root when inside it, as the agent (running
// in root) sees it.
func displayPath(file, root string) string {
	if file == "" {
		return "(no file open)"
	}
	file = filepath.Clean(file)
	if root != "" {
		if rel, err := filepath.Rel(root, file); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return filepath.ToSlash(rel)
		}
	}
	return file
}

func orElse(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// gitContext returns {{diff}} (the uncommitted changes, bounded and with
// secrets redacted) and {{branch}}. Flint's own git calls never run a
// repository's hooks or filters, so this is safe in Restricted Mode too.
func (p *PromptsAPI) gitContext(root string, withDiff bool) (diff, branch string) {
	const none = "(not a git repository)"
	if root == "" {
		return none, none
	}
	type result struct{ diff, branch string }
	r, err := with(p.git, root, gitTimeout, func(ctx context.Context, r *git.Repo) (result, error) {
		st, err := r.Status(ctx)
		if err != nil {
			return result{}, err
		}
		out := result{branch: orElse(st.Branch, "(detached HEAD)")}
		if !withDiff {
			return out, nil
		}
		tracked, untracked, omitted := diffPaths(st.Files)
		d, err := r.ChangesDiff(ctx, tracked, untracked, maxDiffForLLM)
		if err != nil {
			return result{}, err
		}
		parts := []string{}
		if fenced := prompts.Fenced(redact.Secrets(strings.TrimLeft(d, "\n")), "diff"); fenced != "" {
			parts = append(parts, fenced)
		}
		for _, p := range omitted {
			parts = append(parts, "(credentials file omitted: "+p+")")
		}
		out.diff = orElse(strings.Join(parts, "\n"), "(no uncommitted changes)")
		return out, nil
	})
	if errors.Is(err, git.ErrNotRepository) {
		return none, none
	}
	if err != nil {
		msg := "(git failed: " + redact.String(err.Error()) + ")"
		return msg, msg
	}
	return r.diff, r.branch
}

// diffPaths splits the changed files for {{diff}}. A file that holds
// credentials as a whole (redact.SecretFile: .env, keys, kubeconfig…) is left
// out entirely, whichever side of a rename it is on, and only named in omitted,
// as inline edit and agent review do; pattern redaction alone isn't enough there.
func diffPaths(files []git.FileChange) (tracked, untracked, omitted []string) {
	for _, f := range files {
		if redact.SecretFile(f.Path) || (f.OrigPath != "" && redact.SecretFile(f.OrigPath)) {
			omitted = append(omitted, f.Path)
			continue
		}
		switch {
		case f.Kind == git.Untracked:
			untracked = append(untracked, f.Path)
		case f.OrigPath != "":
			tracked = append(tracked, f.OrigPath, f.Path)
		default:
			tracked = append(tracked, f.Path)
		}
	}
	return tracked, untracked, omitted
}

// Create writes a new prompt from a template, in the project's .flint/prompts
// (personal false) or the personal folder, and returns its path for the editor.
func (p *PromptsAPI) Create(root string, personal bool, name string) (string, error) {
	if personal {
		return prompts.Create(prompts.PersonalDir(p.app.configDir), "", name)
	}
	if root == "" {
		return "", errors.New("open a folder to add a project prompt")
	}
	root, err := p.workspaceRoot(root)
	if err != nil {
		return "", err
	}
	return prompts.Create(prompts.ProjectDir(root), root, name)
}

// workspaceRoot maps root to the open workspace folder holding it; "" stays "".
func (p *PromptsAPI) workspaceRoot(root string) (string, error) {
	if root == "" {
		return "", nil
	}
	return p.app.rootOf(root)
}
