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
	"github.com/pandyzhou/atelier/internal/features/todo"
	"github.com/pandyzhou/atelier/internal/store"
)

// features lists the app's features in sidebar order. Add new ones here.
func features(s *store.Store) []feature.Feature {
	return []feature.Feature{
		todo.New(s),
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
	selected string        // the ID of the feature shown, or settingsID
	before   string        // the feature the settings were opened from
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
		// The sidebar: the app's name, the features (the chosen one's recess
		// slides between them) and the settings at the bottom.
		items := make([]mallow.Item, len(a.features))
		for i, f := range a.features {
			items[i] = mallow.Item{ID: f.ID(), Label: f.Title()}
		}
		mallow.Sidebar(c, mallow.SidebarOptions{
			Title:    "Atelier",
			Subtitle: "个人工作室",
			Items:    items,
			Selected: &a.selected,
			// The gear in the corner opens the settings, and goes back to
			// the feature left for them when clicked again.
			Footer: func() {
				on := a.selected == settingsID
				ui.Row(c).Children(func() {
					gear := mallow.IconButton(c, "设置", mallow.GlyphSettings, mallow.IconButtonStyle{Size: 36, Active: on})
					mallow.Tooltip(c, gear, "设置")
					switch {
					case !gear.Clicked():
					case on:
						a.selected = a.before
					default:
						a.before, a.selected = a.selected, settingsID
					}
				})
			},
		})
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			if a.selected == settingsID {
				a.settingsPage(c.Key(settingsID))
			} else if f := a.current(); f != nil {
				f.View(c.Key(f.ID()))
			}
		})
	})
}

// settingsID is what is selected while the settings show: the ID of no
// feature.
const settingsID = "settings"

// version is the app's version, as "v0.1.16": mygo.json's, which a build
// links in. Run without one, as in tests, it is a development build.
func version() string {
	if v := mygo.App.Version(); v != "" {
		return "v" + v
	}
	return "开发版"
}

// settingsPage is the page of the shell's own preferences, one card as a
// feature's page is: each a row, its name and what it is about to the left
// and its control to the right.
func (a *app) settingsPage(c *ui.Context) {
	row := func(name, note string, control func()) {
		ui.Row(c).Padding(16, 4).Gap(24).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, name).FontSize(15).FontWeight(500).TextColor(mallow.Text)
				ui.Text(c, note).FontSize(12).TextColor(mallow.Muted)
			})
			control()
		})
	}
	ui.Row(c).Fill().Padding(8, 28, 4, 16).AlignItems(ui.Stretch).Children(func() {
		mallow.Card(c, mallow.Medium).Grow(1).MinWidth(0).Margin(0, 0, 20, 0).Children(func() {
			// The rows run the width of the card, as a feature's page does:
			// the groove under each leads from its name to its control.
			ui.Scroll(c).Grow(1).Padding(24, 28).Children(func() { a.settingsRows(c, row) })
		})
	})
}

// settingsRows are the settings themselves, under the page's heading.
func (a *app) settingsRows(c *ui.Context, row func(name, note string, control func())) {
	ui.Text(c, "设置").FontSize(28).FontWeight(600).TextColor(mallow.Text).Margin(0, 0, 12, 0)
	row("外观", "浅色、深色，或者跟随系统", func() {
		if mallow.Segmented(c, "外观", &a.settings.Appearance, []mallow.Option{
			{Value: string(mallow.ModeLight), Label: "浅色"},
			{Value: string(mallow.ModeDark), Label: "深色"},
			{Value: string(mallow.ModeSystem), Label: "系统"},
		}) {
			a.saveSettings()
		}
	})
	mallow.Groove(c)
	row("版本", "Atelier 个人工作室", func() {
		ui.Text(c, version()).FontSize(14).TextColor(mallow.Muted)
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
