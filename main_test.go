package main

import (
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/pandyzhou/atelier/internal/features/weekly"
	"github.com/pandyzhou/atelier/internal/store"
)

// The view runs without a window in tests, which click and type as a user
// would. An empty store keeps nothing on disk.
func TestWeeklyAddAndCheck(t *testing.T) {
	a := newApp(store.Open(""))
	tt := ui.NewTester(a.view, 1200, 760)
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
	tt := ui.NewTester(a.view, 1200, 760)
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
