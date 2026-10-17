package main

import (
	"embed"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	// macOS では ⌘Q や入力欄での ⌘C/⌘V のためにアプリメニューが必要
	isMac := goruntime.GOOS == "darwin"
	appMenu := menu.NewMenu()
	if isMac {
		appMenu.Append(menu.AppMenu())
		appMenu.Append(menu.EditMenu())
	}

	// 設定・ヘルプ・スポンサーへの入口。画面側は menu:* イベントでダイアログを開く。
	// Windows では端末のキーとぶつからないよう、ショートカットはフロント側(Ctrl+Shift+...)に任せる
	emit := func(name string) menu.Callback {
		return func(_ *menu.CallbackData) { wruntime.EventsEmit(app.ctx, name) }
	}
	acc := func(key string) *keys.Accelerator {
		if !isMac {
			return nil
		}
		return keys.CmdOrCtrl(key)
	}
	settingsMenu := appMenu.AddSubmenu("Settings")
	settingsMenu.AddText("Appearance…", acc(","), emit("menu:appearance"))
	settingsMenu.AddText("Keyboard Shortcuts", acc("/"), emit("menu:shortcuts"))
	helpMenu := appMenu.AddSubmenu("Help")
	helpMenu.AddText("About jterm", nil, emit("menu:about"))
	helpMenu.AddText("Check for Updates…", nil, emit("menu:update"))
	helpMenu.AddText("Sponsor jojonki ♥", nil, func(_ *menu.CallbackData) {
		wruntime.BrowserOpenURL(app.ctx, "https://github.com/sponsors/jojonki")
	})

	err := wails.Run(&options.App{
		Menu:      appMenu,
		Title:     "jterm",
		Width:     1280,
		Height:    800,
		MinWidth:  640,
		MinHeight: 400,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// 背景の透過度設定を効かせるため、WebView を透過させる(ブラーが入る WindowIsTranslucent / Acrylic は使わない)
		BackgroundColour: &options.RGBA{R: 30, G: 30, B: 30, A: 0},
		Mac: &mac.Options{
			WebviewIsTransparent: true,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: true,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind:       []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
