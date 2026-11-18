package projects

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"flint/internal/git"
)

// Events of Clone…: the progress of a clone and its end; the data is a
// CloneEvent. projects:changed is emitted too when a clone starts and ends.
const (
	EventCloneProgress = "projects:clone-progress"
	EventCloneDone     = "projects:clone-done"
)

// Bounds of a clone.
const (
	cloneTimeout   = 30 * time.Minute
	progressEvery  = 250 * time.Millisecond
	maxCloneName   = 100
	maxProgressLen = 300
)

// CloneEvent is the data of EventCloneProgress and EventCloneDone.
type CloneEvent struct {
	Project string `json:"project"`
	Repo    string `json:"repo"`
	// Destination is the folder the repository is cloned into.
	Destination string `json:"destination"`
	// Progress is git's latest progress line (progress events).
	Progress string `json:"progress"`
	// Done: the clone ended; Error says why it failed ("" when it worked),
	// Cancelled that the user cancelled it. What it created is removed
	// when it did not work.
	Done      bool   `json:"done"`
	Error     string `json:"error"`
	Cancelled bool   `json:"cancelled"`
}

// CloneView is the clone running for a project (Project.Clone).
type CloneView struct {
	Repo        string `json:"repo"`
	URL         string `json:"url"`
	Destination string `json:"destination"`
	Progress    string `json:"progress"`
}

// cloneSource is what git clones: a URL from a project that passed
// CloneSource, the transport it needs, and the folder name it gets.
type cloneSource struct {
	URL       string
	Protocols []string
	Name      string
}

var (
	cloneHost    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*[A-Za-z0-9]$`)
	cloneUser    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)
	clonePath    = regexp.MustCompile(`^[A-Za-z0-9._~/@+-]+$`)
	cloneDirName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
	cloneScheme  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)
)

// errTransport is the refusal of a URL that is not https://, ssh:// or
// scp-like.
var errTransport = errors.New("only https://, ssh:// and user@host:path URLs can be cloned (not file://, local paths, ext:: or other transports)")

// CloneSource checks a repository URL from a project for Clone…: only
// https://host/path (without a user), ssh://[user@]host[:port]/path and
// scp-like [user@]host:path are accepted, with a plain host and user name
// (nothing git or ssh could take as an option), no password, query or
// fragment, and a
// path without "." or ".." parts. The folder name is the path's last part
// without ".git", checked as a file name.
func CloneSource(raw string) (cloneSource, error) {
	if raw == "" || len(raw) > maxURL {
		return cloneSource{}, errors.New("the URL is empty or too long")
	}
	for _, r := range raw {
		if r <= ' ' || r >= 0x7f {
			return cloneSource{}, errors.New("the URL contains spaces, control or non-ASCII characters")
		}
	}
	if strings.HasPrefix(raw, "-") {
		return cloneSource{}, errors.New("the URL starts with -")
	}
	if strings.Contains(raw, "::") || strings.ContainsAny(raw, `\[]`) {
		return cloneSource{}, errTransport
	}
	var src cloneSource
	var p string
	if cloneScheme.MatchString(raw) {
		u, err := url.Parse(raw)
		if err != nil {
			return cloneSource{}, fmt.Errorf("the URL can't be read: %v", err)
		}
		switch {
		case strings.HasPrefix(raw, "https://"):
			src.Protocols = []string{"https"}
		case strings.HasPrefix(raw, "ssh://"):
			src.Protocols = []string{"ssh"}
		default:
			return cloneSource{}, errTransport
		}
		if u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
			return cloneSource{}, errors.New("the URL has a query or a fragment")
		}
		if err := checkHost(u.Hostname()); err != nil {
			return cloneSource{}, err
		}
		if port := u.Port(); port != "" && (len(port) > 5 || strings.Trim(port, "0123456789") != "") {
			return cloneSource{}, errors.New("the URL's port is not a number")
		}
		if u.User != nil {
			if _, ok := u.User.Password(); ok {
				return cloneSource{}, errors.New("the URL holds a password: use a credential helper instead")
			}
			// A user in an https URL is often a token pasted as the user: it
			// would end up in the clone's configuration and in the dialog.
			// ssh:// and scp-like URLs keep theirs (git@host).
			if src.Protocols[0] == "https" {
				return cloneSource{}, errors.New("the https URL holds a user name (it may be a token): use a credential helper instead")
			}
			if !cloneUser.MatchString(u.User.Username()) {
				return cloneSource{}, fmt.Errorf("the user name %q is not allowed", u.User.Username())
			}
		}
		p = u.Path
		if !strings.HasPrefix(p, "/") {
			return cloneSource{}, errors.New("the URL has no repository path")
		}
	} else {
		// scp-like: [user@]host:path. A colon after a slash makes it a
		// local path, which is refused.
		hostPart, rest, found := strings.Cut(raw, ":")
		if !found || strings.Contains(hostPart, "/") || strings.HasPrefix(raw, ".") || strings.HasPrefix(raw, "~") {
			return cloneSource{}, errTransport
		}
		host := hostPart
		if i := strings.LastIndexByte(hostPart, '@'); i >= 0 {
			if user := hostPart[:i]; !cloneUser.MatchString(user) {
				return cloneSource{}, fmt.Errorf("the user name %q is not allowed", user)
			}
			host = hostPart[i+1:]
		}
		if err := checkHost(host); err != nil {
			return cloneSource{}, err
		}
		if rest == "" || strings.HasPrefix(rest, "-") {
			return cloneSource{}, errors.New("the URL's repository path is empty or starts with -")
		}
		p = rest
		src.Protocols = []string{"ssh"}
	}
	if !clonePath.MatchString(p) || badSegment(strings.Trim(p, "/")) {
		return cloneSource{}, fmt.Errorf("the repository path %q is not allowed", p)
	}
	name := strings.TrimSuffix(strings.TrimRight(p, "/"), ".git")
	name = name[strings.LastIndexByte(name, '/')+1:]
	if err := checkCloneName(name); err != nil {
		return cloneSource{}, err
	}
	src.URL, src.Name = raw, name
	return src, nil
}

func checkHost(host string) error {
	if len(host) < 2 || len(host) > 253 || !cloneHost.MatchString(host) {
		// One letter would be a drive (C:path) on Windows.
		return fmt.Errorf("the host %q is not allowed", host)
	}
	return nil
}

// checkCloneName checks the folder name of a clone as a file name.
func checkCloneName(name string) error {
	if len(name) > maxCloneName || !cloneDirName.MatchString(name) || strings.HasSuffix(name, ".") || windowsReserved(name) {
		return fmt.Errorf("the URL's last part %q can't be a folder name", name)
	}
	return nil
}

func windowsReserved(name string) bool {
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	return len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9'
}

// checkDestination: dest must not exist, or be an empty folder (not a link).
func checkDestination(dest string) error {
	info, err := os.Lstat(dest)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s exists already: choose another folder", dest)
	}
	f, err := os.Open(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	if names, _ := f.Readdirnames(1); len(names) > 0 {
		return fmt.Errorf("%s exists and is not empty: choose another folder", dest)
	}
	return nil
}

// cloner runs the clones: at most one per project.
type cloner struct {
	mu   sync.Mutex
	jobs map[string]*cloneJob // by project folder
	wg   sync.WaitGroup
	// source changes what is cloned after CloneSource accepted the URL
	// (tests: a local bare repository over file). Nil outside tests.
	source func(*cloneSource)
}

type cloneJob struct {
	view      CloneView
	cancel    context.CancelFunc // nil until git runs
	cancelled bool
}

func (c *cloner) claim(folder string, view CloneView) (*cloneJob, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if j := c.jobs[folder]; j != nil {
		return nil, fmt.Errorf("%s is being cloned for this project: wait for it or cancel it", j.view.Repo)
	}
	if c.jobs == nil {
		c.jobs = map[string]*cloneJob{}
	}
	j := &cloneJob{view: view}
	c.jobs[folder] = j
	c.wg.Add(1)
	return j, nil
}

func (c *cloner) release(folder string, j *cloneJob) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.jobs[folder] == j {
		delete(c.jobs, folder)
	}
	c.wg.Done()
}

// start gives the job its cancel; false when it was cancelled already.
func (c *cloner) start(j *cloneJob, cancel context.CancelFunc) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	j.cancel = cancel
	return !j.cancelled
}

// cancel stops the clone of folder; false when none runs.
func (c *cloner) cancel(folder string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	j := c.jobs[folder]
	if j == nil {
		return false
	}
	j.cancelled = true
	if j.cancel != nil {
		j.cancel()
	}
	return true
}

func (c *cloner) wasCancelled(j *cloneJob) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return j.cancelled
}

func (c *cloner) progress(j *cloneJob, line string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	j.view.Progress = line
}

func (c *cloner) view(folder string) *CloneView {
	c.mu.Lock()
	defer c.mu.Unlock()
	if j := c.jobs[folder]; j != nil {
		v := j.view
		return &v
	}
	return nil
}

// closeAll cancels every clone and waits (bounded) until they cleaned up.
func (c *cloner) closeAll() {
	c.mu.Lock()
	for _, j := range c.jobs {
		j.cancelled = true
		if j.cancel != nil {
			j.cancel()
		}
	}
	c.mu.Unlock()
	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

// Clone clones the project's repository repo into a new folder inside
// parent (Clone…; the UI asks for parent with WorkspaceAPI.PickFolder) and
// remembers it as the repository's checkout, as Locate… does. The URL comes
// from the project and must pass CloneSource; the new folder is parent/<the
// URL's last part> and must not exist or be empty. Go asks first in a native
// confirmation that shows the URL and the folder. git runs with only the
// URL's transport, no submodules and no hooks, asks for nothing on a
// terminal, and can be cancelled (CancelClone) or runs out after 30
// minutes; a clone that does not finish leaves nothing behind (it is made in
// a temporary folder next to the destination and moved into place at the
// end). One clone per project at a time. It returns when the clone ended;
// progress comes as projects:clone-progress events, the end as
// projects:clone-done. The clone is not added to the workspace (the UI does
// that, as for a located folder). It is trusted only when parent is inside a
// trusted folder (trust covers everything under a folder): the confirmation
// says so, and a project whose folder is not trusted can't clone there. The
// returned RepositoryView carries the clone's real trust.
func (a *API) Clone(folder, repo, parent string) (RepositoryView, error) {
	real, _, err := a.known(folder)
	if err != nil {
		return RepositoryView{}, err
	}
	c, err := LoadConfig(real)
	if err != nil {
		return RepositoryView{}, err
	}
	decl, ok := c.Repository(repo)
	if !ok {
		return RepositoryView{}, fmt.Errorf("the project has no repository %q", repo)
	}
	src, err := CloneSource(decl.URL)
	if err != nil {
		return RepositoryView{}, fmt.Errorf("%s can't be cloned: %w", decl.URL, err)
	}
	par, err := requireDir(parent)
	if err != nil {
		return RepositoryView{}, err
	}
	dest := filepath.Join(par, src.Name)
	if err := checkDestination(dest); err != nil {
		return RepositoryView{}, err
	}
	// Trust goes by folder and everything under it, so a clone inside a
	// trusted folder is trusted at once: agents there would use the
	// repository's own settings and hooks. A URL from a project whose folder
	// is not trusted never gets that.
	projectTrusted := a.host.Trusted(real)
	if err := cloneTrust(projectTrusted, a.host.Trusted(dest)); err != nil {
		return RepositoryView{}, err
	}
	job, err := a.clones.claim(real, CloneView{Repo: repo, URL: decl.URL, Destination: dest})
	if err != nil {
		return RepositoryView{}, err
	}
	defer a.clones.release(real, job)
	if !a.host.Confirm("Clone a repository?", cloneMessage(c.Name, repo, decl.URL, dest, projectTrusted, a.trustedBy(par, dest)), "Clone") {
		return RepositoryView{}, errors.New("the clone was cancelled")
	}
	// Trust may have changed while the question was open.
	if err := cloneTrust(a.host.Trusted(real), a.host.Trusted(dest)); err != nil {
		return RepositoryView{}, err
	}
	base := a.host.Context()
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, cloneTimeout)
	defer cancel()
	if !a.clones.start(job, cancel) {
		return RepositoryView{}, errors.New("the clone was cancelled")
	}
	a.emit(real)
	ev := CloneEvent{Project: real, Repo: repo, Destination: dest}
	err = a.runClone(ctx, job, src, par, dest, ev)
	ev.Done = true
	if err != nil {
		switch {
		case a.clones.wasCancelled(job):
			err, ev.Cancelled = errors.New("the clone was cancelled"), true
		case errors.Is(err, context.DeadlineExceeded):
			err = fmt.Errorf("the clone took longer than %v and was stopped", cloneTimeout)
		}
		ev.Error = err.Error()
		a.host.Emit(EventCloneDone, ev)
		a.emit(real)
		return RepositoryView{}, err
	}
	key, _ := RepoKey(decl.URL)
	a.mu.Lock()
	err = a.host.SaveState(stateCheckouts, setCheckout(a.checkouts(), key, dest))
	a.mu.Unlock()
	if err != nil {
		ev.Error = "cloned, but the checkout could not be remembered: " + err.Error()
	}
	a.host.Emit(EventCloneDone, ev)
	a.emit(real)
	if err != nil {
		return RepositoryView{}, errors.New(ev.Error)
	}
	vctx, vcancel := a.ctx(gitTimeout)
	defer vcancel()
	return a.resolveRepo(vctx, decl, a.roots(vctx), a.checkouts()), nil
}

// runClone clones src into a temporary folder in par and moves it to dest;
// on any failure it removes what it made.
func (a *API) runClone(ctx context.Context, job *cloneJob, src cloneSource, par, dest string, ev CloneEvent) error {
	tmp, err := os.MkdirTemp(par, ".flint-clone-")
	if err != nil {
		return err
	}
	moved := false
	defer func() {
		if !moved {
			_ = os.RemoveAll(tmp)
		}
	}()
	if a.clones.source != nil {
		a.clones.source(&src)
	}
	var last time.Time
	progress := func(line string) {
		if len(line) > maxProgressLen {
			line = strings.ToValidUTF8(line[:maxProgressLen], "")
		}
		a.clones.progress(job, line)
		if now := time.Now(); now.Sub(last) >= progressEvery {
			last = now
			ev.Progress = line
			a.host.Emit(EventCloneProgress, ev)
		}
	}
	if err := git.Clone(ctx, git.CloneOptions{URL: src.URL, Dir: tmp, Protocols: src.Protocols, Progress: progress}); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	// The destination may have been made meanwhile: still empty or absent.
	if err := checkDestination(dest); err != nil {
		return err
	}
	removed := false
	if _, err := os.Lstat(dest); err == nil {
		if err := os.Remove(dest); err != nil { // only an empty folder goes
			return err
		}
		removed = true
	}
	if err := os.Rename(tmp, dest); err != nil {
		if removed {
			_ = os.Mkdir(dest, 0o755)
		}
		return err
	}
	moved = true
	return nil
}

// errCloneTrusted refuses a clone for a project whose folder is not trusted
// into a folder that is: the clone would be trusted at once.
var errCloneTrusted = errors.New("choose a folder that is not inside a trusted folder: a clone there would be trusted at once, " +
	"and the URL comes from a project whose folder is in Restricted Mode")

// cloneTrust refuses a clone that would be trusted at once (destTrusted) for
// a project whose folder is not trusted.
func cloneTrust(projectTrusted, destTrusted bool) error {
	if destTrusted && !projectTrusted {
		return errCloneTrusted
	}
	return nil
}

// trustedBy says why a clone into dest (in parent) is trusted at once, ""
// when it is not.
func (a *API) trustedBy(parent, dest string) string {
	switch {
	case a.host.Trusted(parent):
		return "because it is inside the trusted folder " + parent
	case a.host.Trusted(dest):
		return "because you trusted the folder " + dest + " before"
	}
	return ""
}

// cloneMessage is Clone's question: what is cloned from where into where,
// and whether the new folder is trusted at once (inside a trusted folder) or
// in Restricted Mode; trustedBy says why it is trusted ("" when it is not).
func cloneMessage(project, repo, url, dest string, trusted bool, trustedBy string) string {
	note := ""
	if !trusted {
		note = " The project's folder is in Restricted Mode: check that you expect this URL."
	}
	after := "The new folder is in Restricted Mode until you trust it."
	if trustedBy != "" {
		after = fmt.Sprintf("The new folder WILL BE TRUSTED at once, %s: "+
			"agents, language servers and terminals there use the repository's own settings and hooks. "+
			"To clone it in Restricted Mode, cancel and choose a folder that is not trusted.", trustedBy)
	}
	return fmt.Sprintf("Clone the repository %q of the project %q?\n\nURL: %s\nInto: %s\n\n"+
		"The URL comes from the project's %s.%s git uses your own credentials (credential helper or SSH agent) and asks for nothing here. %s",
		repo, project, url, dest, ConfigFile, note, after)
}

// CancelClone stops the clone running for a project; what it made is removed.
func (a *API) CancelClone(folder string) error {
	real, _, err := a.known(folder)
	if err != nil {
		return err
	}
	if !a.clones.cancel(real) {
		return errors.New("no clone is running for this project")
	}
	return nil
}
