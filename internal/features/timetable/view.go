package timetable

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"
)

// viewState is what the page shows besides the timetable itself.
type viewState struct {
	// editing is whether the dialog of a course shows, and draft the
	// course in it: one to add while its ID is 0. problem is why it could
	// not be kept.
	editing bool
	draft   Course
	problem string
	// periods is whether the dialog of the day's periods shows.
	periods bool
	// span is the periods being dragged over on a day's column, for a
	// course to add: from the one pressed to the one the pointer is on.
	span struct {
		on            bool
		day, from, to int
	}
	menus map[string]*bool
}

const (
	axisWidth = 56 // the column of the periods' numbers and times
	minRow    = 48 // the least height of a period's row
)

// newEntry is the entry of a context menu that adds a course.
var newEntry = mallow.MenuEntry{ID: "new", Label: "新课程", Glyph: mallow.GlyphPlus}

func (t *Timetable) menuOpen(key string) *bool {
	if t.ui.menus == nil {
		t.ui.menus = map[string]*bool{}
	}
	if t.ui.menus[key] == nil {
		t.ui.menus[key] = new(bool)
	}
	return t.ui.menus[key]
}

// View builds the page: one card, a heading over the week's grid. A right
// click adds a course, where it was clicked when that was on the grid.
func (t *Timetable) View(c *ui.Context) {
	ui.Row(c).Fill().Padding(0, 28, 0, 16).AlignItems(ui.Stretch).Children(func() {
		page := mallow.Card(c, mallow.Medium).Grow(1).MinWidth(0).Gap(16).Padding(24, 4, 0, 4)
		page.Children(func() {
			ui.Column(c).PaddingX(28).Children(func() { t.heading(c) })
			t.grid(c)
		})
		if mallow.ContextMenu(c, page, t.menuOpen("page"), []mallow.MenuEntry{newEntry}) == newEntry.ID {
			t.startAdding(t.today(), 1, 1)
		}
	})
	t.editor(c)
	t.periodsEditor(c)
}

// heading is the page's title, how many courses there are, and the button
// to the day's periods.
func (t *Timetable) heading(c *ui.Context) {
	note := "还没有课程"
	if n := len(t.data.Courses); n > 0 {
		note = fmt.Sprintf("每周 %d 节课", n)
	}
	ui.Row(c).Shrink(0).PaddingX(4).Gap(16).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			ui.Text(c, "课表").FontSize(28).FontWeight(600).TextColor(mallow.Text).SingleLine()
			ui.Text(c, note).FontSize(14).TextColor(mallow.Muted)
		})
		if mallow.Button(c, "节次时间").Clicked() {
			t.ui.periods = true
		}
	})
}

// hairline is the color of the lines between the periods: faint, under
// the courses, which are what the page is of.
func hairline() ui.Color { return mallow.Text.Alpha(0.08) }

// hues gives each course its color: those of a name share one, and the
// names take the colors in the order their courses were added in.
func (t *Timetable) hues() map[string]ui.Color {
	first := map[string]int{} // the least ID of the courses of a name
	var names []string
	for _, k := range t.data.Courses {
		if id, ok := first[k.Name]; !ok {
			first[k.Name] = k.ID
			names = append(names, k.Name)
		} else if k.ID < id {
			first[k.Name] = k.ID
		}
	}
	slices.SortFunc(names, func(a, b string) int { return first[a] - first[b] })
	out := map[string]ui.Color{}
	for i, name := range names {
		out[name] = mallow.Hues[i%len(mallow.Hues)]
	}
	return out
}

// grid is the week: a column for each day, a row for each period, and the
// courses over the periods they take, each in its color. The rows share
// the height there is, and scroll where that would make them too low.
func (t *Timetable) grid(c *ui.Context) {
	n := len(t.data.Periods)
	_, h := c.Size()
	row := max(minRow, float32(int((h-250)/float32(n))))
	today := t.today()
	hues := t.hues()
	ui.Column(c).Grow(1).MinHeight(0).Padding(0, 20, 16, 20).Children(func() {
		ui.Row(c).Shrink(0).Padding(0, 0, 8, axisWidth).Children(func() {
			for i, name := range dayNames {
				ui.Row(c.Key(i)).Grow(1).Basis(0).Justify(ui.Center).Children(func() {
					// Today's name is pressed into the surface, in the
					// accent, as what is chosen in the sidebar is.
					mark := ui.Row(c).Padding(4, 14).Radius(mallow.RadiusPill)
					if i == today {
						mallow.Recessed(mark, mallow.Tiny)
					}
					mark.Children(func() {
						text := ui.Text(c, name).FontSize(12).TextColor(mallow.Muted).SingleLine()
						if i == today {
							text.TextColor(mallow.Accent).FontWeight(600)
						}
					})
				})
			}
		})
		ui.Scroll(c).Label("课程表").Grow(1).MinHeight(0).Children(func() {
			ui.Row(c).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Width(axisWidth).Shrink(0).Children(func() {
					for i, p := range t.data.Periods {
						ui.Column(c.Key(i)).Height(row).Children(func() {
							ui.Box(c).Height(1).Background(hairline())
							ui.Column(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Gap(1).Children(func() {
								ui.Text(c, strconv.Itoa(i+1)).FontSize(14).FontWeight(600).TextColor(mallow.Text)
								if p.Start != "" {
									ui.Text(c, p.Start).FontSize(10).TextColor(mallow.Muted).SingleLine()
								}
								if p.End != "" {
									ui.Text(c, p.End).FontSize(10).TextColor(mallow.Muted).SingleLine()
								}
							})
						})
					}
				})
				for day := range dayNames {
					t.day(c.Key(fmt.Sprint("day:", day)), day, row, hues)
				}
			})
		})
	})
}

// day is a day's column: a cell for each period, and the day's courses
// over them. A click on a free cell adds a course there, and a press drawn
// down or up over several adds one that takes them all; a right click
// offers the same for the one cell.
func (t *Timetable) day(c *ui.Context, day int, row float32, hues map[string]ui.Color) {
	n := len(t.data.Periods)
	column := ui.Column(c).Grow(1).Basis(0).MinWidth(0)
	_, y, _ := column.PointerPosition()
	column.Children(func() {
		pressed := 0 // the period whose cell is pressed, 0 for none
		for i := range t.data.Periods {
			cell := ui.Column(c.Key(i)).Label(fmt.Sprintf("%s第%d节", dayNames[day], i+1)).Height(row).Cursor(ui.CursorPointer)
			cell.Children(func() { ui.Box(c).Height(1).Background(hairline()) })
			if cell.Hovered() && !t.ui.span.on {
				cell.Background(mallow.Text.Alpha(0.04))
			}
			// Held with the primary button, wherever the pointer went
			// since: Pressed is only while it is still over the cell.
			if button, held := cell.PressedButton(); held && button == 0 {
				pressed = i + 1
			}
			if cell.PressCanceled() && t.ui.span.day == day {
				t.ui.span.on = false
			}
			key := fmt.Sprintf("cell:%d:%d", day, i)
			if mallow.ContextMenu(c.Key(key), cell, t.menuOpen(key), []mallow.MenuEntry{newEntry}) == newEntry.ID {
				t.startAdding(day, i+1, i+1)
			}
		}
		span := &t.ui.span
		switch {
		case pressed > 0:
			// From the cell pressed to the one the pointer is over now.
			at := min(max(int(y/row)+1, 1), n)
			span.on, span.day, span.from, span.to = true, day, min(pressed, at), max(pressed, at)
			ui.Box(c).Absolute().Left(3).Right(3).Top(float32(span.from-1)*row+4).Height(float32(span.to-span.from+1)*row-7).
				Radius(blockRadius).Background(mallow.Accent.Alpha(0.16)).Border(1, mallow.Accent.Alpha(0.5))
		case span.on && span.day == day:
			// Let go: the course to add takes what was drawn over.
			span.on = false
			t.startAdding(day, span.from, span.to)
		}
		for _, k := range t.data.Courses {
			if k.Day == day {
				t.block(c.Key(fmt.Sprint("block:", k.ID)), k, row, hues[k.Name])
			}
		}
	})
}

// blockRadius rounds the corners of a course on the grid.
const blockRadius = 10

// block is a course on the grid, over its periods: raised from the
// surface as the app's buttons are, faintly of its color, with a bar of it
// at its left and its name written in it. A click opens it, a right click
// offers that and to delete it.
func (t *Timetable) block(c *ui.Context, k *Course, row float32, hue ui.Color) {
	height := float32(k.To-k.From+1)*row - 9
	b := mallow.Plain(c, "打开"+k.Name).Absolute().Left(5).Right(5).
		Top(float32(k.From-1)*row+5).Height(height).
		Padding(7, 9, 7, 14).AlignItems(ui.Start).Radius(blockRadius)
	tint := float32(0.08)
	switch {
	case b.Pressed():
		mallow.Recessed(b, mallow.Tiny)
	case b.Hovered():
		mallow.Raised(b, mallow.Tiny)
		tint = 0.14
	default:
		mallow.Raised(b, 3)
	}
	b.Background(mallow.Base.Mix(hue, tint))
	// The hue, toward the color text is written in: to read on the surface.
	ink := hue.Mix(mallow.Text, 0.25)
	// As many lines of the name as there is room for over the rest.
	lines := max(1, min(4, int((height-14-32)/18)))
	b.Children(func() {
		ui.Box(c).Absolute().Left(5).Top(8).Bottom(8).Width(3).Radius(2).Background(hue)
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, k.Name).FontSize(13).FontWeight(600).TextColor(ink).MaxLines(lines)
			if k.Room != "" {
				ui.Text(c, "@"+k.Room).FontSize(11).TextColor(mallow.Text).SingleLine()
			}
			if k.Teacher != "" {
				ui.Text(c, k.Teacher).FontSize(11).TextColor(mallow.Muted).SingleLine()
			}
		})
	})
	if b.Clicked() {
		t.startEditing(k)
	}
	key := "course:" + strconv.Itoa(k.ID)
	switch mallow.ContextMenu(c.Key(key), b, t.menuOpen(key), []mallow.MenuEntry{
		newEntry,
		{}, // a line: what follows is about this course
		{ID: "edit", Label: "编辑", Glyph: mallow.GlyphEdit},
		{ID: "delete", Label: "删除课程", Glyph: mallow.GlyphTrash, Danger: true},
	}) {
	case "new":
		t.startAdding(k.Day, k.To+1, k.To+1)
	case "edit":
		t.startEditing(k)
	case "delete":
		t.Remove(k.ID)
	}
}

// startAdding shows the dialog of a course to add, on a day over periods.
func (t *Timetable) startAdding(day, from, to int) {
	n := len(t.data.Periods)
	from = min(max(from, 1), n)
	to = min(max(to, from), n)
	t.ui.draft, t.ui.problem, t.ui.editing = Course{Day: day, From: from, To: to}, "", true
}

// startEditing shows the dialog of a course there is.
func (t *Timetable) startEditing(k *Course) {
	t.ui.draft, t.ui.problem, t.ui.editing = *k, "", true
}

// editor is the dialog of a course, to add or to change: its name, where
// the keyboard is, where and by whom it is taught, its day and its periods.
// Enter in the name, or the button, keeps it; Escape, 取消 or a click
// outside keeps nothing.
func (t *Timetable) editor(c *ui.Context) {
	k := &t.ui.draft
	title, done := "新课程", "添加"
	if k.ID != 0 {
		title, done = "编辑课程", "保存"
	}
	keep := func() {
		if _, err := t.Put(*k); err != nil {
			t.ui.problem = err.Error()
			return
		}
		t.ui.editing = false
	}
	mallow.Dialog(c, &t.ui.editing, mallow.DialogOptions{Title: title, Width: 480}, func() {
		if mallow.TextInput(c, &k.Name).Placeholder("课程名").Label("课程名").AutoFocus().Submitted() {
			keep()
		}
		ui.Row(c).Gap(16).Children(func() {
			mallow.TextInput(c, &k.Room).Placeholder("地点").Label("上课地点").Grow(1).Basis(0).MinWidth(0)
			mallow.TextInput(c, &k.Teacher).Placeholder("老师").Label("任课老师").Grow(1).Basis(0).MinWidth(0)
		})
		ui.Column(c).Gap(8).Children(func() {
			mallow.Label(c, "星期")
			ui.Row(c).Gap(8).Wrap().Children(func() {
				for i, name := range dayNames {
					if mallow.Chip(c, name, k.Day == i).Clicked() {
						k.Day, t.ui.problem = i, ""
					}
				}
			})
		})
		ui.Column(c).Gap(8).Children(func() {
			mallow.Label(c, "节次："+t.spanNote(k.From, k.To))
			ui.Row(c).Gap(8).Wrap().Children(func() {
				for i := range t.data.Periods {
					p := i + 1
					chip := mallow.Chip(c, strconv.Itoa(p), k.From <= p && p <= k.To).Label(fmt.Sprintf("第%d节", p)).MinWidth(36)
					if !chip.Clicked() {
						continue
					}
					// A period outside those chosen stretches them to
					// it; one of them starts over from it.
					switch {
					case p < k.From:
						k.From = p
					case p > k.To:
						k.To = p
					default:
						k.From, k.To = p, p
					}
					t.ui.problem = ""
				}
			})
			ui.Text(c, "一门课连着上几节时，点它的第一节和最后一节。").FontSize(12).TextColor(mallow.Muted)
		})
		if t.ui.problem != "" {
			ui.Text(c, t.ui.problem).FontSize(13).TextColor(mallow.Danger)
		}
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			if k.ID != 0 && mallow.GhostIconButton(c, "删除这门课", mallow.GlyphTrash, mallow.IconButtonStyle{Danger: true}).Clicked() {
				t.Remove(k.ID)
				t.ui.editing = false
			}
			ui.Box(c).Grow(1)
			if mallow.Button(c, "取消").Clicked() {
				t.ui.editing = false
			}
			if mallow.PrimaryButton(c, done).Disabled(strings.TrimSpace(k.Name) == "").Clicked() {
				keep()
			}
		})
	})
}

// spanNote writes periods and, when both ends have one, their time: "第
// 1–2 节 08:00–09:40".
func (t *Timetable) spanNote(from, to int) string {
	note := "第 " + span(from, to) + " 节"
	if from < 1 || to > len(t.data.Periods) {
		return note
	}
	if start, end := t.data.Periods[from-1].Start, t.data.Periods[to-1].End; start != "" && end != "" {
		note += "  " + start + "–" + end
	}
	return note
}

// periodsEditor is the dialog of the day's periods: how many there are,
// and when each starts and ends. What is written is kept as it changes.
func (t *Timetable) periodsEditor(c *ui.Context) {
	mallow.Dialog(c, &t.ui.periods, mallow.DialogOptions{Title: "节次时间", Width: 420}, func() {
		n := len(t.data.Periods)
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "每天的节数").Grow(1).FontSize(14).TextColor(mallow.Text)
			if mallow.IconButton(c, "减少一节", mallow.GlyphMinimize, mallow.IconButtonStyle{Size: 32}).Disabled(n <= t.leastPeriods()).Clicked() {
				t.SetPeriods(n - 1)
			}
			ui.Text(c, strconv.Itoa(n)).Width(28).FontSize(15).FontWeight(500).TextColor(mallow.Text).TextAlign(ui.Center)
			if mallow.IconButton(c, "增加一节", mallow.GlyphPlus, mallow.IconButtonStyle{Size: 32}).Disabled(n >= maxPeriods).Clicked() {
				t.SetPeriods(n + 1)
			}
		})
		ui.Text(c, "每节的起止时间可以不填，填了会显示在课表左边。").FontSize(12).TextColor(mallow.Muted)
		// The room at its right is the scroll bar's.
		ui.Scroll(c).Label("各节时间").Height(320).Shrink(1).MinHeight(0).Padding(0, 18, 0, 0).Gap(6).Children(func() {
			for i := range t.data.Periods {
				p := &t.data.Periods[i]
				ui.Row(c.Key(i)).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, fmt.Sprintf("第 %d 节", i+1)).Width(64).Shrink(0).FontSize(13).TextColor(mallow.Muted)
					start := mallow.TextInput(c, &p.Start).Placeholder("开始").Label(fmt.Sprintf("第%d节开始", i+1)).Grow(1).Basis(0).MinWidth(0)
					ui.Text(c, "–").FontSize(13).TextColor(mallow.Muted)
					end := mallow.TextInput(c, &p.End).Placeholder("结束").Label(fmt.Sprintf("第%d节结束", i+1)).Grow(1).Basis(0).MinWidth(0)
					if start.Changed() || end.Changed() {
						p.Start, p.End = strings.TrimSpace(p.Start), strings.TrimSpace(p.End)
						t.save()
					}
				})
			}
		})
		ui.Row(c).Justify(ui.End).Children(func() {
			if mallow.PrimaryButton(c, "完成").Clicked() {
				t.ui.periods = false
			}
		})
	})
}
