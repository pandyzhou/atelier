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
	c.SetTheme(theme.Paper())
	t := c.Theme()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(200).Shrink(0).Padding(24, 12).Gap(20).
			Background(theme.Sidebar).BorderWidth(0, 1, 0, 0).BorderColor(theme.Rule).
			Children(func() {
				ui.Column(c).PaddingX(8).Gap(2).Children(func() {
					ui.Text(c, "Atelier").FontSize(20).Bold()
					ui.Text(c, "个人工作室").FontSize(12).TextColor(t.TextMuted)
				})
				ui.Sidebar(c, &a.selected, func() {
					for _, f := range a.features {
						ui.SidebarItem(c.Key(f.ID()), f.ID(), nil, f.Title())
					}
				}).Grow(1).Background(ui.Transparent).Label("功能")
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
