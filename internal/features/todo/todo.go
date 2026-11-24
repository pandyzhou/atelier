// Package todo is the to-do list: tasks, each with an optional due date, a
// note and a repeat, seen through views (today, the next seven days, a
// week, everything, what is done).
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
	// and moves its Due to the next time. A repeating task has a Due.
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
	// Tasks started and never given a title.
	t.data.Tasks = slices.DeleteFunc(t.data.Tasks, func(k *Task) bool { return strings.TrimSpace(k.Title) == "" })
	t.ui = newViewState(t.now())
	return t
}

func (t *Todo) ID() string    { return "todo" }
func (t *Todo) Title() string { return "待办" }

// Today is today's date.
func (t *Todo) Today() string { return t.now().Format(dateLayout) }

// Tasks returns every task.
func (t *Todo) Tasks() []*Task { return t.data.Tasks }

// Task returns the task of an ID, nil if there is none.
func (t *Todo) Task(id int) *Task {
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
	if k.Repeat != RepeatNone && k.Due == "" {
		k.Due = t.Today()
	}
	t.data.Next++
	k.ID = t.data.Next
	t.data.Tasks = append(t.data.Tasks, &k)
	t.save()
	return &k
}

// Start adds a task that has no title yet, to be written in its details,
// and returns it. One left without a title is dropped: by Drop when its
// details close, and on the next start if the app closed before that.
func (t *Todo) Start(k Task) *Task {
	if k.Repeat != RepeatNone && k.Due == "" {
		k.Due = t.Today()
	}
	t.data.Next++
	k.ID = t.data.Next
	t.data.Tasks = append(t.data.Tasks, &k)
	t.save()
	return &k
}

// Drop removes the task of an ID if it has no title, and reports whether
// it did.
func (t *Todo) Drop(id int) bool {
	k := t.Task(id)
	if k == nil || strings.TrimSpace(k.Title) != "" {
		return false
	}
	t.Remove(k)
	return true
}

// Changed saves after a task was edited in place, and keeps what
// must hold: a repeating task has a due date.
func (t *Todo) Changed(k *Task) {
	if k != nil && k.Repeat != RepeatNone && k.Due == "" {
		k.Due = t.Today()
	}
	t.save()
}

// SetDone checks a task off, or not. Checking off a repeating task leaves a
// done copy of it and moves the task to its next time.
func (t *Todo) SetDone(k *Task, done bool) {
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
	d, err := time.Parse(dateLayout, due)
	if err != nil {
		d, _ = time.Parse(dateLayout, today)
	}
	for {
		switch repeat {
		case RepeatDaily:
			d = d.AddDate(0, 0, 1)
		case RepeatMonthly:
			d = d.AddDate(0, 1, 0)
		default:
			d = d.AddDate(0, 0, 7)
		}
		if s := d.Format(dateLayout); s >= today {
			return s
		}
	}
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

// mondayOf returns the Monday of date's week.
func mondayOf(date string) string {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return date
	}
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7)).Format(dateLayout)
}
