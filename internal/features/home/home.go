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
	"github.com/pandyzhou/atelier/internal/features/trending"
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
	// Effort returns how hard the model asked is to think, one of llm's
	// efforts or "" to leave it to the model, and SetEffort sets it: for
	// that model, from now on.
	Effort    func() string
	SetEffort func(string)
	// Trending is the week's trending repositories of GitHub, for the
	// assistant to tell of and to read the READMEs of: nil for none.
	Trending *trending.Trending
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
	note           // something not done, as a deletion the user declined
)

// entry is a line of a conversation, as the page shows it.
type entry struct {
	Kind int    `json:"kind"`
	Text string `json:"text"`
	// Model is the model an answer is of, Thinking what it thought before
	// it, as far as its service told, and Thought for how many seconds.
	Model    string `json:"model,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	Thought  int    `json:"thought,omitempty"`
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
	e, m := entry{Kind: said, Text: text}, llm.Message{Role: llm.User, Context: h.context()}
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
	// A deletion that waits is not done, nor what was called for after
	// it: each call has its answer, as the model's service wants it.
	for _, call := range h.ui.reading {
		h.ui.history = append(h.ui.history, llm.Message{Role: llm.Result, CallID: call.ID, Text: "用户停止了，没有执行。"})
	}
	h.ui.reading = nil
	if a := h.ui.asking; a != nil {
		for _, call := range a.calls {
			h.ui.history = append(h.ui.history, llm.Message{Role: llm.Result, CallID: call.ID, Text: "用户停止了，没有执行。"})
		}
		h.ui.entries = append(h.ui.entries, entry{Kind: note, Text: "没有删除" + a.what})
		h.ui.asking = nil
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
	if h.Effort != nil {
		req.Effort = h.Effort()
	}
	run := h.ui.run
	h.ui.growing, h.ui.thinking = -1, time.Time{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	h.ui.cancel = cancel
	// The text and the thinking that came and do not show yet: they come
	// off the UI's thread, piece by piece, and are shown on it, all there
	// is at once.
	var mu sync.Mutex
	var came, thought string
	waiting := false
	take := func(into *string) func(string) {
		return func(piece string) {
			mu.Lock()
			*into += piece
			if waiting {
				mu.Unlock()
				return
			}
			waiting = true
			mu.Unlock()
			h.OnUI(func() {
				mu.Lock()
				text, thinking := came, thought
				came, thought, waiting = "", "", false
				mu.Unlock()
				if run == h.ui.run {
					h.think(thinking, cfg.Model)
					h.grow(text, cfg.Model)
				}
			})
		}
	}
	onText := take(&came)
	req.OnThinking = take(&thought)
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
			// The whole answer, and what was thought before it, in place
			// of what of them showed as they came.
			text, thinking := strings.TrimSpace(msg.Text), strings.TrimSpace(msg.Thinking)
			g := h.ui.growing
			if (g < 0 || g >= len(h.ui.entries)) && (text != "" || thinking != "") {
				h.ui.entries = append(h.ui.entries, entry{Kind: answer, Model: cfg.Model})
				g = len(h.ui.entries) - 1
			}
			if g >= 0 && g < len(h.ui.entries) {
				e := &h.ui.entries[g]
				if text != "" {
					e.Text = text
				}
				if thinking != "" {
					e.Thinking = thinking
				}
				h.thoughtFor(e)
			}
			h.ui.growing = -1
			if len(msg.Calls) == 0 {
				h.done()
				return
			}
			h.answer(msg.Calls, n)
		}
	})
}

// asking is a deletion the model called for, which waits for the user to
// say whether it is to be done.
type asking struct {
	what string // what is to be deleted, as the user is asked
	// calls are the calls still to answer: the deletion, and those after it.
	calls []llm.Call
	n     int // the step they are of
}

// answer does what the model called for at step n, tells it what came of
// each, and asks it to go on. A deletion is not done until the user agrees
// to it: the turn waits there, for Confirm.
func (h *Home) answer(calls []llm.Call, n int) {
	for i, call := range calls {
		if what := h.deletes(call); what != "" {
			h.ui.asking, h.ui.follow = &asking{what: what, calls: calls[i:], n: n}, true
			return
		}
		// What is read from the network is read off the UI's thread: the
		// turn goes on from here when it has come.
		if read := h.reads(call); read != nil {
			run := h.ui.run
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			h.ui.reading, h.ui.cancel = calls[i:], cancel
			h.Background(func() func() {
				result, then := read(ctx)
				cancel()
				return func() {
					if run != h.ui.run {
						return // stopped, or another conversation shows
					}
					h.ui.reading = nil
					if then != nil {
						then()
					}
					h.ui.history = append(h.ui.history, llm.Message{Role: llm.Result, CallID: call.ID, Text: result})
					h.answer(calls[i+1:], n)
				}
			})
			return
		}
		h.ui.history = append(h.ui.history, llm.Message{Role: llm.Result, CallID: call.ID, Text: h.call(call)})
	}
	if n+1 >= maxSteps {
		h.fail("这件事步骤太多，先停在这里了。")
		return
	}
	h.step(n + 1)
}

// Confirm answers the deletion that waits: done if yes, else left, which
// the model is told; then the turn goes on.
func (h *Home) Confirm(yes bool) {
	a := h.ui.asking
	if a == nil {
		return
	}
	h.ui.asking = nil
	call := a.calls[0]
	result := "用户没有同意，没有删除。"
	if yes {
		result = h.call(call)
	} else {
		h.ui.entries = append(h.ui.entries, entry{Kind: note, Text: "没有删除" + a.what})
	}
	h.ui.history = append(h.ui.history, llm.Message{Role: llm.Result, CallID: call.ID, Text: result})
	h.ui.follow = true
	h.answer(a.calls[1:], a.n)
}

// repoName is a repository's name as "owner/name", of what the model may
// have written for it: its address, or its name with room around it.
func repoName(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		s = strings.TrimPrefix(s, prefix)
	}
	return strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
}

// trendingLines writes the week's trending repositories for the model.
func trendingLines(repos []trending.Repo) string {
	var b strings.Builder
	for i, r := range repos {
		fmt.Fprintf(&b, "%d. %s", i+1, r.Name)
		if r.Language != "" {
			b.WriteString("（" + r.Language + "）")
		}
		fmt.Fprintf(&b, " star %d，本周 +%d", r.Stars, r.Gained)
		if r.Description != "" {
			b.WriteString("：" + r.Description)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// reads returns what a call reads from the network, to run off the UI's
// thread: what to tell the model, and what to do on the UI's thread once
// it has come. It is nil for a call that reads nothing from there, which
// call answers.
func (h *Home) reads(c llm.Call) func(ctx context.Context) (string, func()) {
	t := h.Trending
	if t == nil || !t.CanRead() {
		return nil
	}
	note := func(text string) func() {
		return func() { h.ui.entries = append(h.ui.entries, entry{Kind: note, Text: text}) }
	}
	switch c.Name {
	case "list_trending":
		// The list at hand, unless it is old or there is none.
		had := t.Repos()
		if len(had) > 0 && t.Fresh() {
			return nil
		}
		return func(ctx context.Context) (string, func()) {
			repos, err := t.Read(ctx)
			switch {
			case err == nil:
				return trendingLines(repos), func() {
					t.Keep(repos)
					note("看了 GitHub 本周热门")()
				}
			case len(had) > 0:
				return trendingLines(had) + "\n（这是之前读到的，刚才没有刷新成功：" + err.Error() + "）", note("看了 GitHub 本周热门")
			}
			return "没有读到热门仓库：" + err.Error(), nil
		}
	case "read_readme":
		var in struct{ Repo string }
		if json.Unmarshal(c.Input, &in) != nil {
			return nil
		}
		repo := repoName(in.Repo)
		if !trending.IsRepo(repo) {
			return nil
		}
		if _, ok := t.Readme(repo); ok {
			return nil
		}
		return func(ctx context.Context) (string, func()) {
			text, err := t.ReadReadme(ctx, repo)
			if err != nil {
				return "没有读到 " + repo + " 的 README：" + err.Error(), nil
			}
			return text, note("读了 " + repo + " 的 README")
		}
	}
	return nil
}

// deletes says what a call would delete, as "任务“交电费”": "" when it is
// not a deletion, or of nothing there is, which needs no asking.
func (h *Home) deletes(c llm.Call) string {
	var in struct{ ID int }
	if json.Unmarshal(c.Input, &in) != nil {
		return ""
	}
	switch c.Name {
	case "delete_task":
		if k := h.tasks.Task(in.ID); k != nil {
			return "任务“" + k.Title + "”"
		}
	case "delete_course":
		if k := h.table.Course(in.ID); k != nil {
			return fmt.Sprintf("课程“%s”（%s第 %s 节）", k.Name, dayNames[k.Day], span(k.From, k.To))
		}
	}
	return ""
}

// grow shows more of the answer that is coming: at the end of the one that
// grows, or as a new one.
func (h *Home) grow(text, model string) {
	if text == "" {
		return
	}
	if g := h.ui.growing; g >= 0 && g < len(h.ui.entries) {
		// The thinking is over when the answer begins.
		h.thoughtFor(&h.ui.entries[g])
		h.ui.entries[g].Text += text
	} else {
		h.ui.entries = append(h.ui.entries, entry{Kind: answer, Text: text, Model: model})
		h.ui.growing = len(h.ui.entries) - 1
	}
	h.ui.follow = true
}

// think shows more of what the model thinks before the answer that is
// coming: over that answer, which begins with it.
func (h *Home) think(text, model string) {
	if text == "" {
		return
	}
	if h.ui.thinking.IsZero() {
		h.ui.thinking = h.now()
	}
	if g := h.ui.growing; g >= 0 && g < len(h.ui.entries) {
		h.ui.entries[g].Thinking += text
	} else {
		h.ui.entries = append(h.ui.entries, entry{Kind: answer, Thinking: text, Model: model})
		h.ui.growing = len(h.ui.entries) - 1
	}
	h.ui.follow = true
}

// thoughtFor notes, once, how long the thinking of an answer took: from
// when it began to come until now.
func (h *Home) thoughtFor(e *entry) {
	if e.Thinking == "" || e.Thought != 0 || h.ui.thinking.IsZero() {
		return
	}
	e.Thought = max(1, int(h.now().Sub(h.ui.thinking).Round(time.Second)/time.Second))
}

// tools are what the model can have done.
var tools = []llm.Tool{
	{
		Name:        "add_task",
		Description: "添加一条待办任务。",
		Parameters: schema([]string{"title"}, map[string]any{
			"title":  prop("string", "任务标题"),
			"due":    prop("string", "到期日期，YYYY-MM-DD；没有日期就不填。重复的任务填第一期的日期，不填就从今天算起"),
			"note":   prop("string", "备注，可不填"),
			"repeat": map[string]any{"type": "string", "enum": []string{"daily", "weekly", "monthly"}, "description": "重复：每天、每周（每周的同一个星期几）或每月（每月的同一号）；不重复就不填"},
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
		Name:        "update_course",
		Description: "修改课表里已有的一门课。只填要改的字段，没填的保持原样。id 是课程前面方括号里的数字。和别的课程冲突时会失败并说明原因。",
		Parameters: schema([]string{"id"}, map[string]any{
			"id":          prop("integer", "课程的 id"),
			"name":        prop("string", "新的课程名"),
			"weekday":     prop("integer", "改到星期几，1 是周一，7 是周日"),
			"from_period": prop("integer", "改为从第几节开始；不填 to_period 时，课的节数不变"),
			"to_period":   prop("integer", "改为到第几节结束"),
			"room":        prop("string", "新的上课地点；填空字符串表示清空"),
			"teacher":     prop("string", "新的任课老师；填空字符串表示清空"),
		}),
	},
	{
		Name:        "delete_course",
		Description: "从课表里删除一门课，删了不能恢复。只在用户明确要求删除时使用；说不清是哪一门时先问。id 是课程前面方括号里的数字。",
		Parameters: schema([]string{"id"}, map[string]any{
			"id": prop("integer", "课程的 id"),
		}),
	},
	{
		Name:        "set_periods",
		Description: "设置课表每天有几节课，以及各节的起止时间。两样都可以只填一样；times 里只写要改的节次，没写的保持原样。节数不能少于已有课程排到的节次。",
		Parameters: schema([]string{}, map[string]any{
			"count": prop("integer", "每天的节数，1 到 16"),
			"times": map[string]any{
				"type":        "array",
				"description": "各节的时间",
				"items": schema([]string{"period", "start", "end"}, map[string]any{
					"period": prop("integer", "第几节，从 1 开始"),
					"start":  prop("string", "开始时间，24 小时制 HH:MM；填空字符串表示清空"),
					"end":    prop("string", "结束时间，24 小时制 HH:MM；填空字符串表示清空"),
				}),
			},
		}),
	},
	{
		Name:        "list_trending",
		Description: "列出 GitHub 本周的热门仓库：名字、简介、语言、star 数和本周新增的 star。用户问最近有什么热门项目、想找某类开源项目时用。",
		Parameters:  schema([]string{}, map[string]any{}),
	},
	{
		Name:        "read_readme",
		Description: "读 GitHub 上一个公开仓库的 README，不限于热门榜上的。用户问某个仓库是做什么的、怎么安装、怎么用时用。",
		Parameters: schema([]string{"repo"}, map[string]any{
			"repo": prop("string", "仓库，写成 owner/name，比如 golang/go"),
		}),
	},
	{
		Name:        "list_tasks",
		Description: "按到期日期查询任务，包括已完成的；重复的任务在这段日子里以后还会出现的几期也一并列出。提示里已经列出未完成的任务，只有要查已完成的、查以后的日子有什么或核对时才需要用。",
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
		note, result := "已添加任务："+k.Title, fmt.Sprintf("已添加，id %d，到期 %s", k.ID, orNone(k.Due))
		if k.Due != "" {
			note += " · " + h.dayOf(k.Due)
			result += " " + weekday(k.Due)
		}
		// When it comes back is said too: the day was not always asked for.
		if every := cadence(k); every != "" {
			note += " · " + every + "重复"
			result += "，以后" + every + "重复"
		}
		did(note)
		return result
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
		refused := "" // why it was not checked off, as asked
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
		result := "已修改，现在是："
		switch {
		case in.Done == nil || *in.Done == k.Done:
		case *in.Done && h.tasks.Ahead(k):
			// The next time's, as the to-do page shows it: not to be
			// checked off before that time begins.
			refused = "这一期已经完成了，下一期从" + h.dayOf(k.Period()) + "开始，到那时才能标为完成"
		default:
			// Checked off, a repeating task is not done: it is the next
			// time's, and a copy of it is.
			if *in.Done && k.Repeat != todo.RepeatNone {
				result = "已修改：这一期记为完成，任务挪到了下一期，现在是："
			}
			h.tasks.SetDone(k, *in.Done)
			if *in.Done {
				changes = append(changes, "标为完成")
			} else {
				changes = append(changes, "标为未完成")
			}
		}
		if len(changes) == 0 {
			if refused != "" {
				return "没有修改：" + refused
			}
			return "没有要改的：任务已经是这样了"
		}
		did("已修改任务：" + was + " · " + strings.Join(changes, "，"))
		if refused != "" {
			result = "已修改（但没有标为完成：" + refused + "），现在是："
		}
		return result + h.taskLine(k)
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
	case "update_course":
		var in struct {
			ID                  int
			Name, Room, Teacher *string
			Weekday             *int
			From                *int `json:"from_period"`
			To                  *int `json:"to_period"`
		}
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return "参数不对：" + err.Error()
		}
		was := h.table.Course(in.ID)
		if was == nil {
			return fmt.Sprintf("没有 id 是 %d 的课程", in.ID)
		}
		k := *was
		if in.Name != nil {
			if k.Name = strings.TrimSpace(*in.Name); k.Name == "" {
				return "没有修改：课程名不能是空的"
			}
		}
		if in.Weekday != nil {
			k.Day = *in.Weekday - 1
		}
		// Moved by where it begins alone, a course stays as long.
		if in.From != nil {
			k.From = *in.From
			if in.To == nil {
				k.To = k.From + was.To - was.From
			}
		}
		if in.To != nil {
			k.To = *in.To
		}
		if in.Room != nil {
			k.Room = strings.TrimSpace(*in.Room)
		}
		if in.Teacher != nil {
			k.Teacher = strings.TrimSpace(*in.Teacher)
		}
		n := len(h.table.Periods())
		if k.Day < 0 || k.Day > 6 || k.From < 1 || k.To < k.From || k.To > n {
			return fmt.Sprintf("没有修改：weekday 要在 1 到 7 之间，节次要在 1 到 %d 之间，且 to_period 不小于 from_period", n)
		}
		var changes []string
		if k.Name != was.Name {
			changes = append(changes, "改名为“"+k.Name+"”")
		}
		if k.Day != was.Day || k.From != was.From || k.To != was.To {
			changes = append(changes, fmt.Sprintf("改到%s第 %s 节", dayNames[k.Day], span(k.From, k.To)))
		}
		if k.Room != was.Room {
			changes = append(changes, "改了地点")
		}
		if k.Teacher != was.Teacher {
			changes = append(changes, "改了老师")
		}
		if len(changes) == 0 {
			return "没有要改的：课程已经是这样了"
		}
		name := was.Name
		kept, err := h.table.Put(k)
		if err != nil {
			return "没有修改：" + err.Error()
		}
		did("已修改课程：" + name + " · " + strings.Join(changes, "，"))
		return "已修改，现在是：" + h.courseLine(kept)
	case "delete_course":
		var in struct{ ID int }
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return "参数不对：" + err.Error()
		}
		k := h.table.Course(in.ID)
		if k == nil {
			return fmt.Sprintf("没有 id 是 %d 的课程", in.ID)
		}
		name := fmt.Sprintf("%s · %s第 %s 节", k.Name, dayNames[k.Day], span(k.From, k.To))
		h.table.Remove(k.ID)
		did("已删除课程：" + name)
		return "已删除"
	case "set_periods":
		var in struct {
			Count *int
			Times []struct {
				Period     int
				Start, End string
			}
		}
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return "参数不对：" + err.Error()
		}
		if in.Count == nil && len(in.Times) == 0 {
			return "没有要改的：count 和 times 都没有填"
		}
		// All of it is looked over before any of it is done.
		n := len(h.table.Periods())
		if in.Count != nil {
			least := 1
			for _, k := range h.table.Courses() {
				least = max(least, k.To)
			}
			switch {
			case *in.Count < 1 || *in.Count > 16:
				return "没有修改：每天的节数要在 1 到 16 之间"
			case *in.Count < least:
				return fmt.Sprintf("没有修改：已有课程排到第 %d 节，每天不能少于 %d 节", least, least)
			}
			n = *in.Count
		}
		for i := range in.Times {
			p := &in.Times[i]
			p.Start, p.End = strings.TrimSpace(p.Start), strings.TrimSpace(p.End)
			if p.Period < 1 || p.Period > n {
				return fmt.Sprintf("没有修改：没有第 %d 节，每天是 %d 节", p.Period, n)
			}
			// A time is kept as 08:20, however it was written.
			for _, at := range []*string{&p.Start, &p.End} {
				if *at == "" {
					continue
				}
				clock, err := time.Parse("15:04", *at)
				if err != nil {
					return fmt.Sprintf("没有修改：第 %d 节的时间“%s”要写成 HH:MM", p.Period, *at)
				}
				*at = clock.Format("15:04")
			}
			if p.Start != "" && p.End != "" && p.End <= p.Start {
				return fmt.Sprintf("没有修改：第 %d 节的结束时间要晚于开始时间", p.Period)
			}
		}
		var changes []string
		if n != len(h.table.Periods()) {
			h.table.SetPeriods(n)
			changes = append(changes, fmt.Sprintf("每天 %d 节", n))
		}
		timed := 0
		for _, p := range in.Times {
			if was := h.table.Periods()[p.Period-1]; was.Start != p.Start || was.End != p.End {
				h.table.SetTime(p.Period, timetable.Period{Start: p.Start, End: p.End})
				timed++
			}
		}
		if timed > 0 {
			changes = append(changes, fmt.Sprintf("改了 %d 节的时间", timed))
		}
		if len(changes) == 0 {
			return "没有要改的：节次已经是这样了"
		}
		did("已设置节次：" + strings.Join(changes, "，"))
		return "已设置，现在是：\n" + h.periodLines()
	case "list_trending":
		// What there is at hand; reads has it read anew when that is old.
		t := h.Trending
		if t == nil || len(t.Repos()) == 0 {
			return "这里看不了热门仓库。"
		}
		h.ui.entries = append(h.ui.entries, entry{Kind: note, Text: "看了 GitHub 本周热门"})
		return trendingLines(t.Repos())
	case "read_readme":
		var in struct{ Repo string }
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return "参数不对：" + err.Error()
		}
		repo := repoName(in.Repo)
		switch t := h.Trending; {
		case !trending.IsRepo(repo):
			return "repo 要写成 owner/name"
		case t == nil:
			return "这里读不了 GitHub。"
		default:
			text, ok := t.Readme(repo)
			if !ok {
				return "这里读不了 GitHub。"
			}
			h.ui.entries = append(h.ui.entries, entry{Kind: note, Text: "读了 " + repo + " 的 README"})
			return text
		}
	case "list_tasks":
		var in struct{ From, To string }
		if err := json.Unmarshal(c.Input, &in); err != nil {
			return "参数不对：" + err.Error()
		}
		from, err := time.Parse(dateLayout, in.From)
		if _, bad := time.Parse(dateLayout, in.To); err != nil || bad != nil {
			return "from 和 to 要写成 YYYY-MM-DD"
		}
		if in.To < in.From {
			return "to 不能早于 from"
		}
		var b strings.Builder
		for _, k := range h.tasks.Tasks() {
			if k.Due == "" || k.Due < in.From || k.Due > in.To {
				continue
			}
			b.WriteString(h.taskLine(k))
		}
		b.WriteString(h.comingLines(in.From, min(in.To, from.AddDate(0, 0, comingDays).Format(dateLayout))))
		if b.Len() == 0 {
			return "这段时间没有任务"
		}
		return b.String()
	}
	return "没有这个工具：" + c.Name
}

const (
	// comingDays is how far ahead of the first day asked of the model is
	// told of the repeating tasks to come, and comingDates how many of the
	// days one of them comes back on.
	comingDays  = 366
	comingDates = 12
)

// comingLines writes for the model the repeating tasks that will come back
// from from to to, a line each with the days: they are no tasks of their
// own, and not to be done yet. It is "" when none will.
func (h *Home) comingLines(from, to string) string {
	var order []*todo.Task
	days := map[*todo.Task][]string{}
	for _, o := range h.tasks.Coming(from, to) {
		if days[o.Task] == nil {
			order = append(order, o.Task)
		}
		days[o.Task] = append(days[o.Task], o.Date)
	}
	if len(order) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n重复的任务在这段日子里还会再出现（是同一条任务以后的几期，id 不变；还没到，不用也不能标为完成）：\n")
	for _, k := range order {
		on := days[k]
		fmt.Fprintf(&b, "[%d] %s（%s）：", k.ID, k.Title, cadence(k))
		switch {
		case k.Repeat == todo.RepeatDaily && len(on) > 1:
			b.WriteString(on[0] + " 到 " + on[len(on)-1] + " 每天")
		case len(on) > comingDates:
			b.WriteString(strings.Join(on[:comingDates], "、") + " 等")
		default:
			b.WriteString(strings.Join(on, "、"))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// cadence says when a repeating task comes back, by the day it is due: 每天,
// 每周三, 每月 25 日. It is "" for a task that does not repeat.
func cadence(k *todo.Task) string {
	d, err := time.Parse(dateLayout, k.Due)
	if err != nil {
		return ""
	}
	switch k.Repeat {
	case todo.RepeatDaily:
		return "每天"
	case todo.RepeatWeekly:
		return "每" + weekday(k.Due)
	case todo.RepeatMonthly:
		return fmt.Sprintf("每月 %d 日", d.Day())
	}
	return ""
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
	if h.tasks.Ahead(k) {
		b.WriteString("（这一期已完成，这是下一期，从" + h.dayOf(k.Period()) + "开始）")
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

// periodLines writes the day's periods for the model, each on a line with
// when it is, where that is set.
func (h *Home) periodLines() string {
	var b strings.Builder
	for i, p := range h.table.Periods() {
		fmt.Fprintf(&b, "第 %d 节 ", i+1)
		switch {
		case p.Start == "" && p.End == "":
			b.WriteString("没有设时间")
		default:
			b.WriteString(orNone(p.Start) + "–" + orNone(p.End))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// courseLine writes a course for the model, on a line: its ID first, by
// which the model says which course it means.
func (h *Home) courseLine(k *timetable.Course) string {
	var b strings.Builder
	periods := h.table.Periods()
	fmt.Fprintf(&b, "[%d] %s 第 %s 节 %s", k.ID, dayNames[k.Day], span(k.From, k.To), k.Name)
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
	return b.String()
}

// listed is how many of the tasks still to do the model is told outright.
const listed = 200

// system tells the model what it is here for. It is the same in every
// request, with the tools: what services keep of what they have read
// begins with it. What changes, the time and the user's data, goes with
// the user's messages (context).
func (h *Home) system() string {
	var b strings.Builder
	b.WriteString("你是桌面应用 Atelier 里的助手，帮用户查看和安排待办任务与课表。\n")
	b.WriteString(`
规则：
- 用简短的中文回答。可以用 Markdown 的列表和加粗让安排一目了然，不要加标题，不要长篇大论。
- 用户说“明天”“下周三”这类相对日期时，换算成具体日期再查或再添加。
- 问什么答什么：只问课就只说课，只问任务就只说任务，不要顺带说另一样，也不要补一句“没有到期的任务”或“没有课”。只有问的是笼统的“安排”“日程”“有什么事”时，才把那天的课和到期的任务都说出来。
- 添加前信息不全（比如课程没说星期几、第几节）就先问清楚，不要猜。
- 添加成功后用一句话确认。工具说失败时，把原因告诉用户。
- 任务可以修改、标为完成和删除，课程可以修改和删除，用前面方括号里的 id 指明是哪一条；回答用户时不要提 id。
- 重复的任务只有一条，日期是当前这一期的。标为完成后，留下一条已完成的记录，任务本身挪到下一期：每天的是第二天，每周的是下周的同一个星期几，每月的是下个月的同一号。下一期开始之前，它不算未完成，也不能标为完成，清单里会注明“这一期已完成，这是下一期”。这一期没做也不会卡住：到了新的一天、新的一周（周一起算）、新的一个月，它自动换成那一期的，之前没做的不保留。待办页会把下一期和以后各期提前显示在它们的日子下面，颜色淡一些，到那一期之前不能勾；要查以后哪天有，用 list_tasks。
- 添加重复的任务时，用户没说从哪天起就从今天算起，确认时说清楚是每周几或每月几号。
- “第几周”按周一到周日为一周来算，和待办页“按周”的标题一致；<context> 里写着今天是第几周。
- 用户问应用为什么这样显示、某个功能怎么运作时，按这里写的和工具查到的回答；这里没写的就直说不清楚，不要猜，更不要为了验证去改用户的数据（比如把没做的任务标为完成再改回来）。没做的事不要建议标为完成。
- 删除不能恢复：只在用户明确说要删时才删，指不清是哪一条时先问清楚。每次删除前应用都会让用户点确认，所以指明了是哪一条就直接调用，不用自己再问一遍“确定吗”；工具说用户没有同意时，就不要再删。
- 用户附上图片或文件时，按里面的内容回答。要据此添加好几条任务或课程时，先把读到的列出来让用户确认，再添加。
- 用户的每条消息前面有一段 <context>：当时的时间，以及课表和任务的清单，清单和上一次相同时省略。以最新的一份为准；你用工具做过的改动，以工具回的结果为准。
- 用户问起开源项目时，可以用 list_trending 看 GitHub 本周的热门仓库，用 read_readme 读任何公开仓库的 README；读到的内容用中文概括给用户，不要整段照搬。
- 每天有几节课、每节的起止时间可以用 set_periods 设置。照着用户给的课表添加课程时，节数不够就先加够，上面写了时间的也一并设好。
`)
	return b.String()
}

// context is what a message of the user's is sent after: the time, and the
// timetable and the tasks still to do as they are now, unless they are as
// the conversation's last message had them.
func (h *Home) context() string {
	now := h.now()
	data := h.listing()
	var b strings.Builder
	// The week's number too, as the to-do list's week view is titled by it.
	_, week := now.ISOWeek()
	fmt.Fprintf(&b, "<context>\n现在是 %s %s %s，第 %d 周。\n", now.Format(dateLayout), dayNames[(int(now.Weekday())+6)%7], now.Format("15:04"), week)
	if data == h.ui.listed {
		b.WriteString("课表和任务和上一条消息时一样。\n")
	} else {
		h.ui.listed = data
		b.WriteString(data)
	}
	b.WriteString("</context>")
	return b.String()
}

// listing writes the timetable and the tasks still to do for the model.
func (h *Home) listing() string {
	var b strings.Builder
	periods := h.table.Periods()
	fmt.Fprintf(&b, "\n每天 %d 节课，各节的时间：\n%s", len(periods), h.periodLines())
	b.WriteString("\n课表（每周相同）：\n")
	if len(h.table.Courses()) == 0 {
		b.WriteString("还没有课程。\n")
	}
	for _, k := range h.table.Courses() {
		b.WriteString(h.courseLine(k))
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
