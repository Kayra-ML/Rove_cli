package main

import (
	"embed"
	"log"

	"github.com/Kayra-ML/rove/internal/config"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	cfg, err := config.Load("")
	if err != nil {
		log.Fatal(err)
	}
	app := NewApp(cfg)
	err = wails.Run(&options.App{
		Title:            "Rove Code",
		Width:            1440,
		Height:           900,
		MinWidth:         960,
		MinHeight:        640,
		BackgroundColour: &options.RGBA{R: 14, G: 16, B: 20, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		Bind:             []interface{}{app},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarHiddenInset(),
			Appearance: mac.NSAppearanceNameDarkAqua,
			About: &mac.AboutInfo{
				Title:   "Rove Code",
				Message: "Local-first coding agent",
				Icon:    appIcon,
			},
		},
		Windows: &windows.Options{Theme: windows.Dark},
		Linux: &linux.Options{
			Icon:        appIcon,
			ProgramName: "rovecode",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
