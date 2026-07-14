package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	desktopApp, err := NewDesktopApp()
	if err != nil {
		log.Fatal(err)
	}

	err = wails.Run(&options.App{
		Title:            "Mini Proxy",
		Width:            1180,
		Height:           760,
		MinWidth:         980,
		MinHeight:        680,
		BackgroundColour: &options.RGBA{R: 18, G: 24, B: 27, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  desktopApp.Startup,
		OnShutdown: desktopApp.Shutdown,
		Bind: []interface{}{
			desktopApp,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
