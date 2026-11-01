package main

import (
	"embed"
	"io/fs"
	"log"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"flint/internal/diag"
	"flint/internal/github"
	"flint/internal/shellenv"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	logger := diag.Setup()
	slog.Info("start", "version", diag.Version(version))
	shellenv.Load() // import the login-shell PATH in the background (git hooks, rg, brew)
	flint, err := NewApp()
	if err != nil {
		log.Fatalf("flint: %v", err)
	}
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatalf("flint: %v", err)
	}

	gitAPI := &GitAPI{app: flint}
	gh := github.New()
	lspAPI := &LSPAPI{app: flint}
	lspAPI.wire()
	app := application.New(application.Options{
		Name:   "Flint",
		Logger: logger,
		PanicHandler: func(p *application.PanicDetails) {
			slog.Error("panic", "error", p.Error, "stack", p.StackTrace)
		},
		Description: "A fast scratchpad IDE for code, SQL and data.",
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(dist)},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		SingleInstance: singleInstance(flint),
		// ⌘Q asks the frontend first, like closing the window (see beforeClose).
		ShouldQuit: func() bool { return !flint.beforeClose() },
		Services: []application.Service{
			application.NewService(&lifecycle{app: flint}), // first: the others use App.ctx
			application.NewService(&WorkspaceAPI{app: flint}),
			application.NewService(&SearchAPI{app: flint}),
			application.NewService(&DatabaseAPI{app: flint}),
			application.NewService(&ScratchAPI{app: flint}),
			application.NewService(&TerminalAPI{app: flint}),
			application.NewService(&RunAPI{app: flint}),
			application.NewService(&DebugAPI{app: flint}),
			application.NewService(&ProfileAPI{app: flint}),
			application.NewService(&AppAPI{app: flint}),
			application.NewService(&SessionAPI{app: flint}),
			application.NewService(gitAPI),
			application.NewService(&AgentAPI{app: flint, git: gitAPI}),
			application.NewService(lspAPI),
			application.NewService(&AgentChatAPI{app: flint}),
			application.NewService(&DiffAPI{}),
			application.NewService(&TodoAPI{app: flint}),
			application.NewService(flint.dockerAPI),
			application.NewService(&HTTPAPI{app: flint}),
			application.NewService(&PreviewAPI{app: flint}),
			application.NewService(&SettingsAPI{app: flint}),
			application.NewService(&GitHubAPI{app: flint, git: gitAPI, gh: gh}),
			application.NewService(&TaskAPI{app: flint, git: gitAPI, pr: gh}),
			application.NewService(newAlertsAPI(flint)),
			application.NewService(newAWSAPI(flint)),
			application.NewService(&KubeAPI{app: flint}),
			application.NewService(&DiagnosticsAPI{app: flint}),
			application.NewService(&UpdateAPI{app: flint}),
		},
	})
	flint.wails = app
	app.Menu.Set(flint.buildMenu())

	appearance, background := flint.startupAppearance()
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "flint",
		Width:            1400,
		Height:           900,
		MinWidth:         800,
		MinHeight:        500,
		BackgroundColour: background,
		// Folders and files dragged from Finder (see filesDropped).
		EnableFileDrop: true,
		Mac: application.MacWindow{
			// Traffic lights in a compact unified toolbar (38pt, like Xcode and
			// Safari) that Flint's own title bar is drawn under.
			TitleBar: application.MacTitleBar{
				AppearsTransparent:   true,
				HideTitle:            true,
				FullSizeContent:      true,
				UseToolbar:           true,
				HideToolbarSeparator: true,
				ToolbarStyle:         application.MacToolbarStyleUnifiedCompact,
			},
			Appearance: appearance,
		},
	})
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if flint.beforeClose() {
			e.Cancel()
		}
	})
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		flint.emit(eventFilesDropped, droppedPaths(e.Context().DroppedFiles()))
	})
	flint.window = win

	if err := app.Run(); err != nil {
		log.Fatalf("flint: %v", err)
	}
}
