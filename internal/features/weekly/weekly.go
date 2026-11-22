// Package weekly is the weekly checklist: one column per day of the week,
// each a list of things to do that can be checked off.
package weekly

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/pandyzhou/atelier/internal/store"
	"github.com/pandyzhou/atelier/internal/theme"
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
// seven days side by side.
func (w *Weekly) View(c *ui.Context) {
	ui.Column(c).Fill().Padding(28, 32, 24, 32).Gap(20).Children(func() {
		w.header(c)
		w.days(c)
	})
}

func (w *Weekly) header(c *ui.Context) {
	t := c.Theme()
	year, week := w.monday.ISOWeek()
	sunday := w.monday.AddDate(0, 0, 6)
	ui.Row(c).AlignItems(ui.End).Gap(16).Children(func() {
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			ui.Text(c, "WEEKLY").FontSize(11).LetterSpacing(2).TextColor(t.Accent).Bold()
			ui.Textf(c, "%d 年 · 第 %d 周", year, week).FontSize(30).Bold()
			ui.Textf(c, "%d月%d日 — %d月%d日", w.monday.Month(), w.monday.Day(), sunday.Month(), sunday.Day()).
				TextColor(t.TextMuted)
		})
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "‹").Label("上一周").Tooltip("上一周").Clicked() {
				w.monday = w.monday.AddDate(0, 0, -7)
			}
			if ui.Button(c, "本周").Clicked() {
				w.monday = mondayOf(w.now())
			}
			if ui.Button(c, "›").Label("下一周").Tooltip("下一周").Clicked() {
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
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).
		BorderWidth(1, 0, 1, 1).BorderColor(theme.Rule).
		Children(func() {
			for d := range 7 {
				w.day(c.Key(d), d, d == today, items)
			}
		})
}

func (w *Weekly) day(c *ui.Context, d int, isToday bool, items []*Item) {
	t := c.Theme()
	date := w.monday.AddDate(0, 0, d)
	col := ui.Column(c).Grow(1).MinWidth(0).BorderWidth(0, 1, 0, 0).BorderColor(theme.Rule)
	if isToday {
		col.Background(theme.Today)
	}
	col.Children(func() {
		// The day's heading: its name on the left, the date large on the right.
		ui.Row(c).Padding(10, 12).AlignItems(ui.End).
			BorderWidth(0, 0, 1, 0).BorderColor(theme.Rule).Children(func() {
			name := ui.Text(c, dayNames[d]).Grow(1).FontSize(13).TextColor(t.TextMuted)
			if isToday {
				name.TextColor(t.Accent).Bold()
			}
			ui.Textf(c, "%d", date.Day()).FontSize(22)
		})
		ui.Scroll(c).Grow(1).Padding(8, 10).Gap(4).Children(func() {
			for _, it := range items {
				if it.Day == d {
					w.item(c.Key(it.ID), it)
				}
			}
			input := ui.TextInput(c, &w.drafts[d]).Placeholder("+ 添加").Label(dayNames[d] + "添加事项").
				Background(ui.Transparent).BorderColor(ui.Transparent)
			if input.Submitted() {
				w.Add(d, w.drafts[d])
				w.drafts[d] = ""
			}
		})
	})
}

func (w *Weekly) item(c *ui.Context, it *Item) {
	t := c.Theme()
	row := ui.Row(c).AlignItems(ui.Start).Gap(4)
	row.Children(func() {
		box := ui.Checkbox(c, &it.Done, it.Text).Grow(1).MinWidth(0)
		if it.Done {
			box.TextColor(t.TextMuted).Strikethrough()
		}
		if box.Changed() {
			w.save()
		}
		if row.Hovered() {
			if ui.Button(c, "×").Label("删除").Tooltip("删除").Padding(0, 6).
				Background(ui.Transparent).BorderColor(ui.Transparent).TextColor(t.TextMuted).Clicked() {
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
