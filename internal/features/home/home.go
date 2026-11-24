// Package home is the app's first page: a field to write to the assistant
// in, which answers about the tasks and the timetable and adds to them, by
// the model the user chose. The conversations are kept, to go back to.
package home

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pandyzhou/atelier/internal/features/timetable"
	"github.com/pandyzhou/atelier/internal/features/todo"
	"github.com/pandyzhou/atelier/internal/llm"
	"github.com/pandyzhou/atelier/internal/store"
)

const dateLayout = "2006-01-02"

var dayNames = [7]string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}

// Model is a model to choose: one of a provider's, in the settings.
type Model struct {
	Provider   string // the provider's name
	ProviderID int
	Name       string
	Current    bool // the one asked now
}

// Options are what the page needs of the app around it.
type Options struct {
	// Model returns the model to ask, and whether one is set.
	Model func() (llm.Config, bool)
	// Models lists the models there are to choose from, and Choose makes
	// one the model asked from now on.
	Models func() []Model
	Choose func(Model)
	// Complete asks the model, giving onText the answer's text as it
	// comes, off the UI's thread: llm.Stream, but in tests.
	Complete func(ctx context.Context, c llm.Config, r llm.Request, onText func(string)) (llm.Message, error)
	// Background runs work off the UI's thread, then what it returns on it.
	Background func(work func() func())
	// OnUI runs fn on the UI's thread, and shows the window anew.
	OnUI func(fn func())
	// OpenSettings shows the models' settings.
	OpenSettings func()
	// PickFiles has the user choose files to attach, off the UI's thread,
	// and returns their paths: none when they chose none.
	PickFiles func() []string
}

// Home is the feature.
type Home struct {
	Options
	store *store.Store
	data  data
	tasks *todo.Todo
	table *timetable.Timetable
	now   func() time.Time
	ui    viewState
	// blobs are the pictures kept for this run, where nothing is saved.
	blobs map[string][]byte
}

// The kinds of entry a conversation shows.
const (
	said    = iota // what the user wrote
	answer         // what the model answered
	action         // something done for the user, as a task added
	problem        // what went wrong
)

// entry is a line of a conversation, as the page shows it.
type entry struct {
	Kind int    `json:"kind"`
	Text string `json:"text"`
	// Model is the model an answer is of.
	Model string `json:"model,omitempty"`
	// Files are what the user's message came with.
	Files []attached `json:"files,omitempty"`
}

// conversation is a conversation that was had, as it is saved.
type conversation struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Updated string `json:"updated"` // when it last went on, RFC 3339
	// Entries are what it shows, and History what the model is sent of it.
	Entries []entry       `json:"entries"`
	History []llm.Message `json:"history"`
}

// data is what is saved: the conversations, the newest first.
type data struct {
	Next          int             `json:"next"`
	NextFile      int             `json:"nextFile,omitempty"` // counts the pictures kept, to name them
	Conversations []*conversation `json:"conversations"`
}

// kept is how many conversations are saved at most: the oldest go.
const kept = 100

// New returns the page of an assistant for tasks and a timetable, with
// the conversations saved in s.
func New(s *store.Store, tasks *todo.Todo, table *timetable.Timetable, o Options) *Home {
	h := &Home{Options: o, store: s, tasks: tasks, table: table, now: time.Now, ui: fresh()}
	if err := s.Load(h.ID(), &h.data); err != nil {
		log.Printf("home: load: %v", err)
	}
	return h
}

func (h *Home) ID() string    { return "home" }
func (h *Home) Title() string { return "首页" }

// maxSteps is how many times the model is asked for one message of the
// user's: once, and again after each round of tools it calls.
const maxSteps = 6

// Send takes a message of the user's to the model, with the files attached
// to it, and its answer, and what it had done, into the conversation.
func (h *Home) Send(text string) {
	text = strings.TrimSpace(text)
	if (text == "" && len(h.ui.pending) == 0) || h.ui.busy {
		return
	}
	// A picture goes as one; a file of text, in the text the model is sent.
	e, m := entry{Kind: said, Text: text}, llm.Message{Role: llm.User}
	var b strings.Builder
	b.WriteString(text)
	for _, p := range h.ui.pending {
		if p.Type == "" {
			e.Files = append(e.Files, attached{Name: p.Name})
			fmt.Fprintf(&b, "\n\n<file name=%q>\n%s\n</file>", p.Name, p.Text)
			continue
		}
		name := h.keepFile(p)
		e.Files = append(e.Files, attached{Name: p.Name, Image: name})
		m.Images = append(m.Images, llm.Image{Name: name, Type: p.Type})
	}
	m.Text = strings.TrimSpace(b.String())
	h.ui.pending, h.ui.note = nil, ""
	h.ui.entries = append(h.ui.entries, e)
	h.ui.history = append(h.ui.history, m)
	h.ask()
}

// ask has the model go on with the conversation as it stands.
func (h *Home) ask() {
	h.ui.busy, h.ui.follow, h.ui.copied = true, true, -1
	h.step(0)
}

// Reset starts a new conversation; the one that showed stays saved.
func (h *Home) Reset() {
	h.Stop()
	h.ui = viewState{run: h.ui.run + 1, growing: -1, copied: -1, panel: h.ui.panel}
}

// Stop ends the answer that is coming: what of it showed stays.
func (h *Home) Stop() {
	if !h.ui.busy {
		return
	}
	if h.ui.cancel != nil {
		h.ui.cancel()
	}
	// What came of the answer is the model's to know of, next time.
	if g := h.ui.growing; g >= 0 && g < len(h.ui.entries) {
		if text := strings.TrimSpace(h.ui.entries[g].Text); text != "" {
			h.ui.entries[g].Text = text
			h.ui.history = append(h.ui.history, llm.Message{Role: llm.Assistant, Text: text})
		}
	}
	h.ui.run++ // what still comes of it is dropped
	h.ui.busy, h.ui.growing, h.ui.cancel = false, -1, nil
	h.keep()
}

// fail ends the turn with what went wrong.
func (h *Home) fail(message string) {
	h.ui.entries = append(h.ui.entries, entry{Kind: problem, Text: message})
	h.done()
}

// done ends the turn.
func (h *Home) done() {
	h.ui.busy, h.ui.follow, h.ui.growing, h.ui.cancel = false, true, -1, nil
	h.keep()
}

// lastSaid is where in the entries the user last wrote, -1 for nowhere.
func (h *Home) lastSaid() int {
	for i := len(h.ui.entries) - 1; i >= 0; i-- {
		if h.ui.entries[i].Kind == said {
			return i
		}
	}
	return -1
}

// CanRegenerate reports whether the last answer can be asked for again:
// not while one comes, and not after a turn that did something for the
// user, which asking again would do twice.
func (h *Home) CanRegenerate() bool {
	i := h.lastSaid()
	if h.ui.busy || i < 0 || i == len(h.ui.entries)-1 {
		return false
	}
	for _, e := range h.ui.entries[i+1:] {
		if e.Kind == action {
			return false
		}
	}
	return true
}

// Regenerate drops what was answered to the user's last message, and asks
// again.
func (h *Home) Regenerate() {
	if !h.CanRegenerate() {
		return
	}
	h.ui.entries = h.ui.entries[:h.lastSaid()+1]
	for i := len(h.ui.history) - 1; i >= 0; i-- {
		if h.ui.history[i].Role == llm.User {
			h.ui.history = h.ui.history[:i+1]
			break
		}
	}
	h.ask()
}

// keep saves the conversation that shows, as the newest; one with nothing
// said is not kept.
func (h *Home) keep() {
	if len(h.ui.entries) == 0 {
		return
	}
	var k *conversation
	for i, o := range h.data.Conversations {
		if o.ID == h.ui.id {
			k = o
			h.data.Conversations = slices.Delete(h.data.Conversations, i, i+1)
			break
		}
	}
	if k == nil {
		h.data.Next++
		first := h.ui.entries[0]
		if first.Text == "" && len(first.Files) > 0 {
			first.Text = first.Files[0].Name
		}
		k = &conversation{ID: h.data.Next, Title: title(first.Text)}
		h.ui.id = k.ID
	}
	k.Updated = h.now().Format(time.RFC3339)
	k.Entries, k.History = slices.Clone(h.ui.entries), slices.Clone(h.ui.history)
	h.data.Conversations = slices.Insert(h.data.Conversations, 0, k)
	if len(h.data.Conversations) > kept {
		for _, old := range h.data.Conversations[kept:] {
			h.forget(old)
		}
		h.data.Conversations = h.data.Conversations[:kept]
	}
	h.save()
}

func (h *Home) save() {
	if err := h.store.Save(h.ID(), &h.data); err != nil {
		log.Printf("home: save: %v", err)
	}
}

// Conversations returns the titles of the conversations saved, the newest
// first.
func (h *Home) Conversations() []string {
	titles := make([]string, len(h.data.Conversations))
	for i, k := range h.data.Conversations {
		titles[i] = k.Title
	}
	return titles
}

// title names a conversation after what it began with.
func title(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if r := []rune(text); len(r) > 24 {
		return string(r[:24]) + "…"
	}
	return text
}

// Open shows a conversation that was saved, to go on with.
func (h *Home) Open(id int) {
	for _, k := range h.data.Conversations {
		if k.ID == id {
			h.Reset()
			h.ui.id, h.ui.entries, h.ui.history = k.ID, slices.Clone(k.Entries), slices.Clone(k.History)
			h.ui.follow = true
			return
		}
	}
}

// Delete forgets a conversation that was saved; the one that shows makes
// way for a new one.
func (h *Home) Delete(id int) {
	if h.ui.id == id {
		h.Reset()
	}
	h.data.Conversations = slices.DeleteFunc(h.data.Conversations, func(k *conversation) bool {
		if k.ID == id {
			h.forget(k)
		}
		return k.ID == id
	})
	h.save()
}

// step asks the model to go on with the conversation, does what it calls
// for, and asks again until it has only something to say.
func (h *Home) step(n int) {
	cfg, ok := h.Model()
	if !ok {
		h.fail("还没有可用的模型，请先到设置里添加。")
		return
	}
	req := llm.Request{System: h.system(), Messages: h.pictures(h.ui.history), Tools: tools}
	run := h.ui.run
	h.ui.growing = -1
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	h.ui.cancel = cancel
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
				h.grow(text, cfg.Model)
			}
		})
	}
	h.Background(func() func() {
		msg, err := h.Complete(ctx, cfg, req, onText)
		cancel()
		return func() {
			if run != h.ui.run {
				return // stopped, or another conversation shows
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
				h.ui.entries[g].Text = text
			} else {
				h.ui.entries = append(h.ui.entries, entry{Kind: answer, Text: text, Model: cfg.Model})
			}
			h.ui.growing = -1
			if len(msg.Calls) == 0 {
				h.done()
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
func (h *Home) grow(text, model string) {
	if text == "" {
		return
	}
	if g := h.ui.growing; g >= 0 && g < len(h.ui.entries) {
		h.ui.entries[g].Text += text
	} else {
		h.ui.entries = append(h.ui.entries, entry{Kind: answer, Text: text, Model: model})
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
		Name:        "update_task",
		Description: "修改一条已有的任务，或把它标为完成、未完成。只填要改的字段，没填的保持原样。id 是任务前面方括号里的数字。",
		Parameters: schema([]string{"id"}, map[string]any{
			"id":     prop("integer", "任务的 id"),
			"title":  prop("string", "新的标题"),
			"due":    prop("string", "新的到期日期，YYYY-MM-DD；填空字符串表示去掉日期"),
			"note":   prop("string", "新的备注；填空字符串表示清空"),
			"repeat": map[string]any{"type": "string", "enum": []string{"", "daily", "weekly", "monthly"}, "description": "新的重复方式；填空字符串表示不再重复"},
			"done":   prop("boolean", "true 标为完成，false 标为未完成"),
		}),
	},
	{
		Name:        "delete_task",
		Description: "删除一条任务，删了不能恢复。只在用户明确要求删除时使用；说不清是哪一条时先问。id 是任务前面方括号里的数字。",
		Parameters: schema([]string{"id"}, map[string]any{
			"id": prop("integer", "任务的 id"),
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
	did := func(text string) { h.ui.entries = append(h.ui.entries, entry{Kind: action, Text: text}) }
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
	case "update_task":
		var in struct {
			ID                       int
			Title, Due, Note, Repeat *string
			Done                     *bool
		}
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return "参数不对：" + err.Error()
		}
		k := h.tasks.Task(in.ID)
		if k == nil {
			return fmt.Sprintf("没有 id 是 %d 的任务", in.ID)
		}
		if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
			return "没有修改：标题不能是空的"
		}
		if in.Due != nil && *in.Due != "" {
			if _, err := time.Parse(dateLayout, *in.Due); err != nil {
				return "due 要写成 YYYY-MM-DD"
			}
		}
		if in.Repeat != nil {
			switch *in.Repeat {
			case todo.RepeatNone, todo.RepeatDaily, todo.RepeatWeekly, todo.RepeatMonthly:
			default:
				return "repeat 只能是 daily、weekly、monthly 或空字符串"
			}
		}
		was := k.Title
		var changes []string
		if in.Title != nil && strings.TrimSpace(*in.Title) != k.Title {
			k.Title = strings.TrimSpace(*in.Title)
			changes = append(changes, "改名为“"+k.Title+"”")
		}
		if in.Due != nil && *in.Due != k.Due {
			if k.Due = *in.Due; k.Due == "" {
				changes = append(changes, "去掉日期")
			} else {
				changes = append(changes, "改到"+h.dayOf(k.Due))
			}
		}
		if in.Note != nil && strings.TrimSpace(*in.Note) != k.Note {
			k.Note = strings.TrimSpace(*in.Note)
			changes = append(changes, "改了备注")
		}
		if in.Repeat != nil && *in.Repeat != k.Repeat {
			k.Repeat = *in.Repeat
			changes = append(changes, "改了重复")
		}
		if len(changes) > 0 {
			h.tasks.Changed(k)
		}
		if in.Done != nil && *in.Done != k.Done {
			h.tasks.SetDone(k, *in.Done)
			if *in.Done {
				changes = append(changes, "标为完成")
			} else {
				changes = append(changes, "标为未完成")
			}
		}
		if len(changes) == 0 {
			return "没有要改的：任务已经是这样了"
		}
		did("已修改任务：" + was + " · " + strings.Join(changes, "，"))
		return "已修改，现在是：" + h.taskLine(k)
	case "delete_task":
		var in struct{ ID int }
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return "参数不对：" + err.Error()
		}
		k := h.tasks.Task(in.ID)
		if k == nil {
			return fmt.Sprintf("没有 id 是 %d 的任务", in.ID)
		}
		name := k.Title
		h.tasks.Remove(k)
		did("已删除任务：" + name)
		return "已删除"
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

// taskLine writes a task for the model, on a line: its ID first, by which
// the model says which task it means.
func (h *Home) taskLine(k *todo.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%d] ", k.ID)
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
- 用简短的中文回答。可以用 Markdown 的列表和加粗让安排一目了然，不要加标题，不要长篇大论。
- 用户说“明天”“下周三”这类相对日期时，换算成具体日期再查或再添加。
- 问某天的安排时，把那天的课和到期的任务都说出来。
- 添加前信息不全（比如课程没说星期几、第几节）就先问清楚，不要猜。
- 添加成功后用一句话确认。工具说失败时，把原因告诉用户。
- 任务可以修改、标为完成和删除，用任务前面方括号里的 id 指明是哪一条；回答用户时不要提 id。
- 删除不能恢复：只在用户明确说要删时才删，指不清是哪一条、或者一次要删好几条时，先列出来问清楚。
- 用户附上图片或文件时，按里面的内容回答。要据此添加好几条任务或课程时，先把读到的列出来让用户确认，再添加。
- 课表里的课程只能查看和添加。用户要修改或删除课程时，请他到“课表”页面操作。
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
