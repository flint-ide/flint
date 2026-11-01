package vscodeimport

import (
	"encoding/json"
	"fmt"
	"strings"
)

// maxKeybindings bounds how many keybindings.json entries are looked at.
const maxKeybindings = 2000

// commandMap maps VS Code command ids to Flint command ids that do the same
// thing. Only Flint commands whose shortcut the keymap may change are listed
// (not the ones CodeMirror owns, such as Save or Find in File).
var commandMap = map[string]string{
	"workbench.action.showCommands":                       "app.commandPalette",
	"workbench.action.openSettings":                       "app.settings",
	"workbench.action.openSettings2":                      "app.settings",
	"workbench.action.openGlobalSettings":                 "app.settings",
	"workbench.action.quickOpen":                          "nav.goToFile",
	"workbench.action.findInFiles":                        "nav.findInFiles",
	"workbench.view.search":                               "nav.findInFiles",
	"workbench.action.replaceInFiles":                     "nav.replaceInFiles",
	"workbench.action.navigateBack":                       "nav.back",
	"workbench.action.navigateForward":                    "nav.forward",
	"workbench.action.closeActiveEditor":                  "editor.closeTab",
	"workbench.action.terminal.toggleTerminal":            "terminal.toggle",
	"workbench.action.togglePanel":                        "terminal.toggle",
	"workbench.action.terminal.new":                       "terminal.new",
	"workbench.action.toggleSidebarVisibility":            "view.toggleSidebar",
	"workbench.view.explorer":                             "view.files",
	"workbench.view.scm":                                  "view.git",
	"workbench.action.openRecent":                         "project.recent",
	"workbench.action.files.openFolder":                   "project.openFolder",
	"workbench.action.addRootFolder":                      "project.addToWorkspace",
	"git.commit":                                          "git.commit",
	"git.push":                                            "git.push",
	"git.pull":                                            "git.pull",
	"git.fetch":                                           "git.fetch",
	"git.refresh":                                         "git.refresh",
	"git.checkout":                                        "git.branches",
	"git.branch":                                          "git.newBranch",
	"git.stash":                                           "git.stash",
	"workbench.action.debug.start":                        "debug.selected",
	"workbench.action.debug.run":                          "run.selected",
	"workbench.action.debug.stop":                         "debug.stop",
	"workbench.action.debug.continue":                     "debug.resume",
	"workbench.action.debug.stepOver":                     "debug.stepOver",
	"workbench.action.debug.stepInto":                     "debug.stepInto",
	"workbench.action.debug.stepOut":                      "debug.stepOut",
	"workbench.action.debug.pause":                        "debug.pause",
	"editor.debug.action.toggleBreakpoint":                "debug.toggleBreakpoint",
	"workbench.debug.viewlet.action.removeAllBreakpoints": "debug.clearBreakpoints",
	"testing.runCurrentFile":                              "test.file",
	"testing.reRunFailTests":                              "test.rerunFailed",
	"testing.runAtCursor":                                 "run.context",
	"markdown.showPreview":                                "preview.toggleMarkdown",
	"markdown.showPreviewToSide":                          "preview.markdownSide",
}

// keybinding is one keybindings.json entry.
type keybinding struct {
	Key     string          `json:"key"`
	Command string          `json:"command"`
	When    string          `json:"when"`
	Args    json.RawMessage `json:"args"`
}

// mapKeybindings turns keybindings.json entries into Flint keymap overrides
// (command id → shortcut). VS Code applies entries in order, so a later entry
// for the same Flint command replaces an earlier one; Flint keeps one
// shortcut per command.
func mapKeybindings(entries []keybinding) (map[string]string, []Item, []Skipped) {
	keymap := map[string]string{}
	var items []Item
	var skipped []Skipped
	index := map[string]int{} // Flint id → position in items
	skip := func(e keybinding, reason string) {
		skipped = append(skipped, Skipped{Kind: KindKeybinding, Name: describe(e), Reason: reason})
	}
	for i, e := range entries {
		if i >= maxKeybindings {
			skipped = append(skipped, Skipped{Kind: KindKeybinding, Name: fmt.Sprintf("%d more", len(entries)-i), Reason: fmt.Sprintf("only the first %d keybindings are read", maxKeybindings)})
			break
		}
		cmd := strings.TrimSpace(e.Command)
		flint, known := commandMap[cmd]
		switch {
		case strings.HasPrefix(cmd, "-"):
			skip(e, "removes a VS Code default shortcut; Flint's defaults are different")
			continue
		case e.When != "":
			skip(e, "only applies when "+e.When+"; Flint shortcuts have no conditions")
			continue
		case len(e.Args) > 0 && string(e.Args) != "null":
			skip(e, "passes arguments to the command, which Flint can't")
			continue
		case !known:
			skip(e, "no matching Flint command")
			continue
		}
		keys, err := ConvertKey(e.Key)
		if err != nil {
			skip(e, err.Error())
			continue
		}
		item := Item{Kind: KindKeybinding, Name: cmd, Value: keys, Target: flint}
		if at, ok := index[flint]; ok {
			prev := items[at]
			skipped = append(skipped, Skipped{Kind: KindKeybinding, Name: prev.Name + " (" + prev.Value + ")", Reason: "replaced by a later shortcut for the same command; Flint keeps one per command"})
			items = append(items[:at], items[at+1:]...)
			for id, j := range index {
				if j > at {
					index[id] = j - 1
				}
			}
		}
		index[flint] = len(items)
		items = append(items, item)
		keymap[flint] = keys
	}
	return keymap, items, skipped
}

func describe(e keybinding) string {
	if e.Key == "" {
		return e.Command
	}
	return e.Command + " (" + e.Key + ")"
}
