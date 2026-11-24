package main

import (
	_ "embed"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"
)

// Closing the window keeps the app in the notification area, unless the
// settings say that it quits: the window hides, and the icon there brings
// it back, or quits. Quitting, as from the icon's menu or for an update,
// still closes the window.

//go:embed resources/tray.png
var trayIcon []byte

// trayState is the icon in the notification area, and what it needs.
type trayState struct {
	win      *mygo.Window
	tray     *mygo.Tray
	quitting bool // the app is quitting: the window closes for good
}

// staysInTray reports whether closing the window hides it: the settings
// say so, and the app is not quitting.
func staysInTray(s settings, quitting bool) bool {
	return !s.CloseQuits && !quitting
}

// watchWindow makes win close into the notification area while the
// settings say so. It runs once, as the window is made.
func (a *app) watchWindow(win *mygo.Window) {
	a.tray.win = win
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { a.tray.quitting = true })
	win.OnClose(func(e *mygo.CloseEvent) {
		// Without the icon there is no way back to a hidden window.
		if staysInTray(a.settings, a.tray.quitting) && a.tray.tray != nil {
			e.PreventDefault()
			win.Hide()
		}
	})
	a.syncTray()
}

// showWindow brings the window back to the front, from the notification
// area or from the taskbar: for a click on the icon, and for the app being
// started again while it runs.
func (a *app) showWindow() {
	win := a.tray.win
	if win == nil {
		return // not made yet
	}
	if win.IsMinimized() {
		win.Restore()
	}
	win.Show()
	win.Focus()
}

// syncTray shows the icon in the notification area while closing the window
// keeps the app there, and takes it away otherwise.
func (a *app) syncTray() {
	t := &a.tray
	if t.win == nil {
		return // in tests: there is no window
	}
	if a.settings.CloseQuits {
		if t.tray != nil {
			t.tray.Destroy()
			t.tray = nil
		}
		return
	}
	if t.tray != nil {
		return
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: trayIcon, ToolTip: "Atelier"})
	if err != nil {
		log.Printf("tray: %v", err)
		return
	}
	t.tray = tray
	show := a.showWindow
	// A click brings the window back; the menu comes for the other button
	// alone: an icon with a menu set shows it for every click.
	tray.OnClick(show)
	tray.OnRightClick(func() {
		tray.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Label: "打开 Atelier", Click: func(*mygo.MenuItem, *mygo.Window) { show() }},
			mygo.Separator(),
			{Label: "退出", Role: mygo.RoleQuit},
		}))
		tray.PopUpMenu()
		tray.SetMenu(nil)
	})
}

// traySettings say what closing the window does.
func (a *app) traySettings(c *ui.Context, row func(name, note string, width float32, control func())) {
	row("关闭窗口时留在托盘", "点关闭只是把窗口收起来，应用还在运行；点托盘里的图标打开，右键图标可以退出", 0, func() {
		stay := !a.settings.CloseQuits
		if mallow.Switch(c.Key("tray"), &stay, "").Label("留在托盘").Changed() {
			a.settings.CloseQuits = !stay
			a.saveSettings()
			a.syncTray()
		}
	})
}
