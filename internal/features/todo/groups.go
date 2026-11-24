package todo

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// The views: what the list in the middle of the page shows.
const (
	viewToday = "today"
	viewNext7 = "next7"
	viewWeek  = "week"
	viewAll   = "all"
)

var dayNames = [7]string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}

// group is a heading and the tasks under it.
type group struct {
	title, note string
	accent      bool // the heading is today's
	dated       bool // the heading says the day: rows need not
	tasks       []*Task
}

// pick returns the tasks keep holds for, in list order.
func (t *Todo) pick(keep func(*Task) bool) []*Task {
	var out []*Task
	for _, k := range t.data.Tasks {
		if keep(k) {
			out = append(out, k)
		}
	}
	slices.SortFunc(out, byOrder)
	return out
}

// groups returns what a view lists; the week view shows the week of monday.
// Groups without tasks are left out, but today's in the views by day.
func (t *Todo) groups(view, monday string) []group {
	today := t.Today()
	open := func(k *Task) bool { return !k.Done }
	overdue := group{title: "已过期", tasks: t.pick(func(k *Task) bool { return open(k) && k.Due != "" && k.Due < today })}
	doneToday := group{title: "今天完成的", tasks: t.pick(func(k *Task) bool { return k.Done && k.DoneAt == today })}
	day := func(date string, withDone bool) group {
		g := group{title: dayTitle(date, today), note: dayNote(date), accent: date == today, dated: true}
		g.tasks = t.pick(func(k *Task) bool { return k.Due == date && (withDone || open(k)) })
		return g
	}

	var gs []group
	switch view {
	case viewToday:
		gs = []group{overdue, day(today, false), doneToday}
	case viewNext7:
		gs = append(gs, overdue)
		for i := range 7 {
			gs = append(gs, day(addDays(today, i), false))
		}
	case viewWeek:
		for i := range 7 {
			g := day(addDays(monday, i), true)
			g.title, g.note = dayNames[i], shortDate(addDays(monday, i), today)
			if g.accent {
				g.note += " · 今天"
			}
			gs = append(gs, g)
		}
	case viewAll:
		week := addDays(today, 7)
		gs = []group{
			overdue,
			day(today, false),
			{title: "最近 7 天", tasks: t.pick(func(k *Task) bool { return open(k) && k.Due > today && k.Due < week })},
			{title: "以后", tasks: t.pick(func(k *Task) bool { return open(k) && k.Due >= week })},
			{title: "没有日期", tasks: t.pick(func(k *Task) bool { return open(k) && k.Due == "" })},
		}
	}
	return slices.DeleteFunc(gs, func(g group) bool { return len(g.tasks) == 0 && !g.accent })
}

// dayTitle names a day: 今天, 明天, 昨天, or its weekday within a week ahead,
// else its date.
func dayTitle(date, today string) string {
	switch date {
	case today:
		return "今天"
	case addDays(today, 1):
		return "明天"
	case addDays(today, -1):
		return "昨天"
	}
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return "没有日期"
	}
	if date > today && date < addDays(today, 7) {
		return dayNames[(int(d.Weekday())+6)%7]
	}
	return shortDate(date, today)
}

// dayNote is what stands beside a day's title: its date and weekday.
func dayNote(date string) string {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d月%d日 %s", int(d.Month()), d.Day(), dayNames[(int(d.Weekday())+6)%7])
}

// shortDate writes a date as "11月23日", with the year if it is not today's.
func shortDate(date, today string) string {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(today, date[:4]) {
		return fmt.Sprintf("%d年%d月%d日", d.Year(), int(d.Month()), d.Day())
	}
	return fmt.Sprintf("%d月%d日", int(d.Month()), d.Day())
}
