package todo

import (
	"fmt"
	"log"
	"slices"
	"time"
)

// The weekly checklist this feature grew out of saved "weekly.json": items
// by ISO week ("2026-W47") and day (0 for Monday to 6, -1 for the week as a
// whole), and rules, things to do every week from one week up to another.

type weeklyItem struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
	Day  int    `json:"day"`
}

type weeklyRule struct {
	ID    int    `json:"id"`
	Text  string `json:"text"`
	Day   int    `json:"day"`
	Since string `json:"since"`
	Until string `json:"until"`
}

type weeklyData struct {
	Weeks    map[string][]*weeklyItem `json:"weeks"`
	Rules    []*weeklyRule            `json:"rules"`
	RuleDone map[string][]int         `json:"ruleDone"`
}

// migrateWeekly takes over the weekly checklist's data, once; weekly.json
// itself is left as it is. An item becomes a task due on its day, one of
// the week as a whole due on that week's Sunday. A rule becomes a task
// repeating weekly, due this week if it is not checked off yet and next
// week if it is; the weeks it was checked off in become done tasks.
func (t *Todo) migrateWeekly() {
	var w weeklyData
	if err := t.store.Load("weekly", &w); err != nil {
		log.Printf("todo: reading the weekly checklist: %v", err)
		return
	}
	today := t.Today()
	thisWeek := weekKey(today)
	add := func(k Task) {
		t.data.Next++
		k.ID = t.data.Next
		t.data.Tasks = append(t.data.Tasks, &k)
	}
	done := func(title, due string) {
		add(Task{Title: title, Due: due, Done: true, DoneAt: min(due, today)})
	}
	weeks := make([]string, 0, len(w.Weeks))
	for k := range w.Weeks {
		weeks = append(weeks, k)
	}
	slices.Sort(weeks)
	for _, wk := range weeks {
		for _, it := range w.Weeks[wk] {
			if due := weekDay(wk, it.Day); it.Done {
				done(it.Text, due)
			} else {
				add(Task{Title: it.Text, Due: due})
			}
		}
	}
	doneWeeks := make([]string, 0, len(w.RuleDone))
	for k := range w.RuleDone {
		doneWeeks = append(doneWeeks, k)
	}
	slices.Sort(doneWeeks)
	for _, r := range w.Rules {
		for _, wk := range doneWeeks {
			if wk != thisWeek && slices.Contains(w.RuleDone[wk], r.ID) {
				done(r.Text, weekDay(wk, r.Day))
			}
		}
		if r.Until != "" && r.Until <= thisWeek {
			continue // ended
		}
		due := weekDay(max(thisWeek, r.Since), r.Day)
		if slices.Contains(w.RuleDone[thisWeek], r.ID) {
			done(r.Text, due)
			due = addDays(due, 7)
		}
		add(Task{Title: r.Text, Due: due, Repeat: RepeatWeekly})
	}
	t.data.Migrated = true
	if len(t.data.Tasks) > 0 {
		t.save()
	}
}

// weekKey names the ISO week of a date, as "2026-W47".
func weekKey(date string) string {
	d, _ := time.Parse(dateLayout, date)
	y, w := d.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// weekDay is the date of a day (0 for Monday to 6, or -1, which is taken
// as Sunday) of an ISO week named as weekKey does.
func weekDay(week string, day int) string {
	var y, w int
	if _, err := fmt.Sscanf(week, "%d-W%d", &y, &w); err != nil {
		return ""
	}
	if day < 0 || day > 6 {
		day = 6
	}
	// January 4th is always in week 1.
	jan4 := time.Date(y, 1, 4, 0, 0, 0, 0, time.UTC)
	monday := jan4.AddDate(0, 0, -((int(jan4.Weekday())+6)%7)+7*(w-1))
	return monday.AddDate(0, 0, day).Format(dateLayout)
}
