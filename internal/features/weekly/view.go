package weekly

import (
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"
)

// entryWidth is the width of an entry on the week's list, which flows them
// in rows.
const entryWidth = 230

// View builds the page: a header with the week and its navigation, the
// week's own list, then the seven days side by side as raised cards.
func (w *Weekly) View(c *ui.Context) {
	ui.Column(c).Fill().Padding(8, 24, 0, 16).Gap(24).Children(func() {
		w.header(c)
		w.weekList(c)
		w.days(c)
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

// weekList is the card of what belongs to the week as a whole: its entries
// flow in rows, the input to add one last.
func (w *Weekly) weekList(c *ui.Context) {
	entries := w.Entries(WeekList)
	done := 0
	for _, e := range entries {
		if e.Done {
			done++
		}
	}
	mallow.Card(c, mallow.Medium).Shrink(0).Margin(0, 4).Padding(14, 20, 16, 20).Gap(10).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Text(c, "本周待办").FontSize(14).FontWeight(600).TextColor(mallow.Text)
			if len(entries) > 0 {
				ui.Text(c, fmt.Sprintf("%d / %d", done, len(entries))).FontSize(12).TextColor(mallow.Muted)
			}
		})
		mallow.Groove(c)
		ui.Row(c).Wrap().GapX(16).GapY(4).AlignItems(ui.Center).Children(func() {
			for _, e := range entries {
				w.entry(c.Key(e.key()), e).Width(entryWidth)
			}
			input := mallow.TextInput(c, &w.weekDraft).Placeholder("+ 添加本周待办").Label("添加本周待办").
				Width(entryWidth).Padding(8, 10)
			if input.Submitted() {
				w.Add(WeekList, w.weekDraft)
				w.weekDraft = ""
			}
		})
	})
}

func (w *Weekly) days(c *ui.Context) {
	today := -1
	if mondayOf(w.now()).Equal(w.monday) {
		today = weekday(w.now())
	}
	// The cards stand on the surface itself. Gaps are at least the shadow
	// blur, so shadows don't meet, and the padding leaves them room.
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Gap(16).Padding(4, 4, 0, 4).Children(func() {
		for d := range 7 {
			w.day(c.Key(d), d, d == today)
		}
	})
}

func (w *Weekly) day(c *ui.Context, d int, isToday bool) {
	date := w.monday.AddDate(0, 0, d)
	mallow.Card(c, mallow.Medium).Grow(1).MinWidth(0).PaddingY(8).Children(func() {
		// The day's heading: its name, and its date, in an accent disc today.
		ui.Row(c).Padding(12, 16, 8, 16).AlignItems(ui.Center).Gap(8).Children(func() {
			name := ui.Text(c, dayNames[d]).Grow(1).FontSize(12).FontWeight(500).TextColor(mallow.Muted)
			num := ui.Box(c).Size(32, 32).Radius(mallow.RadiusPill).AlignItems(ui.Center).Justify(ui.Center)
			numColor := mallow.Text
			if isToday {
				name.TextColor(mallow.Accent)
				num.Background(mallow.Accent)
				numColor = mallow.OnAccent
			}
			num.Children(func() {
				ui.Textf(c, "%d", date.Day()).FontSize(16).FontWeight(600).TextColor(numColor)
			})
		})
		mallow.Groove(c).MarginX(12)
		ui.Scroll(c).Grow(1).Padding(12, 12, 12, 12).Gap(4).Children(func() {
			for _, e := range w.Entries(d) {
				w.entry(c.Key(e.key()), e)
			}
			input := mallow.TextInput(c, &w.drafts[d]).Placeholder("+ 添加").Label(dayNames[d]+"添加事项").
				Padding(8, 10).Margin(4, 0, 0, 0)
			if input.Submitted() {
				w.Add(d, w.drafts[d])
				w.drafts[d] = ""
			}
		})
	})
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
