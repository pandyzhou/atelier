package todo

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"
)

// viewState is what the page shows, which is not saved.
type viewState struct {
	view   string // the view chosen
	monday string // the Monday of the week the week view shows
	// open is the ID of the task whose details show, 0 for none; title is
	// what is written as its title, and titleWas its title as they opened.
	open            int
	title, titleWas string
	// adding is whether the dialog that adds a task shows, and draft the
	// task it writes.
	adding bool
	draft  Task
	// The calendar of the task that is open, or of the draft: whether it
	// shows, and the month it shows.
	calOpen  bool
	calMonth time.Time
	menus    map[string]*bool // the context menus that show, see menuOpen
	// folded holds the groups folded away, as "<view>/<heading>".
	folded map[string]bool
}

func newViewState(now time.Time) viewState {
	today := now.Format(dateLayout)
	return viewState{
		view:   viewToday,
		monday: mondayOf(today),
	}
}

const (
	navWidth = 208
)

// View builds the page: the views to choose from, the tasks of
// the one chosen, and the details of the task opened, if one is; over it,
// the dialog that adds a task, when asked for.
func (t *Todo) View(c *ui.Context) {
	// The page is one card, as tall as the sidebar beside it, its parts set
	// apart by grooves.
	ui.Row(c).Fill().Padding(0, 28, 0, 16).AlignItems(ui.Stretch).Children(func() {
		mallow.Card(c, mallow.Medium).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				t.nav(c)
				mallow.GrooveV(c).Margin(24, 0)
				t.main(c)
			})
		})
	})
	t.adder(c)
}

// nav lists the views, then what is done: each with how many tasks it has
// still to do.
func (t *Todo) nav(c *ui.Context) {
	item := func(view, label string, glyph func(*ui.Context, ui.Color)) mallow.Item {
		it := mallow.Item{ID: view, Label: label, Glyph: glyph}
		if n := t.openIn(view); n > 0 && view != viewDone {
			it.Note = strconv.Itoa(n)
		}
		return it
	}
	ui.Scroll(c).Width(navWidth).Shrink(0).Padding(20, 12, 16, 12).Gap(12).Children(func() {
		mallow.SourceList(c, "视图", &t.ui.view, []mallow.Item{
			item(viewToday, "今天", mallow.GlyphCalendar),
			item(viewNext7, "最近 7 天", mallow.GlyphCalendar),
			item(viewWeek, "按周", mallow.GlyphCalendar),
			item(viewAll, "全部", mallow.GlyphList),
		})
		mallow.Groove(c).MarginX(4)
		mallow.SourceList(c, "其他", &t.ui.view, []mallow.Item{item(viewDone, "已完成", mallow.GlyphDone)})
	})
}

// menuOpen is the bool a context menu keeps: whether the menu of key shows.
func (t *Todo) menuOpen(key string) *bool {
	if t.ui.menus == nil {
		t.ui.menus = map[string]*bool{}
	}
	if t.ui.menus[key] == nil {
		t.ui.menus[key] = new(bool)
	}
	return t.ui.menus[key]
}

// taskMenu is what a right click on a task's row offers.
func (t *Todo) taskMenu(c *ui.Context, row ui.Element, k *Task) {
	today := t.Today()
	check := mallow.MenuEntry{ID: "done", Label: "标为完成", Glyph: mallow.GlyphCheck}
	if k.Done {
		check.Label = "标为未完成"
	}
	repeat := mallow.MenuEntry{ID: "repeat", Label: "设为每周重复", Glyph: mallow.GlyphRepeat}
	if k.Repeat != RepeatNone {
		repeat.Label = "取消重复"
	}
	switch mallow.ContextMenu(c, row, t.menuOpen("task:"+strconv.Itoa(k.ID)), []mallow.MenuEntry{
		{ID: "edit", Label: "编辑", Glyph: mallow.GlyphEdit},
		check,
		repeat,
		{ID: "today", Label: "改到今天", Glyph: mallow.GlyphCalendar},
		{ID: "tomorrow", Label: "改到明天", Glyph: mallow.GlyphCalendar},
		{ID: "delete", Label: "删除任务", Glyph: mallow.GlyphTrash, Danger: true},
	}) {
	case "edit":
		t.openTask(k.ID)
	case "done":
		t.SetDone(k, !k.Done)
	case "repeat":
		if k.Repeat != RepeatNone {
			k.Repeat = RepeatNone
		} else {
			k.Repeat = RepeatWeekly
		}
		t.Changed(k)
	case "today":
		k.Due = today
		t.Changed(k)
	case "tomorrow":
		k.Due = addDays(today, 1)
		t.Changed(k)
	case "delete":
		if t.ui.open == k.ID {
			t.ui.open = 0
		}
		t.Remove(k)
	}
}

// openIn counts the tasks still to do that a view lists.
func (t *Todo) openIn(view string) int {
	n := 0
	for _, g := range t.groups(view, mondayOf(t.Today())) {
		for _, k := range g.tasks {
			if !k.Done && k.Title != "" {
				n++
			}
		}
	}
	return n
}

// main is the view chosen: its heading, then a card with the line that
// adds a task and the tasks in their groups.
func (t *Todo) main(c *ui.Context) {
	groups := t.withOpen(t.groups(t.ui.view, t.ui.monday))
	ui.Column(c).Grow(1).MinWidth(0).Gap(20).Padding(24, 0, 0, 0).Children(func() {
		ui.Column(c).PaddingX(28).Children(func() { t.heading(c, groups) })
		ui.Column(c).Grow(1).MinHeight(0).Padding(0, 0, 8, 0).Children(func() {
			ui.Scroll(c).Grow(1).Padding(4, 20, 16, 20).Gap(2).Children(func() {
				for i, g := range groups {
					t.group(c.Key(i), g)
				}
				if len(groups) == 0 {
					ui.Text(c, t.emptyText()).Padding(20, 4).FontSize(14).TextColor(mallow.Muted)
				}
			})
		})
	})
}

func (t *Todo) emptyText() string {
	if t.ui.view == viewDone {
		return "还没有完成的任务。"
	}
	return "这里还没有任务。点右上角的 + 添加。"
}

// heading is the view's name, a line about it, and what acts on the view as
// a whole: the week view's arrows.
func (t *Todo) heading(c *ui.Context, groups []group) {
	today := t.Today()
	title, note := "", ""
	open := 0
	for _, g := range groups {
		for _, k := range g.tasks {
			if !k.Done && k.Title != "" {
				open++
			}
		}
	}
	if open > 0 {
		note = fmt.Sprintf("%d 项未完成", open)
	} else {
		note = "没有未完成的任务"
	}
	switch t.ui.view {
	case viewToday:
		title, note = "今天", dayNote(today)+" · "+note
	case viewNext7:
		title = "最近 7 天"
	case viewWeek:
		d, _ := time.Parse(dateLayout, t.ui.monday)
		_, w := d.ISOWeek()
		title = fmt.Sprintf("第 %d 周", w)
		note = shortDate(t.ui.monday, today) + " — " + shortDate(addDays(t.ui.monday, 6), today) + " · " + note
	case viewAll:
		title = "全部"
	case viewDone:
		title, note = "已完成", "勾掉的任务都在这里"
	}
	ui.Row(c).Shrink(0).PaddingX(4).Gap(16).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			ui.Text(c, title).FontSize(28).FontWeight(600).TextColor(mallow.Text).SingleLine()
			ui.Text(c, note).FontSize(14).TextColor(mallow.Muted)
		})
		// What acts on the view, the + that adds a task last: it is at
		// the same place in every view.
		defer func() {
			if t.ui.view == viewDone {
				return
			}
			add := mallow.IconButton(c, "添加任务", mallow.GlyphPlus, mallow.IconButtonStyle{Size: 36, Active: true})
			if add.Clicked() {
				t.startAdding()
			}
		}()
		if t.ui.view == viewWeek {
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				if mallow.IconButton(c, "上一周", mallow.GlyphChevronLeft, mallow.IconButtonStyle{Size: 36}).Clicked() {
					t.ui.monday = addDays(t.ui.monday, -7)
				}
				if mallow.Button(c, "本周").Clicked() {
					t.ui.monday = mondayOf(today)
				}
				if mallow.IconButton(c, "下一周", mallow.GlyphChevronRight, mallow.IconButtonStyle{Size: 36}).Clicked() {
					t.ui.monday = addDays(t.ui.monday, 7)
				}
			})
		}
	})
}

// startAdding shows the dialog that adds a task, its draft due today, but
// in the week view in the week shown (today, if that is this week, else its
// Monday).
func (t *Todo) startAdding() {
	t.closeOpen()
	today := t.Today()
	k := Task{Due: today}
	if t.ui.view == viewWeek && mondayOf(today) != t.ui.monday {
		k.Due = t.ui.monday
	}
	t.ui.draft, t.ui.adding = k, true
	t.resetCalendar(k.Due)
}

// adder is the dialog that adds a task: its title, where the keyboard is,
// and the rest of what a task has. Enter in the title, or 添加, adds it;
// Escape, 取消 or a click outside the dialog adds nothing.
func (t *Todo) adder(c *ui.Context) {
	k := &t.ui.draft
	add := func() {
		if t.Add(*k) != nil {
			t.ui.adding = false
		}
	}
	mallow.Dialog(c, &t.ui.adding, mallow.DialogOptions{Title: "新任务", Width: 520}, func() {
		title := mallow.TextInput(c, &k.Title).Placeholder("要做什么？").Label("任务标题").AutoFocus()
		if title.Submitted() {
			add()
		}
		// The rest scrolls where the window is too low for all of it, as
		// with the calendar open, so that the buttons stay in reach. The
		// room around it is for the shadows of what is in it, which a
		// scroll would cut at its edge.
		ui.Scroll(c).Shrink(1).MinHeight(0).Margin(0, -10).Padding(4, 10, 10, 10).Gap(14).Children(func() {
			t.fields(c, k, func() { t.settle(k) })
		})
		ui.Row(c).Gap(12).Justify(ui.End).Children(func() {
			if mallow.Button(c, "取消").Clicked() {
				t.ui.adding = false
			}
			if mallow.PrimaryButton(c, "添加").Disabled(strings.TrimSpace(k.Title) == "").Clicked() {
				add()
			}
		})
	})
}

// resetCalendar closes the calendar and turns it to the month of due, or
// to this one when that is no day.
func (t *Todo) resetCalendar(due string) {
	t.ui.calOpen, t.ui.calMonth = false, t.now()
	if d, err := time.ParseInLocation(dateLayout, due, time.Local); err == nil {
		t.ui.calMonth = d
	}
}

// closeOpen closes the task that is open.
func (t *Todo) closeOpen() { t.ui.open = 0 }

// openTask opens a task's details, closing those of the one that was open.
func (t *Todo) openTask(id int) {
	k := t.Task(id)
	if k == nil || id == t.ui.open {
		return
	}
	t.ui.open, t.ui.title, t.ui.titleWas = id, k.Title, k.Title
	t.resetCalendar(k.Due)
}

// withOpen keeps the task that is open in the list: edited so that the
// view no longer lists it, as given another day, it stays, in a group of
// its own at the top, until it is closed.
func (t *Todo) withOpen(groups []group) []group {
	open := t.Task(t.ui.open)
	if open == nil {
		t.ui.open = 0
		return groups
	}
	for _, g := range groups {
		if slices.Contains(g.tasks, open) {
			return groups
		}
	}
	return append([]group{{title: "正在编辑", tasks: []*Task{open}}}, groups...)
}

// group is a heading, if it has one, and the tasks under it.
func (t *Todo) group(c *ui.Context, g group) {
	fold := t.ui.view + "/" + g.title
	folded := g.title != "" && t.ui.folded[fold]
	ui.Column(c).Gap(2).Children(func() {
		if g.title != "" {
			// The heading folds the group away, and back.
			verb, mark := "收起", "▾"
			if folded {
				verb, mark = "展开", "▸"
			}
			head := mallow.Plain(c, verb+g.title).Margin(12, 0, 2, 0).Padding(6, 8, 6, 4).Gap(8)
			head.Children(func() {
				ui.Text(c, mark).Width(12).FontSize(11).TextColor(mallow.Muted)
				title := ui.Text(c, g.title).FontSize(14).FontWeight(600).TextColor(mallow.Text)
				if g.accent {
					title.TextColor(mallow.Accent)
				}
				if len(g.tasks) > 0 {
					ui.Text(c, strconv.Itoa(len(g.tasks))).FontSize(12).TextColor(mallow.Muted)
				}
				// A groove runs on from the heading to the far end, and
				// sets the group apart from the one above.
				ui.Text(c, g.note).FontSize(12).TextColor(mallow.Muted)
				mallow.Groove(c).Grow(1).Margin(0, 0, 0, 8)
			})
			if head.Clicked() {
				if t.ui.folded == nil {
					t.ui.folded = map[string]bool{}
				}
				t.ui.folded[fold] = !folded
			}
		} else {
			ui.Box(c).Height(8)
		}
		if folded {
			return
		}
		for _, k := range g.tasks {
			if k.ID == t.ui.open {
				t.detail(c.Key(k.ID), k)
			} else {
				t.row(c.Key(k.ID), k, g.dated)
			}
		}
		if len(g.tasks) == 0 {
			ui.Text(c, "今天没有要做的").Padding(6, 8).FontSize(14).TextColor(mallow.Muted)
		}
	})
}

// row is a task in the list: its check, and the rest, which opens its
// details: its title, then what the heading does not say already, as its
// due date.
func (t *Todo) row(c *ui.Context, k *Task, dated bool) {
	today := t.Today()
	row := mallow.ListRow(c, false)
	defer func() { t.taskMenu(c, row, k) }()
	row.Children(func() {
		done := k.Done
		if mallow.CheckMark(c, &done, "完成"+k.Title).Changed() {
			t.SetDone(k, done)
		}
		body := mallow.Plain(c, "打开"+k.Title).Grow(1).MinWidth(0).Padding(7, 4).Gap(8)
		body.Children(func() {
			title := ui.Text(c, k.Title).Grow(1).MinWidth(0).FontSize(14).TextColor(mallow.Text)
			if k.Done {
				title.TextColor(mallow.Muted).Strikethrough()
			}
			if k.Repeat != RepeatNone {
				mallow.GlyphRepeat(c, mallow.Accent.Alpha(0.7))
			}
			if k.Due != "" && !dated {
				due := ui.Text(c, dayTitle(k.Due, today)).FontSize(12).TextColor(mallow.Muted).SingleLine()
				if k.Due < today && !k.Done {
					due.TextColor(mallow.Danger)
				}
			}
		})
		if body.Clicked() {
			t.openTask(k.ID)
		}
	})
}

// detail is the task that is open: its row unfolded, in place, into a card
// that edits it. The title and the note are on it as written; the rest is a
// line each below, and the calendar shows when asked for.
func (t *Todo) detail(c *ui.Context, k *Task) {
	card := mallow.Card(c, mallow.Small).Margin(10, 4).Padding(12, 16, 16, 12).Gap(12)
	card.OnShortcut(0, ui.KeyEscape, func() { t.closeOpen() })
	card.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			done := k.Done
			if mallow.CheckMark(c, &done, "完成这项任务").Changed() {
				t.SetDone(k, done)
			}
			title := mallow.TextInput(c, &t.ui.title).Placeholder("要做什么？").Label("任务标题").Grow(1).MinWidth(0).Margin(0, 8, 0, 4)
			if title.Changed() {
				// A task is never left without a title: while the field
				// is empty, it keeps the one it had as its details opened.
				k.Title = strings.TrimSpace(t.ui.title)
				if k.Title == "" {
					k.Title = t.ui.titleWas
				}
				t.Changed(k)
			}
			// Enter is done writing: the details close.
			if title.Submitted() {
				t.closeOpen()
			}
			if mallow.GhostIconButton(c, "删除任务", mallow.GlyphTrash, mallow.IconButtonStyle{Danger: true}).Clicked() {
				t.Remove(k)
				t.ui.open = 0
			}
			if mallow.GhostIconButton(c, "关闭详情", mallow.GlyphClose, mallow.IconButtonStyle{}).Clicked() {
				t.closeOpen()
			}
		})
		ui.Column(c).Padding(0, 4, 0, 36).Gap(14).Children(func() {
			t.fields(c, k, func() { t.Changed(k) })
		})
	})
}

// fields are what a task has besides its title, each to be changed: the
// note, the day it is due, picked from a calendar when asked for, and the
// repeat. changed runs after one of them changed. They go in a column.
func (t *Todo) fields(c *ui.Context, k *Task, changed func()) {
	today := t.Today()
	line := func(name string, width float32, control func()) {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			mallow.Label(c, name).Width(48).Shrink(0)
			// A column, which stretches what is in it: a Segmented set
			// in a row is only as wide as its shortest label asks.
			ui.Column(c).Grow(1).MinWidth(0).MaxWidth(width).Children(control)
		})
	}
	if mallow.TextArea(c, &k.Note).Placeholder("备注").Label("备注").MinHeight(48).Changed() {
		changed()
	}
	line("日期", 640, func() {
		ui.Row(c).Grow(1).Gap(8).Wrap().Children(func() {
			quick := false
			for _, q := range []struct{ name, date string }{
				{"今天", today}, {"明天", addDays(today, 1)}, {"下周一", addDays(mondayOf(today), 7)}, {"无日期", ""},
			} {
				quick = quick || k.Due == q.date
				if mallow.Chip(c, q.name, k.Due == q.date).Clicked() {
					k.Due = q.date
					if q.date == "" {
						k.Repeat = RepeatNone
					}
					t.ui.calOpen = false
					changed()
				}
			}
			// The last chip is the day itself when it is none of
			// those, and brings the calendar. It is on only as
			// the day chosen: with the calendar open under one of
			// the others, two would look chosen.
			other := "其他日期"
			if !quick {
				other = shortDate(k.Due, today)
			}
			if mallow.Chip(c.Key("other"), other, !quick).Label("其他日期").Clicked() {
				t.ui.calOpen = !t.ui.calOpen
			}
		})
	})
	if t.ui.calOpen {
		ui.Column(c).Padding(0, 0, 0, 60).MaxWidth(360).Children(func() {
			if mallow.Calendar(c, "选择日期", &t.ui.calMonth, &k.Due, t.now()) {
				changed()
			}
		})
	}
	line("重复", 300, func() {
		if mallow.Segmented(c, "重复", &k.Repeat, []mallow.Option{
			{Value: RepeatNone, Label: "不重复"}, {Value: RepeatDaily, Label: "每天"},
			{Value: RepeatWeekly, Label: "每周"}, {Value: RepeatMonthly, Label: "每月"},
		}) {
			changed()
		}
	})
}
