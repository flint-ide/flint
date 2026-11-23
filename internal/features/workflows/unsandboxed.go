package workflows

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"

	"flint/internal/redact"
)

// Script steps without the sandbox run the user's run configuration on
// code an agent wrote, with the user's access to the computer. So the
// first start of such a run in a checkout is the user's own decision, made
// once per checkout in a native dialog the web view cannot answer (like
// trusting a folder): Run Workflow…, flint workflow run and a project
// item's Delegate ask; an agent's start_workflow and a schedule never ask
// and are refused until the user said yes there. The answer is state (the
// workflowUnsandboxed key of state.json), not a setting: a repository
// cannot carry it, and ForgetUnsandboxed takes it back.

// stateUnsandboxed is the state.json key of the checkouts where the user
// accepted script steps without a sandbox, oldest first.
const stateUnsandboxed = "workflowUnsandboxed"

// maxUnsandboxed bounds that list; the oldest folder is dropped first.
const maxUnsandboxed = 200

// The native question (askUnsandboxed).
const (
	unsandboxedTitle  = "Run scripts without a sandbox?"
	unsandboxedButton = "Run without a sandbox"
	unsandboxedWhy    = "These commands run on code an agent wrote, with your access to this computer. A sandbox limits them to the run's folder (Settings ▸ Workflows)."
	// maxConsentLine bounds one script step's line in the question.
	maxConsentLine = 300
)

// The refusals when the user has not accepted the checkout.
var (
	errUnsandboxedDeclined = errors.New("not started: script steps need your OK to run without a sandbox, or switch the sandbox on in Settings ▸ Workflows")
	errUnsandboxedSchedule = errors.New("script steps here still need your OK to run without a sandbox: run this workflow once yourself, or switch the sandbox on")
)

// consentMode is how a start gets the user's OK for script steps without a
// sandbox (runRequest.consent).
type consentMode int

const (
	// consentAsk: the user started it (Run Workflow…, flint workflow run,
	// Delegate): ask once in a native dialog.
	consentAsk consentMode = iota
	// consentAgent: an agent's start_workflow, which the user approves per
	// call; refused until the user ran such a workflow there themselves.
	consentAgent
	// consentSchedule: nobody watches; skipped until the user said yes.
	consentSchedule
)

// unsandboxedScripts reports whether c would run a script step without the
// sandbox: it has one and workflows.sandbox is off. With the setting on and
// no sandbox here the step fails closed instead (SandboxUnavailable).
func (a *API) unsandboxedScripts(c checkedRun) bool {
	return a.sandboxState() == SandboxOff && slices.ContainsFunc(c.steps, func(s Step) bool { return s.Kind == KindScript })
}

// scriptConsent is nil when c may start as far as its script steps go: they
// run in the sandbox, there are none, or the user accepted c's checkout
// (asked now for consentAsk). It holds no engine lock, and asks one
// question at a time: a start that waited for another's answer reads it.
func (a *API) scriptConsent(c checkedRun) error {
	if !a.unsandboxedScripts(c) || a.unsandboxedOK(c.checkout) {
		return nil
	}
	switch c.req.consent {
	case consentAgent:
		return fmt.Errorf("the workflow %q has script steps that would run without a sandbox in %s, and the user has not allowed that there: "+
			"ask the user to run this workflow once themselves in Flint (or to switch the sandbox on in Settings ▸ Workflows), then try again", c.workflow.Name, c.checkout)
	case consentSchedule:
		return errUnsandboxedSchedule
	}
	a.askMu.Lock()
	defer a.askMu.Unlock()
	if a.unsandboxedOK(c.checkout) {
		return nil
	}
	if a.host.Choose(unsandboxedTitle, a.unsandboxedMessage(c), []string{unsandboxedButton}) != unsandboxedButton {
		return errUnsandboxedDeclined
	}
	a.rememberUnsandboxed(c.checkout)
	return nil
}

// unsandboxedMessage is the question's text: the folder, each script step
// with what it runs (bounded, redacted), and why it matters.
func (a *API) unsandboxedMessage(c checkedRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The workflow %q runs these script steps in agent tasks of %s, without a sandbox:\n\n", c.workflow.Name, c.checkout)
	for _, st := range c.steps {
		if st.Kind != KindScript {
			continue
		}
		line, problem := a.scriptCommand(c.checkout, st.Run)
		if problem != "" {
			line = "cannot be resolved: " + problem
		}
		fmt.Fprintf(&b, "- %s (%s): %s\n", st.Title, st.Run, cut(redact.Secrets(line), maxConsentLine))
	}
	b.WriteString("\n" + unsandboxedWhy + "\n\nFlint remembers your answer for this folder.")
	return b.String()
}

// loadUnsandboxed reads the accepted checkouts, keeping only clean,
// absolute paths, each once, at most maxUnsandboxed (the newest).
func (a *API) loadUnsandboxed() []string {
	var stored []string
	if err := a.host.LoadState(stateUnsandboxed, &stored); err != nil {
		slog.Warn("workflows: read the folders allowed to run scripts without a sandbox", "error", err)
		return nil
	}
	out := make([]string, 0, len(stored))
	for _, p := range stored {
		if p != "" && len(p) <= maxPath && filepath.IsAbs(p) && filepath.Clean(p) == p && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	if extra := len(out) - maxUnsandboxed; extra > 0 {
		out = out[extra:]
	}
	return out
}

// unsandboxedOK reports whether the user accepted script steps without a
// sandbox in checkout.
func (a *API) unsandboxedOK(checkout string) bool {
	if checkout == "" {
		return false
	}
	a.consentMu.Lock()
	defer a.consentMu.Unlock()
	return slices.Contains(a.loadUnsandboxed(), filepath.Clean(checkout))
}

// rememberUnsandboxed adds checkout as the newest accepted one.
func (a *API) rememberUnsandboxed(checkout string) {
	checkout = filepath.Clean(checkout)
	a.consentMu.Lock()
	defer a.consentMu.Unlock()
	list := slices.DeleteFunc(a.loadUnsandboxed(), func(p string) bool { return p == checkout })
	list = append(list, checkout)
	if extra := len(list) - maxUnsandboxed; extra > 0 {
		list = list[extra:]
	}
	a.saveUnsandboxed(list)
}

// saveUnsandboxed stores the accepted checkouts. A failure is logged: the
// start the user said yes to goes on, and the next one asks again.
func (a *API) saveUnsandboxed(list []string) {
	if err := a.host.SaveState(stateUnsandboxed, list); err != nil {
		slog.Warn("workflows: save the folders allowed to run scripts without a sandbox", "error", err)
	}
}

// ForgetUnsandboxed takes back the user's OK to run script steps without a
// sandbox for the checkout of dir (a workspace folder, a folder in one, or
// a checkout that was accepted). The next start there asks again.
func (a *API) ForgetUnsandboxed(dir string) error {
	if dir == "" || len(dir) > maxPath || !filepath.IsAbs(dir) {
		return errors.New("choose a folder")
	}
	forget := []string{filepath.Clean(dir)}
	if _, clean, err := a.workspaceDir(dir); err == nil {
		if checkout := a.checkoutOf(clean); checkout != "" {
			forget = append(forget, checkout)
		}
	}
	a.consentMu.Lock()
	list := a.loadUnsandboxed()
	kept := slices.DeleteFunc(slices.Clone(list), func(p string) bool { return slices.Contains(forget, p) })
	if len(kept) != len(list) {
		a.saveUnsandboxed(kept)
	}
	a.consentMu.Unlock()
	a.host.Emit(EventChanged, "")
	return nil
}

// checkoutOf is the repository root of dir, "" when it is in none.
func (a *API) checkoutOf(dir string) string {
	ctx, cancel := a.ctx(requestTimeout)
	defer cancel()
	repo, err := a.host.Repository(ctx, dir)
	if err != nil || repo.Root == "" {
		return ""
	}
	return filepath.Clean(repo.Root)
}

// withConsent sets Listing.UnsandboxedOK for the folder dir (clean, in a
// workspace folder); git is asked only when some folder was accepted.
func (a *API) withConsent(dir string, l Listing) Listing {
	a.consentMu.Lock()
	some := len(a.loadUnsandboxed()) > 0
	a.consentMu.Unlock()
	if some {
		l.UnsandboxedOK = a.unsandboxedOK(a.checkoutOf(dir))
	}
	return l
}
