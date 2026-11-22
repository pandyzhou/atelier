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

// app is the shell: a sidebar of features and the chosen one's page.
type app struct {
	features []feature.Feature
	selected string // the ID of the feature shown
}

func newApp(s *store.Store) *app {
	a := &app{features: features(s)}
	if len(a.features) > 0 {
		a.selected = a.features[0].ID()
	}
	return a
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
	c.SetTheme(theme.Theme())
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(208).Shrink(0).Padding(32, 20).Gap(32).Children(func() {
			ui.Column(c).PaddingX(12).Gap(4).Children(func() {
				ui.Text(c, "Atelier").FontSize(20).FontWeight(600).TextColor(theme.Text)
				ui.Text(c, "个人工作室").FontSize(12).TextColor(theme.Muted)
			})
			ui.Column(c).Gap(16).Role(ui.RoleTabList).Label("功能").Children(func() {
				for _, f := range a.features {
					a.navItem(c.Key(f.ID()), f)
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
			Width:     1200,
			Height:    760,
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
