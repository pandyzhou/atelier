package todo

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"
)

// viewState is what the page shows, which is not saved.
type viewState struct {
	view        string           // the view chosen
	open        int              // the ID of the task whose details show, 0 for none
	draft       string           // what is typed in the adder
	listDraft   string           // what is typed in the new list's input
	monday      string           // the Monday of the week the week view shows
	addDay      string           // the day the week view's adder adds to, "0" to "6"
	calMonth    time.Time        // the month the details' calendar shows
	calOpen     bool             // the details' calendar shows
	calFor      int              // the task that month was set for
	renaming    int              // the list whose name is being changed, 0 for none
	renameDraft string           // what is typed as its new name
	menus       map[string]*bool // the context menus that show, see menuOpen
	addWeekly   bool             // the next task added repeats weekly
	addingList  bool             // the new list's field shows
	focus       string           // what has the keyboard, of what is bound to it
	// folded holds the groups folded away, as "<view>/<heading>".
	folded map[string]bool
}

func newViewState(now time.Time) viewState {
	today := now.Format(dateLayout)
	return viewState{
		view:   viewToday,
		monday: mondayOf(today),
		addDay: strconv.Itoa((int(now.Weekday()) + 6) % 7),
	}
}

// What the keyboard is asked to go to, in viewState.focus.
const (
	focusNewList = "new-list"
	focusRename  = "rename"
)

const (
	navWidth = 208
)

// View builds the page: the views and lists to choose from, the tasks of
// the one chosen, and the details of the task opened, if one is.
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
}

// nav lists the views and the inbox, then the user's lists, where one is
// added, then what is done: each with how many tasks it has still to do.
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
			item(listView(Inbox), "收集箱", mallow.GlyphInbox),
		})
		mallow.Groove(c).MarginX(4)
		// The + beside the heading brings the new list's field, which goes
		// again once a list is added or the field is left.
		ui.Row(c).Padding(0, 4, 0, 12).AlignItems(ui.Center).Children(func() {
			mallow.Label(c, "清单").Grow(1)
			if mallow.GhostIconButton(c, "新建清单", mallow.GlyphPlus, mallow.IconButtonStyle{}).Clicked() {
				t.ui.addingList, t.ui.focus = true, focusNewList
			}
		})
		if len(t.data.Lists) > 0 {
			lists := make([]mallow.Item, len(t.data.Lists))
			for i, l := range t.data.Lists {
				lists[i] = item(listView(l.ID), l.Name, mallow.GlyphList)
				lists[i].With = func(c *ui.Context, entry ui.Element) { t.listMenu(c, entry, l.ID) }
			}
			mallow.SourceList(c, "清单", &t.ui.view, lists)
		}
		if t.ui.addingList {
			input := mallow.TextInput(c, &t.ui.listDraft).Placeholder("清单的名字，回车确认").Label("清单的名字").
				Margin(0, 8).FocusBind(&t.ui.focus, focusNewList)
			// Enter adds the list and shows it; leaving the field adds it
			// too, without leaving what was gone to; Escape drops it.
			cancelled := false
			input.OnShortcut(0, ui.KeyEscape, func() { cancelled = true })
			switch {
			case cancelled:
				t.ui.listDraft, t.ui.addingList, t.ui.focus = "", false, ""
			case input.Submitted():
				if l := t.AddList(t.ui.listDraft); l != nil {
					t.ui.view = listView(l.ID)
				}
				t.ui.listDraft, t.ui.addingList, t.ui.focus = "", false, ""
			case t.ui.focus != focusNewList:
				t.AddList(t.ui.listDraft)
				t.ui.listDraft, t.ui.addingList = "", false
			}
		}
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

// listMenu is what a right click on a list of the nav offers.
func (t *Todo) listMenu(c *ui.Context, entry ui.Element, id int) {
	switch mallow.ContextMenu(c, entry, t.menuOpen(listView(id)), []mallow.MenuEntry{
		{ID: "rename", Label: "重命名", Glyph: mallow.GlyphEdit},
		{ID: "delete", Label: "删除清单", Glyph: mallow.GlyphTrash, Danger: true},
	}) {
	case "rename":
		t.ui.view = listView(id)
		t.ui.renaming, t.ui.renameDraft, t.ui.focus = id, t.ListName(id), focusRename
	case "delete":
		t.RemoveList(id)
		if t.ui.view == listView(id) {
			t.ui.view = listView(Inbox)
		}
	}
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
		t.ui.open = k.ID
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
			if !k.Done {
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
			if t.ui.view != viewDone {
				t.adder(c)
			}
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
	return "这里还没有任务。在上面输入，回车添加。"
}

// heading is the view's name, a line about it, and what acts on the view as
// a whole: the week view's arrows, a list's delete button.
func (t *Todo) heading(c *ui.Context, groups []group) {
	today := t.Today()
	title, note := "", ""
	open := 0
	for _, g := range groups {
		for _, k := range g.tasks {
			if !k.Done {
				open++
			}
		}
	}
	if open > 0 {
		note = fmt.Sprintf("%d 项未完成", open)
	} else {
		note = "没有未完成的任务"
	}
	list, isList := viewList(t.ui.view)
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
	default:
		title = t.ListName(list)
	}
	ui.Row(c).Shrink(0).PaddingX(4).Gap(16).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			if isList && list != Inbox && t.ui.renaming == list {
				t.rename(c, list)
			} else {
				ui.Text(c, title).FontSize(28).FontWeight(600).TextColor(mallow.Text).SingleLine()
			}
			ui.Text(c, note).FontSize(14).TextColor(mallow.Muted)
		})
		switch {
		case t.ui.view == viewWeek:
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
		case isList && list != Inbox:
			if mallow.IconButton(c, "重命名清单", mallow.GlyphEdit, mallow.IconButtonStyle{Size: 36}).Clicked() {
				t.ui.renaming, t.ui.renameDraft, t.ui.focus = list, title, focusRename
			}
			if mallow.IconButton(c, "删除清单（任务移到收集箱）", mallow.GlyphClose, mallow.IconButtonStyle{Size: 36, Danger: true}).Clicked() {
				t.RemoveList(list)
				t.ui.view = listView(Inbox)
			}
		}
	})
}

// rename is the field a list's name is changed in, in the place of the
// heading: Enter, or leaving it, keeps the new name; Escape the old one.
func (t *Todo) rename(c *ui.Context, list int) {
	input := mallow.TextInput(c, &t.ui.renameDraft).Label("清单的新名字").
		FontSize(24).FocusBind(&t.ui.focus, focusRename)
	cancelled := false
	input.OnShortcut(0, ui.KeyEscape, func() { cancelled = true })
	switch {
	case cancelled:
	case input.Submitted(), t.ui.focus != focusRename:
		t.RenameList(list, t.ui.renameDraft)
	default:
		return
	}
	t.ui.renaming = 0
	if t.ui.focus == focusRename {
		t.ui.focus = ""
	}
}

// adder is the line that adds a task to the view: due today in the views of
// days, on the day chosen in the week view, and into the list in a list's.
func (t *Todo) adder(c *ui.Context) {
	// The week view's days go under the input: beside it they leave it no
	// room once the details show.
	ui.Column(c).Shrink(0).Padding(0, 24, 16, 24).Gap(12).Children(func() {
		// The button beside the input makes what is added come back every
		// week, for one task: it is off again once the task is added.
		var input ui.Element
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			input = mallow.TextInput(c, &t.ui.draft).Placeholder("+ 添加任务，回车确认").Label("添加任务").
				Grow(1).MinWidth(0)
			weekly := mallow.IconButton(c, "每周重复", mallow.GlyphRepeat, mallow.IconButtonStyle{Size: 34, Active: t.ui.addWeekly})
			if weekly.Clicked() {
				t.ui.addWeekly = !t.ui.addWeekly
			}
			tip := "添加为每周重复的任务"
			if t.ui.addWeekly {
				tip = "下一个任务会每周重复，再点一次取消"
			}
			mallow.Tooltip(c, weekly, tip)
		})
		if t.ui.view == viewWeek {
			days := make([]mallow.Option, 7)
			for d, name := range dayNames {
				days[d] = mallow.Option{Value: strconv.Itoa(d), Label: name}
			}
			mallow.Segmented(c, "添加到", &t.ui.addDay, days)
		}
		if input.Submitted() {
			k := Task{Title: t.ui.draft}
			switch t.ui.view {
			case viewToday, viewNext7:
				k.Due = t.Today()
			case viewWeek:
				d, _ := strconv.Atoi(t.ui.addDay)
				k.Due = addDays(t.ui.monday, d)
			default:
				k.List, _ = viewList(t.ui.view)
			}
			if t.ui.addWeekly {
				k.Repeat = RepeatWeekly
			}
			if t.Add(k) != nil {
				t.ui.addWeekly = false
			}
			t.ui.draft = ""
		}
	})
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
				ui.Text(c, g.note).Grow(1).FontSize(12).TextColor(mallow.Muted)
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

var priorityMarks = [...]string{"", "!", "!!", "!!!"}

func priorityColor(p int) ui.Color {
	switch p {
	case PriorityHigh:
		return mallow.Danger
	case PriorityMid:
		return mallow.Accent
	}
	return mallow.Muted
}

// row is a task in the list: its check, and the rest, which opens its
// details: its priority, its title, then what the heading does not say
// already, as its due date and its list.
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
			if k.Priority != PriorityNone && !k.Done {
				ui.Text(c, priorityMarks[k.Priority]).FontSize(14).FontWeight(700).TextColor(priorityColor(k.Priority))
			}
			title := ui.Text(c, k.Title).Grow(1).MinWidth(0).FontSize(14).TextColor(mallow.Text)
			if k.Done {
				title.TextColor(mallow.Muted).Strikethrough()
			}
			if k.Repeat != RepeatNone {
				mallow.GlyphRepeat(c, mallow.Accent.Alpha(0.7))
			}
			if _, isList := viewList(t.ui.view); !isList && k.List != Inbox {
				ui.Text(c, t.ListName(k.List)).FontSize(12).TextColor(mallow.Muted).SingleLine()
			}
			if k.Due != "" && !dated {
				due := ui.Text(c, dayTitle(k.Due, today)).FontSize(12).TextColor(mallow.Muted).SingleLine()
				if k.Due < today && !k.Done {
					due.TextColor(mallow.Danger)
				}
			}
		})
		if body.Clicked() {
			t.ui.open = k.ID
		}
	})
}

// detail is the task that is open: its row unfolded, in place, into a card
// that edits it. The title and the note are on it as written; the rest is a
// line each below, and the calendar shows when asked for.
func (t *Todo) detail(c *ui.Context, k *Task) {
	today := t.Today()
	if t.ui.calFor != k.ID {
		// The calendar opens on the month the task is due in.
		t.ui.calFor, t.ui.calMonth, t.ui.calOpen = k.ID, t.now(), false
		if d, err := time.ParseInLocation(dateLayout, k.Due, time.Local); err == nil {
			t.ui.calMonth = d
		}
	}
	line := func(name string, width float32, control func()) {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			mallow.Label(c, name).Width(48).Shrink(0)
			// A column, which stretches what is in it: a Segmented set
			// in a row is only as wide as its shortest label asks.
			ui.Column(c).Grow(1).MinWidth(0).MaxWidth(width).Children(control)
		})
	}
	card := mallow.Card(c, mallow.Small).Margin(10, 4).Padding(12, 16, 16, 12).Gap(12)
	card.OnShortcut(0, ui.KeyEscape, func() { t.ui.open = 0 })
	card.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			done := k.Done
			if mallow.CheckMark(c, &done, "完成这项任务").Changed() {
				t.SetDone(k, done)
			}
			if mallow.TextInput(c, &k.Title).Placeholder("任务标题").Label("任务标题").Grow(1).MinWidth(0).Margin(0, 8, 0, 4).Changed() {
				t.Changed(k)
			}
			if mallow.GhostIconButton(c, "删除任务", mallow.GlyphTrash, mallow.IconButtonStyle{Danger: true}).Clicked() {
				t.Remove(k)
				t.ui.open = 0
			}
			if mallow.GhostIconButton(c, "关闭详情", mallow.GlyphClose, mallow.IconButtonStyle{}).Clicked() {
				t.ui.open = 0
			}
		})
		ui.Column(c).Padding(0, 4, 0, 36).Gap(14).Children(func() {
			if mallow.TextArea(c, &k.Note).Placeholder("备注").Label("备注").MinHeight(48).Changed() {
				t.Changed(k)
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
							t.Changed(k)
						}
					}
					// The last chip is the day itself when it is none of
					// those, and brings the calendar.
					other := "其他日期"
					if !quick {
						other = shortDate(k.Due, today)
					}
					if mallow.Chip(c.Key("other"), other, !quick || t.ui.calOpen).Label("其他日期").Clicked() {
						t.ui.calOpen = !t.ui.calOpen
					}
				})
			})
			if t.ui.calOpen {
				ui.Column(c).Padding(0, 0, 0, 60).MaxWidth(360).Children(func() {
					if mallow.Calendar(c, "选择日期", &t.ui.calMonth, &k.Due, t.now()) {
						t.Changed(k)
					}
				})
			}
			line("优先级", 220, func() {
				priority := strconv.Itoa(k.Priority)
				if mallow.Segmented(c, "优先级", &priority, []mallow.Option{
					{Value: "0", Label: "无"}, {Value: "1", Label: "低"}, {Value: "2", Label: "中"}, {Value: "3", Label: "高"},
				}) {
					k.Priority, _ = strconv.Atoi(priority)
					t.Changed(k)
				}
			})
			line("重复", 300, func() {
				if mallow.Segmented(c, "重复", &k.Repeat, []mallow.Option{
					{Value: RepeatNone, Label: "不重复"}, {Value: RepeatDaily, Label: "每天"},
					{Value: RepeatWeekly, Label: "每周"}, {Value: RepeatMonthly, Label: "每月"},
				}) {
					t.Changed(k)
				}
			})
			line("清单", 640, func() {
				ui.Row(c).Grow(1).Gap(8).Wrap().Children(func() {
					ids := []int{Inbox}
					for _, l := range t.data.Lists {
						ids = append(ids, l.ID)
					}
					for _, id := range ids {
						if mallow.Chip(c.Key(id), t.ListName(id), k.List == id).Clicked() {
							k.List = id
							t.Changed(k)
						}
					}
				})
			})
		})
	})
}
