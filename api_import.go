package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"

	"flint/internal/settings"
	"flint/internal/vscodeimport"
)

// importHome is the home directory the importer looks in (tests replace it).
var importHome = os.UserHomeDir

// ImportAPI imports settings, keybindings and the color theme from VS Code,
// Cursor or VSCodium. Preview only reads; Apply saves the previewed plan
// through the normal settings path once the user confirms it.
type ImportAPI struct {
	app *App

	mu   sync.Mutex
	plan *vscodeimport.Plan // the last preview, until applied
}

// Sources lists the VS Code-family editors found on this Mac.
func (i *ImportAPI) Sources() ([]vscodeimport.Source, error) {
	home, err := importHome()
	if err != nil {
		return nil, fmt.Errorf("find the home folder: %w", err)
	}
	return vscodeimport.Sources(home), nil
}

// Preview reads the editor's files and returns what an import would change
// and what it would skip. Nothing is written.
func (i *ImportAPI) Preview(sourceID string) (vscodeimport.Plan, error) {
	home, err := importHome()
	if err != nil {
		return vscodeimport.Plan{}, fmt.Errorf("find the home folder: %w", err)
	}
	src, ok := vscodeimport.FindSource(home, sourceID)
	if !ok {
		return vscodeimport.Plan{}, fmt.Errorf("unknown or missing editor %q", sourceID)
	}
	plan := vscodeimport.BuildPlan(src, i.app.currentSettings())
	plan.ID = randomID()
	i.mu.Lock()
	i.plan = &plan
	i.mu.Unlock()
	i.app.logf("%s", plan)
	return plan, nil
}

// Apply saves the previewed plan with this id onto the current settings and
// returns what was written. A plan is applied at most once; when saving fails
// it stays, so the user can fix the cause and click Import again.
func (i *ImportAPI) Apply(planID string) (settings.Settings, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	plan := i.plan
	if plan == nil || planID == "" || plan.ID != planID {
		return settings.Settings{}, errors.New("this import preview has expired; open Import from VS Code again")
	}
	saved, err := i.app.updateSettings(plan.Apply)
	if err != nil {
		return settings.Settings{}, err
	}
	i.plan = nil
	return saved, nil
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return hex.EncodeToString(b)
}
