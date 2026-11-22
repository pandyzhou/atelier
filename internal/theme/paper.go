// Package theme holds Atelier's look: warm paper, ink-dark text and a muted
// green accent, with small corners and thin rules instead of cards and
// shadows.
package theme

import "github.com/egoist/mygo/ui"

// Colors used by features beyond what ui.Theme carries.
var (
	// Rule is the thin line between columns and under headers.
	Rule = ui.Hex("#d8d1c3")
	// Today tints the column or row of the current day.
	Today = ui.Hex("#ebe6d6")
	// Sidebar is the background of the navigation column.
	Sidebar = ui.Hex("#ece7dc")
)

// Paper returns the app's theme.
func Paper() *ui.Theme {
	t := ui.LightTheme().WithAccent(ui.Hex("#2f5d50"))
	t.Background = ui.Hex("#f5f2ea")
	t.Surface = ui.Hex("#fbf9f4")
	t.SurfaceHover = ui.Hex("#efeadf")
	t.SurfacePressed = ui.Hex("#e6e0d2")
	t.Border = ui.Hex("#cfc7b8")
	t.Text = ui.Hex("#22201c")
	t.TextMuted = ui.Hex("#8b8476")
	t.Danger = ui.Hex("#a8432f")
	t.InactiveAccent = t.Accent
	t.Radius = 3
	return t
}
