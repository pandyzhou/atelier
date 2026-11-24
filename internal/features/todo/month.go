package todo

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"
)

// The month view is a calendar: a row for each week of the month shown,
// Monday first, a cell for each day with the tasks due on it. A task opens
// in a dialog there, as a cell has no room to unfold it in.

// monthOf returns the first day of date's month.
func monthOf(date string) string {
	if len(date) < 7 {
		return date
	}
	return date[:7] + "-01"
}

// addMonths returns the first day of the month n months after month's.
func addMonths(month string, n int) string {
	d, err := time.Parse(dateLayout, monthOf(month))
	if err != nil {
		return month
	}
	return d.AddDate(0, n, 0).Format(dateLayout)
}

// monthWeeks returns the Mondays of the weeks that have days of a month.
func monthWeeks(month string) []string {
	var weeks []string
	next := addMonths(month, 1)
	for monday := mondayOf(monthOf(month)); monday < next; monday = addDays(monday, 7) {
		weeks = append(weeks, monday)
	}
	return weeks
}

// inMonth returns the tasks due in a month, those still to do first.
func (t *Todo) inMonth(month string) []*Task {
	from, to := monthOf(month), addMonths(month, 1)
	return t.pick(func(k *Task) bool { return k.Due >= from && k.Due < to })
}

// dueOn returns the tasks due on a day: those still to do, then those done.
func (t *Todo) dueOn(date string) []*Task {
	open := t.pick(func(k *Task) bool { return k.Due == date && !k.Done })
	return append(open, t.pick(func(k *Task) bool { return k.Due == date && k.Done })...)
}

const (
	monthChip    = 20 // the height of a task in a cell
	monthChipGap = 2
	monthDay     = 26 // the height of a cell's day number, with the room under it
)

// monthView is the calendar of the month shown, under the view's heading.
func (t *Todo) monthView(c *ui.Context) {
	today := t.Today()
	month := t.ui.month
	weeks := monthWeeks(month)
	// As many tasks show in a cell as its height has room for, which the
	// window's gives: the rest is counted under them.
	_, h := c.Size()
	cell := (h - 220) / float32(len(weeks))
	room := max(1, int((cell-monthDay-10)/(monthChip+monthChipGap)))

	ui.Column(c).Label("任务列表").Grow(1).MinHeight(0).Padding(0, 20, 16, 20).Children(func() {
		ui.Row(c).Shrink(0).Padding(0, 0, 6, 0).Children(func() {
			for _, name := range dayNames {
				ui.Text(c, name).Grow(1).Basis(0).FontSize(12).TextColor(mallow.Muted).TextAlign(ui.Center)
			}
		})
		for _, monday := range weeks {
			mallow.Groove(c)
			ui.Row(c.Key(monday)).Grow(1).Basis(0).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				for i := range 7 {
					if i > 0 {
						mallow.GrooveV(c)
					}
					date := addDays(monday, i)
					t.monthCell(c.Key(date), date, today, monthOf(date) == month, room)
				}
			})
		}
	})
}

// monthCell is a day of the calendar: its number, marked when it is today
// and dim when it is of another month, and its tasks, as many as room says.
// A right click on it adds a task due that day.
func (t *Todo) monthCell(c *ui.Context, date, today string, ofMonth bool, room int) {
	tasks := t.dueOn(date)
	day, _ := strconv.Atoi(date[8:])
	cell := ui.Column(c).Label(dayNote(date)).Grow(1).Basis(0).MinWidth(0).Padding(6, 5).Gap(monthChipGap)
	cell.Children(func() {
		num := ui.Row(c).Size(22, 22).Shrink(0).Margin(0, 0, monthDay-22-monthChipGap, 1).Radius(11).
			Justify(ui.Center).AlignItems(ui.Center)
		color := mallow.Text
		switch {
		case date == today:
			num.Background(mallow.Accent)
			color = mallow.OnAccent
		case !ofMonth:
			color = mallow.Muted.Alpha(0.55)
		}
		num.Children(func() {
			ui.Text(c, strconv.Itoa(day)).FontSize(12).FontWeight(600).TextColor(color)
		})
		shown := tasks
		if len(shown) > room {
			// The last line counts those it leaves out.
			shown = shown[:room-1]
		}
		for _, k := range shown {
			t.monthChip(c.Key(k.ID), k, today)
		}
		if rest := len(tasks) - len(shown); rest > 0 {
			ui.Text(c, fmt.Sprintf("还有 %d 项", rest)).Height(monthChip).PaddingX(6).FontSize(11).TextColor(mallow.Muted).SingleLine()
		}
	})
	if mallow.ContextMenu(c, cell, t.menuOpen("day:"+date), []mallow.MenuEntry{newEntry}) == newEntry.ID {
		t.startAddingOn(date)
	}
}

// monthChip is a task in a cell: a line of its title, tinted while it is
// still to do, red when it is overdue, struck through when done. It opens
// the task; a right click has the task's menu.
func (t *Todo) monthChip(c *ui.Context, k *Task, today string) {
	chip := mallow.Plain(c, "打开"+k.Title).Height(monthChip).Shrink(0).PaddingX(6).Radius(6)
	tint, color := mallow.Accent, mallow.Text
	switch {
	case k.Done:
		tint, color = mallow.Muted, mallow.Muted
	case k.Due < today:
		tint = mallow.Danger
	}
	alpha := float32(0.16)
	if chip.Hovered() {
		alpha = 0.28
	}
	chip.Background(tint.Alpha(alpha))
	chip.Children(func() {
		title := ui.Text(c, k.Title).Grow(1).MinWidth(0).FontSize(12).TextColor(color).SingleLine()
		if k.Done {
			title.Strikethrough()
		}
	})
	if chip.Clicked() {
		t.openTask(k.ID)
	}
	t.taskMenu(c, chip, k)
}

// editor is the dialog a task opens in where the view has no room to unfold
// it in place: in the month view. It edits the task as its details do.
func (t *Todo) editor(c *ui.Context) {
	k := t.Task(t.ui.open)
	open := k != nil && t.ui.view == viewMonth
	if !open {
		return
	}
	mallow.Dialog(c, &open, mallow.DialogOptions{Width: 560}, func() {
		ui.Scroll(c).Shrink(1).MinHeight(0).Margin(-6, -10).Padding(6, 10, 10, 10).Gap(12).Children(func() {
			t.editing(c, k)
		})
	})
	// Left by Escape or a click outside.
	if !open {
		t.closeOpen()
	}
}
