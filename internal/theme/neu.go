// Package theme holds Atelier's look: neumorphism (soft UI). Every element is
// the color of the background; a light from the top left gives raised ones a
// white highlight above and a grey shadow below, and recessed ones the same
// pair inside. No borders, large radii, one blurple accent.
//
// Spec: https://www.stylekit.top/zh/styles/neumorphism
package theme

import (
	"time"

	"github.com/egoist/mygo/ui"
)

var (
	// Base is the color of the background and of every element on it.
	Base = ui.Hex("#e0e5ec")
	// Dark is the shadow on the side away from the light; Light the highlight.
	Dark  = ui.Hex("#b8bcc2")
	Light = ui.Hex("#ffffff")

	Text  = ui.Hex("#3d4451")
	Muted = ui.Hex("#6b7280")

	// Accent marks the primary action and what is chosen, on buttons and
	// icons only. Danger is for destructive actions, Success for what went
	// well.
	Accent  = ui.Hex("#6d5dfc")
	Danger  = ui.Hex("#ff6b6b")
	Success = ui.Hex("#4ecdc4")
)

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

// Theme returns the widget theme matching the look, for the controls Atelier
// takes from MyGo as they are (text inputs, scroll bars, tooltips).
func Theme() *ui.Theme {
	t := ui.LightTheme().WithAccent(Accent)
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
