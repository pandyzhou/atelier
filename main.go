// Atelier is a personal all-in-one desktop app in MyGo native UI, drawn with
// the mallow component library. Each feature is a package under
// internal/features, registered in features().
package main

import (
	"context"
	"log"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"

	"github.com/pandyzhou/atelier/internal/feature"
	"github.com/pandyzhou/atelier/internal/features/todo"
	"github.com/pandyzhou/atelier/internal/llm"
	"github.com/pandyzhou/atelier/internal/secret"
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
	// Style is a mallow.Style: "neumorphism", also when it is empty, or
	// "organic".
	Style string `json:"style,omitempty"`
	// Font is the family text is set in, "" for defaultFont.
	Font string `json:"font,omitempty"`
	// Size is how large the interface is, one of sizes: "" is "normal".
	Size string `json:"size,omitempty"`
	// Models are the language models configured, Model the ID of the one
	// the app's features use, and NextModel the last ID given.
	Models    []modelConfig `json:"models,omitempty"`
	Model     int           `json:"model,omitempty"`
	NextModel int           `json:"nextModel,omitempty"`
}

// app is the shell: a title bar, a sidebar of features and the chosen one's
// page.
type app struct {
	store    *store.Store
	settings settings
	features []feature.Feature
	selected string       // the ID of the feature shown, or settingsID
	before   string       // the feature the settings were opened from
	section  string       // the part of the settings shown
	fonts    []fontChoice // the fonts to choose from, once the settings asked
	box      *secret.Box  // seals the models' keys
	models   modelsState  // what the models' part of the settings shows
	updater  updater      // where new versions come from
	update   updateState  // what is known of a newer version
	// ping tests a model's connection: llm.Ping, but in tests.
	ping func(context.Context, llm.Config) error
	win  mallow.Window // the window, for the title bar's buttons; nil in tests
}

func newApp(s *store.Store) *app {
	a := &app{store: s, features: features(s), settings: settings{Appearance: string(mallow.ModeSystem)}}
	a.box, a.ping = secret.New(s.Dir()), llm.Ping
	// No updates until main says where they come from: tests have none.
	a.updater = updater{enabled: func() bool { return false }}
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
	// The font and the size first: the theme takes the one, and the frame is
	// laid out at the other.
	mallow.Font = defaultFont
	if a.settings.Font != "" {
		mallow.Font = a.settings.Font
	}
	c.SetZoom(zoomOf(a.settings.Size))
	mallow.UseStyle(c, mallow.Style(a.settings.Style), mallow.Mode(a.settings.Appearance))
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
					// A dot beside the gear: a newer version waits in the
					// settings.
					if a.update.phase == updateAvailable || a.update.phase == updateReady {
						ui.Box(c).Size(8, 8).Radius(4).Margin(2, 0, 0, 6).Background(mallow.Accent)
					}
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

// defaultFont is the family text is set in unless another is chosen.
const defaultFont = "Microsoft YaHei UI"

// sizes are how large the interface can be: the whole of it is zoomed, the
// text and what holds it together, so that nothing is cut or crowded.
var sizes = []struct {
	id, label string
	zoom      float32
}{
	{"small", "小", 0.9},
	{"normal", "标准", 1},
	{"large", "大", 1.1},
	{"xlarge", "特大", 1.25},
}

// zoomOf is the zoom of a size, 1 for one it does not know.
func zoomOf(size string) float32 {
	for _, s := range sizes {
		if s.id == size {
			return s.zoom
		}
	}
	return 1
}

// fontChoice is a font the settings offer: its family, and what to call it.
type fontChoice struct {
	family, label string
}

// knownFonts are the families worth offering, where they are installed:
// those with Chinese glyphs, as text in a fallback font is set unevenly and
// takes no strikethrough. A line is installed if a family's name begins
// with one of its names, without regard to case: foundries put versions
// and weights after them, as "Alibaba PuHuiTi 3.0". Of several that do,
// the shortest name is the family's plain one.
var knownFonts = []struct {
	label string
	names []string
}{
	{"等线", []string{"DengXian"}},
	{"MiSans", []string{"MiSans"}},
	{"鸿蒙黑体", []string{"HarmonyOS Sans SC", "HarmonyOS Sans"}},
	{"阿里巴巴普惠体", []string{"Alibaba PuHuiTi", "阿里巴巴普惠体"}},
	{"OPPO Sans", []string{"OPPO Sans", "OPPOSans"}},
	{"vivo Sans", []string{"vivo Sans"}},
	{"思源黑体", []string{"Source Han Sans SC", "Noto Sans SC", "Noto Sans CJK SC", "思源黑体"}},
	{"思源宋体", []string{"Source Han Serif SC", "Noto Serif SC", "Noto Serif CJK SC", "思源宋体"}},
	{"更纱黑体", []string{"Sarasa Gothic SC", "Sarasa UI SC", "更纱黑体 SC"}},
	{"霞鹜文楷", []string{"LXGW WenKai", "霞鹜文楷"}},
	{"霞鹜新晰黑", []string{"LXGW Neo XiHei", "霞鹜新晰黑"}},
	{"得意黑", []string{"Smiley Sans", "得意黑"}},
	{"黑体", []string{"SimHei"}},
	{"宋体", []string{"SimSun"}},
	{"楷体", []string{"KaiTi"}},
	{"仿宋", []string{"FangSong"}},
}

// installed returns the family, of those the system has, that one of
// names stands for, "" if none does.
func installed(families, names []string) string {
	for _, name := range names {
		best := ""
		for _, f := range families {
			if len(f) >= len(name) && strings.EqualFold(f[:len(name)], name) && (best == "" || len(f) < len(best)) {
				best = f
			}
		}
		if best != "" {
			return best
		}
	}
	return ""
}

// fontChoices lists the fonts to choose from: the default, then those of
// knownFonts the system has. It asks the system once.
func (a *app) fontChoices() []fontChoice {
	if a.fonts != nil {
		return a.fonts
	}
	var families []string
	for _, f := range ui.FontFamilies() {
		families = append(families, f.Name)
	}
	a.fonts = []fontChoice{{"", "微软雅黑"}}
	for _, k := range knownFonts {
		if family := installed(families, k.names); family != "" {
			a.fonts = append(a.fonts, fontChoice{family, k.label})
		}
	}
	return a.fonts
}

// settingsSection is a part of the settings, an entry of the menu down the
// side of their page.
type settingsSection struct {
	id, title string
	// rows builds the section's settings with row: each a line, its name
	// and what it is about to the left and its control to the right, as
	// wide as width, or as it asks when that is 0.
	rows func(c *ui.Context, row func(name, note string, width float32, control func()))
}

// settingsSections lists the parts of the settings, in the menu's order.
// Add new ones here.
func (a *app) settingsSections() []settingsSection {
	return []settingsSection{
		{"look", "界面", a.lookSettings},
		{"models", "模型", a.modelSettings},
		{"about", "关于", a.aboutSettings},
	}
}

// settingsPage is the page of the shell's own preferences, laid out as a
// feature's page is: one card, the menu of sections down its side and,
// past a groove, the section chosen.
func (a *app) settingsPage(c *ui.Context) {
	sections := a.settingsSections()
	chosen := sections[0]
	items := make([]mallow.Item, len(sections))
	for i, s := range sections {
		items[i] = mallow.Item{ID: s.id, Label: s.title}
		if s.id == a.section {
			chosen = s
		}
	}
	a.section = chosen.id
	row := func(name, note string, width float32, control func()) {
		ui.Row(c).Padding(16, 4).Gap(24).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, name).FontSize(15).FontWeight(500).TextColor(mallow.Text)
				ui.Text(c, note).FontSize(12).TextColor(mallow.Muted)
			})
			// A column, which gives a Segmented its width: in a row it is
			// only as wide as its shortest label asks, and cuts the rest.
			if width > 0 {
				ui.Column(c).Shrink(0).Width(width).Children(control)
			} else {
				control()
			}
		})
	}
	// Between two rows of a section, a groove.
	first := true
	divided := func(name, note string, width float32, control func()) {
		if !first {
			mallow.Groove(c)
		}
		first = false
		row(name, note, width, control)
	}
	ui.Row(c).Fill().Padding(0, 28, 0, 16).AlignItems(ui.Stretch).Children(func() {
		mallow.Card(c, mallow.Medium).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				ui.Scroll(c).Width(208).Shrink(0).Padding(24, 12, 16, 12).Gap(12).Children(func() {
					mallow.Label(c, "设置").PaddingX(12)
					mallow.SourceList(c, "设置", &a.section, items)
				})
				mallow.GrooveV(c).Margin(24, 0)
				ui.Scroll(c.Key(chosen.id)).Grow(1).MinWidth(0).Padding(24, 28).Children(func() {
					ui.Text(c, chosen.title).FontSize(28).FontWeight(600).TextColor(mallow.Text).Margin(0, 0, 12, 0)
					chosen.rows(c, divided)
				})
			})
		})
	})
}

// lookSettings are how the app looks.
func (a *app) lookSettings(c *ui.Context, row func(name, note string, width float32, control func())) {
	row("风格", "界面的样子：柔和凹凸的新拟态，或者大地色的自然有机", 200, func() {
		style := a.settings.Style
		if style == "" {
			style = string(mallow.StyleNeumorphism)
		}
		if mallow.Segmented(c, "风格", &style, []mallow.Option{
			{Value: string(mallow.StyleNeumorphism), Label: "新拟态"},
			{Value: string(mallow.StyleOrganic), Label: "自然有机"},
		}) {
			a.settings.Style = style
			a.saveSettings()
		}
	})
	row("外观", "浅色、深色，或者跟随系统", 200, func() {
		if mallow.Segmented(c, "外观", &a.settings.Appearance, []mallow.Option{
			{Value: string(mallow.ModeLight), Label: "浅色"},
			{Value: string(mallow.ModeDark), Label: "深色"},
			{Value: string(mallow.ModeSystem), Label: "系统"},
		}) {
			a.saveSettings()
		}
	})
	a.textSettings(c, row)
}

// textSettings are the font and the size, the rest of how the app looks.
func (a *app) textSettings(c *ui.Context, row func(name, note string, width float32, control func())) {
	row("大小", "文字和整个界面一起放大或缩小", 240, func() {
		size := a.settings.Size
		if size == "" {
			size = "normal"
		}
		options := make([]mallow.Option, len(sizes))
		for i, s := range sizes {
			options[i] = mallow.Option{Value: s.id, Label: s.label}
		}
		if mallow.Segmented(c, "大小", &size, options) {
			a.settings.Size = size
			a.saveSettings()
		}
	})
	row("字体", "只列出这台电脑上装了的中文字体", 360, func() {
		ui.Row(c).Gap(8).Wrap().Justify(ui.End).Children(func() {
			for _, f := range a.fontChoices() {
				if mallow.Chip(c, f.label, a.settings.Font == f.family).Clicked() {
					a.settings.Font = f.family
					a.saveSettings()
				}
			}
		})
	})
}

// aboutSettings say what the app is.
func (a *app) aboutSettings(c *ui.Context, row func(name, note string, width float32, control func())) {
	row("版本", "Atelier 个人工作室", 0, func() {
		ui.Text(c, version()).FontSize(14).TextColor(mallow.Muted)
	})
	a.updateRows(c, row)
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
		// Released builds look for a newer version as they start, and say
		// so only if there is one.
		a.updater = mygoUpdater()
		a.checkForUpdate(true)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
