package home

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"

	"github.com/pandyzhou/atelier/internal/llm"
)

// viewState is the conversation, and what the page shows of it.
type viewState struct {
	draft   string        // what is being written to the assistant
	entries []entry       // the conversation as it shows
	history []llm.Message // the conversation as the model is sent it
	busy    bool          // an answer is on its way
	follow  bool          // the conversation grew: show its end
	run     int           // counts the conversations, to tell an old answer
}

// suggestions are things to ask, to start from.
var suggestions = []string{"今天有什么安排？", "明天有哪些课？", "这周还有哪些任务没做？"}

// width is how wide the conversation and the field under it are at most.
const width = 720

// View builds the page: before anything is said, the field in the middle
// with things to ask under it; then the conversation over the field.
func (h *Home) View(c *ui.Context) {
	ui.Row(c).Fill().Padding(0, 28, 0, 16).AlignItems(ui.Stretch).Children(func() {
		mallow.Card(c, mallow.Medium).Grow(1).MinWidth(0).Padding(24, 28).Children(func() {
			if len(h.ui.entries) == 0 {
				h.start(c)
			} else {
				h.conversation(c)
			}
		})
	})
}

// start is the page before anything is said.
func (h *Home) start(c *ui.Context) {
	_, ok := h.Model()
	ui.Column(c).Grow(1).Justify(ui.Center).Padding(0, 0, 48, 0).Children(func() {
		centered(c, 20, func() {
			ui.Column(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "今天想安排点什么？").FontSize(28).FontWeight(600).TextColor(mallow.Text)
				ui.Text(c, "问问哪天有什么安排，或者让我添加任务和课程。").FontSize(14).TextColor(mallow.Muted)
			})
			h.field(c, ok)
			if !ok {
				ui.Row(c).Gap(12).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "还没有可用的模型。").FontSize(13).TextColor(mallow.Muted)
					if mallow.Button(c, "去设置模型").Clicked() && h.OpenSettings != nil {
						h.OpenSettings()
					}
				})
				return
			}
			ui.Row(c).Gap(10).Wrap().Justify(ui.Center).Children(func() {
				for _, s := range suggestions {
					if mallow.Chip(c, s, false).Clicked() {
						h.Send(s)
					}
				}
			})
		})
	})
}

// centered builds body as a column in the middle of the page, as wide as
// the page up to width: what is read and written there.
func centered(c *ui.Context, gap float32, body func()) {
	ui.Row(c).Justify(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).MaxWidth(width).Gap(gap).Children(body)
	})
}

// field is where the user writes to the assistant: Enter sends, Shift and
// Enter starts a new line.
func (h *Home) field(c *ui.Context, usable bool) {
	send := func() {
		if text := strings.TrimSpace(h.ui.draft); text != "" && usable && !h.ui.busy {
			h.ui.draft = ""
			h.Send(text)
		}
	}
	ui.Row(c).Gap(12).AlignItems(ui.End).Children(func() {
		in := mallow.TextArea(c, &h.ui.draft).Lines(1, 6).MinHeight(0).Placeholder("问日程，或者说要添加什么").Label("给助手的话").
			Grow(1).MinWidth(0).AutoFocus()
		in.OnTextKey(func(k ui.TextKey) bool {
			if k.Key != ui.KeyEnter || k.Mods != 0 {
				return false // Shift and Enter breaks the line
			}
			if text := strings.TrimSpace(k.Text()); text != "" && usable && !h.ui.busy {
				k.SetText("")
				h.Send(text)
			}
			return true
		})
		if mallow.PrimaryButton(c, "发送").Disabled(!usable || h.ui.busy || strings.TrimSpace(h.ui.draft) == "").Clicked() {
			send()
		}
	})
}

// conversation is what was said, the newest at the bottom, over the field.
func (h *Home) conversation(c *ui.Context) {
	_, ok := h.Model()
	ui.Row(c).Shrink(0).AlignItems(ui.Center).Padding(0, 0, 8, 0).Children(func() {
		ui.Text(c, "首页").Grow(1).FontSize(20).FontWeight(600).TextColor(mallow.Text)
		if mallow.Button(c, "新对话").Clicked() {
			h.Reset()
		}
	})
	ui.Scroll(c).Label("对话").Grow(1).MinHeight(0).Padding(8, 18, 16, 0).Children(func() {
		centered(c, 14, func() {
			var last ui.Element
			for i, e := range h.ui.entries {
				last = h.entry(c.Key(i), e)
			}
			if h.ui.busy {
				last = ui.Text(c, "正在想…").FontSize(13).TextColor(mallow.Muted)
			}
			if h.ui.follow {
				h.ui.follow = false
				last.ScrollIntoView()
				c.Invalidate()
			}
		})
	})
	ui.Column(c).Shrink(0).Padding(8, 18, 0, 0).Children(func() {
		centered(c, 0, func() { h.field(c, ok) })
	})
}

// entry is a line of the conversation: what the user wrote to the right,
// on a surface of its own; what came back to the left.
func (h *Home) entry(c *ui.Context, e entry) ui.Element {
	switch e.kind {
	case said:
		return ui.Row(c).Justify(ui.End).Children(func() {
			bubble := ui.Column(c).MaxWidth(520).Shrink(1).Padding(8, 14).Radius(mallow.RadiusSmall)
			mallow.Raised(bubble, mallow.Tiny)
			bubble.Children(func() {
				ui.Text(c, e.text).FontSize(14).TextColor(mallow.Text)
			})
		})
	case action:
		return ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(16, 16).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				mallow.GlyphCheck(c, mallow.Accent)
			})
			ui.Text(c, e.text).Grow(1).MinWidth(0).FontSize(13).TextColor(mallow.Accent)
		})
	case problem:
		return ui.Text(c, e.text).FontSize(13).TextColor(mallow.Danger)
	}
	return ui.Text(c, e.text).FontSize(14).TextColor(mallow.Text).Selectable()
}
