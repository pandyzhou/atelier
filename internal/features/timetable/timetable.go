// Package timetable is the class timetable: the courses of a week, the same
// every week, each on a day over some of the day's periods.
package timetable

import (
	"cmp"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/pandyzhou/atelier/internal/store"
)

// The periods a day can have.
const (
	defaultPeriods = 10
	maxPeriods     = 16
)

// Period is one of the day's periods: when it starts and ends, as the user
// wrote them ("08:00"), "" when not said.
type Period struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// Course is a class of the week.
type Course struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Room    string `json:"room,omitempty"`
	Teacher string `json:"teacher,omitempty"`
	Day     int    `json:"day"` // 0 for Monday to 6 for Sunday
	// From and To are the first and the last of its periods, from 1.
	From int `json:"from"`
	To   int `json:"to"`
}

// data is what is saved.
type data struct {
	Next    int       `json:"next"`
	Periods []Period  `json:"periods"`
	Courses []*Course `json:"courses"`
}

// Timetable is the feature.
type Timetable struct {
	store *store.Store
	data  data
	now   func() time.Time
	ui    viewState
}

// New loads the saved timetable from s.
func New(s *store.Store) *Timetable {
	t := &Timetable{store: s, now: time.Now}
	if err := s.Load(t.ID(), &t.data); err != nil {
		log.Printf("timetable: load: %v", err)
	}
	if len(t.data.Periods) == 0 {
		t.data.Periods = make([]Period, defaultPeriods)
	}
	return t
}

func (t *Timetable) ID() string    { return "timetable" }
func (t *Timetable) Title() string { return "课表" }

// Courses returns every course, by day and then by period.
func (t *Timetable) Courses() []*Course { return t.data.Courses }

// Periods returns the day's periods.
func (t *Timetable) Periods() []Period { return t.data.Periods }

// Course returns the course of an ID, nil if there is none.
func (t *Timetable) Course(id int) *Course {
	for _, k := range t.data.Courses {
		if k.ID == id {
			return k
		}
	}
	return nil
}

// today is the day of the week it is, 0 for Monday.
func (t *Timetable) today() int { return (int(t.now().Weekday()) + 6) % 7 }

// check tidies a course and says what keeps it from the timetable: no
// name, or periods another course has.
func (t *Timetable) check(k *Course) error {
	k.Name, k.Room, k.Teacher = strings.TrimSpace(k.Name), strings.TrimSpace(k.Room), strings.TrimSpace(k.Teacher)
	if k.Name == "" {
		return errors.New("还没有填课程名")
	}
	n := len(t.data.Periods)
	k.Day = min(max(k.Day, 0), 6)
	k.From = min(max(k.From, 1), n)
	k.To = min(max(k.To, k.From), n)
	for _, o := range t.data.Courses {
		if o.ID != k.ID && o.Day == k.Day && o.From <= k.To && k.From <= o.To {
			return fmt.Errorf("%s第 %s 节已经有“%s”了", dayNames[o.Day], span(o.From, o.To), o.Name)
		}
	}
	return nil
}

// Put adds a course, when its ID is 0, or changes the one of its ID, and
// returns it; the error says why it could not.
func (t *Timetable) Put(k Course) (*Course, error) {
	if err := t.check(&k); err != nil {
		return nil, err
	}
	kept := t.Course(k.ID)
	if kept == nil {
		t.data.Next++
		k.ID = t.data.Next
		kept = &Course{}
		t.data.Courses = append(t.data.Courses, kept)
	}
	*kept = k
	slices.SortStableFunc(t.data.Courses, func(a, b *Course) int {
		return cmp.Or(cmp.Compare(a.Day, b.Day), cmp.Compare(a.From, b.From))
	})
	t.save()
	return kept, nil
}

// Remove takes a course from the timetable.
func (t *Timetable) Remove(id int) {
	t.data.Courses = slices.DeleteFunc(t.data.Courses, func(k *Course) bool { return k.ID == id })
	t.save()
}

// leastPeriods is the fewest periods a day can have: one, and as many as
// the courses there are go to.
func (t *Timetable) leastPeriods() int {
	n := 1
	for _, k := range t.data.Courses {
		n = max(n, k.To)
	}
	return n
}

// SetPeriods makes a day have n periods, within what it can have: those
// added have no times yet.
func (t *Timetable) SetPeriods(n int) {
	n = min(max(n, t.leastPeriods()), maxPeriods)
	for len(t.data.Periods) < n {
		t.data.Periods = append(t.data.Periods, Period{})
	}
	t.data.Periods = t.data.Periods[:n]
	t.save()
}

// Starts returns when a course starts on the day of a time, and whether
// that is known: its first period has a start that reads as a time of day.
func (t *Timetable) Starts(k *Course, day time.Time) (time.Time, bool) {
	if k.From < 1 || k.From > len(t.data.Periods) {
		return time.Time{}, false
	}
	// As it may have been typed: 8:20, 08：20 or 8.20.
	text := strings.NewReplacer("：", ":", ".", ":", " ", "").Replace(t.data.Periods[k.From-1].Start)
	clock, err := time.Parse("15:04", text)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, day.Location()), true
}

// Due returns the courses to remind of now: those of now's day that have
// not begun, and whose reminder, lead before they start, fell after the
// time given and not after now.
func (t *Timetable) Due(after, now time.Time, lead time.Duration) []*Course {
	var due []*Course
	for _, k := range t.data.Courses {
		if k.Day != (int(now.Weekday())+6)%7 {
			continue
		}
		start, ok := t.Starts(k, now)
		if !ok || !start.After(now) {
			continue
		}
		if at := start.Add(-lead); at.After(after) && !at.After(now) {
			due = append(due, k)
		}
	}
	return due
}

// Timed reports whether a period has a start set, by which courses can be
// reminded of.
func (t *Timetable) Timed() bool {
	for _, p := range t.data.Periods {
		if strings.TrimSpace(p.Start) != "" {
			return true
		}
	}
	return false
}

// When writes a course's periods and, when they have one, their time: "第
// 1–2 节 08:00–09:40".
func (t *Timetable) When(k *Course) string { return t.spanNote(k.From, k.To) }

// SetTime sets when a period, from 1, starts and ends: nothing of one
// there is not.
func (t *Timetable) SetTime(period int, p Period) {
	if period < 1 || period > len(t.data.Periods) {
		return
	}
	t.data.Periods[period-1] = Period{Start: strings.TrimSpace(p.Start), End: strings.TrimSpace(p.End)}
	t.save()
}

func (t *Timetable) save() {
	if err := t.store.Save(t.ID(), &t.data); err != nil {
		log.Printf("timetable: save: %v", err)
	}
}

var dayNames = [7]string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}

// span writes periods from one to another: "3", or "3–4".
func span(from, to int) string {
	if from == to {
		return fmt.Sprint(from)
	}
	return fmt.Sprintf("%d–%d", from, to)
}
