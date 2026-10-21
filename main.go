package main

import (
	"embed"
	"log"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app, err := NewApp()
	if err != nil {
		log.Fatal("Error:", err.Error())
	}
	err = wails.Run(&options.App{
		Title:            "sonar",
		Width:            1024,
		Height:           600,
		MinWidth:         1024,
		MinHeight:        600,
		Frameless:        runtime.GOOS != "darwin",
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		Mac:              &mac.Options{TitleBar: mac.TitleBarHiddenInset()},
	})
	if err != nil {
		log.Fatal("Error:", err.Error())
	}
}
