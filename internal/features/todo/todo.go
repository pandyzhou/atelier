// Package todo is the to-do list: tasks, each with an optional due date, a
// note and a repeat, seen through views (today, the next seven days, a
// week, everything).
package todo

import (
	"cmp"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/pandyzhou/atelier/internal/store"
)

// dateLayout is how dates are saved: "2026-11-23", which sort as the days do.
const dateLayout = "2006-01-02"

// Repeats.
const (
	RepeatNone    = ""
	RepeatDaily   = "daily"
	RepeatWeekly  = "weekly"
	RepeatMonthly = "monthly"
)

// Task is one thing to do.
type Task struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Note  string `json:"note,omitempty"`
	// List is the list the task was in when tasks had lists, and Priority
	// how much it mattered, from 1 to 3, when they had that. Nothing shows
	// them or goes by them now; they are kept as they were saved.
	List     int    `json:"list,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Due      string `json:"due,omitempty"` // a date, "" for none
	// Repeat makes the task come back: checked off, it leaves a done copy
	// and moves its Due to the next time. A repeating task has a Due, which
	// while it is to do is never of a time gone by: see current.
	Repeat string `json:"repeat,omitempty"`
	Done   bool   `json:"done,omitempty"`
	DoneAt string `json:"doneAt,omitempty"` // the date it was checked off
}

// List is a list tasks were sorted into, when they were: see Task.List.
type List struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// data is what is saved.
type data struct {
	Next  int     `json:"next"`
	Lists []*List `json:"lists,omitempty"` // of old, kept as saved
	Tasks []*Task `json:"tasks"`
	// Migrated is set once the weekly checklist's data was taken over.
	Migrated bool `json:"migrated,omitempty"`
}

// Todo is the feature.
type Todo struct {
	store *store.Store
	data  data
	now   func() time.Time
	ui    viewState
	// rolled is the day the repeating tasks were last moved on: see roll.
	rolled string
}

// New loads the saved tasks from s, taking over the weekly checklist's on
// the first run.
func New(s *store.Store) *Todo { return newAt(s, time.Now) }

func newAt(s *store.Store, now func() time.Time) *Todo {
	t := &Todo{store: s, now: now}
	if err := s.Load(t.ID(), &t.data); err != nil {
		log.Printf("todo: load: %v", err)
	}
	if !t.data.Migrated {
		t.migrateWeekly()
	}
	// Tasks an older version started and left without a title.
	t.data.Tasks = slices.DeleteFunc(t.data.Tasks, func(k *Task) bool { return strings.TrimSpace(k.Title) == "" })
	t.ui = newViewState(t.now())
	return t
}

func (t *Todo) ID() string    { return "todo" }
func (t *Todo) Title() string { return "待办" }

// Today is today's date.
func (t *Todo) Today() string { return t.now().Format(dateLayout) }

// Tasks returns every task.
func (t *Todo) Tasks() []*Task {
	t.roll()
	return t.data.Tasks
}

// Task returns the task of an ID, nil if there is none.
func (t *Todo) Task(id int) *Task {
	t.roll()
	for _, k := range t.data.Tasks {
		if k.ID == id {
			return k
		}
	}
	return nil
}

// Add adds a task and returns it, nil for a blank title. A repeat without a
// due date starts today.
func (t *Todo) Add(k Task) *Task {
	k.Title = strings.TrimSpace(k.Title)
	if k.Title == "" {
		return nil
	}
	t.settle(&k)
	t.data.Next++
	k.ID = t.data.Next
	t.data.Tasks = append(t.data.Tasks, &k)
	t.save()
	return &k
}

// settle keeps what must hold of a task: one that repeats has a due date,
// and while it is to do, one of the time it is in now, or later.
func (t *Todo) settle(k *Task) {
	if k.Repeat == RepeatNone {
		return
	}
	today := t.Today()
	if k.Due == "" {
		k.Due = today
	}
	if !k.Done {
		k.Due = current(k.Due, k.Repeat, today)
	}
}

// roll moves the repeating tasks left undone on to the time they are of
// now (current), once a day: before the tasks are read, as the app may
// have stayed open overnight.
func (t *Todo) roll() {
	today := t.Today()
	if t.rolled == today {
		return
	}
	t.rolled = today
	moved := false
	for _, k := range t.data.Tasks {
		if k.Done || k.Repeat == RepeatNone {
			continue
		}
		if due := current(k.Due, k.Repeat, today); due != k.Due {
			k.Due, moved = due, true
		}
	}
	if moved {
		t.save()
	}
}

// Changed saves after a task was edited in place, and keeps what must hold
// of it.
func (t *Todo) Changed(k *Task) {
	if k != nil {
		t.settle(k)
	}
	t.save()
}

// SetDone checks a task off, or not. Checking off a repeating task leaves a
// done copy of it and moves the task to its next time.
func (t *Todo) SetDone(k *Task, done bool) {
	t.roll()
	today := t.Today()
	switch {
	case done && k.Repeat != RepeatNone:
		cp := *k
		t.data.Next++
		cp.ID, cp.Repeat, cp.Done, cp.DoneAt = t.data.Next, RepeatNone, true, today
		t.data.Tasks = append(t.data.Tasks, &cp)
		k.Due = nextDue(k.Due, k.Repeat, today)
	case done:
		k.Done, k.DoneAt = true, today
	default:
		k.Done, k.DoneAt = false, ""
	}
	t.save()
}

// nextDue is when a task due on due comes back: one step on, and further
// while that is still before today.
func nextDue(due, repeat, today string) string {
	if _, err := time.Parse(dateLayout, due); err != nil {
		due = today
	}
	for {
		if due = step(due, repeat); due >= today {
			return due
		}
	}
}

// step is the day a repeat comes back on after date: the next day, the same
// weekday of the next week, or the same day of the next month, that month's
// last where it has no such day, as the 31st in February. A date that is
// none comes back as it is.
func step(date, repeat string) string {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return date
	}
	switch repeat {
	case RepeatDaily:
		d = d.AddDate(0, 0, 1)
	case RepeatMonthly:
		first := time.Date(d.Year(), d.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		last := first.AddDate(0, 1, -1).Day()
		d = first.AddDate(0, 0, min(d.Day(), last)-1)
	default:
		d = d.AddDate(0, 0, 7)
	}
	return d.Format(dateLayout)
}

// period names the time a date is in for a repeat: the day itself, the
// Monday of its week, the first day of its month. They sort as the times do.
func period(date, repeat string) string {
	switch repeat {
	case RepeatDaily:
		return date
	case RepeatMonthly:
		return monthOf(date)
	}
	return mondayOf(date)
}

// Period is the first day of the time a repeating task is due in, the one it
// repeats by: the day itself, the Monday of its week, the first of its month.
func (k *Task) Period() string { return period(k.Due, k.Repeat) }

// Ahead reports whether a repeating task still to do is of a time to come, a
// day, a week or a month after today's, as it is once this time's was checked
// off: it is not to be done before that time begins (Period). Until then it
// shows as the times it will come back on do, and is not counted as to do.
func (t *Todo) Ahead(k *Task) bool { return ahead(k, t.Today()) }

func ahead(k *Task, today string) bool {
	return !k.Done && k.Repeat != RepeatNone && k.Due != "" && k.Period() > period(today, k.Repeat)
}

// current is the day a repeating task that is to do is due on now. It does
// not stay behind in a time gone by, the day, the week or the month it
// repeats by: there it is due as it comes back in today's, on the same
// weekday of this week, say. What was left undone before is not kept: each
// week has the task once, done in it or not.
func current(due, repeat, today string) string {
	if _, err := time.Parse(dateLayout, due); err != nil {
		return due
	}
	for period(due, repeat) < period(today, repeat) {
		due = step(due, repeat)
	}
	return due
}

// Coming is a day a repeating task will come back on.
type Coming struct {
	Date string
	Task *Task
}

// Coming returns the days from from to to, both of them too, that the
// repeating tasks still to do come back on after the day they are due, the
// earlier first: what the days ahead show of them, not to be done yet.
func (t *Todo) Coming(from, to string) []Coming {
	t.roll()
	if _, err := time.Parse(dateLayout, from); err != nil {
		return nil
	}
	if _, err := time.Parse(dateLayout, to); err != nil {
		return nil
	}
	var out []Coming
	for _, k := range t.data.Tasks {
		if k.Done || k.Repeat == RepeatNone {
			continue
		}
		d := step(k.Due, k.Repeat)
		if d == k.Due {
			continue // due on no day
		}
		// Not step by step up to days far ahead.
		if d < from {
			switch k.Repeat {
			case RepeatDaily:
				d = from
			case RepeatWeekly:
				d = addDays(d, 7*((daysFrom(d, from)+6)/7))
			}
		}
		for ; d <= to; d = step(d, k.Repeat) {
			if d >= from {
				out = append(out, Coming{Date: d, Task: k})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Coming) int { return cmp.Compare(a.Date, b.Date) })
	return out
}

// comingBy is Coming by day, after the tasks of a time to come (see Ahead) on
// the day they are due: neither are to be done yet.
func (t *Todo) comingBy(from, to string) map[string][]*Task {
	by := map[string][]*Task{}
	for _, k := range t.pick(func(k *Task) bool { return t.Ahead(k) && k.Due >= from && k.Due <= to }) {
		by[k.Due] = append(by[k.Due], k)
	}
	for _, o := range t.Coming(from, to) {
		by[o.Date] = append(by[o.Date], o.Task)
	}
	return by
}

// Remove deletes a task.
func (t *Todo) Remove(k *Task) {
	t.data.Tasks = slices.DeleteFunc(t.data.Tasks, func(x *Task) bool { return x == k })
	t.save()
}

func (t *Todo) save() {
	if err := t.store.Save(t.ID(), t.data); err != nil {
		log.Printf("todo: save: %v", err)
	}
}

// byOrder sorts tasks for a list: the earlier due first, those without a
// date last, then the older.
func byOrder(a, b *Task) int {
	if (a.Due == "") != (b.Due == "") {
		if a.Due == "" {
			return 1
		}
		return -1
	}
	return cmp.Or(cmp.Compare(a.Due, b.Due), cmp.Compare(a.ID, b.ID))
}

// addDays returns the date n days after date.
func addDays(date string, n int) string {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return date
	}
	return d.AddDate(0, 0, n).Format(dateLayout)
}

// daysFrom returns how many days b is after a.
func daysFrom(a, b string) int {
	x, err := time.Parse(dateLayout, a)
	if err != nil {
		return 0
	}
	y, err := time.Parse(dateLayout, b)
	if err != nil {
		return 0
	}
	return int(y.Sub(x).Hours() / 24)
}

// mondayOf returns the Monday of date's week.
func mondayOf(date string) string {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return date
	}
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7)).Format(dateLayout)
}
