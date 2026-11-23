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

// A new list's name is kept when its field is left, dropped on Escape, and
// can be changed afterwards.
func TestTodoListNames(t *testing.T) {
	d, tt := newTodoTester(t)
	click(t, tt, "新建清单")
	tt.Type("Work")
	click(t, tt, "全部")
	if len(d.Lists()) != 1 || d.Lists()[0].Name != "Work" {
		t.Fatalf("lists after leaving the field: %+v", d.Lists())
	}
	click(t, tt, "新建清单")
	tt.Type("Nope")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if len(d.Lists()) != 1 || tt.HasText("Nope") {
		t.Fatalf("lists after Escape: %+v", d.Lists())
	}
	click(t, tt, "Work", "重命名清单")
	tt.Type("2")
	tt.Key(0, ui.KeyEnter)
	if got := d.Lists()[0].Name; got != "Work2" {
		t.Errorf("the list renamed is %q", got)
	}
}

// rightClick opens the context menu of what is labelled so.
func rightClick(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.RightClick(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

// A right click on a list renames or deletes it; on a task, it edits it.
func TestTodoContextMenus(t *testing.T) {
	d, tt := newTodoTester(t)
	click(t, tt, "新建清单")
	tt.Type("Work")
	tt.Key(0, ui.KeyEnter)
	click(t, tt, "今天")
	rightClick(t, tt, "Work")
	click(t, tt, "重命名")
	tt.Type("2")
	tt.Key(0, ui.KeyEnter)
	if got := d.Lists()[0].Name; got != "Work2" {
		t.Fatalf("the list renamed from its menu is %q", got)
	}

	click(t, tt, "今天")
	add(t, tt, "Buy milk")
	rightClick(t, tt, "打开Buy milk")
	click(t, tt, "改到明天")
	if k := d.Tasks()[0]; k.Due == d.Today() {
		t.Fatalf("the task moved to tomorrow is due %s, today", k.Due)
	}
	click(t, tt, "最近 7 天")
	rightClick(t, tt, "打开Buy milk")
	click(t, tt, "删除任务")
	if len(d.Tasks()) != 0 {
		t.Errorf("tasks after deleting from the menu: %+v", d.Tasks())
	}

	rightClick(t, tt, "Work2")
	click(t, tt, "删除清单")
	if len(d.Lists()) != 0 {
		t.Errorf("lists after deleting from the menu: %+v", d.Lists())
	}
}

// A task is made to repeat weekly as it is added, or from its menu.
func TestTodoWeeklyRepeat(t *testing.T) {
	d, tt := newTodoTester(t)
	click(t, tt, "每周重复")
	add(t, tt, "Check upstream")
	add(t, tt, "Buy milk")
	ks := d.Tasks()
	if ks[0].Repeat != todo.RepeatWeekly || ks[1].Repeat != todo.RepeatNone {
		t.Fatalf("repeats after adding one weekly and one plain: %q, %q", ks[0].Repeat, ks[1].Repeat)
	}
	rightClick(t, tt, "打开Buy milk")
	click(t, tt, "设为每周重复")
	if ks[1].Repeat != todo.RepeatWeekly || ks[1].Due == "" {
		t.Fatalf("after 设为每周重复: repeat %q, due %q", ks[1].Repeat, ks[1].Due)
	}
	rightClick(t, tt, "打开Buy milk")
	click(t, tt, "取消重复")
	if ks[1].Repeat != todo.RepeatNone {
		t.Errorf("after 取消重复: repeat %q", ks[1].Repeat)
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

// A group's heading folds its tasks away and back; the list's count stays.
func TestTodoFold(t *testing.T) {
	_, tt := newTodoTester(t)
	add(t, tt, "Buy milk")
	if !tt.HasText("1") {
		t.Errorf("the nav shows no count for today's one task")
	}
	click(t, tt, "收起今天")
	if tt.HasText("Buy milk") {
		t.Errorf("the folded group still shows its task")
	}
	click(t, tt, "展开今天")
	if !tt.HasText("Buy milk") {
		t.Errorf("the unfolded group does not show its task")
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
	// ATELIER_SCREENSHOT_RIGHTCLICK opens the context menu of a label.
	if s := os.Getenv("ATELIER_SCREENSHOT_RIGHTCLICK"); s != "" {
		rightClick(t, tt, s)
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
	// The switch is on the settings page, with the version.
	click(t, tt, "设置", "浅色")
	if !tt.HasText("版本") || !tt.HasText("开发版") {
		t.Errorf("the settings page shows no version: %q", tt.Texts())
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
