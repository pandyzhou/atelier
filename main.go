// Atelier is a personal all-in-one desktop app in MyGo native UI. Each
// feature is a package under internal/features, registered in features().
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/pandyzhou/atelier/internal/feature"
	"github.com/pandyzhou/atelier/internal/features/weekly"
	"github.com/pandyzhou/atelier/internal/store"
	"github.com/pandyzhou/atelier/internal/theme"
)

// features lists the app's features in sidebar order. Add new ones here.
func features(s *store.Store) []feature.Feature {
	return []feature.Feature{
		weekly.New(s),
	}
}

// settings are the shell's own preferences, saved as settings.json.
type settings struct {
	// Appearance is a theme.Mode: "system", "light" or "dark".
	Appearance string `json:"appearance"`
}

// app is the shell: a sidebar of features and the chosen one's page.
type app struct {
	store    *store.Store
	settings settings
	features []feature.Feature
	selected string // the ID of the feature shown
}

func newApp(s *store.Store) *app {
	a := &app{store: s, features: features(s), settings: settings{Appearance: string(theme.ModeSystem)}}
	if err := s.Load("settings", &a.settings); err != nil {
		log.Printf("settings: load: %v", err)
	}
	if len(a.features) > 0 {
		a.selected = a.features[0].ID()
	}
	return a
}

func (a *app) saveSettings() {
	if err := a.store.Save("settings", a.settings); err != nil {
		log.Printf("settings: save: %v", err)
	}
}

func (a *app) current() feature.Feature {
	for _, f := range a.features {
		if f.ID() == a.selected {
			return f
		}
	}
	return nil
}

func (a *app) view(c *ui.Context) {
	// Before SetTheme, the frame's theme is MyGo's default, which follows the
	// system's appearance as it changes.
	theme.Use(theme.PaletteFor(theme.Mode(a.settings.Appearance), c.Theme().Dark))
	c.SetTheme(theme.Theme())
	ui.Row(c).Fill().AlignItems(ui.Stretch).Padding(24, 0, 24, 24).Children(func() {
		// The sidebar is a raised panel, set apart from the page beside it.
		side := ui.Column(c).Width(184).Shrink(0).Padding(28, 16).Gap(24).Radius(theme.RadiusCard)
		theme.Raised(side, theme.Large).Children(func() {
			ui.Column(c).PaddingX(12).Gap(4).Children(func() {
				ui.Text(c, "Atelier").FontSize(20).FontWeight(600).TextColor(theme.Text)
				ui.Text(c, "个人工作室").FontSize(12).TextColor(theme.Muted)
			})
			theme.Groove(c).MarginX(4)
			ui.Column(c).Grow(1).Gap(12).Role(ui.RoleTabList).Label("功能").Children(func() {
				for _, f := range a.features {
					a.navItem(c.Key(f.ID()), f)
				}
			})
			ui.Column(c).Gap(10).Children(func() {
				ui.Text(c, "外观").PaddingX(8).FontSize(12).FontWeight(500).TextColor(theme.Muted)
				if theme.Segmented(c, "外观", &a.settings.Appearance, []theme.Option{
					{Value: string(theme.ModeLight), Label: "浅色"},
					{Value: string(theme.ModeDark), Label: "深色"},
					{Value: string(theme.ModeSystem), Label: "系统"},
				}) {
					a.saveSettings()
				}
			})
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			if f := a.current(); f != nil {
				f.View(c.Key(f.ID()))
			}
		})
	})
}

// navItem is a feature in the sidebar: recessed while chosen, raised a little
// while hovered, flat otherwise.
func (a *app) navItem(c *ui.Context, f feature.Feature) {
	chosen := f.ID() == a.selected
	item := ui.ButtonBase(c).Role(ui.RoleTab).Label(f.Title()).AccessSelected(chosen).
		Padding(12, 16).Radius(theme.RadiusControl).Cursor(ui.CursorPointer)
	switch {
	case chosen:
		theme.Recessed(item, theme.Small)
	case item.Hovered():
		theme.Raised(item, theme.Small)
	}
	if item.Clicked() {
		a.selected = f.ID()
	}
	theme.Soft(item).Children(func() {
		label := ui.Text(c, f.Title()).FontSize(14).TextColor(theme.Muted)
		if chosen {
			label.TextColor(theme.Accent).FontWeight(500)
		}
	})
}

func main() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("no data directory, nothing will be saved: %v", err)
	}
	a := newApp(store.Open(dir))
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:     "Atelier",
			Width:     1280,
			Height:    800,
			MinWidth:  860,
			MinHeight: 520,
			StateKey:  "main",
			Content:   ui.View(a.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
