package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/pandyzhou/atelier/internal/features/home"
	"github.com/pandyzhou/atelier/internal/features/timetable"
	"github.com/pandyzhou/atelier/internal/features/todo"
	"github.com/pandyzhou/atelier/internal/features/trending"
	"github.com/pandyzhou/atelier/internal/llm"
	"github.com/pandyzhou/atelier/internal/store"
	"github.com/pandyzhou/mallow"
)

// The view runs without a window in tests, which click and type as a user
// would. An empty store keeps nothing on disk.
func newTodoTester(t *testing.T) (*todo.Todo, *ui.Tester) {
	t.Helper()
	a := newTodoApp(store.Open(""))
	a.win = &fakeWindow{}
	tt := ui.NewTester(a.view, 1280, 800)
	if !tt.HasText("待办") {
		t.Fatalf("sidebar missing the to-do list, texts %q", tt.Texts())
	}
	return a.current().(*todo.Todo), tt
}

// newTodoApp is the app as most tests want it: on the to-do list, not on
// the first page.
func newTodoApp(s *store.Store) *app {
	a := newApp(s)
	a.selected = "todo"
	return a
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
	a := newTodoApp(store.Open(""))
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

// The timetable stands between the to-do list and the calendar. A right
// click on a period of a day adds a course there, over the periods chosen;
// it shows on the grid, opens to be changed, and cannot take the periods of
// another. The day's periods and their times are set in a dialog.
func TestTimetable(t *testing.T) {
	dir := t.TempDir()
	a := newTodoApp(store.Open(dir))
	a.win = &fakeWindow{}
	tt := ui.NewTester(a.view, 1280, 800)
	var titles []string
	for _, f := range a.features {
		titles = append(titles, f.Title())
	}
	if !slices.Equal(titles, []string{"首页", "待办", "课表", "月历", "热门"}) {
		t.Fatalf("the features are %q", titles)
	}
	click(t, tt, "课表")
	tb := a.current().(*timetable.Timetable)
	if len(tb.Periods()) != 10 || len(tb.Courses()) != 0 {
		t.Fatalf("a new timetable has %d periods and %d courses", len(tb.Periods()), len(tb.Courses()))
	}

	// Until it is asked to be changed the timetable only shows: a click
	// on a cell does nothing, and the periods' button is not there.
	click(t, tt, "周三第3节")
	if tt.HasText("新课程") || tt.HasText("节次时间") {
		t.Fatalf("the timetable can be changed without asking: %q", tt.Texts())
	}
	click(t, tt, "编辑课表")
	if !tt.HasText("节次时间") || !tt.HasText("完成编辑") {
		t.Fatalf("after 编辑课表: %q", tt.Texts())
	}
	rightClick(t, tt, "周三第3节")
	click(t, tt, "新课程")
	if !tt.HasText("第 3 节") {
		t.Fatalf("the dialog is not on the period clicked: %q", tt.Texts())
	}
	tt.Type("高等数学")
	click(t, tt, "上课地点")
	tt.Type("A301")
	click(t, tt, "第4节", "添加")
	if len(tb.Courses()) != 1 {
		t.Fatalf("after adding: %d courses (texts %q)", len(tb.Courses()), tt.Texts())
	}
	if k := tb.Courses()[0]; k.Name != "高等数学" || k.Room != "A301" || k.Day != 2 || k.From != 3 || k.To != 4 {
		t.Fatalf("the course added: %+v", k)
	}
	if !tt.HasText("高等数学") || !tt.HasText("A301") || tt.HasText("编辑课程") {
		t.Fatalf("the course does not show on the grid: %q", tt.Texts())
	}
	// A click on a free cell adds a course there, and a press drawn over
	// cells one that takes them all, from whichever end it began.
	click(t, tt, "周五第2节")
	if !tt.HasText("新课程") || !tt.HasText("节次：第 2 节") {
		t.Fatalf("a click on a cell: %q", tt.Texts())
	}
	click(t, tt, "取消")
	first, _ := tt.Find("周五第6节")
	last, _ := tt.Find("周五第8节")
	tt.Press(last.X+last.W/2, last.Y+last.H/2)
	tt.Move(first.X+first.W/2, first.Y+first.H/2)
	tt.Frame()
	if tt.HasText("新课程") {
		t.Fatalf("the dialog shows before the pointer lets go: %q", tt.Texts())
	}
	tt.Release(first.X+first.W/2, first.Y+first.H/2)
	tt.Frame()
	if !tt.HasText("新课程") || !tt.HasText("节次：第 6–8 节") {
		t.Fatalf("after drawing over three cells: %q", tt.Texts())
	}
	tt.Type("实验")
	click(t, tt, "添加")
	if k := tb.Courses()[1]; len(tb.Courses()) != 2 || k.Name != "实验" || k.Day != 4 || k.From != 6 || k.To != 8 {
		t.Fatalf("the course drawn: %+v", k)
	}
	tb.Remove(tb.Courses()[1].ID)
	tt.Frame()

	// It is two rows high.
	one, _ := tt.Find("周三第3节")
	block, ok := tt.Find("打开高等数学")
	if !ok || block.H < one.H*1.5 || block.H > one.H*2 || block.Y < one.Y {
		t.Errorf("the course is at %+v, a period at %+v", block, one)
	}

	// Another course cannot take its periods.
	rightClick(t, tt, "周三第5节")
	click(t, tt, "新课程")
	tt.Type("英语")
	click(t, tt, "第4节", "添加")
	if len(tb.Courses()) != 1 || !tt.HasText("已经有“高等数学”了") {
		t.Fatalf("a clashing course: %d courses, %q", len(tb.Courses()), tt.Texts())
	}
	// Chosen anew, on a period that is free, it is added.
	click(t, tt, "第5节", "添加")
	if len(tb.Courses()) != 2 || tb.Courses()[1].From != 5 || tb.Courses()[1].To != 5 {
		t.Fatalf("after choosing a free period: %+v", tb.Courses())
	}

	// A click opens a course to change it, another day too.
	click(t, tt, "打开英语", "周五", "保存")
	if k := tb.Courses()[1]; k.Name != "英语" || k.Day != 4 {
		t.Fatalf("after moving 英语 to Friday: %+v", k)
	}
	// Its menu deletes it.
	rightClick(t, tt, "打开英语")
	click(t, tt, "删除课程")
	if len(tb.Courses()) != 1 {
		t.Fatalf("after deleting: %+v", tb.Courses())
	}

	// The periods: more of them, times for them, and never fewer than the
	// courses go to.
	click(t, tt, "节次时间", "增加一节", "第3节开始")
	tt.Type("10:00")
	click(t, tt, "第4节结束")
	tt.Type("11:40")
	click(t, tt, "完成")
	if len(tb.Periods()) != 11 || tb.Periods()[2].Start != "10:00" || !tt.HasText("10:00") {
		t.Fatalf("the periods: %+v, texts %q", tb.Periods(), tt.Texts())
	}
	click(t, tt, "打开高等数学")
	if !tt.HasText("10:00–11:40") {
		t.Errorf("the course's dialog does not say its time: %q", tt.Texts())
	}
	click(t, tt, "取消")
	tb.SetPeriods(1)
	if len(tb.Periods()) != 4 {
		t.Errorf("with a course to the 4th period, the periods can be made %d", len(tb.Periods()))
	}

	// After a restart it is all there.
	b := newTodoApp(store.Open(dir))
	for _, f := range b.features {
		if again, ok := f.(*timetable.Timetable); ok {
			if len(again.Courses()) != 1 || again.Courses()[0].Name != "高等数学" || again.Periods()[2].Start != "10:00" {
				t.Errorf("after restarting: %+v, %+v", again.Courses(), again.Periods())
			}
		}
	}
}

// The app opens on the first page. What is written there goes to the
// default model with the tasks and the timetable as they are; the tools it
// calls add a task and a course, their results go back to it, and what it
// says, and what was done, shows.
func TestHome(t *testing.T) {
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	a.win = &fakeWindow{}
	var tasks *todo.Todo
	var table *timetable.Timetable
	for _, f := range a.features {
		switch f := f.(type) {
		case *todo.Todo:
			tasks = f
		case *timetable.Timetable:
			table = f
		}
	}
	tasks.Add(todo.Task{Title: "交水电费", Due: tasks.Today()})
	tt := ui.NewTester(a.view, 1280, 800)
	if _, ok := a.current().(*home.Home); !ok {
		t.Fatalf("the app opens on %T", a.current())
	}
	// Without a model there is nothing to send to, and a way to the
	// settings.
	if !tt.HasText("还没有可用的模型") {
		t.Fatalf("no word of the missing model: %q", tt.Texts())
	}
	click(t, tt, "去设置模型")
	if a.selected != settingsID || a.section != "models" {
		t.Fatalf("去设置模型 leads to %q, %q", a.selected, a.section)
	}
	key, err := a.box.Seal("k")
	if err != nil {
		t.Fatal(err)
	}
	a.settings.Models = []modelConfig{
		{ID: 1, Name: "P", Kind: string(llm.Chat), Key: key, Models: []string{"m", "m2"}},
		{ID: 2, Name: "Q", Kind: string(llm.Anthropic), Key: key, Models: []string{"q1"}},
	}
	a.settings.Model, a.settings.ModelName = 1, "m2"
	click(t, tt, "首页")
	h := a.current().(*home.Home)
	// The model asked is chosen in the field, among those of every
	// provider, and is the default from then on.
	click(t, tt, "选择模型")
	tt.Frame()
	if !tt.HasText("q1") || shown(tt, "Q") == 0 {
		t.Fatalf("the models to choose from: %q", tt.Texts())
	}
	click(t, tt, "用模型：m")
	tt.Frame()
	if a.settings.Model != 1 || a.settings.ModelName != "m" || tt.HasText("q1") {
		t.Fatalf("after choosing m: %d %q, texts %q", a.settings.Model, a.settings.ModelName, tt.Texts())
	}

	var asked []llm.Request
	call := func(id, name, input string) llm.Call {
		return llm.Call{ID: id, Name: name, Input: json.RawMessage(input)}
	}
	answers := []llm.Message{
		{Role: llm.Assistant, Calls: []llm.Call{
			call("c1", "add_task", `{"title":"交报告","due":"2030-01-02"}`),
			call("c2", "add_course", `{"name":"高等数学","weekday":3,"from_period":3,"to_period":4,"room":"A301"}`),
		}},
		{Role: llm.Assistant, Text: "都加好了。"},
	}
	// The text comes in two pieces, and shows as far as it has come.
	var growing []string
	a.complete = func(_ context.Context, c llm.Config, r llm.Request, onText func(string)) (llm.Message, error) {
		if c.Key != "k" || c.Model != "m" {
			t.Errorf("asked with %+v", c)
		}
		asked = append(asked, r)
		if len(asked) > len(answers) {
			onText("我来")
			return llm.Message{}, &llm.Error{Message: "Key 不对，或者没有权限（401）"}
		}
		if text := []rune(answers[len(asked)-1].Text); len(text) > 2 {
			onText(string(text[:2]))
			tt.Frame()
			growing = append(growing, strings.Join(tt.Texts(), "|"))
			onText(string(text[2:]))
		}
		return answers[len(asked)-1], nil
	}
	click(t, tt, "给助手的话")
	tt.Type("2030年1月2日交报告，再加一门周三三四节的高等数学")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Frame()

	if len(asked) != 2 {
		t.Fatalf("the model was asked %d times (texts %q)", len(asked), tt.Texts())
	}
	// The model is told what there is, and what it can do.
	if sys := asked[0].System; !strings.Contains(sys, "交水电费") || !strings.Contains(sys, tasks.Today()) {
		t.Errorf("the model is not told the tasks and the day: %s", sys)
	}
	if len(asked[0].Tools) < 2 || len(asked[0].Messages) != 1 || asked[0].Messages[0].Text == "" {
		t.Errorf("the first request: %+v", asked[0])
	}
	// What it called for was done, and it was told so.
	if n := len(tasks.Tasks()); n != 2 || tasks.Tasks()[n-1].Title != "交报告" && tasks.Tasks()[0].Title != "交报告" {
		t.Fatalf("the tasks after: %+v", tasks.Tasks())
	}
	if len(table.Courses()) != 1 || table.Courses()[0].Day != 2 || table.Courses()[0].From != 3 || table.Courses()[0].To != 4 || table.Courses()[0].Room != "A301" {
		t.Fatalf("the courses after: %+v", table.Courses())
	}
	ms := asked[1].Messages
	if len(ms) != 4 || ms[2].Role != llm.Result || ms[2].CallID != "c1" || ms[3].CallID != "c2" || !strings.Contains(ms[3].Text, "已添加") {
		t.Fatalf("the second request's messages: %+v", ms)
	}
	// The second request tells the model of the course there is now.
	if !strings.Contains(asked[1].System, "高等数学") {
		t.Errorf("the model is not told the course it added: %s", asked[1].System)
	}
	if len(growing) != 1 || !strings.Contains(growing[0], "都加") || strings.Contains(growing[0], "都加好了") || strings.Contains(growing[0], "正在想") {
		t.Errorf("as the answer came, the page showed %q", growing)
	}
	if n := strings.Count(strings.Join(tt.Texts(), "|"), "都加好了。"); n != 1 {
		t.Errorf("the answer shows %d times: %q", n, tt.Texts())
	}
	for _, want := range []string{"2030年1月2日交报告", "已添加任务：交报告", "已添加课程：高等数学 · 周三第 3–4 节", "都加好了。"} {
		if !tt.HasText(want) {
			t.Errorf("the conversation does not show %q: %q", want, tt.Texts())
		}
	}

	// A course that clashes is not added, and the model is told why.
	answers = append(answers,
		llm.Message{Role: llm.Assistant, Calls: []llm.Call{call("c3", "add_course", `{"name":"英语","weekday":3,"from_period":4,"to_period":4}`)}},
		llm.Message{Role: llm.Assistant, Text: "那一节已经有课了。"})
	click(t, tt, "给助手的话")
	tt.Type("周三第四节加英语")
	click(t, tt, "发送")
	tt.Frame()
	if len(table.Courses()) != 1 || len(asked) != 4 {
		t.Fatalf("after a clashing course: %d courses, asked %d times", len(table.Courses()), len(asked))
	}
	if last := asked[3].Messages[len(asked[3].Messages)-1]; !strings.Contains(last.Text, "已经有“高等数学”了") {
		t.Errorf("the model is told of the clash: %q", last.Text)
	}

	// A task is changed, checked off and deleted by its ID, which the
	// model is told; one there is not is said to be missing.
	bill := tasks.Tasks()[0]
	for _, k := range tasks.Tasks() {
		if k.Title == "交水电费" {
			bill = k
		}
	}
	if !strings.Contains(asked[0].System, fmt.Sprintf("[%d] ", bill.ID)) {
		t.Errorf("the model is not told the task's ID: %s", asked[0].System)
	}
	answers = append(answers,
		llm.Message{Role: llm.Assistant, Calls: []llm.Call{
			call("c4", "update_task", fmt.Sprintf(`{"id":%d,"title":"交电费","due":"2030-02-03"}`, bill.ID)),
			call("c5", "update_task", `{"id":9999,"done":true}`),
		}},
		llm.Message{Role: llm.Assistant, Text: "改好了。"},
		llm.Message{Role: llm.Assistant, Calls: []llm.Call{call("c6", "update_task", fmt.Sprintf(`{"id":%d,"done":true}`, bill.ID))}},
		llm.Message{Role: llm.Assistant, Text: "标好了。"},
		llm.Message{Role: llm.Assistant, Calls: []llm.Call{call("c7", "delete_task", fmt.Sprintf(`{"id":%d}`, bill.ID))}},
		llm.Message{Role: llm.Assistant, Text: "删掉了。"})
	say := func(text string) {
		t.Helper()
		click(t, tt, "给助手的话")
		tt.Type(text)
		tt.Key(0, ui.KeyEnter)
		tt.Frame()
	}
	say("水电费改成电费，2030年2月3日交")
	if bill.Title != "交电费" || bill.Due != "2030-02-03" || bill.Done {
		t.Fatalf("the task after update_task: %+v", bill)
	}
	if got := asked[len(asked)-1].Messages; !strings.Contains(got[len(got)-2].Text, "已修改") || !strings.Contains(got[len(got)-1].Text, "没有 id 是 9999 的任务") {
		t.Errorf("the model is told of the changes: %+v", got[len(got)-2:])
	}
	if !tt.HasText("已修改任务：交水电费 · 改名为“交电费”，改到2月3日 周日") {
		t.Errorf("the change does not show: %q", tt.Texts())
	}
	say("电费交了")
	if !bill.Done || !tt.HasText("已修改任务：交电费 · 标为完成") {
		t.Fatalf("after checking off: %+v, %q", bill, tt.Texts())
	}
	// A turn that changed something is not asked for again.
	if h.CanRegenerate() {
		t.Errorf("a turn that changed a task can be regenerated")
	}
	// A deletion waits for the user's word: declined, nothing is deleted,
	// which the model is told; agreed to, it is done.
	n0 := len(tasks.Tasks())
	answers = append(answers[:len(answers)-2:len(answers)-2],
		llm.Message{Role: llm.Assistant, Calls: []llm.Call{call("c7a", "delete_task", fmt.Sprintf(`{"id":%d}`, bill.ID))}},
		llm.Message{Role: llm.Assistant, Text: "好的，先留着。"},
		llm.Message{Role: llm.Assistant, Calls: []llm.Call{call("c7", "delete_task", fmt.Sprintf(`{"id":%d}`, bill.ID))}},
		llm.Message{Role: llm.Assistant, Text: "删掉了。"})
	already := len(asked)
	say("把电费那条删了")
	if len(tasks.Tasks()) != n0 || len(asked) != already+1 || !tt.HasText("要删除任务“交电费”吗？") || tt.HasText("正在想…") || !tt.HasText("停止") {
		t.Fatalf("a deletion does not wait to be confirmed: %d tasks, asked %d more times, %q", len(tasks.Tasks()), len(asked)-already, tt.Texts())
	}
	click(t, tt, "不删")
	tt.Frame()
	if got := asked[len(asked)-1].Messages; len(tasks.Tasks()) != n0 || !strings.Contains(got[len(got)-1].Text, "用户没有同意") || !tt.HasText("没有删除任务“交电费”") || !tt.HasText("好的，先留着。") || tt.HasText("要删除任务") {
		t.Fatalf("after declining: %d tasks, the model told %q, %q", len(tasks.Tasks()), got[len(got)-1].Text, tt.Texts())
	}
	say("还是删了吧")
	if len(tasks.Tasks()) != n0 || !tt.HasText("要删除任务“交电费”吗？") {
		t.Fatalf("the second deletion does not wait: %q", tt.Texts())
	}
	click(t, tt, "删除")
	tt.Frame()
	if len(tasks.Tasks()) != n0-1 || tasks.Task(bill.ID) != nil || !tt.HasText("已删除任务：交电费") {
		t.Fatalf("after delete_task: %d tasks, %q", len(tasks.Tasks()), tt.Texts())
	}

	// A course is changed and deleted by its ID, which the model is told;
	// one moved onto another stays where it was, and the model is told why.
	math := table.Courses()[0]
	if _, err := table.Put(timetable.Course{Name: "英语", Day: 0, From: 1, To: 2}); err != nil {
		t.Fatal(err)
	}
	answers = append(answers,
		llm.Message{Role: llm.Assistant, Calls: []llm.Call{
			call("c8", "update_course", fmt.Sprintf(`{"id":%d,"weekday":1,"from_period":1}`, math.ID)),
			call("c9", "update_course", fmt.Sprintf(`{"id":%d,"weekday":4,"from_period":5,"to_period":6,"room":"B202","name":"高数"}`, math.ID)),
			call("c10", "delete_course", `{"id":9999}`),
		}},
		llm.Message{Role: llm.Assistant, Text: "改好了。"},
		llm.Message{Role: llm.Assistant, Calls: []llm.Call{call("c11", "delete_course", fmt.Sprintf(`{"id":%d}`, math.ID))}},
		llm.Message{Role: llm.Assistant, Text: "删掉了。"})
	say("高数改到周四五六节，在 B202")
	if !strings.Contains(asked[len(asked)-2].System, fmt.Sprintf("[%d] 周三 第 3–4 节 高等数学", math.ID)) {
		t.Errorf("the model is not told the course's ID: %s", asked[len(asked)-2].System)
	}
	if k := table.Course(math.ID); k == nil || k.Name != "高数" || k.Day != 3 || k.From != 5 || k.To != 6 || k.Room != "B202" {
		t.Fatalf("the course after update_course: %+v", k)
	}
	if got := asked[len(asked)-1].Messages; !strings.Contains(got[len(got)-3].Text, "已经有“英语”了") || !strings.Contains(got[len(got)-2].Text, "已修改") || !strings.Contains(got[len(got)-1].Text, "没有 id 是 9999 的课程") {
		t.Errorf("the model is told of the courses' changes: %+v", got[len(got)-3:])
	}
	if !tt.HasText("已修改课程：高等数学 · 改名为“高数”，改到周四第 5–6 节，改了地点") {
		t.Errorf("the course's change does not show: %q", tt.Texts())
	}
	// Stopped while a deletion waits, nothing is deleted, and the model
	// has an answer to its call, so that the conversation can go on.
	answers = append(answers[:len(answers)-2:len(answers)-2],
		llm.Message{Role: llm.Assistant, Calls: []llm.Call{call("c11a", "delete_course", fmt.Sprintf(`{"id":%d}`, math.ID)), call("c11b", "add_task", `{"title":"不该加的"}`)}},
		llm.Message{Role: llm.Assistant, Calls: []llm.Call{call("c11", "delete_course", fmt.Sprintf(`{"id":%d}`, math.ID))}},
		llm.Message{Role: llm.Assistant, Text: "删掉了。"})
	say("把高数删了")
	if !tt.HasText("要删除课程“高数”（周四第 5–6 节）吗？") {
		t.Fatalf("a course's deletion does not wait: %q", tt.Texts())
	}
	n1 := len(tasks.Tasks())
	click(t, tt, "停止")
	tt.Frame()
	if table.Course(math.ID) == nil || len(tasks.Tasks()) != n1 || tt.HasText("要删除课程") || !tt.HasText("没有删除课程“高数”") {
		t.Fatalf("after stopping at a deletion: %q", tt.Texts())
	}
	say("把高数删了")
	if got := asked[len(asked)-1].Messages; got[len(got)-2].Role != llm.Result || got[len(got)-3].Role != llm.Result || !strings.Contains(got[len(got)-2].Text, "没有执行") {
		t.Fatalf("after stopping, the model is sent %+v", got[len(got)-4:])
	}
	click(t, tt, "删除")
	tt.Frame()
	if table.Course(math.ID) != nil || len(table.Courses()) != 1 || !tt.HasText("已删除课程：高数 · 周四第 5–6 节") {
		t.Fatalf("after delete_course: %d courses, %q", len(table.Courses()), tt.Texts())
	}

	// The day's periods and their times are set, all of what is asked or
	// none of it: not fewer than the courses go to, nor a time that is none.
	answers = append(answers,
		llm.Message{Role: llm.Assistant, Calls: []llm.Call{
			call("c12", "set_periods", `{"count":2}`),
			call("c13", "set_periods", `{"count":12,"times":[{"period":1,"start":"8:20","end":"09:00"},{"period":13,"start":"20:00","end":"20:40"}]}`),
			call("c14", "set_periods", `{"times":[{"period":2,"start":"九点","end":"09:45"}]}`),
			call("c15", "set_periods", `{"count":12,"times":[{"period":1,"start":"08:20","end":"09:00"},{"period":12,"start":"20:00","end":"20:40"}]}`),
		}},
		llm.Message{Role: llm.Assistant, Text: "设好了。"})
	if _, err := table.Put(timetable.Course{Name: "晚自习", Day: 5, From: 9, To: 10}); err != nil {
		t.Fatal(err)
	}
	say("每天十二节，第一节八点二十到九点，第十二节晚上八点到八点四十")
	if !strings.Contains(asked[len(asked)-2].System, "每天 10 节课") || !strings.Contains(asked[len(asked)-2].System, "第 1 节 没有设时间") {
		t.Errorf("the model is not told the periods: %s", asked[len(asked)-2].System)
	}
	if p := table.Periods(); len(p) != 12 || p[0] != (timetable.Period{Start: "08:20", End: "09:00"}) || p[11] != (timetable.Period{Start: "20:00", End: "20:40"}) || p[1] != (timetable.Period{}) {
		t.Fatalf("the periods after set_periods: %+v", p)
	}
	got := asked[len(asked)-1].Messages
	for i, want := range []string{"每天不能少于 10 节", "没有第 13 节", "要写成 HH:MM", "第 12 节 20:00–20:40"} {
		if text := got[len(got)-4+i].Text; !strings.Contains(text, want) {
			t.Errorf("the model is told %q, not %q", text, want)
		}
	}
	if !tt.HasText("已设置节次：每天 12 节，改了 2 节的时间") {
		t.Errorf("the periods' change does not show: %q", tt.Texts())
	}
	if !strings.Contains(asked[len(asked)-1].System, "每天 12 节课") {
		t.Errorf("the model is not told the new periods: %s", asked[len(asked)-1].System)
	}

	// What goes wrong shows, and the field takes the next message.
	click(t, tt, "给助手的话")
	tt.Type("再试一次")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !tt.HasText("Key 不对，或者没有权限（401）") || tt.HasText("正在想") || !tt.HasText("我来") {
		t.Errorf("a failed request: %q", tt.Texts())
	}

	// A new conversation starts from nothing.
	click(t, tt, "新对话")
	if tt.HasText("都加好了。") || !tt.HasText("今天想安排点什么？") {
		t.Errorf("after 新对话: %q", tt.Texts())
	}
	// A suggestion is sent as it is. Its answer is Markdown, shown as it
	// reads, and can be copied and asked for again.
	n := len(asked)
	answers = append(append(answers, make([]llm.Message, n-len(answers))...), llm.Message{Role: llm.Assistant, Text: "明天有 **高等数学**。"}, llm.Message{Role: llm.Assistant, Text: "明天只有一门课。"})
	click(t, tt, "明天有哪些课？")
	tt.Frame()
	if len(asked) != n+1 || len(asked[n].Messages) != 1 || asked[n].Messages[0].Text != "明天有哪些课？" {
		t.Fatalf("a suggestion sent: %+v", asked[n:])
	}
	if texts := strings.Join(tt.Texts(), "|"); !strings.Contains(texts, "高等数学") || strings.Contains(texts, "**") {
		t.Errorf("the answer's Markdown shows as %q", tt.Texts())
	}
	click(t, tt, "复制回答", "重新生成")
	tt.Frame()
	if len(asked) != n+2 || len(asked[n+1].Messages) != 1 || !tt.HasText("明天只有一门课。") || tt.HasText("高等数学") {
		t.Fatalf("after 重新生成: asked %d times with %+v; texts %q", len(asked), asked[len(asked)-1].Messages, tt.Texts())
	}

	// An answer that is coming can be stopped: what came of it stays, and
	// the model is told of it next time.
	answers = append(answers, llm.Message{}, llm.Message{Role: llm.Assistant, Text: "好的。"})
	stop := len(asked)
	inner := a.complete
	a.complete = func(ctx context.Context, c llm.Config, r llm.Request, onText func(string)) (llm.Message, error) {
		if len(asked) == stop {
			asked = append(asked, r)
			onText("周四有")
			tt.Frame()
			if !tt.HasText("停止") {
				t.Errorf("no way to stop while the answer comes: %q", tt.Texts())
			}
			click(t, tt, "停止")
			if ctx.Err() == nil {
				t.Errorf("stopping does not end the request")
			}
			return llm.Message{}, &llm.Error{Message: "连不上：context canceled"}
		}
		return inner(ctx, c, r, onText)
	}
	click(t, tt, "给助手的话")
	tt.Type("那周四呢")
	tt.Key(0, ui.KeyEnter)
	// The text comes in over a moment, as it would while it is written.
	tt.Advance(2 * time.Second)
	if !tt.HasText("周四有") || tt.HasText("context canceled") || tt.HasText("停止") || shown(tt, "发送") == 0 {
		t.Fatalf("after stopping: %q", tt.Texts())
	}
	click(t, tt, "给助手的话")
	tt.Type("继续")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	last := asked[len(asked)-1].Messages
	if len(last) != 5 || last[3].Role != llm.Assistant || last[3].Text != "周四有" || last[4].Text != "继续" {
		t.Fatalf("the request after a stopped answer: %+v", last)
	}

	// The conversations are saved: the panel at the right, shut until
	// asked for, opens one to go on with.
	if tt.HasText("打开对话：") {
		t.Fatalf("the saved conversations show before they are asked for: %q", tt.Texts())
	}
	click(t, tt, "历史对话")
	tt.Frame()
	first := "打开对话：2030年1月2日交报告，再加一门周三三四节的高…"
	if shown(tt, first) != 1 || shown(tt, "打开对话：明天有哪些课？") != 1 {
		t.Fatalf("the saved conversations: %q", tt.Texts())
	}
	click(t, tt, first)
	tt.Frame()
	if !tt.HasText("都加好了。") || !tt.HasText("已添加任务：交报告") || tt.HasText("好的。") {
		t.Fatalf("the first conversation opened again: %q", tt.Texts())
	}
	// It goes on from where it was: the model is sent all of it.
	answers = append(answers, llm.Message{Role: llm.Assistant, Text: "不客气。"})
	before := len(h.Conversations())
	click(t, tt, "给助手的话")
	tt.Type("谢谢")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if got := asked[len(asked)-1].Messages; len(got) < 8 || got[0].Text != "2030年1月2日交报告，再加一门周三三四节的高等数学" || got[len(got)-1].Text != "谢谢" {
		t.Fatalf("a saved conversation gone on with sends %+v", got)
	}
	if len(h.Conversations()) != before || h.Conversations()[0] != "2030年1月2日交报告，再加一门周三三四节的高…" {
		t.Errorf("after going on with it: %q", h.Conversations())
	}
	// After a restart they are there, and one deleted is gone for good.
	b := newApp(store.Open(dir))
	b.win = &fakeWindow{}
	bt := ui.NewTester(b.view, 1280, 800)
	click(t, bt, "历史对话")
	bt.Frame()
	if shown(bt, first) != 1 || shown(bt, "打开对话：明天有哪些课？") != 1 {
		t.Fatalf("after a restart, the saved conversations: %q", bt.Texts())
	}
	click(t, bt, first)
	bt.Frame()
	// Opening one shuts the panel, which floats over the page.
	if !bt.HasText("不客气。") || bt.HasText("打开对话：明天有哪些课？") {
		t.Errorf("after a restart, the conversation opened: %q", bt.Texts())
	}
	click(t, bt, "历史对话", "删除对话：明天有哪些课？")
	bt.Frame()
	if shown(bt, "打开对话：明天有哪些课？") != 0 || len(newApp(store.Open(dir)).features[0].(*home.Home).Conversations()) != 1 {
		t.Errorf("after deleting a conversation: %q", bt.Texts())
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
	a := newTodoApp(store.Open(""))
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
	// A model that answers by rote, for the first page: unless
	// ATELIER_SCREENSHOT_NOMODEL is set, which shows it without one.
	if os.Getenv("ATELIER_SCREENSHOT_NOMODEL") == "" {
		a.settings.Models = []modelConfig{
			{ID: 1, Name: "DeepSeek", Kind: string(llm.Chat), Models: []string{"deepseek-chat", "deepseek-reasoner"}},
			{ID: 2, Name: "Anthropic", Kind: string(llm.Anthropic), Models: []string{"claude-opus-5-5"}},
		}
		a.settings.Model, a.settings.ModelName = 1, "deepseek-chat"
		asked := 0
		a.complete = func(context.Context, llm.Config, llm.Request, func(string)) (llm.Message, error) {
			asked++
			// ATELIER_SCREENSHOT_DELETE has it call for a deletion first,
			// which waits to be confirmed.
			if asked == 1 && os.Getenv("ATELIER_SCREENSHOT_DELETE") != "" {
				return llm.Message{Role: llm.Assistant, Calls: []llm.Call{{ID: "d", Name: "delete_task", Input: json.RawMessage(`{"id":1}`)}}}, nil
			}
			if asked%2 == 1 {
				return llm.Message{Role: llm.Assistant, Calls: []llm.Call{{ID: "c", Name: "add_task", Input: json.RawMessage(`{"title":"交实验报告","due":"` + day(1) + `"}`)}}}, nil
			}
			return llm.Message{Role: llm.Assistant, Text: "今天有两节课：\n\n- **高等数学**，第 1–2 节，A301\n- **大学英语**，第 5–6 节，B102\n\n到期的任务有三项：写周报、修登录页的 bug、跑步 30 分钟。另外已经帮你加上明天要交的 `实验报告`。", Thinking: "用户问今天的安排。今天是周一，课表里周一有高等数学和大学英语两门课。\n\n任务里今天到期的有三项。实验报告已经加上了，回答里顺便确认一下。"}, nil
		}
	}
	for _, f := range a.features {
		tb, ok := f.(*timetable.Timetable)
		if !ok {
			continue
		}
		for _, k := range []timetable.Course{
			{Name: "高等数学", Room: "A301", Teacher: "王老师", Day: 0, From: 1, To: 2},
			{Name: "大学英语", Room: "B102", Day: 0, From: 5, To: 6},
			{Name: "数据结构与算法分析", Room: "计算机楼 204", Teacher: "李老师", Day: 1, From: 3, To: 4},
			{Name: "体育", Day: 2, From: 7, To: 7},
			{Name: "线性代数", Room: "A105", Day: 3, From: 1, To: 3},
			{Name: "实验", Room: "实验楼", Day: 4, From: 5, To: 8},
			{Name: "大学英语", Room: "B102", Day: 2, From: 3, To: 4},
			{Name: "程序设计", Room: "机房 2", Day: 1, From: 6, To: 8},
			{Name: "马克思主义基本原理概论", Day: 4, From: 1, To: 2},
			{Name: "高等数学", Room: "A301", Teacher: "王老师", Day: 3, From: 5, To: 6},
		} {
			if _, err := tb.Put(k); err != nil {
				t.Fatal(err)
			}
		}
		for i, at := range []string{"08:20", "09:05", "10:00", "10:45", "11:30", "13:40", "14:25", "15:20", "16:05"} {
			tb.SetTime(i+1, timetable.Period{Start: at})
		}
	}
	// ATELIER_SCREENSHOT_TRENDING is a file of GitHub's page of trending
	// repositories, for the page of them to show.
	if path := os.Getenv("ATELIER_SCREENSHOT_TRENDING"); path != "" {
		for _, f := range a.features {
			if tr, ok := f.(*trending.Trending); ok {
				tr.Fetch = func(context.Context) ([]byte, error) { return os.ReadFile(path) }
			}
		}
	}
	tt := ui.NewTester(a.view, 1280, 800)
	// ATELIER_SCREENSHOT_SIZE is the window's size, as 860x520.
	var w, h int
	if n, _ := fmt.Sscanf(os.Getenv("ATELIER_SCREENSHOT_SIZE"), "%dx%d", &w, &h); n == 2 {
		tt.SetSize(w, h)
	}
	if os.Getenv("ATELIER_SCREENSHOT_SCALE") == "2" {
		tt.SetScale(2)
	}
	// ATELIER_SCREENSHOT_ATTACH lists files to attach on the first page,
	// separated by commas, and ATELIER_SCREENSHOT_SEND is a message to
	// send with them.
	if files, text := os.Getenv("ATELIER_SCREENSHOT_ATTACH"), os.Getenv("ATELIER_SCREENSHOT_SEND"); files != "" || text != "" {
		click(t, tt, "首页")
		first := a.current().(*home.Home)
		if files != "" {
			first.Attach(strings.Split(files, ",")...)
		}
		if text != "" {
			first.Send(text)
		}
		tt.Frame()
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
	a := newTodoApp(store.Open(dir))
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
	b := newTodoApp(store.Open(dir))
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
	a := newTodoApp(store.Open(dir))
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
	b := newTodoApp(store.Open(dir))
	ui.NewTester(b.view, 1280, 800).Frame()
	if b.settings.Style != string(mallow.StyleOrganic) || mallow.CurrentLook() != mallow.Organic {
		t.Errorf("after restarting: style %q", b.settings.Style)
	}
}

// The size zooms the whole interface, and the font is the theme's: both are
// saved.
func TestTextSettings(t *testing.T) {
	dir := t.TempDir()
	a := newTodoApp(store.Open(dir))
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
	b := newTodoApp(store.Open(dir))
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
	a := newTodoApp(store.Open(""))
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
	for _, f := range a.features {
		if tb, ok := f.(*timetable.Timetable); ok {
			if _, err := tb.Put(timetable.Course{Name: "高等数学", Room: "A301", Teacher: "王老师", Day: 1, From: 1, To: 2}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, page := range []string{"首页", "待办", "课表", "月历", "热门", "设置"} {
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
	a := newTodoApp(store.Open(dir))
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
	b := newTodoApp(store.Open(dir))
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
	a := newTodoApp(store.Open(dir))
	tt := ui.NewTester(a.view, 1280, 800)
	if !staysInTray(a.settings, false) || staysInTray(a.settings, true) {
		t.Fatalf("at first: closing stays %v, quitting stays %v", staysInTray(a.settings, false), staysInTray(a.settings, true))
	}
	click(t, tt, "设置", "通用", "留在托盘")
	if !a.settings.CloseQuits || staysInTray(a.settings, false) {
		t.Fatalf("after turning it off: %+v", a.settings.CloseQuits)
	}
	if b := newTodoApp(store.Open(dir)); !b.settings.CloseQuits {
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
	a := newTodoApp(store.Open(dir))
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
	if c, ok := newTodoApp(store.Open(dir)).DefaultModel(); !ok || c != want {
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
	a := newTodoApp(store.Open(dir))
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
	a := newTodoApp(store.Open(""))
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
	a := newTodoApp(store.Open(""))
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

// TestAttachments walks through sending files to the assistant: chosen in
// the system's dialog, dropped on the page and pasted; a picture goes to
// the model as one and a file of text in the message's text; what cannot be
// sent is said; the pictures are kept with the conversation, and go with it.
func TestAttachments(t *testing.T) {
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	a.win = &fakeWindow{}
	key, err := a.box.Seal("k")
	if err != nil {
		t.Fatal(err)
	}
	a.settings.Models = []modelConfig{{ID: 1, Name: "P", Kind: string(llm.Chat), Key: key, Models: []string{"m"}}}
	a.settings.Model, a.settings.ModelName = 1, "m"
	var asked []llm.Request
	a.complete = func(_ context.Context, _ llm.Config, r llm.Request, _ func(string)) (llm.Message, error) {
		asked = append(asked, r)
		return llm.Message{Role: llm.Assistant, Text: "看到了。"}, nil
	}
	files := t.TempDir()
	var shot bytes.Buffer
	if err := png.Encode(&shot, image.NewRGBA(image.Rect(0, 0, 60, 40))); err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) string {
		path := filepath.Join(files, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	photo, notes, program := write("照片.png", shot.Bytes()), write("笔记.txt", []byte("周三交报告")), write("程序.exe", []byte{0x4D, 0x5A, 0, 0xFF})

	tt := ui.NewTester(a.view, 1280, 800)
	h := a.current().(*home.Home)
	// The button asks for files in the system's dialog.
	h.PickFiles = func() []string { return []string{photo, notes} }
	click(t, tt, "添加附件")
	tt.Frame()
	if !tt.HasText("照片.png") || !tt.HasText("笔记.txt") {
		t.Fatalf("the files chosen do not show: %q", tt.Texts())
	}
	// One is taken out again; dropped on the page, it is back, and what is
	// neither a picture nor text is said not to be.
	click(t, tt, "移除附件：笔记.txt")
	if tt.HasText("笔记.txt") {
		t.Fatalf("the file taken out still shows: %q", tt.Texts())
	}
	if !tt.DropFiles(640, 300, notes, program) {
		t.Fatal("the page does not take dropped files")
	}
	tt.Frame()
	if !tt.HasText("笔记.txt") || !tt.HasText("“程序.exe”不是图片、文档，也不是文本文件") {
		t.Fatalf("after the drop: %q", tt.Texts())
	}
	saved := func() []string {
		entries, _ := os.ReadDir(filepath.Join(dir, "home-files"))
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		return names
	}
	if len(saved()) != 0 {
		t.Errorf("pictures are kept before they are sent: %q", saved())
	}

	click(t, tt, "给助手的话")
	tt.Type("看看这些")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(asked) != 1 {
		t.Fatalf("asked %d times", len(asked))
	}
	first := asked[0].Messages[0]
	if len(first.Images) != 1 || first.Images[0].Type != "image/png" || !bytes.Equal(first.Images[0].Data, shot.Bytes()) {
		t.Errorf("the picture sent: %+v", first.Images)
	}
	if !strings.HasPrefix(first.Text, "看看这些") || !strings.Contains(first.Text, `<file name="笔记.txt">`) || !strings.Contains(first.Text, "周三交报告") {
		t.Errorf("the text sent: %q", first.Text)
	}
	// The message shows its picture and its file, without the file's text,
	// and the field is clear of them.
	if !tt.HasText("图片：照片.png") || !tt.HasText("笔记.txt") || tt.HasText("周三交报告") || tt.HasText("移除附件") || tt.HasText("程序.exe") {
		t.Errorf("the message shows as %q", tt.Texts())
	}
	if names := saved(); len(names) != 1 || names[0] != first.Images[0].Name {
		t.Errorf("the pictures kept: %q, the one sent %q", names, first.Images[0].Name)
	}

	// A picture pasted is attached, and goes without a word.
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, shot.Bytes()))))
	click(t, tt, "给助手的话")
	tt.Key(ui.Ctrl, ui.KeyV)
	tt.Frame()
	if !tt.HasText("移除附件：粘贴的图片") {
		t.Fatalf("the pasted picture does not show: %q", tt.Texts())
	}
	click(t, tt, "发送")
	tt.Frame()
	if got := asked[len(asked)-1].Messages; len(asked) != 2 || len(got) != 3 || got[2].Text != "" || len(got[2].Images) != 1 || len(got[2].Images[0].Data) == 0 || len(got[0].Images[0].Data) == 0 {
		t.Fatalf("after the paste, asked %d times with %+v", len(asked), got)
	}
	if len(saved()) != 2 {
		t.Errorf("the pictures kept: %q", saved())
	}
	// No more files than a message takes.
	many := make([]string, 12)
	for i := range many {
		many[i] = notes
	}
	h.Attach(many...)
	tt.Frame()
	if n := strings.Count(strings.Join(tt.Texts(), "|"), "移除附件：笔记.txt"); n != 8 || !tt.HasText("一次最多带 8 个附件") {
		t.Errorf("%d of 12 files attached: %q", n, tt.Texts())
	}

	// After a restart the conversation has its pictures still, to show and
	// to send; deleting it deletes them.
	b := newApp(store.Open(dir))
	b.win = &fakeWindow{}
	b.complete, b.settings = a.complete, a.settings
	tb := ui.NewTester(b.view, 1280, 800)
	click(t, tb, "历史对话", "打开对话：看看这些")
	tb.Frame()
	if !tb.HasText("图片：照片.png") || !tb.HasText("图片：粘贴的图片") {
		t.Fatalf("the conversation opened again shows %q", tb.Texts())
	}
	click(t, tb, "给助手的话")
	tb.Type("再看一眼")
	tb.Key(0, ui.KeyEnter)
	tb.Frame()
	if got := asked[len(asked)-1].Messages; len(asked) != 3 || !bytes.Equal(got[0].Images[0].Data, shot.Bytes()) {
		t.Fatalf("after the restart, asked %d times with %+v", len(asked), got[0].Images)
	}
	click(t, tb, "历史对话", "删除对话：看看这些")
	if len(saved()) != 0 {
		t.Errorf("the pictures of a deleted conversation are kept: %q", saved())
	}
}

// TestThinking walks through how hard a model thinks and what it thought:
// the effort is chosen by the model's name, kept for that model, and sent;
// the thinking shows as it comes, then shut over the answer, to open.
func TestThinking(t *testing.T) {
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	a.win = &fakeWindow{}
	key, err := a.box.Seal("k")
	if err != nil {
		t.Fatal(err)
	}
	a.settings.Models = []modelConfig{{ID: 1, Name: "P", Kind: string(llm.Chat), Key: key, Models: []string{"m", "m2"}}}
	a.settings.Model, a.settings.ModelName = 1, "m"
	tt := ui.NewTester(a.view, 1280, 800)
	var asked []llm.Request
	var coming string
	a.complete = func(_ context.Context, _ llm.Config, r llm.Request, onText func(string)) (llm.Message, error) {
		asked = append(asked, r)
		if len(asked) == 1 {
			// It thinks, then calls a tool, with nothing to say yet.
			r.OnThinking("得先加任务")
			return llm.Message{Role: llm.Assistant, Thinking: "得先加任务", Calls: []llm.Call{{ID: "c1", Name: "add_task", Input: json.RawMessage(`{"title":"交报告"}`)}}}, nil
		}
		r.OnThinking("先看")
		r.OnThinking("课表")
		tt.Frame()
		coming = strings.Join(tt.Texts(), "|")
		onText("加好了。")
		return llm.Message{Role: llm.Assistant, Text: "加好了。", Thinking: "先看课表"}, nil
	}

	if !tt.HasText("思考：默认") {
		t.Fatalf("the effort does not show: %q", tt.Texts())
	}
	click(t, tt, "思考强度", "思考强度：高")
	tt.Frame()
	if a.settings.Efforts["1/m"] != llm.High || !tt.HasText("思考：高") || tt.HasText("思考强度：低") {
		t.Fatalf("after choosing 高: %v, %q", a.settings.Efforts, tt.Texts())
	}
	click(t, tt, "给助手的话")
	tt.Type("加个交报告的任务")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(asked) != 2 || asked[0].Effort != llm.High || asked[1].Effort != llm.High {
		t.Fatalf("asked %d times, with efforts %+v", len(asked), asked)
	}
	// As it came the thinking showed, open; now it is shut, over the
	// answer and over the tool's call, each to open by itself.
	if !strings.Contains(coming, "正在思考…") || !strings.Contains(coming, "先看课表") {
		t.Errorf("while the thinking came: %q", coming)
	}
	if tt.HasText("先看课表") || tt.HasText("得先加任务") || tt.HasText("正在思考…") || shown(tt, "思考过程") != 2 || shown(tt, "复制回答") != 1 {
		t.Fatalf("after the answer: %q", tt.Texts())
	}
	if !tt.HasText("想了 1 秒") {
		t.Errorf("how long it thought does not show: %q", tt.Texts())
	}
	answer, _ := tt.Find("加好了。")
	tt.Click("思考过程") // the first: the one over the tool's call
	tt.Frame()
	if !tt.HasText("得先加任务") || tt.HasText("先看课表") {
		t.Errorf("after opening the first thinking: %q", tt.Texts())
	}
	if moved, _ := tt.Find("加好了。"); moved.Y <= answer.Y {
		t.Errorf("the opened thinking takes no room: the answer at %v, then %v", answer.Y, moved.Y)
	}

	// Another model has its own effort, none until one is chosen; the
	// first keeps its own, also after a restart, as the thinking is kept.
	click(t, tt, "选择模型", "用模型：m2")
	tt.Frame()
	if !tt.HasText("思考：默认") {
		t.Errorf("another model's effort: %q", tt.Texts())
	}
	click(t, tt, "思考强度", "思考强度：低")
	click(t, tt, "思考强度", "思考强度：默认")
	if _, has := a.settings.Efforts["1/m2"]; has || a.settings.Efforts["1/m"] != llm.High {
		t.Errorf("the efforts kept: %v", a.settings.Efforts)
	}
	b := newApp(store.Open(dir))
	b.win = &fakeWindow{}
	if b.settings.Efforts["1/m"] != llm.High {
		t.Errorf("after a restart, the efforts: %v", b.settings.Efforts)
	}
	tb := ui.NewTester(b.view, 1280, 800)
	click(t, tb, "历史对话", "打开对话：加个交报告的任务")
	tb.Frame()
	if shown(tb, "思考过程") != 2 || !tb.HasText("想了 1 秒") {
		t.Errorf("after a restart, the conversation: %q", tb.Texts())
	}
}

// TestTimetableOnlyShows checks that a timetable that is not being changed
// shows its courses and lets none of them, nor a cell, be clicked into a
// dialog; and that it is so again once the changing is done.
func TestTimetableOnlyShows(t *testing.T) {
	a := newTodoApp(store.Open(""))
	a.win = &fakeWindow{}
	tt := ui.NewTester(a.view, 1280, 800)
	click(t, tt, "课表")
	tb := a.current().(*timetable.Timetable)
	if _, err := tb.Put(timetable.Course{Name: "高等数学", Room: "A301", Day: 0, From: 1, To: 2}); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	shows := func(when string) {
		t.Helper()
		if !tt.HasText("课程：高等数学") || tt.HasText("打开高等数学") || tt.HasText("节次时间") {
			t.Fatalf("%s: %q", when, tt.Texts())
		}
		click(t, tt, "课程：高等数学")
		click(t, tt, "周二第1节")
		first, _ := tt.Find("周二第1节")
		last, _ := tt.Find("周二第3节")
		tt.Press(first.X+first.W/2, first.Y+first.H/2)
		tt.Move(last.X+last.W/2, last.Y+last.H/2)
		tt.Release(last.X+last.W/2, last.Y+last.H/2)
		tt.Frame()
		if tt.HasText("编辑课程") || tt.HasText("新课程") || len(tb.Courses()) != 1 {
			t.Fatalf("%s, a click changes the timetable: %q", when, tt.Texts())
		}
	}
	shows("at first")
	click(t, tt, "编辑课表", "打开高等数学")
	if !tt.HasText("编辑课程") {
		t.Fatalf("while changing, a click on a course: %q", tt.Texts())
	}
	click(t, tt, "取消", "完成编辑")
	shows("after the changing is done")
}

// TestReminders checks that a course is reminded of once, a lead before it
// starts, on its day, with how long there is left; sooner when the app
// began to run after that; not once it has begun, nor with the reminders
// off; and that the settings turn them off and set the lead.
func TestReminders(t *testing.T) {
	a := newTodoApp(store.Open(t.TempDir()))
	a.win = &fakeWindow{}
	tb := a.table()
	for i, at := range []string{"08:20", "9：05", "10:00"} {
		tb.SetTime(i+1, timetable.Period{Start: at, End: "23:00"})
	}
	// 2030-01-07 is a Monday.
	for _, k := range []timetable.Course{
		{Name: "高等数学", Room: "A301", Teacher: "王老师", Day: 0, From: 1, To: 2},
		{Name: "大学英语", Day: 0, From: 3, To: 3},
		{Name: "体育", Day: 1, From: 1, To: 1},
		{Name: "没有时间的课", Day: 0, From: 5, To: 5},
	} {
		if _, err := tb.Put(k); err != nil {
			t.Fatal(err)
		}
	}
	var sent []string
	clock := time.Date(2030, 1, 7, 7, 0, 0, 0, time.Local)
	a.reminders.now = func() time.Time { return clock }
	a.reminders.notify = func(id, title, body string) { sent = append(sent, title+"|"+body) }
	at := func(hour, minute, second int) []string {
		t.Helper()
		clock = time.Date(2030, 1, 7, hour, minute, second, 0, time.Local)
		sent = nil
		a.remind()
		return sent
	}
	if got := at(7, 0, 0); len(got) != 0 {
		t.Errorf("at 7:00: %q", got)
	}
	if got := at(7, 59, 50); len(got) != 0 {
		t.Errorf("just before the reminder is due: %q", got)
	}
	if got := at(8, 0, 10); len(got) != 1 || got[0] != "20 分钟后上课：高等数学|第 1–2 节  08:20–23:00 · A301 · 王老师" {
		t.Errorf("when the reminder is due: %q", got)
	}
	if got := at(8, 0, 30); len(got) != 0 {
		t.Errorf("the reminder comes again: %q", got)
	}
	// After a long sleep what began meanwhile is not reminded of, and what
	// starts within the lead is, with what is left.
	if got := at(9, 50, 0); len(got) != 1 || !strings.HasPrefix(got[0], "10 分钟后上课：大学英语|") {
		t.Errorf("after sleeping past a reminder: %q", got)
	}
	if got := at(10, 30, 0); len(got) != 0 {
		t.Errorf("after the courses began: %q", got)
	}

	// The settings: a shorter lead, and none at all.
	tt := ui.NewTester(a.view, 1280, 800)
	click(t, tt, "设置", "通用")
	if !tt.HasText("上课提醒") || shown(tt, "提前几分钟") == 0 || shown(tt, "20") == 0 {
		t.Fatalf("the reminders' settings: %q", tt.Texts())
	}
	click(t, tt, "5")
	if a.settings.RemindLead != 5 {
		t.Fatalf("after choosing 5: %d", a.settings.RemindLead)
	}
	a.reminders.last = time.Time{}
	if got := at(8, 0, 10); len(got) != 0 {
		t.Errorf("twenty minutes before, with a lead of five: %q", got)
	}
	if got := at(8, 15, 5); len(got) != 1 || !strings.HasPrefix(got[0], "5 分钟后上课：高等数学") {
		t.Errorf("five minutes before: %q", got)
	}
	click(t, tt, "开启上课提醒")
	tt.Frame()
	if !a.settings.RemindOff || tt.HasText("提前几分钟") {
		t.Fatalf("after turning the reminders off: %v, %q", a.settings.RemindOff, tt.Texts())
	}
	a.reminders.last = time.Time{}
	if got := at(8, 15, 5); len(got) != 0 {
		t.Errorf("with the reminders off: %q", got)
	}
	if b := newApp(store.Open(a.store.Dir())); !b.settings.RemindOff || b.settings.RemindLead != 5 {
		t.Errorf("after a restart: %+v", b.settings)
	}
}

// TestTrending walks through the page of GitHub's trending repositories:
// it reads the list when it first shows, and not again while that is new;
// a click opens a repository in the browser; a reading that fails says so
// and leaves the list; and the list is there after a restart.
func TestTrending(t *testing.T) {
	const page = `<article class="Box-row"><h2 class="h3"><a href="/acme/rocket"><span>acme /</span> rocket</a></h2>
<p class="col-9">Rockets for agents</p><span itemprop="programmingLanguage">Go</span>
<a href="/acme/rocket/stargazers">14,814</a><a href="/acme/rocket/forks">1,430</a><span>2,395 stars this week</span></article>
<article class="Box-row"><h2 class="h3"><a href="/solo/tiny"><span>solo /</span> tiny</a></h2>
<a href="/solo/tiny/stargazers">987</a><a href="/solo/tiny/forks">12</a><span>61 stars this week</span></article>`
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	a.win = &fakeWindow{}
	var tr *trending.Trending
	for _, f := range a.features {
		if f, ok := f.(*trending.Trending); ok {
			tr = f
		}
	}
	tt := ui.NewTester(a.view, 1280, 800)
	// Until it is told where from, it reads nothing.
	click(t, tt, "热门")
	if tt.HasText("刷新热门") || tt.HasText("没有刷新成功") || !tt.HasText("还没有读过") {
		t.Fatalf("before it can read: %q", tt.Texts())
	}
	click(t, tt, "待办")
	fetches, fail := 0, false
	tr.Fetch = func(context.Context) ([]byte, error) {
		fetches++
		if fail {
			return nil, errors.New("连不上 GitHub，检查一下网络")
		}
		return []byte(page), nil
	}
	click(t, tt, "热门")
	tt.Frame()
	if fetches != 1 || len(tr.Repos()) != 2 {
		t.Fatalf("after the page first shows: read %d times, %d repositories", fetches, len(tr.Repos()))
	}
	for _, want := range []string{"acme /", "rocket", "Rockets for agents", "Go", "★ 14,814", "本周 +2,395", "solo /", "本周 +61", "更新于 今天"} {
		if !tt.HasText(want) {
			t.Errorf("the list lacks %q: %q", want, tt.Texts())
		}
	}
	click(t, tt, "待办", "热门")
	tt.Frame()
	if fetches != 1 {
		t.Errorf("a list just read is read again: %d times", fetches)
	}
	click(t, tt, "打开 solo/tiny")
	if got := tt.OpenedURLs(); len(got) != 1 || got[0] != "https://github.com/solo/tiny" {
		t.Errorf("a click opens %q", got)
	}
	// The button reads it anew; when that fails the list stays.
	fail = true
	click(t, tt, "刷新热门")
	tt.Frame()
	if fetches != 2 || !tt.HasText("没有刷新成功：连不上 GitHub，检查一下网络") || !tt.HasText("rocket") {
		t.Fatalf("after a reading that failed: read %d times, %q", fetches, tt.Texts())
	}
	tt.Frame()
	if fetches != 2 {
		t.Errorf("a failed reading is tried again at once: %d times", fetches)
	}
	b := newApp(store.Open(dir))
	b.win = &fakeWindow{}
	bt := ui.NewTester(b.view, 1280, 800)
	click(t, bt, "热门")
	if !bt.HasText("rocket") || !bt.HasText("本周 +2,395") {
		t.Errorf("after a restart: %q", bt.Texts())
	}
}
