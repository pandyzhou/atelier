// Atelier is a personal all-in-one desktop app in MyGo native UI, drawn with
// the mallow component library. Each feature is a package under
// internal/features, registered in features().
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"

	"github.com/pandyzhou/atelier/internal/feature"
	"github.com/pandyzhou/atelier/internal/features/weekly"
	"github.com/pandyzhou/atelier/internal/store"
)

// features lists the app's features in sidebar order. Add new ones here.
func features(s *store.Store) []feature.Feature {
	return []feature.Feature{
		weekly.New(s),
	}
}

// settings are the shell's own preferences, saved as settings.json.
type settings struct {
	// Appearance is a mallow.Mode: "system", "light" or "dark".
	Appearance string `json:"appearance"`
}

// app is the shell: a title bar, a sidebar of features and the chosen one's
// page.
type app struct {
	store    *store.Store
	settings settings
	features []feature.Feature
	selected string        // the ID of the feature shown
	win      mallow.Window // the window, for the title bar's buttons; nil in tests
}

func newApp(s *store.Store) *app {
	a := &app{store: s, features: features(s), settings: settings{Appearance: string(mallow.ModeSystem)}}
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
	mallow.UseMode(c, mallow.Mode(a.settings.Appearance))
	ui.Column(c).Fill().Children(func() {
		mallow.TitleBar(c, a.win, nil)
		a.body(c)
	})
}

// body is the sidebar and the chosen feature's page, under the title bar.
func (a *app) body(c *ui.Context) {
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Padding(4, 0, 24, 24).Children(func() {
		// The sidebar is a raised panel, set apart from the page beside it.
		mallow.Card(c, mallow.Large).Width(184).Shrink(0).Padding(28, 16).Gap(24).Children(func() {
			ui.Column(c).PaddingX(12).Gap(4).Children(func() {
				ui.Text(c, "Atelier").FontSize(20).FontWeight(600).TextColor(mallow.Text)
				ui.Text(c, "个人工作室").FontSize(12).TextColor(mallow.Muted)
			})
			mallow.Groove(c).MarginX(4)
			ui.Column(c).Grow(1).Gap(12).Role(ui.RoleTabList).Label("功能").Children(func() {
				for _, f := range a.features {
					if mallow.NavItem(c.Key(f.ID()), f.Title(), f.ID() == a.selected).Clicked() {
						a.selected = f.ID()
					}
				}
			})
			ui.Column(c).Gap(10).Children(func() {
				mallow.Label(c, "外观").PaddingX(8)
				if mallow.Segmented(c, "外观", &a.settings.Appearance, []mallow.Option{
					{Value: string(mallow.ModeLight), Label: "浅色"},
					{Value: string(mallow.ModeDark), Label: "深色"},
					{Value: string(mallow.ModeSystem), Label: "系统"},
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

func main() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("no data directory, nothing will be saved: %v", err)
	}
	a := newApp(store.Open(dir))
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "Atelier",
			Width:     1280,
			Height:    800,
			MinWidth:  860,
			MinHeight: 520,
			StateKey:  "main",
			// mallow.TitleBar draws the title bar and its buttons; the window
			// keeps its edges, which resize it, and its shadow.
			TitleBarStyle:     mygo.TitleBarHidden,
			HideWindowButtons: true,
			Content:           ui.View(a.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
