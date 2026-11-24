// Package home is the app's first page: a field to write to the assistant
// in, which answers about the tasks and the timetable and adds to them, by
// the model the user chose in the settings.
package home

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pandyzhou/atelier/internal/features/timetable"
	"github.com/pandyzhou/atelier/internal/features/todo"
	"github.com/pandyzhou/atelier/internal/llm"
)

const dateLayout = "2006-01-02"

var dayNames = [7]string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}

// Options are what the page needs of the app around it.
type Options struct {
	// Model returns the model to ask, and whether one is set.
	Model func() (llm.Config, bool)
	// Complete asks it, giving onText the answer's text as it comes, off
	// the UI's thread: llm.Stream, but in tests.
	Complete func(ctx context.Context, c llm.Config, r llm.Request, onText func(string)) (llm.Message, error)
	// Background runs work off the UI's thread, then what it returns on it.
	Background func(work func() func())
	// OnUI runs fn on the UI's thread, and shows the window anew.
	OnUI func(fn func())
	// OpenSettings shows the models' settings.
	OpenSettings func()
}

// Home is the feature.
type Home struct {
	Options
	tasks *todo.Todo
	table *timetable.Timetable
	now   func() time.Time
	ui    viewState
}

// New returns the page of an assistant for tasks and a timetable.
func New(tasks *todo.Todo, table *timetable.Timetable, o Options) *Home {
	return &Home{Options: o, tasks: tasks, table: table, now: time.Now}
}

func (h *Home) ID() string    { return "home" }
func (h *Home) Title() string { return "首页" }

// The kinds of entry a conversation shows.
const (
	said    = iota // what the user wrote
	answer         // what the model answered
	action         // something done for the user, as a task added
	problem        // what went wrong
)

// entry is a line of the conversation, as the page shows it.
type entry struct {
	kind int
	text string
}

// maxSteps is how many times the model is asked for one message of the
// user's: once, and again after each round of tools it calls.
const maxSteps = 6

// Send takes a message of the user's to the model, and its answer, and what
// it had done, into the conversation.
func (h *Home) Send(text string) {
	text = strings.TrimSpace(text)
	if text == "" || h.ui.busy {
		return
	}
	h.ui.entries = append(h.ui.entries, entry{said, text})
	h.ui.history = append(h.ui.history, llm.Message{Role: llm.User, Text: text})
	h.ui.busy, h.ui.follow = true, true
	h.step(0)
}

// Reset starts a new conversation; an answer still on its way is dropped.
func (h *Home) Reset() {
	h.ui = viewState{run: h.ui.run + 1, growing: -1}
}

// fail ends the turn with what went wrong.
func (h *Home) fail(message string) {
	h.ui.entries = append(h.ui.entries, entry{problem, message})
	h.ui.busy, h.ui.follow, h.ui.growing = false, true, -1
}

// step asks the model to go on with the conversation, does what it calls
// for, and asks again until it has only something to say.
func (h *Home) step(n int) {
	cfg, ok := h.Model()
	if !ok {
		h.fail("还没有可用的模型，请先到设置里添加。")
		return
	}
	req := llm.Request{System: h.system(), Messages: append([]llm.Message(nil), h.ui.history...), Tools: tools}
	run := h.ui.run
	h.ui.growing = -1
	// The text that came and does not show yet: it comes off the UI's
	// thread, piece by piece, and is shown on it, all there is at once.
	var mu sync.Mutex
	var came string
	waiting := false
	onText := func(piece string) {
		mu.Lock()
		came += piece
		if waiting {
			mu.Unlock()
			return
		}
		waiting = true
		mu.Unlock()
		h.OnUI(func() {
			mu.Lock()
			text := came
			came, waiting = "", false
			mu.Unlock()
			if run == h.ui.run {
				h.grow(text)
			}
		})
	}
	h.Background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		msg, err := h.Complete(ctx, cfg, req, onText)
		return func() {
			if run != h.ui.run {
				return // a new conversation was started meanwhile
			}
			if err != nil {
				h.fail(err.Error())
				return
			}
			h.ui.history = append(h.ui.history, msg)
			h.ui.follow = true
			// The whole answer, in place of what of it showed as it came.
			if text := strings.TrimSpace(msg.Text); text == "" {
			} else if g := h.ui.growing; g >= 0 && g < len(h.ui.entries) {
				h.ui.entries[g].text = text
			} else {
				h.ui.entries = append(h.ui.entries, entry{answer, text})
			}
			h.ui.growing = -1
			if len(msg.Calls) == 0 {
				h.ui.busy = false
				return
			}
			for _, call := range msg.Calls {
				h.ui.history = append(h.ui.history, llm.Message{Role: llm.Result, CallID: call.ID, Text: h.call(call)})
			}
			if n+1 >= maxSteps {
				h.fail("这件事步骤太多，先停在这里了。")
				return
			}
			h.step(n + 1)
		}
	})
}

// grow shows more of the answer that is coming: at the end of the one that
// grows, or as a new one.
func (h *Home) grow(text string) {
	if text == "" {
		return
	}
	if g := h.ui.growing; g >= 0 && g < len(h.ui.entries) {
		h.ui.entries[g].text += text
	} else {
		h.ui.entries = append(h.ui.entries, entry{answer, text})
		h.ui.growing = len(h.ui.entries) - 1
	}
	h.ui.follow = true
}

// tools are what the model can have done.
var tools = []llm.Tool{
	{
		Name:        "add_task",
		Description: "添加一条待办任务。",
		Parameters: schema([]string{"title"}, map[string]any{
			"title":  prop("string", "任务标题"),
			"due":    prop("string", "到期日期，YYYY-MM-DD；没有日期就不填"),
			"note":   prop("string", "备注，可不填"),
			"repeat": map[string]any{"type": "string", "enum": []string{"daily", "weekly", "monthly"}, "description": "重复：每天、每周或每月；不重复就不填"},
		}),
	},
	{
		Name:        "add_course",
		Description: "往课表里添加一门课。课表每周相同。和已有课程冲突时会失败并说明原因。",
		Parameters: schema([]string{"name", "weekday", "from_period", "to_period"}, map[string]any{
			"name":        prop("string", "课程名"),
			"weekday":     prop("integer", "星期几，1 是周一，7 是周日"),
			"from_period": prop("integer", "从第几节开始"),
			"to_period":   prop("integer", "到第几节结束，只上一节时和 from_period 相同"),
			"room":        prop("string", "上课地点，可不填"),
			"teacher":     prop("string", "任课老师，可不填"),
		}),
	},
	{
		Name:        "list_tasks",
		Description: "按到期日期查询任务，包括已完成的。提示里已经列出未完成的任务，只有要查已完成的或核对时才需要用。",
		Parameters: schema([]string{"from", "to"}, map[string]any{
			"from": prop("string", "起始日期，YYYY-MM-DD"),
			"to":   prop("string", "结束日期（含），YYYY-MM-DD"),
		}),
	},
}

func prop(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

func schema(required []string, properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

// call does what the model called for and returns what to tell it, noting
// in the conversation what was done for the user.
func (h *Home) call(c llm.Call) string {
	did := func(text string) { h.ui.entries = append(h.ui.entries, entry{action, text}) }
	switch c.Name {
	case "add_task":
		var in struct {
			Title, Due, Note, Repeat string
		}
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return "参数不对：" + err.Error()
		}
		if in.Due != "" {
			if _, err := time.Parse(dateLayout, in.Due); err != nil {
				return "due 要写成 YYYY-MM-DD"
			}
		}
		switch in.Repeat {
		case todo.RepeatNone, todo.RepeatDaily, todo.RepeatWeekly, todo.RepeatMonthly:
		default:
			return "repeat 只能是 daily、weekly 或 monthly"
		}
		k := h.tasks.Add(todo.Task{Title: in.Title, Due: in.Due, Note: strings.TrimSpace(in.Note), Repeat: in.Repeat})
		if k == nil {
			return "没有添加：标题是空的"
		}
		note := "已添加任务：" + k.Title
		if k.Due != "" {
			note += " · " + h.dayOf(k.Due)
		}
		did(note)
		return fmt.Sprintf("已添加，id %d，到期 %s", k.ID, orNone(k.Due))
	case "add_course":
		var in struct {
			Name, Room, Teacher string
			Weekday             int
			From                int `json:"from_period"`
			To                  int `json:"to_period"`
		}
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return "参数不对：" + err.Error()
		}
		n := len(h.table.Periods())
		if in.Weekday < 1 || in.Weekday > 7 || in.From < 1 || in.To < in.From || in.To > n {
			return fmt.Sprintf("没有添加：weekday 要在 1 到 7 之间，节次要在 1 到 %d 之间，且 to_period 不小于 from_period", n)
		}
		k, err := h.table.Put(timetable.Course{Name: in.Name, Room: in.Room, Teacher: in.Teacher, Day: in.Weekday - 1, From: in.From, To: in.To})
		if err != nil {
			return "没有添加：" + err.Error()
		}
		did(fmt.Sprintf("已添加课程：%s · %s第 %s 节", k.Name, dayNames[k.Day], span(k.From, k.To)))
		return "已添加"
	case "list_tasks":
		var in struct{ From, To string }
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return "参数不对：" + err.Error()
		}
		var b strings.Builder
		for _, k := range h.tasks.Tasks() {
			if k.Due == "" || k.Due < in.From || k.Due > in.To {
				continue
			}
			b.WriteString(h.taskLine(k))
		}
		if b.Len() == 0 {
			return "这段时间没有任务"
		}
		return b.String()
	}
	return "没有这个工具：" + c.Name
}

func orNone(s string) string {
	if s == "" {
		return "无"
	}
	return s
}

func span(from, to int) string {
	if from == to {
		return fmt.Sprint(from)
	}
	return fmt.Sprintf("%d–%d", from, to)
}

// weekday names the day of the week of a date.
func weekday(date string) string {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return ""
	}
	return dayNames[(int(d.Weekday())+6)%7]
}

// dayOf writes a date for the user: 今天, 明天, or the month, day and weekday.
func (h *Home) dayOf(date string) string {
	d, err := time.Parse(dateLayout, date)
	if err != nil {
		return date
	}
	now := h.now()
	switch date {
	case now.Format(dateLayout):
		return "今天"
	case now.AddDate(0, 0, 1).Format(dateLayout):
		return "明天"
	}
	return fmt.Sprintf("%d月%d日 %s", int(d.Month()), d.Day(), weekday(date))
}

// taskLine writes a task for the model, on a line.
func (h *Home) taskLine(k *todo.Task) string {
	var b strings.Builder
	if k.Due == "" {
		b.WriteString("无日期")
	} else {
		b.WriteString(k.Due + " " + weekday(k.Due))
	}
	b.WriteString(" " + k.Title)
	switch k.Repeat {
	case todo.RepeatDaily:
		b.WriteString("（每天重复）")
	case todo.RepeatWeekly:
		b.WriteString("（每周重复）")
	case todo.RepeatMonthly:
		b.WriteString("（每月重复）")
	}
	if k.Done {
		b.WriteString("（已完成）")
	}
	if note := strings.Join(strings.Fields(k.Note), " "); note != "" {
		b.WriteString(" 备注：" + note)
	}
	b.WriteString("\n")
	return b.String()
}

// listed is how many of the tasks still to do the model is told outright.
const listed = 200

// system tells the model what it is here for, what day it is, and what the
// user has: the timetable and the tasks still to do, as they are now.
func (h *Home) system() string {
	now := h.now()
	var b strings.Builder
	b.WriteString("你是桌面应用 Atelier 里的助手，帮用户查看和安排待办任务与课表。\n")
	fmt.Fprintf(&b, "现在是 %s %s %s。\n", now.Format(dateLayout), dayNames[(int(now.Weekday())+6)%7], now.Format("15:04"))
	b.WriteString(`
规则：
- 用简短的中文回答，只写纯文本，不要用 Markdown 符号。
- 用户说“明天”“下周三”这类相对日期时，换算成具体日期再查或再添加。
- 问某天的安排时，把那天的课和到期的任务都说出来。
- 添加前信息不全（比如课程没说星期几、第几节）就先问清楚，不要猜。
- 添加成功后用一句话确认。工具说失败时，把原因告诉用户。
- 你只能查看和添加。用户要修改、完成或删除时，请他到“待办”或“课表”页面操作。
`)
	periods := h.table.Periods()
	fmt.Fprintf(&b, "\n课表（每周相同，每天 %d 节）：\n", len(periods))
	if len(h.table.Courses()) == 0 {
		b.WriteString("还没有课程。\n")
	}
	for _, k := range h.table.Courses() {
		fmt.Fprintf(&b, "%s 第 %s 节 %s", dayNames[k.Day], span(k.From, k.To), k.Name)
		if k.From >= 1 && k.To <= len(periods) && periods[k.From-1].Start != "" && periods[k.To-1].End != "" {
			fmt.Fprintf(&b, " %s–%s", periods[k.From-1].Start, periods[k.To-1].End)
		}
		if k.Room != "" {
			b.WriteString(" 地点：" + k.Room)
		}
		if k.Teacher != "" {
			b.WriteString(" 老师：" + k.Teacher)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n未完成的任务：\n")
	n := 0
	for _, k := range h.tasks.Tasks() {
		if k.Done {
			continue
		}
		if n++; n > listed {
			b.WriteString("（还有更多，用 list_tasks 按日期查）\n")
			break
		}
		b.WriteString(h.taskLine(k))
	}
	if n == 0 {
		b.WriteString("没有。\n")
	}
	b.WriteString("\n已完成的任务没有列在这里，需要时用 list_tasks 查。\n")
	return b.String()
}
