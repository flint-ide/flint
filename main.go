package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"

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
		Menu:             menu.NewMenuFromItems(menu.AppMenu(), menu.EditMenu(), menu.WindowMenu()),
		BackgroundColour: &options.RGBA{R: 24, G: 25, B: 28, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		Bind: []any{
			&WorkspaceAPI{app: app},
			&SearchAPI{app: app},
			&DatabaseAPI{app: app},
			&ScratchAPI{app: app},
			&TerminalAPI{app: app},
			&AppAPI{app: app},
			gitAPI,
			&AgentAPI{app: app, git: gitAPI},
			lspAPI,
			&AgentChatAPI{app: app},
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
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
