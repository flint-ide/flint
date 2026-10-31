package main

import (
	"context"
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

	"flint/internal/github"
	"flint/internal/shellenv"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	shellenv.Load() // import the login-shell PATH in the background (git hooks, rg, brew)
	app, err := NewApp()
	if err != nil {
		log.Fatalf("flint: %v", err)
	}

	appearance, background := app.startupAppearance()
	gitAPI := &GitAPI{app: app}
	lspAPI := &LSPAPI{app: app}
	lspAPI.wire()
	err = wails.Run(&options.App{
		Title:     "flint",
		Width:     1400,
		Height:    900,
		MinWidth:  800,
		MinHeight: 500,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// Without an Edit menu macOS webviews ignore Cmd+C / Cmd+V / Cmd+Z.
		Menu:             app.buildMenu(),
		BackgroundColour: background,
		OnStartup:        app.startup,
		OnDomReady:       func(context.Context) { styleNativeWindow(); addSettingsMenuItem() },
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		Bind: []any{
			&WorkspaceAPI{app: app},
			&SearchAPI{app: app},
			&DatabaseAPI{app: app},
			&ScratchAPI{app: app},
			&TerminalAPI{app: app},
			&RunAPI{app: app},
			&DebugAPI{app: app},
			&ProfileAPI{app: app},
			&AppAPI{app: app},
			&SessionAPI{app: app},
			gitAPI,
			&AgentAPI{app: app, git: gitAPI},
			lspAPI,
			&AgentChatAPI{app: app},
			&DiffAPI{},
			&TodoAPI{app: app},
			app.dockerAPI,
			&HTTPAPI{app: app},
			&PreviewAPI{app: app},
			&SettingsAPI{app: app},
			&GitHubAPI{app: app, git: gitAPI, gh: github.New()},
		},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarHiddenInset(),
			Appearance: appearance,
			About: &mac.AboutInfo{
				Title:   "Flint",
				Message: "A fast scratchpad IDE for code, SQL and data.",
			},
		},
	})
	if err != nil {
		log.Fatalf("flint: %v", err)
	}
}
