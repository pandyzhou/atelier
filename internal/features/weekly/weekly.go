// Package weekly is the weekly checklist: one column per day of the week,
// each a list of things to do that can be checked off.
package weekly

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/pandyzhou/mallow"

	"github.com/pandyzhou/atelier/internal/store"
)

// Item is one thing to do on one day.
type Item struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
	// Day is the day of the week, 0 for Monday through 6 for Sunday.
	Day int `json:"day"`
}

// data is what is saved: every week's items, by week key ("2026-W47").
type data struct {
	Next  int                `json:"next"`
	Weeks map[string][]*Item `json:"weeks"`
}

var dayNames = [7]string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}

// Weekly is the feature.
type Weekly struct {
	store  *store.Store
	data   data
	monday time.Time // the Monday of the week shown
	drafts [7]string // what is typed in each day's input
	now    func() time.Time
}

// New loads the saved checklist from s.
func New(s *store.Store) *Weekly {
	w := &Weekly{store: s, now: time.Now}
	if err := s.Load(w.ID(), &w.data); err != nil {
		log.Printf("weekly: load: %v", err)
	}
	if w.data.Weeks == nil {
		w.data.Weeks = map[string][]*Item{}
	}
	w.monday = mondayOf(w.now())
	return w
}

func (w *Weekly) ID() string    { return "weekly" }
func (w *Weekly) Title() string { return "每周清单" }

// Items returns the items of the week shown.
func (w *Weekly) Items() []*Item { return w.data.Weeks[weekKey(w.monday)] }

// Add adds an item to a day of the week shown.
func (w *Weekly) Add(day int, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	w.data.Next++
	k := weekKey(w.monday)
	w.data.Weeks[k] = append(w.data.Weeks[k], &Item{ID: w.data.Next, Text: text, Day: day})
	w.save()
}

// Remove removes an item from the week shown.
func (w *Weekly) Remove(id int) {
	k := weekKey(w.monday)
	items := w.data.Weeks[k]
	for i, it := range items {
		if it.ID == id {
			w.data.Weeks[k] = append(items[:i], items[i+1:]...)
			break
		}
	}
	if len(w.data.Weeks[k]) == 0 {
		delete(w.data.Weeks, k)
	}
	w.save()
}

func (w *Weekly) save() {
	if err := w.store.Save(w.ID(), w.data); err != nil {
		log.Printf("weekly: save: %v", err)
	}
}

// View builds the page: a header with the week and its navigation, then the
// seven days side by side as raised cards.
func (w *Weekly) View(c *ui.Context) {
	ui.Column(c).Fill().Padding(8, 24, 0, 16).Gap(24).Children(func() {
		w.header(c)
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

func (w *Weekly) days(c *ui.Context) {
	today := -1
	if mondayOf(w.now()).Equal(w.monday) {
		today = weekday(w.now())
	}
	items := w.Items()
	// The cards stand on the surface itself. Gaps are at least the shadow
	// blur, so shadows don't meet, and the padding leaves them room.
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Gap(16).Padding(4, 4, 0, 4).Children(func() {
		for d := range 7 {
			w.day(c.Key(d), d, d == today, items)
		}
	})
}

func (w *Weekly) day(c *ui.Context, d int, isToday bool, items []*Item) {
	date := w.monday.AddDate(0, 0, d)
	mallow.Card(c, mallow.Medium).Grow(1).MinWidth(0).PaddingY(8).Children(func() {
		// The day's heading: its name above, the date below; today's date in
		// an accent disc.
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
			for _, it := range items {
				if it.Day == d {
					w.item(c.Key(it.ID), it)
				}
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

// item is one thing to do, a mallow check; a delete button floats over its
// top right corner while the item is hovered, so the text keeps its width
// and does not wrap anew.
func (w *Weekly) item(c *ui.Context, it *Item) {
	wrap := ui.Box(c)
	hovered := wrap.Hovered()
	wrap.Children(func() {
		if mallow.Check(c, &it.Done, it.Text).Changed() {
			w.save()
		}
		if hovered {
			del := mallow.GhostIconButton(c, "删除", mallow.GlyphClose, mallow.IconButtonStyle{Danger: true}).
				Absolute().Top(3).Right(0)
			if del.Clicked() {
				w.Remove(it.ID)
			}
		}
	})
}

// mondayOf returns midnight of the Monday of t's week, in t's location.
func mondayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d-weekday(t), 0, 0, 0, 0, t.Location())
}

// weekday returns 0 for Monday through 6 for Sunday.
func weekday(t time.Time) int { return (int(t.Weekday()) + 6) % 7 }

// weekKey names an ISO week, as "2026-W47".
func weekKey(monday time.Time) string {
	y, w := monday.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}
