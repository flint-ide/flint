package workflows

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
)

// A schedule starts a run with nobody watching, so it runs only what the
// user saw when they saved it. A repository's workflow file overrides a
// built-in or personal one of the same name, and the checkout's
// .flint/run.json says what a script step runs: either can change after the
// schedule was saved. So the scheduler keeps, per schedule, a fingerprint of
// the resolved workflow (every step's id, kind, agent, run configuration,
// on-fail and text) and of the resolved launch of each script step's run
// configuration (programs, arguments, working folder, environment), taken
// the first time it sees the schedule after it was saved or changed. When
// the schedule is due and the fingerprint differs, it starts nothing and
// says so; AcceptSchedule (Settings ▸ Workflows) takes the new one.

// stateSchedules is the state.json key of the schedules' fingerprints.
const stateSchedules = "workflowSchedules"

// errScheduleChanged is the skip of a schedule whose workflow changed.
var errScheduleChanged = errors.New("the workflow or a run configuration it uses changed since this schedule was saved: open Settings ▸ Workflows and save the schedule again to accept it")

// scheduleTrust is what the scheduler accepted for one schedule: Entry is
// a hash of the schedule's own settings entry, Steps the fingerprint of
// what it runs ("" while it could not be taken yet).
type scheduleTrust struct {
	Entry string `json:"entry"`
	Steps string `json:"steps"`
}

// entryHash identifies a schedule's settings entry: a change of any field
// is a new schedule, fingerprinted again.
func entryHash(sc Schedule) string {
	raw, _ := json.Marshal(sc)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// fingerprint is what a run of sc would take now: its workflow's resolved
// steps, and for each script step the launch of its run configuration as
// the checkout resolves it. It changes nothing and starts nothing.
func (a *API) fingerprint(sc Schedule) (string, error) {
	root, dir, err := a.workspaceDir(sc.Folder)
	if err != nil {
		return "", err
	}
	_, steps, err := a.startable(root, sc.Workflow)
	if err != nil {
		return "", err
	}
	ctx, cancel := a.ctx(requestTimeout)
	repo, err := a.host.Repository(ctx, dir)
	cancel()
	if err != nil {
		return "", fmt.Errorf("%s is not in a git repository: %w", dir, err)
	}
	h := sha256.New()
	field := func(parts ...string) {
		for _, p := range parts {
			fmt.Fprintf(h, "%d:%s;", len(p), p)
		}
		h.Write([]byte{'\n'})
	}
	field("checkout", repo.Root)
	for _, st := range steps {
		field("step", st.ID, string(st.Kind), st.Agent, st.Run, st.OnFail, st.Text)
		if st.Kind != KindScript {
			continue
		}
		l, err := a.host.RunLaunch(repo.Root, st.Run)
		if err != nil {
			return "", fmt.Errorf("the step %q: %w", st.Title, err)
		}
		field("cwd", l.Cwd)
		for _, c := range l.Commands {
			field(append([]string{"command", c.Program}, c.Args...)...)
		}
		field(append([]string{"env"}, l.Env...)...)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// loadTrust reads the accepted fingerprints (once).
func (s *scheduler) loadTrust() {
	s.mu.Lock()
	loaded := s.trust != nil
	s.mu.Unlock()
	if loaded {
		return
	}
	stored := map[string]scheduleTrust{}
	_ = s.a.host.LoadState(stateSchedules, &stored)
	s.mu.Lock()
	if s.trust == nil {
		s.trust = stored
	}
	s.mu.Unlock()
}

// syncTrust takes the fingerprint of every schedule that is new or whose
// settings entry changed since it was taken, and forgets the schedules
// that are gone. It runs after the settings changed and when folders open,
// so a schedule is fingerprinted as soon as it can be (its folder open).
func (s *scheduler) syncTrust(list []Schedule) {
	s.loadTrust()
	s.mu.Lock()
	trust := s.trust
	s.mu.Unlock()
	next := map[string]scheduleTrust{}
	for _, sc := range list {
		t, ok := trust[sc.Name]
		entry := entryHash(sc)
		if !ok || t.Entry != entry || t.Steps == "" {
			t = scheduleTrust{Entry: entry}
			if fp, err := s.a.fingerprint(sc); err == nil {
				t.Steps = fp
			}
		}
		next[sc.Name] = t
	}
	s.storeTrust(next)
}

// sync fingerprints the schedules of the settings now, in the background
// (it reads files and asks git): after Start, a change of the settings and
// a change of the open folders. A due schedule waits for it (runSchedule).
func (s *scheduler) sync() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.syncing.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.syncing.Done()
		s.syncMu.Lock()
		defer s.syncMu.Unlock()
		s.syncTrust(Section.Get(s.a.host.Settings()).Schedules)
	}()
}

// trustState is the scheduler's part about fingerprints.
type trustState struct {
	// trust is what was accepted per schedule (nil until loaded);
	// syncing counts the syncs in flight, syncMu runs one at a time.
	trust   map[string]scheduleTrust
	syncing sync.WaitGroup
	syncMu  sync.Mutex
}

// storeTrust replaces the accepted fingerprints and saves them.
func (s *scheduler) storeTrust(next map[string]scheduleTrust) {
	s.mu.Lock()
	s.trust = next
	s.mu.Unlock()
	if err := s.a.host.SaveState(stateSchedules, next); err != nil {
		slog.Warn("workflows: save the schedules' fingerprints", "error", err)
	}
}

// checkTrust is nil when what sc would run now is what was accepted for it.
// A schedule seen for the first time (its folder was never open since it
// was saved) is accepted now.
func (s *scheduler) checkTrust(sc Schedule) error {
	s.loadTrust()
	fp, err := s.a.fingerprint(sc)
	if err != nil {
		return nil // startRun refuses it with the same reason
	}
	s.mu.Lock()
	t, ok := s.trust[sc.Name]
	s.mu.Unlock()
	entry := entryHash(sc)
	switch {
	case ok && t.Entry == entry && t.Steps == fp:
		return nil
	case ok && t.Entry == entry && t.Steps != "":
		return errScheduleChanged
	}
	s.accept(sc.Name, scheduleTrust{Entry: entry, Steps: fp})
	return nil
}

// accept stores t as what schedule name runs.
func (s *scheduler) accept(name string, t scheduleTrust) {
	s.mu.Lock()
	next := make(map[string]scheduleTrust, len(s.trust)+1)
	for k, v := range s.trust {
		next[k] = v
	}
	s.mu.Unlock()
	next[name] = t
	s.storeTrust(next)
}

// AcceptSchedule accepts what the schedule called name runs now (its
// workflow and the run configurations it uses, as they are), after the user
// was told they changed since the schedule was saved. It fails when the
// schedule does not exist or what it runs cannot be read now (its folder is
// not open or not trusted, the workflow cannot run).
func (a *API) AcceptSchedule(name string) error {
	if !ValidName(name) {
		return errors.New("choose a schedule")
	}
	list := Section.Get(a.host.Settings()).Schedules
	i := slices.IndexFunc(list, func(sc Schedule) bool { return sc.Name == name })
	if i < 0 {
		return fmt.Errorf("there is no schedule named %q", name)
	}
	sc := list[i]
	if !a.host.Trusted(filepath.Clean(sc.Folder)) {
		return fmt.Errorf("%s is in Restricted Mode", sc.Folder)
	}
	fp, err := a.fingerprint(sc)
	if err != nil {
		return fmt.Errorf("the schedule %q cannot be accepted now: %w", name, err)
	}
	a.sched.loadTrust()
	a.sched.accept(name, scheduleTrust{Entry: entryHash(sc), Steps: fp})
	a.sched.clearChanged(name)
	return nil
}

// clearChanged forgets that schedule name was last skipped because what it
// runs changed (it was accepted since).
func (s *scheduler) clearChanged(name string) {
	s.mu.Lock()
	o, ok := s.last[name]
	if ok && o.skipped == errScheduleChanged.Error() {
		o.skipped = "accepted: it runs at its next time"
		s.last[name] = o
	}
	s.mu.Unlock()
	s.a.host.Emit(EventChanged, "")
}
