// Package theme holds Atelier's look: neumorphism (soft UI). Every element is
// the color of the background; a light from the top left gives raised ones a
// highlight above and a shadow below, and recessed ones the same pair inside.
// No borders, large radii, one blurple accent. It comes in a light and a dark
// palette; Use switches between them.
//
// Spec: https://www.stylekit.top/zh/styles/neumorphism
package theme

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Palette is the colors of one variant of the look.
type Palette struct {
	IsDark bool
	// Base is the color of the background and of every element on it.
	Base ui.Color
	// Dark is the shadow on the side away from the light; Light the highlight.
	Dark, Light ui.Color
	Text, Muted ui.Color
	// Accent marks the primary action and what is chosen, on buttons and
	// icons only; OnAccent is text and marks on it. Danger is for
	// destructive actions, Success for what went well.
	Accent, OnAccent ui.Color
	Danger, Success  ui.Color
}

// LightPalette is the spec's own palette.
var LightPalette = Palette{
	Base:     ui.Hex("#e0e5ec"),
	Dark:     ui.Hex("#b8bcc2"),
	Light:    ui.Hex("#ffffff"),
	Text:     ui.Hex("#3d4451"),
	Muted:    ui.Hex("#6b7280"),
	Accent:   ui.Hex("#6d5dfc"),
	OnAccent: ui.Hex("#ffffff"),
	Danger:   ui.Hex("#ff6b6b"),
	Success:  ui.Hex("#4ecdc4"),
}

// DarkPalette carries the same light into a dim room: a charcoal surface, a
// near-black shadow and a highlight only a little lighter than the surface,
// as white would glare; text and accent are lifted to keep their contrast.
var DarkPalette = Palette{
	IsDark:   true,
	Base:     ui.Hex("#2a2d33"),
	Dark:     ui.Hex("#1b1d21"),
	Light:    ui.Hex("#393d44"),
	Text:     ui.Hex("#dde1e7"),
	Muted:    ui.Hex("#8d939d"),
	Accent:   ui.Hex("#8b7dff"),
	OnAccent: ui.Hex("#ffffff"),
	Danger:   ui.Hex("#ff7b7b"),
	Success:  ui.Hex("#5ad8cf"),
}

// The colors of the palette in use, which Use sets; read them while building
// a view.
var (
	Base, Dark, Light, Text, Muted ui.Color
	Accent, OnAccent               ui.Color
	Danger, Success                ui.Color
	isDark                         bool
)

func init() { Use(LightPalette) }

// Use makes p the palette in use.
func Use(p Palette) {
	Base, Dark, Light, Text, Muted = p.Base, p.Dark, p.Light, p.Text, p.Muted
	Accent, OnAccent, Danger, Success = p.Accent, p.OnAccent, p.Danger, p.Success
	isDark = p.IsDark
}

// Mode is the appearance the user chose.
type Mode string

const (
	ModeSystem Mode = "system" // follow the system's light or dark appearance
	ModeLight  Mode = "light"
	ModeDark   Mode = "dark"
)

// PaletteFor returns the palette of mode, given whether the system is dark.
func PaletteFor(mode Mode, systemDark bool) Palette {
	if mode == ModeDark || (mode != ModeLight && systemDark) {
		return DarkPalette
	}
	return LightPalette
}

// Radii.
const (
	RadiusCard    = 24
	RadiusControl = 16
	RadiusSmall   = 12
	RadiusPill    = 999
)

// Depth is how far an element stands out of, or sinks into, the surface: the
// shadow offset, whose blur is twice that.
type Depth float32

const (
	Small  Depth = 4 // buttons, hover, small controls
	Medium Depth = 6 // cards in dense layouts
	Large  Depth = 8 // standalone cards and fields
)

// Raised makes e stand out of the surface.
func Raised(e ui.Element, d Depth) ui.Element {
	o := float32(d)
	return e.Background(Base).
		Shadow(o, o, 2*o, 0, Dark).
		Shadow(-o, -o, 2*o, 0, Light)
}

// Recessed makes e sink into the surface.
func Recessed(e ui.Element, d Depth) ui.Element {
	o := float32(d)
	return e.Background(Base).
		InsetShadow(o, o, 2*o, 0, Dark).
		InsetShadow(-o, -o, 2*o, 0, Light)
}

// Groove is a divider pressed into the surface: a thin channel, the way soft
// UI separates parts without a line of another color.
func Groove(c *ui.Context) ui.Element {
	return ui.Box(c).Height(4).Shrink(0).Radius(2).Background(Base).
		InsetShadow(1, 1, 2, 0, Dark).
		InsetShadow(-1, -1, 2, 0, Light)
}

// Soft fades shadow and color changes over 200 ms, as the spec's
// transitions do.
func Soft(e ui.Element) ui.Element {
	return e.Transition(ui.ElementTransition{Duration: 200 * time.Millisecond})
}

// Button is a raised button: shallower while hovered, pressed in while held.
// It returns the element; ask Clicked of it.
func Button(c *ui.Context, label string) ui.Element {
	b := ui.ButtonBase(c).Label(label).Padding(8, 16).Radius(RadiusControl).
		AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
	switch {
	case b.Pressed():
		Recessed(b, Small)
	case b.Hovered():
		Raised(b, Small/2)
	default:
		Raised(b, Small)
	}
	Soft(b).Children(func() {
		ui.Text(c, label).FontSize(14).FontWeight(500).TextColor(Text).SingleLine()
	})
	return b
}

// Option is one choice of a Segmented control.
type Option struct {
	Value, Label string
}

// Segmented chooses one of options into *value: a recessed track whose chosen
// segment rises out of it. It reports whether the choice changed.
func Segmented(c *ui.Context, label string, value *string, options []Option) bool {
	changed := false
	track := ui.Row(c).Role(ui.RoleRadioGroup).Label(label).Padding(4).Gap(4).Radius(RadiusPill)
	Recessed(track, 3).Children(func() {
		for _, o := range options {
			chosen := *value == o.Value
			seg := ui.ButtonBase(c.Key(o.Value)).Role(ui.RoleRadio).Label(o.Label).AccessSelected(chosen).
				Grow(1).Padding(6, 0).Radius(RadiusPill).AlignItems(ui.Center).Justify(ui.Center).
				Cursor(ui.CursorPointer)
			if chosen {
				Raised(seg, 2)
			}
			if seg.Clicked() && !chosen {
				*value, changed = o.Value, true
			}
			color := Muted
			if chosen {
				color = Accent
			}
			Soft(seg).Children(func() {
				ui.Text(c, o.Label).FontSize(12).FontWeight(500).TextColor(color).SingleLine()
			})
		}
	})
	return changed
}

// Theme returns the widget theme matching the look, for the controls Atelier
// takes from MyGo as they are (text inputs, scroll bars, tooltips), in the
// palette in use.
func Theme() *ui.Theme {
	t := ui.LightTheme()
	if isDark {
		t = ui.DarkTheme()
	}
	t = t.WithAccent(Accent)
	t.Background = Base
	t.Surface = Base
	t.SurfaceHover = Base
	t.SurfacePressed = Base
	t.Border = ui.Transparent
	t.Text = Text
	t.TextMuted = Muted
	t.Danger = Danger
	t.Success = Success
	t.InactiveAccent = Accent
	t.Scrollbar = Dark
	t.Radius = RadiusSmall
	t.Spacing = 4
	t.FontSize = 14
	// A CJK font first: text in a fallback font gets no strikethrough.
	t.Font = "Microsoft YaHei UI"
	t.Inverse, t.InverseText = Text, Base
	return t
}
