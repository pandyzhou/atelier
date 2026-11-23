package weekly

import (
	"testing"
	"time"

	"github.com/pandyzhou/atelier/internal/store"
)

// newAt returns a checklist in dir ("" keeps nothing) whose today is a
// Wednesday, showing that week.
func newAt(t *testing.T, dir string) *Weekly {
	t.Helper()
	w := New(store.Open(dir))
	w.now = func() time.Time { return time.Date(2026, 11, 25, 10, 0, 0, 0, time.Local) }
	w.monday = mondayOf(w.now())
	return w
}

// texts returns the texts of what the week shown lists on day, marking the
// weekly ones with ↻ and the done ones with ✓.
func texts(w *Weekly, day int) []string {
	var out []string
	for _, e := range w.Entries(day) {
		s := e.Text
		if e.Repeats() {
			s += "↻"
		}
		if e.Done {
			s += "✓"
		}
		out = append(out, s)
	}
	return out
}

func equal(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %q, want %q", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: %q, want %q", what, got, want)
		}
	}
}

func (w *Weekly) shift(weeks int) { w.monday = w.monday.AddDate(0, 0, 7*weeks) }

func TestRepeatComesBackEveryWeek(t *testing.T) {
	w := newAt(t, "")
	w.Add(0, "写周报")
	w.SetDone(w.Entries(0)[0], true)
	w.SetRepeats(w.Entries(0)[0], true)
	equal(t, "this week", texts(w, 0), "写周报↻✓")
	if len(w.Items()) != 0 {
		t.Errorf("the item stayed beside its rule: %v", w.Items())
	}

	w.shift(1)
	equal(t, "next week", texts(w, 0), "写周报↻")
	w.SetDone(w.Entries(0)[0], true)
	w.shift(5)
	equal(t, "six weeks on", texts(w, 0), "写周报↻")

	w.shift(-7)
	equal(t, "the week before it was set", texts(w, 0))
}

func TestStopRepeatingKeepsThisWeek(t *testing.T) {
	w := newAt(t, "")
	w.Add(2, "健身")
	w.SetRepeats(w.Entries(2)[0], true)
	w.shift(2)
	w.SetDone(w.Entries(2)[0], true)
	w.SetRepeats(w.Entries(2)[0], false)
	equal(t, "the week it stopped", texts(w, 2), "健身✓")

	w.shift(1)
	equal(t, "the week after", texts(w, 2))
	w.shift(-2)
	equal(t, "a week before it stopped", texts(w, 2), "健身↻")
}

func TestRemoveRule(t *testing.T) {
	w := newAt(t, "")
	w.Add(5, "整理房间")
	w.SetRepeats(w.Entries(5)[0], true)
	w.shift(3)
	w.Remove(w.Entries(5)[0])
	equal(t, "the week it was removed", texts(w, 5))
	w.shift(1)
	equal(t, "after", texts(w, 5))
	w.shift(-2)
	equal(t, "before", texts(w, 5), "整理房间↻")

	// Removed in the week it was set on, a rule goes altogether.
	w.Add(1, "读书")
	w.SetRepeats(w.Entries(1)[0], true)
	w.Remove(w.Entries(1)[0])
	for _, r := range w.Rules() {
		if r.Text == "读书" {
			t.Errorf("a rule removed in its first week stayed: %+v", r)
		}
	}
}

func TestWeekList(t *testing.T) {
	w := newAt(t, "")
	w.Add(WeekList, "读完一本书")
	w.Add(0, "写周报")
	equal(t, "week list", texts(w, WeekList), "读完一本书")
	equal(t, "Monday", texts(w, 0), "写周报")

	w.SetRepeats(w.Entries(WeekList)[0], true)
	w.shift(1)
	equal(t, "next week's list", texts(w, WeekList), "读完一本书↻")
	equal(t, "next Monday", texts(w, 0))
}

func TestRulesAreSaved(t *testing.T) {
	dir := t.TempDir()
	w := newAt(t, dir)
	w.Add(3, "复习英语")
	w.SetRepeats(w.Entries(3)[0], true)
	w.SetDone(w.Entries(3)[0], true)

	again := newAt(t, dir)
	equal(t, "after a restart", texts(again, 3), "复习英语↻✓")
	again.shift(1)
	equal(t, "the week after, after a restart", texts(again, 3), "复习英语↻")
}
