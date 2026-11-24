package trending

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/pandyzhou/atelier/internal/llm"
)

// A README can be shown in Chinese: the model the user chose says in a few
// lines what the repository is, then translates the README, a stretch of
// it at a time, each short enough for any model to answer whole. What it
// wrote is kept, to show again without asking.

// The most of a README that is translated, and the most of it sent at once.
const (
	maxTranslated = 60 << 10
	chunkSize     = 5 << 10
)

// keptTranslations is how many translations are saved: the oldest go.
const keptTranslations = 40

// translation is a README in Chinese, as it is saved.
type translation struct {
	Name string `json:"name"` // the repository's
	Text string `json:"text"` // as the model wrote it
	// Of is how long the README it was made of is: another README is
	// translated anew.
	Of int `json:"of"`
}

// translating is a translation under way, of the repository of a name.
type translating struct {
	name   string
	text   string // what came of it so far
	cancel context.CancelFunc
	run    int // tells it from those begun before and after
}

const translator = `你把 GitHub 仓库的 README 翻译成简体中文，给不熟悉这个项目的读者看。

要求：
- 只输出翻译结果，不要加解释，不要用代码块把整篇包起来。
- 保持原来的 Markdown 和 HTML 结构：标题层级、列表、表格、链接、图片、HTML 标签和它们的属性都照原样保留，只翻译给人读的文字。
- 链接地址、图片地址、代码块和行内代码里的内容、命令、文件名、标识符不要翻译，也不要改动。
- 项目名、产品名、专有名词保留原文，必要时在后面用括号加一句中文说明。
- 译文要通顺，像中文写的文档，不要逐词硬译。`

const introducer = `

这是第一段。在译文之前，先用引用块（每行以 "> " 开头）写一段简介：用两到四句中文说明这个仓库是做什么的、适合谁用、有什么特点。简介第一行写 "> **简介**"。简介之后空一行，再接译文。`

// chunks cuts a README into stretches of about size, at the blank lines
// between its paragraphs, before a heading where one is near: never
// within a block of code.
func chunks(text string, size int) []string {
	var out []string
	var b strings.Builder
	inCode := false
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inCode = !inCode
		}
		b.WriteString(line + "\n")
		if inCode || trimmed != "" || i == len(lines)-1 {
			continue
		}
		// A blank line: cut here once there is enough, sooner before a
		// heading, which begins something else.
		next := ""
		if i+1 < len(lines) {
			next = strings.TrimSpace(lines[i+1])
		}
		heading := strings.HasPrefix(next, "#") || strings.HasPrefix(strings.ToLower(next), "<h")
		if b.Len() >= size || (heading && b.Len() >= size/2) {
			out = append(out, b.String())
			b.Reset()
		}
	}
	if strings.TrimSpace(b.String()) != "" {
		out = append(out, b.String())
	}
	return out
}

// translated returns the translation kept of a repository's README, ""
// when there is none of the README as it is now.
func (t *Trending) translated(name string, r *readme) string {
	for _, k := range t.data.Translations {
		if k.Name == name && k.Of == len(r.text) {
			return k.Text
		}
	}
	return ""
}

// CanTranslate reports whether a repository's README can be put into
// Chinese: it was read, as Markdown, and the app says how models are asked.
func (t *Trending) CanTranslate(name string) bool {
	r := t.ui.readmes[name]
	return t.Complete != nil && t.Model != nil && r != nil && !r.loading && r.problem == "" && !r.missing && !r.plain
}

// Translate has the model put a repository's README into Chinese, which
// shows as it comes; what there is already shows at once.
func (t *Trending) Translate(name string) {
	r := t.ui.readmes[name]
	if !t.CanTranslate(name) || (t.ui.translating != nil && t.ui.translating.name == name) {
		return
	}
	t.ui.original, t.ui.translateProblem = false, ""
	if t.translated(name, r) != "" {
		return
	}
	cfg, ok := t.Model()
	if !ok {
		t.ui.translateProblem = "还没有可用的模型，请先到设置里添加。"
		return
	}
	t.StopTranslating()
	source, cut := r.text, false
	if len(source) > maxTranslated {
		source, cut = source[:strings.LastIndex(source[:maxTranslated], "\n")+1], true
	}
	parts := chunks(source, chunkSize)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.ui.runs++
	job := &translating{name: name, cancel: cancel, run: t.ui.runs}
	t.ui.translating = job
	about := "仓库：" + name
	if repo, ok := t.opened(); ok && repo.Name == name && repo.Description != "" {
		about += "\n一句话描述：" + repo.Description
	}
	// What came and does not show yet: it comes off the UI's thread.
	var mu sync.Mutex
	came, waiting := "", false
	onText := func(piece string) {
		mu.Lock()
		came += piece
		if waiting {
			mu.Unlock()
			return
		}
		waiting = true
		mu.Unlock()
		t.OnUI(func() {
			mu.Lock()
			text := came
			came, waiting = "", false
			mu.Unlock()
			if t.ui.translating == job {
				job.text += text
			}
		})
	}
	t.Background(func() func() {
		var whole strings.Builder
		var failed error
		for i, part := range parts {
			system := translator
			said := about + "\n\n下面是 README 的第 " + itoa(i+1) + " 段，共 " + itoa(len(parts)) + " 段：\n\n" + part
			if i == 0 {
				system += introducer
			}
			msg, err := t.Complete(ctx, cfg, llm.Request{System: system, Messages: []llm.Message{{Role: llm.User, Text: said}}}, onText)
			if err != nil {
				failed = err
				break
			}
			whole.WriteString(strings.TrimSpace(msg.Text) + "\n\n")
			onText("\n\n")
		}
		cancel()
		return func() {
			if t.ui.translating != job {
				return // stopped, or another began
			}
			t.ui.translating = nil
			if failed != nil {
				t.ui.translateProblem = "没有翻译完：" + failed.Error()
				return
			}
			text := strings.TrimSpace(whole.String())
			if cut {
				text += "\n\n---\n\n*README 太长，后面的部分没有翻译。*"
			}
			t.keepTranslation(translation{Name: name, Text: text, Of: len(r.text)})
		}
	})
}

// StopTranslating ends the translation under way: nothing of it is kept.
func (t *Trending) StopTranslating() {
	if job := t.ui.translating; job != nil {
		job.cancel()
		t.ui.translating = nil
	}
}

// keepTranslation saves a translation, as the newest.
func (t *Trending) keepTranslation(k translation) {
	kept := []translation{k}
	for _, o := range t.data.Translations {
		if o.Name != k.Name && len(kept) < keptTranslations {
			kept = append(kept, o)
		}
	}
	t.data.Translations = kept
	if err := t.store.Save(t.ID(), &t.data); err != nil {
		log.Printf("trending: save: %v", err)
	}
}

// itoa writes a small count.
func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
