package main

import (
	"context"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The few places Flint talks to the Wails runtime, so the bound APIs only see
// App methods: emit, window title and background, dialogs, quit.

// lifecycle is registered as a Wails service to start and stop the App with the
// application (it binds no methods).
type lifecycle struct{ app *App }

func (l *lifecycle) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	l.app.startup(ctx)
	return nil
}

func (l *lifecycle) ServiceShutdown() error {
	l.app.shutdown()
	return nil
}

// emit sends an event to the frontend (a no-op before the application runs)
// and to Flint Remote's open streams.
func (a *App) emit(name string, data ...any) {
	a.remoteFanout(name, data)
	if a.wails == nil || a.ctx == nil {
		return
	}
	a.wails.Event.Emit(name, data...)
}

func (a *App) setWindowTitle(title string) {
	if a.window != nil {
		a.window.SetTitle(title)
	}
}

func (a *App) setWindowBackground(c application.RGBA) {
	if a.window != nil {
		a.window.SetBackgroundColour(c)
	}
}

// secondLaunch handles `flint <folder>` while Flint already runs: the folder is
// opened (like Open Recent) and the window comes to the front.
func (a *App) secondLaunch(args []string, cwd string) {
	if folder := folderFromArgs(args[min(1, len(args)):], cwd); folder != "" {
		a.emit(eventMenuOpenFolder, folder)
	}
	if a.window != nil {
		a.window.UnMinimise()
		a.window.Show()
		a.window.Focus()
	}
}

// quit closes the application (the quit handshake has already run).
func (a *App) quit() {
	if a.wails != nil {
		a.wails.Quit()
	}
}

func (a *App) logf(format string, args ...any) { log.Printf("flint: "+format, args...) }

func (a *App) copyText(text string) {
	if a.wails != nil {
		a.wails.Clipboard.SetText(text)
	}
}

// openURL opens an http(s) link in the default browser.
func (a *App) openURL(link string) error {
	if a.wails == nil {
		return nil
	}
	return a.wails.Browser.OpenURL(link)
}

// pickFolder shows the native folder picker; "" when cancelled.
func (a *App) pickFolder(title string) (string, error) {
	return a.wails.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title:                title,
		CanChooseDirectories: true,
		CanCreateDirectories: true,
		Window:               a.window,
	}).PromptForSingleSelection()
}

// pickFile shows the native file picker; "" when cancelled.
func (a *App) pickFile(title string, filters ...application.FileFilter) (string, error) {
	return a.wails.Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title:          title,
		CanChooseFiles: true,
		Filters:        filters,
		Window:         a.window,
	}).PromptForSingleSelection()
}

// pickSaveFile asks where to save a file; "" when cancelled.
func (a *App) pickSaveFile(title, filename string) (string, error) {
	return a.wails.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:                title,
		Filename:             filename,
		CanCreateDirectories: true,
		Window:               a.window,
	}).PromptForSingleSelection()
}

// eventFilesDropped carries folders and files dragged into the window.
const eventFilesDropped = "files:dropped"

// DroppedPath is one path dragged from Finder.
type DroppedPath struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
}

// droppedPaths tells folders from files; paths that vanished are left out.
func droppedPaths(paths []string) []DroppedPath {
	out := make([]DroppedPath, 0, len(paths))
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		out = append(out, DroppedPath{Path: p, Dir: info.IsDir()})
	}
	return out
}
