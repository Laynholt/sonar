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
	err := wails.Run(&options.App{
		Title:            "sonar",
		Width:            900,
		Height:           640,
		MinWidth:         720,
		MinHeight:        520,
		Frameless:        true,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 255},
		Bind:             []interface{}{&App{}},
	})
	if err != nil {
		log.Fatal("Error:", err.Error())
	}
}
