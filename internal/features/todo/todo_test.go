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

// titles returns what a view lists, as "heading: task, task" for each group.
func titles(t *Todo, view string) []string {
	var out []string
	for _, g := range t.groups(view, mondayOf(t.Today())) {
		s := g.title + ":"
		for _, k := range g.tasks {
			s += " " + k.Title
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
	d.Add(Task{Title: "要紧的", Due: "2026-11-25", Priority: PriorityHigh})
	d.Add(Task{Title: "周五的", Due: "2026-11-27"})
	d.Add(Task{Title: "下个月的", Due: "2026-12-20"})
	d.Add(Task{Title: "没日期的"})
	d.SetDone(d.Add(Task{Title: "做完的", Due: "2026-11-25"}), true)

	equal(t, "today", titles(d, viewToday), "已过期: 过期的", "今天: 要紧的 今天的", "今天完成的: 做完的")
	equal(t, "next 7 days", titles(d, viewNext7), "已过期: 过期的", "今天: 要紧的 今天的", "周五: 周五的")
	equal(t, "week", titles(d, viewWeek), "周二: 过期的", "周三: 要紧的 今天的 做完的", "周五: 周五的")
	equal(t, "all", titles(d, viewAll),
		"已过期: 过期的", "今天: 要紧的 今天的", "最近 7 天: 周五的", "以后: 下个月的", "没有日期: 没日期的")
	equal(t, "done", titles(d, viewDone), "今天完成: 做完的")
}

func TestRepeat(t *testing.T) {
	for _, c := range []struct{ due, repeat, want string }{
		{"2026-11-25", RepeatDaily, "2026-11-26"},
		{"2026-11-25", RepeatWeekly, "2026-12-02"},
		{"2026-11-25", RepeatMonthly, "2026-12-25"},
		{"2026-11-27", RepeatWeekly, "2026-12-04"}, // done early: the time after the one due
		{"2026-11-09", RepeatWeekly, "2026-11-30"}, // long overdue: the next time from today on
		{"2026-11-20", RepeatDaily, "2026-11-25"},  // overdue: today's is still to do
	} {
		d := newTest("")
		k := d.Add(Task{Title: "跑步", Due: c.due, Repeat: c.repeat})
		d.SetDone(k, true)
		if k.Done || k.Due != c.want {
			t.Errorf("%s due %s, checked off on the 25th: now due %s, done %v; want %s", c.repeat, c.due, k.Due, k.Done, c.want)
		}
		if n := len(d.Tasks()); n != 2 || !d.Tasks()[1].Done || d.Tasks()[1].Repeat != RepeatNone || d.Tasks()[1].Due != c.due {
			t.Errorf("%s: no done copy left: %+v", c.repeat, d.Tasks())
		}
	}

	// A repeat needs a day to start from.
	d := newTest("")
	if k := d.Add(Task{Title: "浇花", Repeat: RepeatWeekly}); k.Due != "2026-11-25" {
		t.Errorf("a repeating task added without a date is due %q", k.Due)
	}
}

// Tasks are there again after a restart, those of a list of old too: lists
// are gone, what was in them is not.
func TestSaving(t *testing.T) {
	dir := t.TempDir()
	d := newTest(dir)
	d.data.Lists = []*List{{ID: 7, Name: "生活"}}
	d.Add(Task{Title: "买菜", List: 7, Note: "鸡蛋", Priority: PriorityMid})

	again := newTest(dir)
	if len(again.Tasks()) != 1 || again.Tasks()[0].Note != "鸡蛋" {
		t.Fatalf("after a restart: tasks %+v", again.Tasks())
	}
	equal(t, "all, with a task of a list of old", titles(again, viewAll), "今天:", "没有日期: 买菜")
	if k := again.Tasks()[0]; k.List != 7 || len(again.data.Lists) != 1 {
		t.Errorf("the list of old was not kept: task %+v, lists %+v", k, again.data.Lists)
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
