// Package feature defines what every part of Atelier provides to the shell.
//
// Each feature (the weekly checklist, and whatever comes later) lives in its
// own package under internal/features and implements Feature. The shell lists
// the registered features in its sidebar and shows the chosen one's view.
package feature

import "github.com/egoist/mygo/ui"

// Feature is one page of the app.
type Feature interface {
	// ID is a stable identifier, also used as the sidebar item's ID and as
	// the name of the feature's data file. Never change it once released.
	ID() string
	// Title is the name shown in the sidebar.
	Title() string
	// View builds the feature's page. It runs on the UI thread, every frame
	// the window needs.
	View(c *ui.Context)
}
