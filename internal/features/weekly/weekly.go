// Package weekly is the weekly checklist, a to-do list of the week: the
// week's own things, then each day's, which can be checked off. Any of them
// can repeat every week, from the week it was set on.
package weekly

import (
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/pandyzhou/atelier/internal/store"
)

// WeekList is the Day of what belongs to the week as a whole, not to one of
// its days: the list above the days.
const WeekList = -1

// Item is one thing to do in one week, on a day or on the week's list.
type Item struct {
	ID   int    `json:"id"`
	Text string `json:"text"`
	Done bool   `json:"done"`
	// Day is the day of the week, 0 for Monday through 6 for Sunday, or
	// WeekList.
	Day int `json:"day"`
}

// Rule is a thing to do every week: it shows on its Day of every week from
// the week Since, up to but not including the week Until ("" for no end).
// Each week checks it off on its own.
type Rule struct {
	ID    int    `json:"id"`
	Text  string `json:"text"`
	Day   int    `json:"day"`
	Since string `json:"since"`
	Until string `json:"until,omitempty"`
}

// data is what is saved. Week keys are ISO weeks, as "2026-W47", which sort
// as the weeks do.
type data struct {
	Next  int                `json:"next"`
	Weeks map[string][]*Item `json:"weeks"`
	Rules []*Rule            `json:"rules,omitempty"`
	// RuleDone is, for each week, the IDs of the rules checked off in it.
	RuleDone map[string][]int `json:"ruleDone,omitempty"`
}

// Entry is a line of a list: an item of the week, or the week's occurrence
// of a rule.
type Entry struct {
	Item *Item
	Rule *Rule
	Text string
	Done bool
}

// Repeats reports whether the entry comes back every week.
func (e Entry) Repeats() bool { return e.Rule != nil }

// key names the entry among its list's, for the view.
func (e Entry) key() string {
	if e.Rule != nil {
		return fmt.Sprintf("r%d", e.Rule.ID)
	}
	return fmt.Sprintf("i%d", e.Item.ID)
}

var dayNames = [7]string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}

// Weekly is the feature.
type Weekly struct {
	store     *store.Store
	data      data
	monday    time.Time // the Monday of the week shown
	draft     string    // what is typed in the adder
	addTo     string    // where the adder puts it: "week", or a day "0" to "6"
	addRepeat bool      // whether the adder makes it come back every week
	now       func() time.Time
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
	if w.data.RuleDone == nil {
		w.data.RuleDone = map[string][]int{}
	}
	w.monday = mondayOf(w.now())
	w.addTo = fmt.Sprint(weekday(w.now()))
	return w
}

func (w *Weekly) ID() string    { return "weekly" }
func (w *Weekly) Title() string { return "每周清单" }

// week is the key of the week shown.
func (w *Weekly) week() string { return weekKey(w.monday) }

// Items returns the items of the week shown, its rules' occurrences aside.
func (w *Weekly) Items() []*Item { return w.data.Weeks[w.week()] }

// Rules returns every rule, those that ended too.
func (w *Weekly) Rules() []*Rule { return w.data.Rules }

// Entries returns what the week shown lists on day (or WeekList): the rules
// that hold that week first, then its own items.
func (w *Weekly) Entries(day int) []Entry {
	k := w.week()
	var out []Entry
	for _, r := range w.data.Rules {
		if r.Day == day && r.holds(k) {
			out = append(out, Entry{Rule: r, Text: r.Text, Done: slices.Contains(w.data.RuleDone[k], r.ID)})
		}
	}
	for _, it := range w.data.Weeks[k] {
		if it.Day == day {
			out = append(out, Entry{Item: it, Text: it.Text, Done: it.Done})
		}
	}
	return out
}

// holds reports whether the rule shows in week k.
func (r *Rule) holds(k string) bool {
	return r.Since <= k && (r.Until == "" || k < r.Until)
}

// Add adds an item to a day (or the list) of the week shown.
func (w *Weekly) Add(day int, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	w.data.Next++
	k := w.week()
	w.data.Weeks[k] = append(w.data.Weeks[k], &Item{ID: w.data.Next, Text: text, Day: day})
	w.save()
}

// AddWeekly adds a thing to do every week, from the week shown on, to a day
// (or the list).
func (w *Weekly) AddWeekly(day int, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	w.data.Next++
	w.data.Rules = append(w.data.Rules, &Rule{ID: w.data.Next, Text: text, Day: day, Since: w.week()})
	w.save()
}

// SetDone checks the entry off in the week shown, or not.
func (w *Weekly) SetDone(e Entry, done bool) {
	if e.Item != nil {
		e.Item.Done = done
	} else {
		w.setRuleDone(e.Rule.ID, done)
	}
	w.save()
}

func (w *Weekly) setRuleDone(id int, done bool) {
	k := w.week()
	ids := slices.DeleteFunc(w.data.RuleDone[k], func(x int) bool { return x == id })
	if done {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		delete(w.data.RuleDone, k)
	} else {
		w.data.RuleDone[k] = ids
	}
}

// Remove takes the entry away: an item from the week shown; a rule from the
// week shown on, the weeks before keeping it.
func (w *Weekly) Remove(e Entry) {
	if e.Item != nil {
		w.removeItem(e.Item.ID)
	} else {
		w.setRuleDone(e.Rule.ID, false)
		w.endRule(e.Rule)
	}
	w.save()
}

// SetRepeats makes the entry come back every week from the week shown, or
// stop doing so. An item becomes a rule starting this week, checked off this
// week as the item was; a rule ends after this week, which keeps it as an
// item of its own.
func (w *Weekly) SetRepeats(e Entry, repeat bool) {
	k := w.week()
	switch {
	case repeat && e.Item != nil:
		w.data.Next++
		r := &Rule{ID: w.data.Next, Text: e.Item.Text, Day: e.Item.Day, Since: k}
		w.data.Rules = append(w.data.Rules, r)
		if e.Item.Done {
			w.setRuleDone(r.ID, true)
		}
		w.removeItem(e.Item.ID)
	case !repeat && e.Rule != nil:
		w.data.Next++
		w.data.Weeks[k] = append(w.data.Weeks[k], &Item{ID: w.data.Next, Text: e.Rule.Text, Day: e.Rule.Day, Done: e.Done})
		w.setRuleDone(e.Rule.ID, false)
		w.endRule(e.Rule)
	default:
		return
	}
	w.save()
}

// endRule stops the rule from the week shown on: a rule that started this
// week goes altogether.
func (w *Weekly) endRule(r *Rule) {
	k := w.week()
	if r.Since >= k {
		w.data.Rules = slices.DeleteFunc(w.data.Rules, func(x *Rule) bool { return x == r })
		return
	}
	r.Until = k
}

func (w *Weekly) removeItem(id int) {
	k := w.week()
	w.data.Weeks[k] = slices.DeleteFunc(w.data.Weeks[k], func(it *Item) bool { return it.ID == id })
	if len(w.data.Weeks[k]) == 0 {
		delete(w.data.Weeks, k)
	}
}

func (w *Weekly) save() {
	if err := w.store.Save(w.ID(), w.data); err != nil {
		log.Printf("weekly: save: %v", err)
	}
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
