package main

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/pandyzhou/atelier/internal/features/todo"
	"github.com/pandyzhou/atelier/internal/llm"
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

// shown is how many of the last frame's texts are text and nothing more: an
// element shows twice among them, as its label and as what it writes.
// Tester.HasText finds text within longer ones too.
func shown(tt *ui.Tester, text string) int {
	n := 0
	for _, s := range tt.Texts() {
		if s == text {
			n++
		}
	}
	return n
}

// taskList is the label of the list of tasks of the view shown.
const taskList = "任务列表"

// newTask brings the dialog that adds a task, from the context menu of the
// list of the view shown.
func newTask(t *testing.T, tt *ui.Tester) {
	t.Helper()
	rightClick(t, tt, taskList)
	click(t, tt, "新任务")
}

// add adds a task of a title in the view shown.
func add(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	newTask(t, tt)
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
	click(t, tt, "打开交水电费", "下周一", "每月")
	if k.Due <= d.Today() || k.Repeat != todo.RepeatMonthly {
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

// rightClick opens the context menu of what is labelled so. That of the
// list of tasks opens at the list's lower edge, under its tasks: its
// middle may have a task, which has a menu of its own. Where the tasks
// fill the list, that edge has one too, whose menu adds a task as well.
func rightClick(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if label == taskList {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("no list of tasks (texts %q)", tt.Texts())
		}
		tt.RightClickAt(r.X+r.W/2, r.Y+r.H-10)
	} else if err := tt.RightClick(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

// A right click on a task edits it.
func TestTodoContextMenus(t *testing.T) {
	d, tt := newTodoTester(t)
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
}

// A right click offers to add a task too: where there is no task, and on a
// task, above what is done with that one.
func TestTodoNewFromContextMenu(t *testing.T) {
	d, tt := newTodoTester(t)
	// In the list, which is empty yet.
	tt.RightClickAt(800, 500)
	tt.Frame()
	click(t, tt, "新任务")
	if !tt.HasText("任务标题") {
		t.Fatalf("no dialog after 新任务 in the list's menu: %q", tt.Texts())
	}
	tt.Type("Buy milk")
	tt.Key(0, ui.KeyEnter)
	if ks := d.Tasks(); len(ks) != 1 || ks[0].Title != "Buy milk" || ks[0].Due != d.Today() {
		t.Fatalf("after adding from the list's menu: %+v", ks)
	}
	// Above the list too, by the view's heading.
	if r, ok := tt.Find(taskList); ok {
		tt.RightClickAt(r.X+r.W/2, r.Y-30)
		tt.Frame()
	}
	if n := shown(tt, "新任务"); n != 2 {
		t.Fatalf("新任务 shows %d times after a right click by the heading, not twice as one entry does: %q", n, tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	// Beside the list too, on a view to choose and under them.
	for _, at := range [][2]float32{{330, 126}, {330, 600}} {
		tt.RightClickAt(at[0], at[1])
		tt.Frame()
		if n := shown(tt, "新任务"); n != 2 {
			t.Fatalf("新任务 shows %d times after a right click at %v, not twice as one entry does: %q", n, at, tt.Texts())
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
	}
	// On a task: its menu alone, not the page's under it too.
	rightClick(t, tt, "打开Buy milk")
	if n := shown(tt, "新任务"); n != 2 {
		t.Fatalf("新任务 shows %d times after a right click on a task, not twice as one entry does: %q", n, tt.Texts())
	}
	click(t, tt, "新任务")
	if !tt.HasText("任务标题") {
		t.Fatalf("no dialog after 新任务 in a task's menu: %q", tt.Texts())
	}
	tt.Type("Buy bread")
	tt.Key(0, ui.KeyEnter)
	if ks := d.Tasks(); len(ks) != 2 || ks[0].Title != "Buy milk" || ks[1].Title != "Buy bread" {
		t.Fatalf("after adding from a task's menu: %+v", ks)
	}
	// Under the line, the task's own entries are chosen as before.
	rightClick(t, tt, "打开Buy bread")
	click(t, tt, "删除任务")
	if ks := d.Tasks(); len(ks) != 1 || ks[0].Title != "Buy milk" {
		t.Fatalf("after deleting from the menu that adds too: %+v", ks)
	}

}

// The month view is a calendar: a task is under the day it is due, opens in
// a dialog, and a right click on a day adds a task due that day.
func TestTodoMonthView(t *testing.T) {
	d, tt := newTodoTester(t)
	add(t, tt, "Buy milk")
	k := d.Tasks()[0]
	// A task unfolded in the list is not open in the calendar.
	click(t, tt, "打开Buy milk", "月历")
	if tt.HasText("任务标题") || tt.HasText("按月") {
		t.Fatalf("the calendar opens with a dialog, or the lists have a month: %q", tt.Texts())
	}
	day, _ := time.Parse("2006-01-02", d.Today())
	if !tt.HasText(fmt.Sprintf("%d 年 %d 月", day.Year(), int(day.Month()))) || !tt.HasText("Buy milk") {
		t.Fatalf("the month view: %q", tt.Texts())
	}
	// The task opens in a dialog, which edits it.
	click(t, tt, "打开Buy milk")
	if !tt.HasText("任务标题") {
		t.Fatalf("no dialog after opening a task of the month: %q", tt.Texts())
	}
	click(t, tt, "每周", "关闭详情")
	if k.Repeat != todo.RepeatWeekly || tt.HasText("任务标题") {
		t.Fatalf("after editing in the dialog and closing it: %+v, %q", k, tt.Texts())
	}
	click(t, tt, "打开Buy milk")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if tt.HasText("任务标题") {
		t.Fatalf("the dialog stays after Escape")
	}

	// A right click on a day adds a task due that day.
	other := day.AddDate(0, 0, 3)
	if other.Month() != day.Month() {
		other = day.AddDate(0, 0, -3)
	}
	weekdays := [7]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	rightClick(t, tt, fmt.Sprintf("%d月%d日 %s", int(other.Month()), other.Day(), weekdays[other.Weekday()]))
	click(t, tt, "新任务")
	tt.Type("Buy bread")
	tt.Key(0, ui.KeyEnter)
	if ks := d.Tasks(); len(ks) != 2 || ks[1].Due != other.Format("2006-01-02") {
		t.Fatalf("after adding on a day of the month: %+v", ks)
	}

	// Another month has neither, and 本月 comes back.
	click(t, tt, "下一月", "下一月")
	if tt.HasText("Buy milk") || tt.HasText("Buy bread") {
		t.Errorf("two months on, the tasks of this one show: %q", tt.Texts())
	}
	click(t, tt, "本月")
	if !tt.HasText("Buy milk") || !tt.HasText("Buy bread") {
		t.Errorf("back in this month: %q", tt.Texts())
	}
	// A task added in another month is due on its first day.
	click(t, tt, "上一月")
	first := time.Date(day.Year(), day.Month()-1, 1, 0, 0, 0, 0, time.UTC)
	rightClick(t, tt, "上一月")
	click(t, tt, "新任务")
	tt.Type("Old")
	tt.Key(0, ui.KeyEnter)
	if ks := d.Tasks(); len(ks) != 3 || ks[2].Due != first.Format("2006-01-02") {
		t.Fatalf("after adding in the month before: %+v", ks)
	}
}

// The tasks done have no view of their own: they stay under 今天完成的 for
// the day, and in the week view on the day they were due.
func TestTodoNoDoneView(t *testing.T) {
	_, tt := newTodoTester(t)
	add(t, tt, "Buy milk")
	click(t, tt, "完成Buy milk")
	if tt.HasText("已完成") || !tt.HasText("今天完成的") || !tt.HasText("Buy milk") {
		t.Fatalf("after checking a task off: %q", tt.Texts())
	}
	click(t, tt, "按周")
	if !tt.HasText("Buy milk") {
		t.Errorf("the week does not show the task done: %q", tt.Texts())
	}
}

// Tasks are not sorted into lists and have no priority: nothing of either
// shows, for a task that was saved with them too.
func TestTodoNoListsOrPriority(t *testing.T) {
	d, tt := newTodoTester(t)
	d.Add(todo.Task{Title: "Buy milk", Due: d.Today(), List: 7, Priority: 3})
	tt.Frame()
	click(t, tt, "打开Buy milk")
	for _, text := range []string{"清单", "收集箱", "新建清单", "优先级", "!!!"} {
		if tt.HasText(text) {
			t.Errorf("the page shows %q: %q", text, tt.Texts())
		}
	}
}

// The + brings a dialog that adds a task: what repeats and when it is due
// are set there, before it is added. Left by Escape, 取消 or a click outside,
// it adds nothing, and starts blank the next time.
func TestTodoAddDialog(t *testing.T) {
	d, tt := newTodoTester(t)
	// No button adds a task, and no line says how: the context menus do.
	if tt.HasText("添加任务") || tt.HasText("点右键") {
		t.Fatalf("the page has a button that adds a task, or says how: %q", tt.Texts())
	}
	newTask(t, tt)
	if !tt.HasText("任务标题") {
		t.Fatalf("no dialog after 新任务: %q", tt.Texts())
	}
	// The keyboard is in the title as the dialog shows.
	tt.Type("Check upstream")
	click(t, tt, "每周", "明天", "添加")
	ks := d.Tasks()
	if len(ks) != 1 || ks[0].Title != "Check upstream" || ks[0].Repeat != todo.RepeatWeekly || ks[0].Due <= d.Today() {
		t.Fatalf("after adding a weekly task for tomorrow: %+v", ks)
	}
	if tt.HasText("任务标题") {
		t.Fatalf("the dialog still shows after adding")
	}

	// Without a title nothing is added, by the button or by Enter, and the
	// dialog stays.
	newTask(t, tt)
	click(t, tt, "添加")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(d.Tasks()) != 1 || !tt.HasText("任务标题") {
		t.Fatalf("after 添加 without a title: %d tasks, %q", len(d.Tasks()), tt.Texts())
	}
	click(t, tt, "取消")

	for name, leave := range map[string]func(){
		"取消":              func() { click(t, tt, "取消") },
		"Escape":          func() { tt.Key(0, ui.KeyEscape) },
		"a click outside": func() { tt.ClickAt(8, 400) },
	} {
		newTask(t, tt)
		tt.Type("Nope")
		leave()
		tt.Frame()
		if len(d.Tasks()) != 1 || tt.HasText("任务标题") {
			t.Fatalf("after leaving the dialog by %s: %d tasks, %q", name, len(d.Tasks()), tt.Texts())
		}
		// What was written is not there the next time.
		newTask(t, tt)
		if tt.HasText("Nope") {
			t.Fatalf("the dialog left by %s shows what was written again", name)
		}
		click(t, tt, "取消")
	}

	// A day picked in the dialog's calendar is the task's.
	day, _ := time.Parse("2006-01-02", d.Today())
	far := day.AddDate(0, 0, 3)
	if far.Month() != day.Month() {
		far = day.AddDate(0, 0, -3)
	}
	newTask(t, tt)
	tt.Type("Buy milk")
	click(t, tt, "其他日期", fmt.Sprintf("%d月%d日", int(far.Month()), far.Day()), "添加")
	if ks := d.Tasks(); len(ks) != 2 || ks[1].Title != "Buy milk" || ks[1].Due != far.Format("2006-01-02") {
		t.Fatalf("after adding a task for a day of the calendar: %+v", ks)
	}
}

// The dialog fits a window as low as the lowest, at the largest size: with
// the calendar open, its buttons are still in reach.
func TestTodoAddDialogInLowWindow(t *testing.T) {
	a := newApp(store.Open(""))
	a.settings.Size = "xlarge"
	tt := ui.NewTester(a.view, 860, 520)
	d := a.current().(*todo.Todo)
	newTask(t, tt)
	tt.Type("Buy milk")
	click(t, tt, "其他日期")
	tt.Frame()
	if !tt.HasText("上个月") {
		t.Fatalf("no calendar in the dialog: %q", tt.Texts())
	}
	// The heading and the buttons are in the window: what scrolls is
	// between them.
	for _, text := range []string{"新任务", "取消", "添加"} {
		if r, ok := tt.Find(text); !ok || r.Y < 0 || r.Y+r.H > 520 {
			t.Errorf("%q is at %+v, outside the window", text, r)
		}
	}
	click(t, tt, "添加")
	if len(d.Tasks()) != 1 {
		t.Fatalf("%d tasks after 添加 in a low window", len(d.Tasks()))
	}
}

// A task is never left without a title: one erased in its details keeps
// the title it had.
func TestTodoTitleKept(t *testing.T) {
	d, tt := newTodoTester(t)
	add(t, tt, "Buy milk")
	k := d.Tasks()[0]
	click(t, tt, "打开Buy milk", "任务标题")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	if k.Title != "Buy milk" {
		t.Fatalf("with its title erased, the task is called %q", k.Title)
	}
	click(t, tt, "关闭详情")
	if len(d.Tasks()) != 1 || k.Title != "Buy milk" || !tt.HasText("Buy milk") {
		t.Fatalf("after closing the details of a task with its title erased: %+v", d.Tasks())
	}
	// Written anew, it is the new title.
	click(t, tt, "打开Buy milk", "任务标题")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Buy oat milk")
	tt.Key(0, ui.KeyEnter)
	if k.Title != "Buy oat milk" || !tt.HasText("Buy oat milk") {
		t.Fatalf("after writing a new title: %q, %q", k.Title, tt.Texts())
	}
}

// Of the chips for the day, one is chosen at a time: 其他日期 brings the
// calendar without looking chosen beside another, and is the day itself
// once one is picked there.
func TestTodoOtherDate(t *testing.T) {
	d, tt := newTodoTester(t)
	add(t, tt, "Buy milk")
	k := d.Tasks()[0]
	click(t, tt, "打开Buy milk", "其他日期")
	if !tt.HasText("其他日期") || k.Due != d.Today() {
		t.Fatalf("opening the calendar changed the day to %q", k.Due)
	}
	day, _ := time.Parse("2006-01-02", d.Today())
	far := day.AddDate(0, 0, 3)
	if far.Month() != day.Month() {
		far = day.AddDate(0, 0, -3)
	}
	click(t, tt, fmt.Sprintf("%d月%d日", int(far.Month()), far.Day()))
	if k.Due != far.Format("2006-01-02") || !tt.HasText(fmt.Sprintf("%d月%d日", int(far.Month()), far.Day())) {
		t.Fatalf("after picking a day in the calendar: due %q, texts %q", k.Due, tt.Texts())
	}
	// A quick day closes the calendar again.
	click(t, tt, "无日期")
	if k.Due != "" || tt.HasText("上个月") {
		t.Errorf("after 无日期: due %q, the calendar still shows: %v", k.Due, tt.HasText("上个月"))
	}
}

// A task is made to repeat weekly from its menu.
func TestTodoWeeklyRepeat(t *testing.T) {
	d, tt := newTodoTester(t)
	add(t, tt, "Check upstream")
	add(t, tt, "Buy milk")
	ks := d.Tasks()
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
	// Added in this week, a task is due today; in another, on its Monday.
	click(t, tt, "按周")
	add(t, tt, "大扫除")
	if k := d.Tasks()[0]; k.Due != d.Today() {
		t.Fatalf("task added in this week is due %s, today is %s", k.Due, d.Today())
	}
	click(t, tt, "下一周")
	if tt.HasText("大扫除") {
		t.Errorf("next week shows this week's task")
	}
	add(t, tt, "Water plants")
	day, err := time.Parse("2006-01-02", d.Tasks()[1].Due)
	if err != nil || day.Weekday() != time.Monday || d.Tasks()[1].Due <= d.Today() {
		t.Fatalf("task added in next week: %+v", d.Tasks()[1])
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
// as "按周" for the week view or "打开写周报" for a task's details; "右键:"
// before one clicks it with the secondary button, as "右键:任务列表,新任务"
// for the dialog that adds a task.
func TestScreenshot(t *testing.T) {
	path := os.Getenv("ATELIER_SCREENSHOT")
	if path == "" {
		t.Skip("set ATELIER_SCREENSHOT to write a screenshot")
	}
	a := newApp(store.Open(""))
	// ATELIER_SCREENSHOT_STYLE picks the style: neumorphism or organic.
	a.settings.Style = os.Getenv("ATELIER_SCREENSHOT_STYLE")
	// ATELIER_SCREENSHOT_THEME picks the appearance: light, dark or system.
	if m := os.Getenv("ATELIER_SCREENSHOT_THEME"); m != "" {
		a.settings.Appearance = m
	}
	a.win = &fakeWindow{}
	d := a.current().(*todo.Todo)
	today, _ := time.Parse("2006-01-02", d.Today())
	day := func(n int) string { return today.AddDate(0, 0, n).Format("2006-01-02") }
	for _, k := range []todo.Task{
		{Title: "交水电费", Due: day(-1)},
		{Title: "写周报", Due: day(0), Repeat: todo.RepeatWeekly},
		{Title: "修登录页的 bug", Due: day(0), Note: "复现步骤在群里"},
		{Title: "跑步 30 分钟", Due: day(0), Repeat: todo.RepeatDaily},
		{Title: "给家里打电话", Due: day(1)},
		{Title: "读完《置身事内》", Due: day(3)},
		{Title: "采购日用品", Due: day(5)},
		{Title: "整理书架"},
	} {
		d.Add(k)
	}
	d.SetDone(d.Add(todo.Task{Title: "预约体检", Due: day(0)}), true)
	tt := ui.NewTester(a.view, 1280, 800)
	// ATELIER_SCREENSHOT_SIZE is the window's size, as 860x520.
	var w, h int
	if n, _ := fmt.Sscanf(os.Getenv("ATELIER_SCREENSHOT_SIZE"), "%dx%d", &w, &h); n == 2 {
		tt.SetSize(w, h)
	}
	if os.Getenv("ATELIER_SCREENSHOT_SCALE") == "2" {
		tt.SetScale(2)
	}
	if s := os.Getenv("ATELIER_SCREENSHOT_CLICK"); s != "" {
		for _, label := range strings.Split(s, ",") {
			if target, ok := strings.CutPrefix(label, "右键:"); ok {
				rightClick(t, tt, target)
			} else {
				click(t, tt, label)
			}
		}
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
	// The switch is in the first part of the settings; the version in
	// another, chosen in the menu down their side.
	click(t, tt, "设置", "浅色")
	if tt.HasText("版本") {
		t.Errorf("the settings show the version beside how the app looks")
	}
	click(t, tt, "关于")
	if !tt.HasText("版本") || !tt.HasText("开发版") || tt.HasText("浅色") {
		t.Errorf("the part about the app: %q", tt.Texts())
	}
	if a.settings.Appearance != string(mallow.ModeLight) || mallow.Base != mallow.LightPalette.Base {
		t.Fatalf("after choosing light: appearance %q, base %v", a.settings.Appearance, mallow.Base)
	}
	// The gear goes back to the feature the settings were opened from.
	click(t, tt, "设置")
	if a.selected != "todo" {
		t.Errorf("after the gear a second time, %q is selected", a.selected)
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

// The style is chosen on the settings page, and saved.
func TestStyle(t *testing.T) {
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if mallow.CurrentLook() != mallow.Neumorphism {
		t.Fatalf("a first start is not in neumorphism")
	}
	click(t, tt, "设置", "自然有机")
	if a.settings.Style != string(mallow.StyleOrganic) || mallow.CurrentLook() != mallow.Organic {
		t.Fatalf("after choosing 自然有机: style %q", a.settings.Style)
	}
	// The to-do page works as it does in the other look.
	click(t, tt, "待办")
	add(t, tt, "Buy milk")
	click(t, tt, "打开Buy milk", "每周", "关闭详情")
	if k := a.current().(*todo.Todo).Tasks()[0]; !tt.HasText("Buy milk") || k.Repeat != todo.RepeatWeekly {
		t.Errorf("the task edited on the organic to-do page: %+v, %q", k, tt.Texts())
	}
	b := newApp(store.Open(dir))
	ui.NewTester(b.view, 1280, 800).Frame()
	if b.settings.Style != string(mallow.StyleOrganic) || mallow.CurrentLook() != mallow.Organic {
		t.Errorf("after restarting: style %q", b.settings.Style)
	}
}

// The size zooms the whole interface, and the font is the theme's: both are
// saved.
func TestTextSettings(t *testing.T) {
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	tt := ui.NewTester(a.view, 1280, 800)
	click(t, tt, "设置", "特大")
	if a.settings.Size != "xlarge" || zoomOf(a.settings.Size) != 1.25 {
		t.Fatalf("after choosing 特大: size %q", a.settings.Size)
	}
	// At the largest size the page still works.
	click(t, tt, "待办")
	add(t, tt, "Buy milk")
	if !tt.HasText("Buy milk") {
		t.Errorf("no task added at the largest size: %q", tt.Texts())
	}
	// The default font is always offered; another where it is installed.
	click(t, tt, "设置", "微软雅黑")
	if a.settings.Font != "" || mallow.Font != defaultFont {
		t.Errorf("after choosing the default font: %q, drawn in %q", a.settings.Font, mallow.Font)
	}
	// Not one to download: this test asks nothing of the network.
	for _, f := range a.fontChoices()[1:] {
		if f.web != nil {
			continue
		}
		click(t, tt, f.label)
		tt.Frame()
		if a.settings.Font != f.family || mallow.Font != f.family {
			t.Errorf("after choosing %s: %q, drawn in %q", f.label, a.settings.Font, mallow.Font)
		}
		break
	}
	b := newApp(store.Open(dir))
	ui.NewTester(b.view, 1280, 800).Frame()
	if b.settings.Size != "xlarge" || b.settings.Font != a.settings.Font {
		t.Errorf("after restarting: size %q, font %q", b.settings.Size, b.settings.Font)
	}
	mallow.Font = defaultFont
}

// Choosing another font moves nothing: every text of the pages is at the
// height it was, and as high, in each font the system has, at each size.
// Only what a font draws in its place differs.
func TestFontsKeepLayout(t *testing.T) {
	defer func() { mallow.Font = defaultFont }()
	a := newApp(store.Open(""))
	st := ui.NewTester(a.view, 1280, 800)
	add(t, st, "Buy milk")
	click(t, st, "打开Buy milk")

	places := func(tt *ui.Tester) map[string]place {
		tt.Frame()
		tt.Frame()
		out := map[string]place{}
		for _, text := range tt.Texts() {
			if r, ok := tt.Find(text); ok {
				out[text] = place{r.Y, r.H}
			}
		}
		return out
	}
	fonts := 0
	for _, page := range []string{"待办", "月历", "设置"} {
		click(t, st, page)
		for _, s := range sizes {
			fonts += steady(t, a, s.id, page, func() map[string]place { return places(st) })
		}
	}
	// The dialog that adds a task, its calendar open.
	a.settings.Size, a.settings.Font = "", ""
	st.Frame()
	click(t, st, "待办")
	newTask(t, st)
	click(t, st, "其他日期")
	st.Advance(time.Second)
	for _, s := range sizes {
		fonts += steady(t, a, s.id, "新任务", func() map[string]place { return places(st) })
	}
	if fonts == 0 {
		t.Skip("the system has none of the fonts to choose from")
	}
}

// place is where a text is down the page, and how high.
type place struct{ y, h float32 }

// steady reports the places that differ, of those places returns, between
// the default font and each other the system has, at a size; and how many
// fonts it looked at.
func steady(t *testing.T, a *app, size, page string, places func() map[string]place) int {
	t.Helper()
	a.settings.Size = size
	var base map[string]place
	fonts := 0
	for _, f := range a.fontChoices() {
		if f.web != nil {
			continue
		}
		a.settings.Font = f.family
		got := places()
		if base == nil {
			base = got
			continue
		}
		fonts++
		for text, want := range base {
			if got[text] != want {
				t.Errorf("%s at size %s in %s: %q is at %+v, in the default font at %+v", page, size, f.label, text, got[text], want)
			}
		}
	}
	return fonts
}

// A font the system does not have is downloaded when chosen, kept, and
// used again after a restart without another download.
func TestWebFont(t *testing.T) {
	dir := t.TempDir()
	font := &webFont{family: "Test Web Font", file: "test.ttf", size: 4}
	webFonts["测试字体"] = font
	knownFonts = append(knownFonts, struct {
		label string
		names []string
	}{"测试字体", []string{"Test Web Font"}})
	defer func() {
		delete(webFonts, "测试字体")
		knownFonts = knownFonts[:len(knownFonts)-1]
		mallow.Font = defaultFont
	}()

	fetched, fetchErr := 0, error(nil)
	var registered []string
	fake := func(a *app) {
		a.webFonts.fetch = func(_ context.Context, _ *webFont, progress func(float64)) ([]byte, error) {
			fetched++
			progress(0.5)
			return []byte("font"), fetchErr
		}
		a.webFonts.register = func(data []byte, family string) error {
			registered = append(registered, family+":"+string(data))
			return nil
		}
	}
	a := newApp(store.Open(dir))
	fake(a)
	tt := ui.NewTester(a.view, 1280, 800)
	click(t, tt, "设置")
	// A download that fails says so and changes nothing.
	fetchErr = errFontChanged
	click(t, tt, "测试字体 ↓")
	tt.Frame()
	if a.settings.Font != "" || !tt.HasText("测试字体 没有下载成功：下载到的文件和预期的不一样") {
		t.Fatalf("after a failed download: font %q, %q", a.settings.Font, tt.Texts())
	}
	fetchErr = nil
	click(t, tt, "测试字体 ↓")
	tt.Frame()
	if a.settings.Font != font.family || mallow.Font != font.family || !slices.Equal(registered, []string{"Test Web Font:font"}) {
		t.Fatalf("after downloading: font %q, drawn in %q, registered %q", a.settings.Font, mallow.Font, registered)
	}
	// Here, it no longer says it is to download, and is not fetched again.
	if !tt.HasText("测试字体") || tt.HasText("测试字体 ↓") {
		t.Errorf("the font downloaded still says it is to download: %q", tt.Texts())
	}
	click(t, tt, "微软雅黑", "测试字体")
	if fetched != 2 || a.settings.Font != font.family || len(registered) != 1 {
		t.Errorf("choosing it again: fetched %d times, font %q, registered %q", fetched, a.settings.Font, registered)
	}

	// After a restart it is read from where it was kept.
	registered = nil
	b := newApp(store.Open(dir))
	fake(b)
	b.restoreFont()
	if !slices.Equal(registered, []string{"Test Web Font:font"}) || fetched != 2 {
		t.Errorf("after restarting: registered %q, fetched %d times", registered, fetched)
	}
}

// Closing the window keeps the app in the notification area unless the
// settings say it quits, which is saved; quitting closes it either way.
func TestCloseToTray(t *testing.T) {
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	tt := ui.NewTester(a.view, 1280, 800)
	if !staysInTray(a.settings, false) || staysInTray(a.settings, true) {
		t.Fatalf("at first: closing stays %v, quitting stays %v", staysInTray(a.settings, false), staysInTray(a.settings, true))
	}
	click(t, tt, "设置", "通用", "留在托盘")
	if !a.settings.CloseQuits || staysInTray(a.settings, false) {
		t.Fatalf("after turning it off: %+v", a.settings.CloseQuits)
	}
	if b := newApp(store.Open(dir)); !b.settings.CloseQuits {
		t.Errorf("after restarting, closing the window does not quit")
	}
	click(t, tt, "留在托盘")
	if a.settings.CloseQuits {
		t.Errorf("after turning it on again, closing the window quits")
	}
}

// A known font is found under the name its foundry gives the version
// installed, and the plain family before its weights.
func TestInstalledFont(t *testing.T) {
	families := []string{"Alibaba PuHuiTi 3.0 Heavy", "Alibaba PuHuiTi 3.0", "SimSun-ExtB", "SimSun", "noto sans sc"}
	for _, c := range []struct {
		names []string
		want  string
	}{
		{[]string{"Alibaba PuHuiTi"}, "Alibaba PuHuiTi 3.0"},
		{[]string{"SimSun"}, "SimSun"},
		{[]string{"Source Han Sans SC", "Noto Sans SC"}, "noto sans sc"},
		{[]string{"LXGW WenKai"}, ""},
	} {
		if got := installed(families, c.names); got != c.want {
			t.Errorf("installed(%q) = %q, want %q", c.names, got, c.want)
		}
	}
}

// A provider is added empty, whatever its interface: its address, its key
// and its models are the user's to fill in. The key is saved sealed; a
// model added is the default until another is; the service's own list
// adds models too.
func TestModelSettings(t *testing.T) {
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	var pinged []llm.Config
	var answer error
	a.ping = func(_ context.Context, c llm.Config) error {
		pinged = append(pinged, c)
		return answer
	}
	var listed []llm.Config
	a.listModels = func(_ context.Context, c llm.Config) ([]string, error) {
		listed = append(listed, c)
		return []string{"some-model", "other-model"}, nil
	}
	tt := ui.NewTester(a.view, 1280, 900)
	click(t, tt, "设置", "模型", "添加供应商")
	if len(a.settings.Models) != 1 {
		t.Fatalf("after adding a provider: %+v", a.settings.Models)
	}
	// Nothing is filled in for the user, for any interface.
	for _, kind := range []string{"Anthropic", "Responses", "Chat Completions"} {
		click(t, tt, kind)
		if m := a.settings.Models[0]; m.Name != "" || m.BaseURL != "" || len(m.Models) != 0 || m.Model != "" {
			t.Fatalf("a new provider of %s has something set: %+v", kind, m)
		}
		if tt.HasText("claude") {
			t.Fatalf("a new provider of %s names models: %q", kind, tt.Texts())
		}
	}
	if _, ok := a.DefaultModel(); ok {
		t.Errorf("there is a default model before any is added")
	}
	// Tested without a model, it says what is missing.
	click(t, tt, "测试连接")
	if len(pinged) != 0 || !tt.HasText("先添加一个模型") {
		t.Fatalf("a test without a model: sent %+v, %q", pinged, tt.Texts())
	}

	click(t, tt, "供应商名称")
	tt.Type("Example")
	click(t, tt, "接口地址")
	tt.Type("https://api.example.com/v1")
	click(t, tt, "API Key")
	tt.Type("sk-secret-123")
	// The dialog that adds models asks the service for its list, with the
	// key there is now; a name typed is added by Enter, listed or not.
	click(t, tt, "添加模型")
	tt.Frame()
	if len(listed) != 1 || listed[0].Key != "sk-secret-123" || listed[0].BaseURL != "https://api.example.com/v1" {
		t.Fatalf("the list was asked with %+v", listed)
	}
	tt.Type("some-model")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	m := a.settings.Models[0]
	if m.Name != "Example" || m.Kind != string(llm.Chat) || m.BaseURL != "https://api.example.com/v1" || !slices.Equal(m.Models, []string{"some-model"}) {
		t.Fatalf("the provider filled in: %+v", m)
	}
	if m.Key == "" || strings.Contains(m.Key, "sk-secret") {
		t.Fatalf("the key is saved as %q", m.Key)
	}
	if tt.HasText("sk-secret-123") {
		t.Errorf("the key shows on the page")
	}
	// The settings file holds the sealed key alone.
	if data, err := os.ReadFile(filepath.Join(dir, "settings.json")); err != nil || strings.Contains(string(data), "sk-secret") {
		t.Fatalf("settings.json: %v, holds the key: %v", err, strings.Contains(string(data), "sk-secret"))
	}
	// The list offers the models not added yet, and what is typed narrows
	// it; a name it does not have can be added all the same.
	if shown(tt, "添加other-model") != 1 || shown(tt, "添加some-model") != 0 || !tt.HasText("已添加") {
		t.Fatalf("the list offers: %q", tt.Texts())
	}
	tt.Type("zzz")
	tt.Frame()
	if shown(tt, "添加other-model") != 0 || shown(tt, "添加zzz") != 1 {
		t.Fatalf("with zzz typed, the list offers: %q", tt.Texts())
	}
	for range 3 {
		tt.Key(0, ui.KeyBackspace)
	}
	click(t, tt, "完成")
	// The first model added is the default.
	want := llm.Config{Kind: llm.Chat, BaseURL: "https://api.example.com/v1", Key: "sk-secret-123", Model: "some-model"}
	if c, ok := a.DefaultModel(); !ok || c != want {
		t.Fatalf("the default model is %+v, %v", c, ok)
	}

	click(t, tt, "测试连接")
	tt.Frame()
	if len(pinged) != 1 || pinged[0] != want || !tt.HasText("连接正常，some-model 有回应") {
		t.Fatalf("the test sent %+v; texts %q", pinged, tt.Texts())
	}
	answer = &llm.Error{Message: "Key 不对，或者没有权限（401）"}
	click(t, tt, "测试连接")
	tt.Frame()
	if !tt.HasText("Key 不对，或者没有权限（401）") {
		t.Errorf("a failed test does not say why: %q", tt.Texts())
	}

	click(t, tt, "添加模型")
	tt.Frame()
	click(t, tt, "添加other-model")
	if got := a.settings.Models[0].Models; !slices.Equal(got, []string{"some-model", "other-model"}) {
		t.Fatalf("after adding from the list: %q", got)
	}
	click(t, tt, "完成")
	// Another model is made the default; taking it away gives that back.
	click(t, tt, "把other-model设为默认")
	if c, _ := a.DefaultModel(); c.Model != "other-model" {
		t.Fatalf("after 设为默认: %+v", c)
	}
	// After a restart the provider is there, and its key opens.
	want.Model = "other-model"
	if c, ok := newApp(store.Open(dir)).DefaultModel(); !ok || c != want {
		t.Fatalf("after restarting, the default model is %+v, %v", c, ok)
	}
	click(t, tt, "移除other-model")
	if c, ok := a.DefaultModel(); !ok || c.Model != "some-model" {
		t.Fatalf("after removing the default model: %+v, %v", c, ok)
	}

	// A second provider's model can be the default; deleting that provider
	// gives it back to the first.
	click(t, tt, "添加供应商", "添加模型")
	tt.Type("second-model")
	tt.Key(0, ui.KeyEnter)
	click(t, tt, "完成", "把second-model设为默认")
	if a.settings.Model != a.settings.Models[1].ID || a.settings.ModelName != "second-model" {
		t.Fatalf("after 设为默认 in a second provider: %d %q", a.settings.Model, a.settings.ModelName)
	}
	click(t, tt, "删除供应商")
	if len(a.settings.Models) != 1 || a.settings.Model != a.settings.Models[0].ID || a.settings.ModelName != "some-model" {
		t.Errorf("after deleting the default's provider: %+v, default %d %q", a.settings.Models, a.settings.Model, a.settings.ModelName)
	}
}

// Settings saved when each entry was one model are read as providers: the
// model becomes its provider's, and the default entry's the default.
func TestModelSettingsOfOld(t *testing.T) {
	dir := t.TempDir()
	old := `{"models":[{"id":1,"name":"Claude","kind":"anthropic","model":"claude-opus-5-5"},{"id":2,"name":"DS","kind":"chat","baseURL":"https://api.example.com/v1","model":"deep"}],"model":2,"nextModel":2}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp(store.Open(dir))
	ms := a.settings.Models
	if len(ms) != 2 || !slices.Equal(ms[0].Models, []string{"claude-opus-5-5"}) || !slices.Equal(ms[1].Models, []string{"deep"}) || ms[0].Model != "" {
		t.Fatalf("the providers read: %+v", ms)
	}
	if c, ok := a.DefaultModel(); !ok || c.Model != "deep" || c.Kind != llm.Chat {
		t.Errorf("the default model read: %+v, %v", c, ok)
	}
}

// A newer version is found in the settings, installed, and run by a
// relaunch; a build that cannot update says so.
func TestUpdate(t *testing.T) {
	a := newApp(store.Open(""))
	tt := ui.NewTester(a.view, 1280, 800)
	click(t, tt, "设置", "关于")
	if !tt.HasText("这个版本不能自动更新：它不是发布版，或者装在了没有写入权限的位置") || tt.HasText("检查更新") {
		t.Fatalf("a build without updates: %q", tt.Texts())
	}

	var found *newVersion
	var checkErr, installErr error
	relaunched := false
	a.updater = updater{
		enabled:  func() bool { return true },
		check:    func(context.Context) (*newVersion, error) { return found, checkErr },
		relaunch: func() { relaunched = true },
	}
	// Quiet, as at launch, a check that finds nothing says nothing.
	a.checkForUpdate(true)
	tt.Frame()
	if a.update.phase != updateIdle || !tt.HasText("检查更新") {
		t.Fatalf("after a quiet check that found nothing: phase %d", a.update.phase)
	}
	click(t, tt, "检查更新")
	tt.Frame()
	if !tt.HasText("已经是最新版本") {
		t.Fatalf("after a check that found nothing: %q", tt.Texts())
	}

	found = &newVersion{
		version: "9.9.9",
		notes:   "### 新功能\n\n可以自动更新了\n",
		install: func(_ context.Context, progress func(float64)) error {
			progress(0.5)
			return installErr
		},
	}
	// Found as the app starts, it waits in the settings, with its notes.
	a.checkForUpdate(true)
	tt.Frame()
	if a.update.phase != updateAvailable || !tt.HasText("发现新版本 v9.9.9") || !tt.HasText("· 新功能\n\n可以自动更新了") {
		t.Fatalf("after finding a version: phase %d, %q", a.update.phase, tt.Texts())
	}
	// A download that fails can be tried again.
	installErr = errors.New("connection reset")
	click(t, tt, "下载并安装")
	tt.Frame()
	if a.update.phase != updateFailed || !tt.HasText("没有成功：connection reset") {
		t.Fatalf("after a failed download: phase %d, %q", a.update.phase, tt.Texts())
	}
	installErr = nil
	click(t, tt, "再试一次")
	tt.Frame()
	if a.update.phase != updateReady || !tt.HasText("v9.9.9 已经装好，重启后生效") {
		t.Fatalf("after installing: phase %d, %q", a.update.phase, tt.Texts())
	}
	click(t, tt, "立即重启")
	if !relaunched {
		t.Errorf("立即重启 did not relaunch the app")
	}
}

func TestPlainNotes(t *testing.T) {
	got := plainNotes("### 标题一\n\n\n正文\n- 一条\n\n## 标题二\r\n")
	if want := "· 标题一\n\n正文\n- 一条\n\n· 标题二"; got != want {
		t.Errorf("plainNotes: %q, want %q", got, want)
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
