package weekly

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"
)

// View builds the page as a to-do list: a header with the week and its
// navigation, then one card holding the line that adds a thing and, under
// it, the week's things in groups, the week's own first and then each day's.
func (w *Weekly) View(c *ui.Context) {
	ui.Column(c).Fill().Padding(8, 24, 0, 16).Gap(24).Children(func() {
		w.header(c)
		// The card stands on the surface itself: the padding leaves its
		// shadow room.
		ui.Column(c).Grow(1).MinHeight(0).Padding(4, 4, 0, 4).Children(func() {
			mallow.Card(c, mallow.Medium).Grow(1).MinHeight(0).Padding(20, 0, 8, 0).Children(func() {
				w.adder(c)
				mallow.Groove(c).MarginX(24)
				ui.Scroll(c).Grow(1).Padding(0, 24, 16, 24).Gap(4).Children(func() {
					w.groups(c)
				})
			})
		})
	})
}

func (w *Weekly) header(c *ui.Context) {
	year, week := w.monday.ISOWeek()
	sunday := w.monday.AddDate(0, 0, 6)
	ui.Row(c).AlignItems(ui.End).Gap(16).Children(func() {
		ui.Column(c).Grow(1).Gap(8).Children(func() {
			mallow.Label(c, "WEEKLY").LetterSpacing(1)
			ui.Textf(c, "%d 年 · 第 %d 周", year, week).FontSize(28).FontWeight(600).TextColor(mallow.Text)
			ui.Textf(c, "%d月%d日 — %d月%d日", w.monday.Month(), w.monday.Day(), sunday.Month(), sunday.Day()).
				FontSize(14).TextColor(mallow.Muted)
		})
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			if mallow.IconButton(c, "上一周", mallow.GlyphChevronLeft, mallow.IconButtonStyle{Size: 36}).Clicked() {
				w.monday = w.monday.AddDate(0, 0, -7)
			}
			if mallow.Button(c, "本周").Clicked() {
				w.monday = mondayOf(w.now())
			}
			if mallow.IconButton(c, "下一周", mallow.GlyphChevronRight, mallow.IconButtonStyle{Size: 36}).Clicked() {
				w.monday = w.monday.AddDate(0, 0, 7)
			}
		})
	})
}

// addTargets are where the adder can put a thing: the week's own list, or
// one of its days.
var addTargets = func() []mallow.Option {
	o := []mallow.Option{{Value: "week", Label: "整周"}}
	for d, name := range dayNames {
		o = append(o, mallow.Option{Value: fmt.Sprint(d), Label: name})
	}
	return o
}()

// addDay is the day the adder puts things on, or WeekList.
func (w *Weekly) addDay() int {
	for d := range 7 {
		if w.addTo == fmt.Sprint(d) {
			return d
		}
	}
	return WeekList
}

// adder is the line that adds a thing: what to do, where it goes (the week
// or a day), and whether it comes back every week.
func (w *Weekly) adder(c *ui.Context) {
	ui.Row(c).Shrink(0).Padding(0, 24, 16, 24).Gap(16).AlignItems(ui.Center).Children(func() {
		input := mallow.TextInput(c, &w.draft).Placeholder("+ 添加事项，回车确认").Label("添加事项").
			Grow(1).MinWidth(0).Padding(10, 14)
		mallow.Segmented(c, "添加到", &w.addTo, addTargets)
		label := "设为每周重复"
		if w.addRepeat {
			label = "每周重复：开"
		}
		if mallow.IconButton(c.Key("repeat"), label, mallow.GlyphRepeat, mallow.IconButtonStyle{Size: 36, Active: w.addRepeat}).Clicked() {
			w.addRepeat = !w.addRepeat
		}
		if input.Submitted() {
			if w.addRepeat {
				w.AddWeekly(w.addDay(), w.draft)
			} else {
				w.Add(w.addDay(), w.draft)
			}
			w.draft = ""
		}
	})
}

// groups lists the week's things: its own list, then each day that has
// some, and today whether it has or not.
func (w *Weekly) groups(c *ui.Context) {
	today := -1
	if mondayOf(w.now()).Equal(w.monday) {
		today = weekday(w.now())
	}
	shown := 0
	if e := w.Entries(WeekList); len(e) > 0 {
		w.group(c.Key("week"), "本周待办", "", false, e)
		shown++
	}
	for d := range 7 {
		e := w.Entries(d)
		if len(e) == 0 && d != today {
			continue
		}
		date := w.monday.AddDate(0, 0, d)
		w.group(c.Key(d), dayNames[d], fmt.Sprintf("%d月%d日", date.Month(), date.Day()), d == today, e)
		shown++
	}
	if shown == 0 {
		ui.Text(c, "这一周还没有事项。在上面输入，回车添加。").Padding(16, 4).FontSize(14).TextColor(mallow.Muted)
	}
}

// group is a heading and the entries under it: the heading's name, in the
// accent today, a note beside it, and how many of the entries are done.
func (w *Weekly) group(c *ui.Context, name, note string, isToday bool, entries []Entry) {
	done := 0
	for _, e := range entries {
		if e.Done {
			done++
		}
	}
	ui.Row(c).Padding(20, 4, 6, 4).Gap(8).AlignItems(ui.Center).Children(func() {
		title := ui.Text(c, name).FontSize(14).FontWeight(600).TextColor(mallow.Text)
		if isToday {
			title.TextColor(mallow.Accent)
			note += " · 今天"
		}
		ui.Text(c, note).Grow(1).FontSize(12).TextColor(mallow.Muted)
		if len(entries) > 0 {
			ui.Text(c, fmt.Sprintf("%d / %d", done, len(entries))).FontSize(12).TextColor(mallow.Muted)
		}
	})
	for _, e := range entries {
		w.entry(c.Key(e.key()), e)
	}
	if len(entries) == 0 {
		ui.Text(c, "今天还没有事项").Padding(6, 4).FontSize(14).TextColor(mallow.Muted)
	}
}

// entry is one line of a list, a mallow check. A weekly one carries a small
// ↻. While the line is hovered, buttons float over its top right corner, so
// that the text keeps its width: ↻ makes it repeat every week, or stop, and
// × takes it away.
func (w *Weekly) entry(c *ui.Context, e Entry) ui.Element {
	wrap := ui.Box(c)
	hovered := wrap.Hovered()
	return wrap.Children(func() {
		done := e.Done
		if mallow.Check(c, &done, e.Text).Changed() {
			w.SetDone(e, done)
		}
		if !hovered {
			if e.Repeats() {
				ui.Box(c).Absolute().Top(5).Right(2).PassThrough().Children(func() {
					mallow.GlyphRepeat(c, mallow.Accent.Alpha(0.7))
				})
			}
			return
		}
		repeatLabel, removeLabel := "每周重复", "删除"
		if e.Repeats() {
			repeatLabel, removeLabel = "取消每周重复（本周保留）", "删除（从本周起）"
		}
		ui.Row(c).Absolute().Top(3).Right(0).Gap(2).Background(mallow.Base).Radius(mallow.RadiusPill).Children(func() {
			if mallow.GhostIconButton(c, repeatLabel, mallow.GlyphRepeat, mallow.IconButtonStyle{Active: e.Repeats()}).Clicked() {
				w.SetRepeats(e, !e.Repeats())
			}
			if mallow.GhostIconButton(c, removeLabel, mallow.GlyphClose, mallow.IconButtonStyle{Danger: true}).Clicked() {
				w.Remove(e)
			}
		})
	})
}
