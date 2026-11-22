package main

import (
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/pandyzhou/atelier/internal/features/weekly"
	"github.com/pandyzhou/atelier/internal/store"
	"github.com/pandyzhou/mallow"
)

// The view runs without a window in tests, which click and type as a user
// would. An empty store keeps nothing on disk.
func TestWeeklyAddAndCheck(t *testing.T) {
	a := newApp(store.Open(""))
	tt := ui.NewTester(a.view, 1280, 800)
	if !tt.HasText("每周清单") {
		t.Fatalf("sidebar missing the weekly checklist, texts %q", tt.Texts())
	}
	if err := tt.Click("周一添加事项"); err != nil {
		t.Fatal(err)
	}
	tt.Type("写周报")
	tt.Key(0, ui.KeyEnter)
	if !tt.HasText("写周报") {
		t.Fatalf("item not shown after adding, texts %q", tt.Texts())
	}
	w := a.current().(*weekly.Weekly)
	items := w.Items()
	if len(items) != 1 || items[0].Day != 0 {
		t.Fatalf("items %+v", items)
	}
	if err := tt.Click("写周报"); err != nil {
		t.Fatal(err)
	}
	if !items[0].Done {
		t.Errorf("item not checked after a click")
	}
}

// TestScreenshot renders the window without opening it and writes a PNG to
// the path in ATELIER_SCREENSHOT, to look at the UI from scripts:
//
//	ATELIER_SCREENSHOT=shot.png go test -run Screenshot
func TestScreenshot(t *testing.T) {
	path := os.Getenv("ATELIER_SCREENSHOT")
	if path == "" {
		t.Skip("set ATELIER_SCREENSHOT to write a screenshot")
	}
	a := newApp(store.Open(""))
	// ATELIER_SCREENSHOT_THEME picks the appearance: light, dark or system.
	if m := os.Getenv("ATELIER_SCREENSHOT_THEME"); m != "" {
		a.settings.Appearance = m
	}
	a.win = &fakeWindow{}
	tt := ui.NewTester(a.view, 1280, 800)
	if os.Getenv("ATELIER_SCREENSHOT_SCALE") == "2" {
		tt.SetScale(2)
	}
	for _, it := range []struct {
		day  string
		text string
	}{
		{"周一添加事项", "写周报"}, {"周一添加事项", "修 bug"}, {"周二添加事项", "读书 30 分钟"},
		{"周三添加事项", "复习英语"}, {"周四添加事项", "采购日用品"}, {"周六添加事项", "整理房间"},
	} {
		if err := tt.Click(it.day); err != nil {
			t.Fatal(err)
		}
		tt.Type(it.text)
		tt.Key(0, ui.KeyEnter)
	}
	for _, done := range []string{"写周报", "修 bug"} {
		if err := tt.Click(done); err != nil {
			t.Fatal(err)
		}
	}
	// Let the 200 ms transitions finish before taking the picture.
	tt.Advance(time.Second)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

// The appearance follows the system until the user picks one, which is saved.
func TestAppearance(t *testing.T) {
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetDark(true)
	tt.Frame()
	if mallow.Base != mallow.DarkPalette.Base {
		t.Fatalf("following a dark system, base %v", mallow.Base)
	}
	if err := tt.Click("浅色"); err != nil {
		t.Fatal(err)
	}
	if a.settings.Appearance != string(mallow.ModeLight) || mallow.Base != mallow.LightPalette.Base {
		t.Fatalf("after choosing light: appearance %q, base %v", a.settings.Appearance, mallow.Base)
	}
	// A new start reads the choice back, whatever the system is.
	b := newApp(store.Open(dir))
	tb := ui.NewTester(b.view, 1280, 800)
	tb.SetDark(true)
	tb.Frame()
	if b.settings.Appearance != string(mallow.ModeLight) || mallow.Base != mallow.LightPalette.Base {
		t.Fatalf("after restarting: appearance %q, base %v", b.settings.Appearance, mallow.Base)
	}
}

// fakeWindow records what the title bar's buttons ask of the window.
type fakeWindow struct {
	maximized bool
	calls     []string
}

func (w *fakeWindow) Minimize()         { w.calls = append(w.calls, "minimize") }
func (w *fakeWindow) Maximize()         { w.calls = append(w.calls, "maximize"); w.maximized = true }
func (w *fakeWindow) Unmaximize()       { w.calls = append(w.calls, "unmaximize"); w.maximized = false }
func (w *fakeWindow) Close()            { w.calls = append(w.calls, "close") }
func (w *fakeWindow) IsMaximized() bool { return w.maximized }

// The drawn title bar's buttons drive the window, the middle one maximizing
// or restoring as the window is.
func TestTitleBarButtons(t *testing.T) {
	win := &fakeWindow{}
	a := newApp(store.Open(""))
	a.win = win
	tt := ui.NewTester(a.view, 1280, 800)
	for _, label := range []string{"最小化", "最大化", "还原", "关闭"} {
		if err := tt.Click(label); err != nil {
			t.Fatalf("click %s: %v (texts %q)", label, err, tt.Texts())
		}
	}
	want := "minimize maximize unmaximize close"
	if got := strings.Join(win.calls, " "); got != want {
		t.Errorf("calls %q, want %q", got, want)
	}
}
