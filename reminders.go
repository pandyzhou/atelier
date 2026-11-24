package main

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"

	"github.com/pandyzhou/atelier/internal/features/timetable"
)

// The app reminds of a course a while before it starts, with a notification
// of the system's: while it runs, also with its window shut to the tray.

// defaultLead is how many minutes before a course its reminder comes, until
// the settings say another.
const defaultLead = 20

// leads are the minutes before a course there are to choose from.
var leads = []int{5, 10, 15, 20, 30}

// reminders is what the app keeps to remind by.
type reminders struct {
	// last is when the courses were last looked over: the reminders that
	// fall after it and not after now are shown.
	last time.Time
	// notify shows a notification, and now tells the time: the system's,
	// but in tests.
	notify func(id, title, body string)
	now    func() time.Time
}

// lead is how long before a course its reminder comes.
func (a *app) lead() time.Duration {
	minutes := a.settings.RemindLead
	if minutes <= 0 {
		minutes = defaultLead
	}
	return time.Duration(minutes) * time.Minute
}

// table returns the timetable among the features, nil if there is none.
func (a *app) table() *timetable.Timetable {
	for _, f := range a.features {
		if t, ok := f.(*timetable.Timetable); ok {
			return t
		}
	}
	return nil
}

// remind shows the reminders of the courses that have come due since it
// last looked: those that start a lead from now, or sooner when the app
// was not running, or asleep, as their reminder fell.
func (a *app) remind() {
	r := &a.reminders
	now := r.now()
	lead := a.lead()
	// What fell due longer ago than a lead is of a course that has begun.
	after := r.last
	if after.IsZero() || now.Sub(after) > lead {
		after = now.Add(-lead)
	}
	r.last = now
	table := a.table()
	if a.settings.RemindOff || table == nil || r.notify == nil {
		return
	}
	for _, k := range table.Due(after, now, lead) {
		start, _ := table.Starts(k, now)
		minutes := max(1, int(start.Sub(now).Round(time.Minute)/time.Minute))
		body := table.When(k)
		for _, more := range []string{k.Room, k.Teacher} {
			if more != "" {
				body += " · " + more
			}
		}
		// A course has one reminder a day: shown again, it takes the
		// place of itself.
		r.notify(fmt.Sprintf("course-%d-%s", k.ID, now.Format("20060102")), fmt.Sprintf("%d 分钟后上课：%s", minutes, k.Name), body)
	}
}

// startReminders has the courses looked over as time goes by, from now on:
// often enough for a reminder to come within the minute it is due.
func (a *app) startReminders() {
	a.reminders.now = time.Now
	a.reminders.notify = func(id, title, body string) {
		if !mygo.NotificationsSupported() {
			return
		}
		n := mygo.NewNotification(mygo.NotificationOptions{ID: id, Title: title, Body: body})
		// A click on it shows the timetable.
		n.OnClick(func() {
			a.onUI(func() {
				if t := a.table(); t != nil {
					a.selected = t.ID()
				}
				a.showWindow()
			})
		})
		go func() {
			if err := n.Show(); err != nil {
				log.Printf("reminder: %v", err)
			}
		}()
	}
	a.onUI(a.remind)
	go func() {
		for range time.Tick(20 * time.Second) {
			a.onUI(a.remind)
		}
	}()
}

// reminderSettings are whether the app reminds of courses, and how long
// before them.
func (a *app) reminderSettings(c *ui.Context, row func(name, note string, width float32, control func())) {
	note := "快上课时在屏幕角上弹出系统通知。应用要开着，关到托盘里也算"
	if t := a.table(); t != nil && !t.Timed() {
		note += "。课表里还没有填每节的开始时间，填了才会提醒"
	}
	row("上课提醒", note, 0, func() {
		on := !a.settings.RemindOff
		if mallow.Switch(c.Key("remind"), &on, "").Label("开启上课提醒").Changed() {
			a.settings.RemindOff = !on
			a.saveSettings()
		}
	})
	if a.settings.RemindOff {
		return
	}
	row("提前几分钟", "上课前多久提醒", 200, func() {
		options := make([]mallow.Option, len(leads))
		for i, m := range leads {
			options[i] = mallow.Option{Value: strconv.Itoa(m), Label: strconv.Itoa(m)}
		}
		lead := strconv.Itoa(int(a.lead() / time.Minute))
		if mallow.Segmented(c, "提前几分钟", &lead, options) {
			a.settings.RemindLead, _ = strconv.Atoi(lead)
			a.saveSettings()
		}
	})
}
