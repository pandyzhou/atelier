package todo

import (
	"slices"
	"testing"
	"time"

	"github.com/pandyzhou/atelier/internal/store"
)

// wednesday is today in tests: 2026-11-25, in ISO week 48.
func wednesday() time.Time { return time.Date(2026, 11, 25, 10, 0, 0, 0, time.Local) }

func newTest(dir string) *Todo { return newAt(store.Open(dir), wednesday) }

// titles returns what a view lists, as "heading: task task" for each group,
// with the repeating tasks that will come back there in brackets.
func titles(t *Todo, view string) []string { return titlesOf(t, view, mondayOf(t.Today())) }

// titlesOf is titles, of the week of a Monday in the week view.
func titlesOf(t *Todo, view, monday string) []string {
	var out []string
	for _, g := range t.groups(view, monday) {
		s := g.title + ":"
		for _, k := range g.tasks {
			s += " " + k.Title
		}
		for _, k := range g.coming {
			s += " (" + k.Title + ")"
		}
		out = append(out, s)
	}
	return out
}

func equal(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s: %q, want %q", what, got, want)
	}
}

func TestViews(t *testing.T) {
	d := newTest("")
	d.Add(Task{Title: "过期的", Due: "2026-11-24"})
	d.Add(Task{Title: "今天的", Due: "2026-11-25"})
	// A priority of old does not put a task before an older one.
	d.Add(Task{Title: "后加的", Due: "2026-11-25", Priority: 3})
	d.Add(Task{Title: "周五的", Due: "2026-11-27"})
	d.Add(Task{Title: "下个月的", Due: "2026-12-20"})
	d.Add(Task{Title: "没日期的"})
	d.SetDone(d.Add(Task{Title: "做完的", Due: "2026-11-25"}), true)

	equal(t, "today", titles(d, viewToday), "已过期: 过期的", "今天: 今天的 后加的", "今天完成的: 做完的")
	equal(t, "next 7 days", titles(d, viewNext7), "已过期: 过期的", "今天: 今天的 后加的", "周五: 周五的")
	equal(t, "week", titles(d, viewWeek), "周二: 过期的", "周三: 今天的 后加的 做完的", "周五: 周五的")
	equal(t, "all", titles(d, viewAll),
		"已过期: 过期的", "今天: 今天的 后加的", "最近 7 天: 周五的", "以后: 下个月的", "没有日期: 没日期的")
}

func TestRepeat(t *testing.T) {
	// A task due on due, checked off on the 25th, a Wednesday: the copy it
	// leaves done is of the day done, and it is due again on next.
	for _, c := range []struct{ due, repeat, done, next string }{
		{"2026-11-25", RepeatDaily, "2026-11-25", "2026-11-26"},
		{"2026-11-25", RepeatWeekly, "2026-11-25", "2026-12-02"},
		{"2026-11-25", RepeatMonthly, "2026-11-25", "2026-12-25"},
		{"2026-11-27", RepeatWeekly, "2026-11-27", "2026-12-04"}, // done early: the time after the one due
		{"2026-11-23", RepeatWeekly, "2026-11-23", "2026-11-30"}, // late, in its week
		// Left from a time gone by, it is of this one: done for this week,
		// for today, for this month.
		{"2026-11-09", RepeatWeekly, "2026-11-23", "2026-11-30"},
		{"2026-11-20", RepeatDaily, "2026-11-25", "2026-11-26"},
		{"2026-10-05", RepeatMonthly, "2026-11-05", "2026-12-05"},
	} {
		d := newTest("")
		k := d.Add(Task{Title: "跑步", Due: c.due, Repeat: c.repeat})
		d.SetDone(k, true)
		if k.Done || k.Due != c.next {
			t.Errorf("%s due %s, checked off on the 25th: now due %s, done %v; want %s", c.repeat, c.due, k.Due, k.Done, c.next)
		}
		if n := len(d.Tasks()); n != 2 || !d.Tasks()[1].Done || d.Tasks()[1].Repeat != RepeatNone || d.Tasks()[1].Due != c.done {
			t.Errorf("%s due %s: the done copy is %+v, want one due %s", c.repeat, c.due, d.Tasks()[1], c.done)
		}
	}

	// A repeat needs a day to start from.
	d := newTest("")
	if k := d.Add(Task{Title: "浇花", Repeat: RepeatWeekly}); k.Due != "2026-11-25" {
		t.Errorf("a repeating task added without a date is due %q", k.Due)
	}

	// A month on is the same day, or the month's last where it has none.
	for _, c := range [][2]string{
		{"2027-01-31", "2027-02-28"}, {"2028-01-31", "2028-02-29"}, {"2026-12-15", "2027-01-15"}, {"2027-03-31", "2027-04-30"},
	} {
		if got := step(c[0], RepeatMonthly); got != c[1] {
			t.Errorf("a month after %s is %s, want %s", c[0], got, c[1])
		}
	}
}

// A repeating task left undone does not stay behind: in a new day, week or
// month it is that one's, on the day it comes back on. Other tasks stay.
func TestRoll(t *testing.T) {
	dir := t.TempDir()
	now := wednesday()
	on := func(date string) {
		now, _ = time.ParseInLocation(dateLayout+" 15:04", date+" 09:00", time.Local)
	}
	d := newAt(store.Open(dir), func() time.Time { return now })
	daily := d.Add(Task{Title: "跑步", Due: "2026-11-25", Repeat: RepeatDaily})
	weekly := d.Add(Task{Title: "同步上游", Due: "2026-11-25", Repeat: RepeatWeekly})
	monthly := d.Add(Task{Title: "交房租", Due: "2026-11-25", Repeat: RepeatMonthly})
	last := d.Add(Task{Title: "对账", Due: "2026-11-30", Repeat: RepeatMonthly})
	plain := d.Add(Task{Title: "交水电费", Due: "2026-11-24"})
	done := d.Add(Task{Title: "做完的", Due: "2026-11-25"})
	d.SetDone(done, true)
	dues := func() []string {
		d.Tasks()
		return []string{daily.Due, weekly.Due, monthly.Due, last.Due, plain.Due, done.Due}
	}

	// Sunday: the day's is today's, the week's and the months' are overdue.
	on("2026-11-29")
	equal(t, "on Sunday", dues(), "2026-11-29", "2026-11-25", "2026-11-25", "2026-11-30", "2026-11-24", "2026-11-25")
	equal(t, "Sunday's views", titles(d, viewToday), "已过期: 交水电费 同步上游 交房租", "今天: 跑步")
	// Monday, of a new week: the week's is this week's, on its Wednesday.
	on("2026-11-30")
	equal(t, "on Monday", dues(), "2026-11-30", "2026-12-02", "2026-11-25", "2026-11-30", "2026-11-24", "2026-11-25")
	equal(t, "Monday's week", titles(d, viewWeek), "周一: 跑步 对账", "周二: (跑步)", "周三: 同步上游 (跑步)",
		"周四: (跑步)", "周五: (跑步)", "周六: (跑步)", "周日: (跑步)")
	// The first of a new month: the months' are this month's.
	on("2026-12-01")
	equal(t, "on the 1st", dues(), "2026-12-01", "2026-12-02", "2026-12-25", "2026-12-30", "2026-11-24", "2026-11-25")
	// Long after: each of the time it is in, February without a 30th.
	on("2027-02-10")
	equal(t, "in February", dues(), "2027-02-10", "2027-02-10", "2027-02-25", "2027-02-28", "2026-11-24", "2026-11-25")

	// What moved is saved.
	again := newAt(store.Open(dir), func() time.Time { return now })
	if k := again.Task(weekly.ID); k == nil || k.Due != "2027-02-10" {
		t.Errorf("after a restart the weekly task is %+v", k)
	}
	// A repeating task given a day of a time gone by is of this one too.
	weekly.Due = "2027-01-06"
	d.Changed(weekly)
	if weekly.Due != "2027-02-10" {
		t.Errorf("moved to a Wednesday of January, the weekly task is due %s", weekly.Due)
	}
}

// The views by day show a repeating task on the days ahead it will come
// back on, apart from the tasks to do; a view that lists tasks does not.
func TestComing(t *testing.T) {
	d := newTest("")
	d.Add(Task{Title: "同步上游", Due: "2026-11-25", Repeat: RepeatWeekly})
	d.Add(Task{Title: "跑步", Due: "2026-11-25", Repeat: RepeatDaily})
	d.Add(Task{Title: "交房租", Due: "2026-11-05", Repeat: RepeatMonthly})
	d.Add(Task{Title: "大扫除", Due: "2026-12-05"})
	d.SetDone(d.Add(Task{Title: "已经做完的", Due: "2026-11-25", Repeat: RepeatNone}), true)

	equal(t, "this week", titles(d, viewWeek), "周三: 同步上游 跑步 已经做完的", "周四: (跑步)", "周五: (跑步)", "周六: (跑步)", "周日: (跑步)")
	equal(t, "next week", titlesOf(d, viewWeek, "2026-11-30"), "周一: (跑步)", "周二: (跑步)", "周三: (同步上游) (跑步)",
		"周四: (跑步)", "周五: (跑步)", "周六: 大扫除 (跑步) (交房租)", "周日: (跑步)")
	equal(t, "last week", titlesOf(d, viewWeek, "2026-11-16"))
	equal(t, "next 7 days", titles(d, viewNext7), "已过期: 交房租", "今天: 同步上游 跑步", "明天: (跑步)", "周五: (跑步)",
		"周六: (跑步)", "周日: (跑步)", "周一: (跑步)", "周二: (跑步)")
	equal(t, "today", titles(d, viewToday), "已过期: 交房租", "今天: 同步上游 跑步", "今天完成的: 已经做完的")
	equal(t, "all", titles(d, viewAll), "已过期: 交房租", "今天: 同步上游 跑步", "以后: 大扫除")
	// What will come back is not counted as to do.
	if n := d.openIn(viewWeek); n != 2 {
		t.Errorf("the week has %d tasks to do, want 2", n)
	}

	var got []string
	for _, o := range d.Coming("2026-12-01", "2026-12-09") {
		if o.Task.Title != "跑步" {
			got = append(got, o.Date+" "+o.Task.Title)
		}
	}
	equal(t, "coming in December", got, "2026-12-02 同步上游", "2026-12-05 交房租", "2026-12-09 同步上游")
	// Far ahead: every day's, the Wednesday's and the 5th's.
	if far := d.Coming("2030-01-01", "2030-01-07"); len(far) != 9 || far[0].Date != "2030-01-01" || far[1].Date != "2030-01-02" || far[1].Task.Title != "同步上游" {
		t.Errorf("coming in 2030: %+v", far)
	}
	if n := len(d.Coming("2026-11-01", "2026-11-25")); n != 0 {
		t.Errorf("%d come back on or before the day they are due", n)
	}
	if d.Coming("下周", "2026-12-09") != nil || d.Coming("2026-12-01", "") != nil {
		t.Errorf("days that are none have tasks coming")
	}
}

// Tasks are there again after a restart, those of a list of old too: lists
// and priorities are gone, what had them is not, and keeps them as saved.
func TestSaving(t *testing.T) {
	dir := t.TempDir()
	d := newTest(dir)
	d.data.Lists = []*List{{ID: 7, Name: "生活"}}
	d.Add(Task{Title: "买菜", List: 7, Note: "鸡蛋", Priority: 2})

	again := newTest(dir)
	if len(again.Tasks()) != 1 || again.Tasks()[0].Note != "鸡蛋" {
		t.Fatalf("after a restart: tasks %+v", again.Tasks())
	}
	equal(t, "all, with a task of a list of old", titles(again, viewAll), "今天:", "没有日期: 买菜")
	if k := again.Tasks()[0]; k.List != 7 || k.Priority != 2 || len(again.data.Lists) != 1 {
		t.Errorf("the list and priority of old were not kept: task %+v, lists %+v", k, again.data.Lists)
	}
}

// The weekly checklist's data is taken over once.
func TestMigrateWeekly(t *testing.T) {
	dir := t.TempDir()
	s := store.Open(dir)
	type m = map[string]any
	err := s.Save("weekly", m{
		"weeks": m{
			"2026-W47": []m{{"text": "上周做完的", "done": true, "day": 0}},
			"2026-W48": []m{{"text": "周五的事", "day": 4}, {"text": "本周目标", "day": -1}},
		},
		"rules": []m{
			{"id": 7, "text": "写周报", "day": 0, "since": "2026-W46"},
			{"id": 8, "text": "健身", "day": 5, "since": "2026-W46"},
			{"id": 9, "text": "早就停了的", "day": 1, "since": "2026-W40", "until": "2026-W45"},
		},
		"ruleDone": m{"2026-W47": []int{7}, "2026-W48": []int{7}},
	})
	if err != nil {
		t.Fatal(err)
	}

	d := newAt(s, wednesday)
	var got []string
	for _, k := range d.Tasks() {
		s := k.Title + " " + k.Due
		if k.Done {
			s += " ✓"
		}
		if k.Repeat != RepeatNone {
			s += " " + k.Repeat
		}
		got = append(got, s)
	}
	equal(t, "migrated", got,
		"上周做完的 2026-11-16 ✓",
		"周五的事 2026-11-27",
		"本周目标 2026-11-29",
		"写周报 2026-11-16 ✓", // checked off last week
		"写周报 2026-11-23 ✓", // and this week: so due next Monday
		"写周报 2026-11-30 weekly",
		"健身 2026-11-28 weekly",
	)

	// A second start reads todo.json and migrates nothing again.
	if n := len(newAt(s, wednesday).Tasks()); n != len(got) {
		t.Errorf("%d tasks after a second start, want %d", n, len(got))
	}
}
