package main

import (
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/pandyzhou/atelier/internal/features/todo"
	"github.com/pandyzhou/atelier/internal/store"
	"github.com/pandyzhou/mallow"
)

// The view runs without a window in tests, which click and type as a user
// would. An empty store keeps nothing on disk.
func newTodoTester(t *testing.T) (*todo.Todo, *ui.Tester) {
	t.Helper()
	a := newApp(store.Open(""))
	a.win = &fakeWindow{}
	tt := ui.NewTester(a.view, 1280, 800)
	if !tt.HasText("待办") {
		t.Fatalf("sidebar missing the to-do list, texts %q", tt.Texts())
	}
	return a.current().(*todo.Todo), tt
}

func click(t *testing.T, tt *ui.Tester, labels ...string) {
	t.Helper()
	for _, label := range labels {
		if err := tt.Click(label); err != nil {
			t.Fatalf("click %q: %v (texts %q)", label, err, tt.Texts())
		}
	}
}

// add types a task into the adder of the view shown.
func add(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	click(t, tt, "添加任务")
	tt.Type(title)
	tt.Key(0, ui.KeyEnter)
}

func TestTodoAddAndCheck(t *testing.T) {
	d, tt := newTodoTester(t)
	add(t, tt, "写周报")
	tasks := d.Tasks()
	if len(tasks) != 1 || tasks[0].Due != d.Today() || !tt.HasText("写周报") {
		t.Fatalf("after adding in 今天: tasks %+v, texts %q", tasks, tt.Texts())
	}
	click(t, tt, "完成写周报")
	if !tasks[0].Done || !tt.HasText("今天完成的") {
		t.Errorf("after checking off: %+v, texts %q", tasks[0], tt.Texts())
	}
}

func TestTodoDetail(t *testing.T) {
	d, tt := newTodoTester(t)
	add(t, tt, "交水电费")
	k := d.Tasks()[0]
	click(t, tt, "打开交水电费", "下周一", "高", "每月")
	if k.Due <= d.Today() || k.Priority != todo.PriorityHigh || k.Repeat != todo.RepeatMonthly {
		t.Fatalf("after editing: %+v", k)
	}
	due := k.Due
	// Checked off, a repeating task leaves a done copy and moves on.
	click(t, tt, "完成这项任务")
	if n := len(d.Tasks()); n != 2 || k.Done || k.Due <= due || !d.Tasks()[1].Done {
		t.Fatalf("after checking off a monthly task: %d tasks, %+v", n, k)
	}
	click(t, tt, "删除任务")
	if n := len(d.Tasks()); n != 1 || d.Tasks()[0].Repeat != todo.RepeatNone {
		t.Fatalf("after deleting: %+v", d.Tasks())
	}
}

func TestTodoLists(t *testing.T) {
	d, tt := newTodoTester(t)
	click(t, tt, "新建清单")
	tt.Type("工作")
	tt.Key(0, ui.KeyEnter)
	if len(d.Lists()) != 1 {
		t.Fatalf("lists %+v", d.Lists())
	}
	// The new list is shown: what is added goes into it, without a date.
	add(t, tt, "回邮件")
	k := d.Tasks()[0]
	if k.List != d.Lists()[0].ID || k.Due != "" {
		t.Fatalf("task added in the list: %+v", k)
	}
	click(t, tt, "删除清单（任务移到收集箱）")
	if len(d.Lists()) != 0 || k.List != todo.Inbox || !tt.HasText("回邮件") {
		t.Fatalf("after deleting the list: lists %+v, task %+v", d.Lists(), k)
	}
}

func TestTodoWeekView(t *testing.T) {
	d, tt := newTodoTester(t)
	click(t, tt, "按周", "周日")
	add(t, tt, "大扫除")
	k := d.Tasks()[0]
	day, err := time.Parse("2006-01-02", k.Due)
	if err != nil || day.Weekday() != time.Sunday {
		t.Fatalf("task added to Sunday in the week view: %+v", k)
	}
	click(t, tt, "下一周")
	if tt.HasText("大扫除") {
		t.Errorf("next week shows this week's task")
	}
}

// TestScreenshot renders the window without opening it and writes a PNG to
// the path in ATELIER_SCREENSHOT, to look at the UI from scripts:
//
//	ATELIER_SCREENSHOT=shot.png go test -run Screenshot
//
// ATELIER_SCREENSHOT_CLICK lists what to click first, separated by commas,
// as "按周" for the week view or "打开写周报" for a task's details.
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
	d := a.current().(*todo.Todo)
	today, _ := time.Parse("2006-01-02", d.Today())
	day := func(n int) string { return today.AddDate(0, 0, n).Format("2006-01-02") }
	work, life := d.AddList("工作"), d.AddList("生活")
	for _, k := range []todo.Task{
		{Title: "交水电费", Due: day(-1), List: life.ID, Priority: todo.PriorityHigh},
		{Title: "写周报", Due: day(0), List: work.ID, Repeat: todo.RepeatWeekly, Priority: todo.PriorityMid},
		{Title: "修登录页的 bug", Due: day(0), List: work.ID, Note: "复现步骤在群里"},
		{Title: "跑步 30 分钟", Due: day(0), Repeat: todo.RepeatDaily},
		{Title: "给家里打电话", Due: day(1), List: life.ID},
		{Title: "读完《置身事内》", Due: day(3), Priority: todo.PriorityLow},
		{Title: "采购日用品", Due: day(5), List: life.ID},
		{Title: "整理书架"},
	} {
		d.Add(k)
	}
	d.SetDone(d.Add(todo.Task{Title: "预约体检", Due: day(0), List: life.ID}), true)
	tt := ui.NewTester(a.view, 1280, 800)
	if os.Getenv("ATELIER_SCREENSHOT_SCALE") == "2" {
		tt.SetScale(2)
	}
	if s := os.Getenv("ATELIER_SCREENSHOT_CLICK"); s != "" {
		click(t, tt, strings.Split(s, ",")...)
	}
	tt.Move(5, 400)
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
