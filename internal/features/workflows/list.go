package workflows

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"flint/internal/prompts"
)

// maxPath bounds a folder from the UI.
const maxPath = 4096

// List offers the workflows for the workspace folder dir: the built-in
// ones, the user's own and, only when the workspace folder that holds dir
// is trusted, its repository's (.flint/workflows, read without leaving it).
func (a *API) List(dir string) (Listing, error) {
	root, clean, err := a.workspaceDir(dir)
	if err != nil {
		return Listing{}, err
	}
	list, problems, hidden := a.load(root)
	out := Listing{Workflows: make([]WorkflowView, 0, len(list)), Problems: problems, Restricted: !a.host.Trusted(root), Hidden: hidden}
	if out.Problems == nil {
		out.Problems = []string{}
	}
	find := Find(list)
	for _, w := range list {
		v := WorkflowView{Name: w.Name, Description: w.Description, Source: string(w.Source), Path: w.Path, Steps: []StepView{}}
		if steps, err := Resolve(w, find); err != nil {
			v.Problem = err.Error()
		} else {
			v.Steps = stepViews(steps)
		}
		out.Workflows = append(out.Workflows, v)
	}
	return a.withConsent(clean, a.withScripts(clean, out)), nil
}

// load reads the workflows offered under root (Merge's order), the
// problems of their files, and how many project files Restricted Mode hid.
func (a *API) load(root string) (list []Workflow, problems []string, hidden int) {
	builtin, problems := Builtins()
	var personal, project []Workflow
	if cfg := a.host.ConfigDir(); cfg != "" {
		var more []string
		personal, more = LoadDir(PersonalDir(cfg), "", SourcePersonal)
		problems = append(problems, more...)
	}
	if a.host.Trusted(root) {
		var more []string
		project, more = LoadDir(ProjectDir(root), root, SourceProject)
		problems = append(problems, more...)
	} else {
		hidden = prompts.CountDir(ProjectDir(root), root, MaxWorkflows)
	}
	return Merge(builtin, personal, project), problems, hidden
}

// startable finds the workflow name among those offered under root and
// resolves its steps.
func (a *API) startable(root, name string) (Workflow, []Step, error) {
	if !ValidName(name) {
		return Workflow{}, nil, errors.New("choose a workflow")
	}
	list, _, _ := a.load(root)
	w, ok := Find(list)(name)
	if !ok {
		return Workflow{}, nil, fmt.Errorf("there is no workflow named %q here", name)
	}
	steps, err := Resolve(w, Find(list))
	if err != nil {
		return Workflow{}, nil, fmt.Errorf("the workflow %q cannot run: %w", name, err)
	}
	return w, steps, nil
}

// workspaceDir checks a folder from the UI: an existing folder that is an
// open workspace folder or inside one, also once its links are resolved.
// It returns the workspace folder (the innermost that holds it) and dir
// cleaned.
func (a *API) workspaceDir(dir string) (root, clean string, err error) {
	if dir == "" || len(dir) > maxPath || !filepath.IsAbs(dir) {
		return "", "", errors.New("choose a workspace folder")
	}
	clean = filepath.Clean(dir)
	for _, r := range a.host.Roots() {
		r = filepath.Clean(r)
		if within(clean, r) && len(r) > len(root) {
			root = r
		}
	}
	if root == "" {
		return "", "", fmt.Errorf("%s is not in an open workspace folder", clean)
	}
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", "", fmt.Errorf("open %s: %w", clean, err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil || !within(real, realRoot) {
		return "", "", fmt.Errorf("%s leads out of its workspace folder", clean)
	}
	return root, clean, nil
}

// within reports whether path is dir or under it (both clean).
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// NewWorkflow writes a starter workflow, .flint/workflows/<name>.md, into
// the trusted workspace folder that holds dir, and returns its path. It
// never overwrites a file nor follows a link out of the folder.
func (a *API) NewWorkflow(dir, name string) (string, error) {
	root, _, err := a.workspaceDir(dir)
	if err != nil {
		return "", err
	}
	if !a.host.Trusted(root) {
		return "", fmt.Errorf("%s is in Restricted Mode: trust the folder to add workflows there", root)
	}
	if !ValidName(name) {
		return "", errors.New("name the workflow with lowercase letters, digits and dashes")
	}
	path, err := prompts.WriteNew(ProjectDir(root), root, name+".md", starter)
	if errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("a workflow named %s already exists", name)
	}
	return path, err
}

// starter is what NewWorkflow writes; it parses and resolves.
const starter = `---
description: Plan, implement, review, then wait for your approval
---

A workflow: each line that starts with two hashes and a title is a step,
taken in order in one agent task. The lines right under a title, up to the
first empty line, are the step's keys:

  type:     agent (the default), script, review, gate or workflow
  agent:    who takes an agent or review step (default: the run's agent)
  run:      a script step's run configuration, by name (e.g. "run: test")
  on-fail:  an earlier agent step the work goes back to when this fails
  workflow: another workflow whose steps take this step's place

The rest is the step's text. Text before the first step, like this, is a
note for people. An agent step passes when the agent ends with
"RESULT: DONE"; a review step when the reviewer ends with "RESULT: PASS".

## Plan

Read the task below and the code it touches. Write a short plan to PLAN.md: what to change, in which files, and how you will check it works. Do not change any code. Commit PLAN.md.

## Implement

Do what PLAN.md says, and nothing more. Add tests for the new behaviour, run them, and make sure they pass. Commit your work.

## Review
type: review
on-fail: Implement

Check that the change does what the task asks and follows PLAN.md, that it has tests, and that it has no bugs or leftovers. List what must change, if anything.

## Approve
type: gate

Read the diff. Approve to finish, or send the run back to a step with a note.
`
