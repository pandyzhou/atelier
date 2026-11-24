package home

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"

	"github.com/pandyzhou/atelier/internal/llm"
)

// viewState is the conversation that shows, and what the page shows of it.
type viewState struct {
	draft   string        // what is being written to the assistant
	id      int           // the conversation's ID once it is saved, else 0
	entries []entry       // the conversation as it shows
	history []llm.Message // the conversation as the model is sent it
	busy    bool          // an answer is on its way
	follow  bool          // the conversation grew: show its end
	run     int           // counts the turns begun, to tell an old answer
	// growing is the entry of the answer that is coming, which grows as
	// it does: -1 when none shows yet.
	growing int
	cancel  context.CancelFunc // ends the request under way
	copied  int                // the entry whose answer was just copied, -1 for none
	panel   bool               // the saved conversations show at the side
	picking bool               // the models to choose from show
	query   string             // what is typed to find one of them
}

func fresh() viewState { return viewState{growing: -1, copied: -1} }

// suggestions are things to ask, to start from.
var suggestions = []string{"今天有什么安排？", "明天有哪些课？", "这周还有哪些任务没做？"}

const (
	width      = 720 // how wide the conversation and the field are at most
	panelWidth = 248 // how wide the saved conversations' panel is
)

// View builds the page: before anything is said, the field in the middle
// with things to ask under it; then the conversation over the field. The
// conversations saved are in a panel at the right, shut until asked for.
func (h *Home) View(c *ui.Context) {
	ui.Row(c).Fill().Padding(0, 28, 0, 16).AlignItems(ui.Stretch).Children(func() {
		mallow.Card(c, mallow.Medium).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Padding(20, 28, 24, 28).Children(func() {
					h.bar(c)
					if len(h.ui.entries) == 0 {
						h.start(c)
					} else {
						h.conversation(c)
					}
				})
				if h.ui.panel {
					mallow.GrooveV(c).Margin(24, 0)
					h.saved(c)
				}
			})
		})
	})
}

// bar is the row of buttons over the page: a new conversation, once there
// is one, and the panel of those saved.
func (h *Home) bar(c *ui.Context) {
	ui.Row(c).Shrink(0).Gap(12).Justify(ui.End).AlignItems(ui.Center).Children(func() {
		if len(h.ui.entries) > 0 && mallow.Button(c, "新对话").Clicked() {
			h.Reset()
		}
		button := mallow.IconButton(c, "历史对话", mallow.GlyphHistory, mallow.IconButtonStyle{Size: 36, Active: h.ui.panel})
		mallow.Tooltip(c, button, "历史对话")
		if button.Clicked() {
			h.ui.panel = !h.ui.panel
		}
	})
}

// saved is the panel of the conversations saved, the newest first: a click
// opens one to go on with.
func (h *Home) saved(c *ui.Context) {
	ui.Column(c).Width(panelWidth).Shrink(0).Padding(24, 12, 16, 12).Gap(8).Children(func() {
		mallow.Label(c, "历史对话").PaddingX(8)
		if len(h.data.Conversations) == 0 {
			ui.Text(c, "还没有保存的对话。").FontSize(13).TextColor(mallow.Muted).Padding(8)
			return
		}
		open, drop := 0, 0
		ui.Scroll(c).Label("历史对话列表").Grow(1).MinHeight(0).Gap(2).Children(func() {
			for _, k := range h.data.Conversations {
				mallow.ListRow(c.Key(k.ID), k.ID == h.ui.id).Children(func() {
					body := mallow.Plain(c, "打开对话："+k.Title).Grow(1).MinWidth(0).Padding(6, 6)
					body.Children(func() {
						ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
							ui.Text(c, k.Title).FontSize(13).TextColor(mallow.Text).SingleLine()
							ui.Text(c, h.when(k.Updated)).FontSize(11).TextColor(mallow.Muted).SingleLine()
						})
					})
					if body.Clicked() {
						open = k.ID
					}
					if mallow.GhostIconButton(c, "删除对话："+k.Title, mallow.GlyphTrash, mallow.IconButtonStyle{Danger: true}).Clicked() {
						drop = k.ID
					}
				})
			}
		})
		switch {
		case drop != 0:
			h.Delete(drop)
		case open != 0:
			h.Open(open)
		}
	})
}

// when writes the time a conversation last went on: the time of day if
// that was today, else the day.
func (h *Home) when(updated string) string {
	t, err := time.Parse(time.RFC3339, updated)
	if err != nil {
		return ""
	}
	if t.Format(dateLayout) == h.now().Format(dateLayout) {
		return "今天 " + t.Format("15:04")
	}
	return fmt.Sprintf("%d月%d日", int(t.Month()), t.Day())
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

// field is where the user writes to the assistant, in a tray: Enter sends,
// Shift and Enter starts a new line. Under what is written, the model
// asked, to choose another, and the button that sends, or stops the
// answer that is coming.
func (h *Home) field(c *ui.Context, usable bool) {
	can := func() bool { return usable && !h.ui.busy }
	mallow.Tray(c, mallow.Small).Padding(10, 12, 8, 14).Gap(6).Children(func() {
		in := ui.TextAreaBase(c, &h.ui.draft).Lines(1, 6).MinHeight(0).Placeholder("问日程，或者说要添加什么").Label("给助手的话").
			FontSize(14).TextColor(mallow.Text).PlaceholderColor(mallow.Muted).FocusRing(false).Padding(4, 0).AutoFocus()
		in.OnTextKey(func(k ui.TextKey) bool {
			if k.Key != ui.KeyEnter || k.Mods != 0 {
				return false // Shift and Enter breaks the line
			}
			if text := strings.TrimSpace(k.Text()); text != "" && can() {
				k.SetText("")
				h.Send(text)
			}
			return true
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			h.picker(c)
			ui.Box(c).Grow(1)
			if h.ui.busy {
				if mallow.Button(c, "停止").Clicked() {
					h.Stop()
				}
			} else if mallow.PrimaryButton(c, "发送").Disabled(!usable || strings.TrimSpace(h.ui.draft) == "").Clicked() {
				text := h.ui.draft
				h.ui.draft = ""
				h.Send(text)
			}
		})
	})
}

// searchable is how many models there are before the list of them has a
// field to look through it with.
const searchable = 8

// picker is the model asked, and, on a click, the models to ask instead,
// under the providers they are of: the one chosen is asked from then on,
// here and after a restart.
func (h *Home) picker(c *ui.Context) {
	if h.Models == nil {
		return
	}
	models := h.Models()
	if len(models) == 0 {
		return
	}
	current := "选择模型"
	for _, m := range models {
		if m.Current {
			current = m.Name
		}
	}
	button := mallow.Plain(c, "选择模型").Padding(4, 8).Gap(2).MinWidth(0).Shrink(1)
	button.Children(func() {
		ui.Text(c, current).FontSize(12).TextColor(mallow.Muted).SingleLine()
		mallow.GlyphChevronDown(c, mallow.Muted)
	})
	if button.Clicked() {
		h.ui.picking, h.ui.query = !h.ui.picking, ""
	}
	mallow.Popover(c, button, &h.ui.picking, func() {
		if len(models) > searchable {
			mallow.TextInput(c, &h.ui.query).Placeholder("搜索模型").Label("搜索模型").Width(260).AutoFocus()
		}
		query := strings.ToLower(strings.TrimSpace(h.ui.query))
		ui.Scroll(c).Label("可选的模型").MaxHeight(300).MinWidth(240).Gap(2).Children(func() {
			provider, found := 0, false
			for _, m := range models {
				if !strings.Contains(strings.ToLower(m.Name), query) {
					continue
				}
				found = true
				if m.ProviderID != provider {
					provider = m.ProviderID
					mallow.Label(c.Key(fmt.Sprint("provider:", m.ProviderID)), m.Provider).Padding(6, 8, 2, 8)
				}
				item := mallow.Plain(c.Key(fmt.Sprint(m.ProviderID, "/", m.Name)), "用模型："+m.Name).Padding(7, 8).Gap(8)
				color := mallow.Text
				if m.Current {
					color = mallow.Accent
				}
				item.Children(func() {
					ui.Text(c, m.Name).Grow(1).MinWidth(0).FontSize(13).TextColor(color).SingleLine()
					if m.Current {
						mallow.GlyphCheck(c, mallow.Accent)
					}
				})
				if item.Clicked() {
					h.Choose(m)
					h.ui.picking = false
				}
			}
			if !found {
				ui.Text(c, "没有名字带这几个字的模型。").FontSize(12).TextColor(mallow.Muted).Padding(8)
			}
		})
	})
}

// conversation is what was said, the newest at the bottom, over the field.
func (h *Home) conversation(c *ui.Context) {
	_, ok := h.Model()
	ui.Scroll(c).Label("对话").Grow(1).MinHeight(0).Padding(8, 18, 16, 0).Children(func() {
		centered(c, 14, func() {
			var last ui.Element
			for i, e := range h.ui.entries {
				last = h.entry(c.Key(i), i, e)
			}
			if h.ui.busy && h.ui.growing < 0 {
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
// on a surface of its own; what came back to the left, an answer as the
// Markdown it is written in, with what can be done with it under it.
func (h *Home) entry(c *ui.Context, i int, e entry) ui.Element {
	switch e.Kind {
	case said:
		return ui.Row(c).Justify(ui.End).Children(func() {
			bubble := ui.Column(c).MaxWidth(520).Shrink(1).Padding(8, 14).Radius(mallow.RadiusSmall)
			mallow.Raised(bubble, mallow.Tiny)
			bubble.Children(func() {
				ui.Text(c, e.Text).FontSize(14).TextColor(mallow.Text).Selectable()
			})
		})
	case action:
		return ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(16, 16).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				mallow.GlyphCheck(c, mallow.Accent)
			})
			ui.Text(c, e.Text).Grow(1).MinWidth(0).FontSize(13).TextColor(mallow.Accent)
		})
	case problem:
		return ui.Text(c, e.Text).FontSize(13).TextColor(mallow.Danger)
	}
	growing := i == h.ui.growing
	return ui.Column(c).Gap(2).Children(func() {
		mallow.Markdown(c, strings.TrimSpace(e.Text), growing)
		if growing {
			return
		}
		ui.Row(c).Gap(2).Margin(0, 0, 0, -6).Children(func() {
			glyph, tip := mallow.GlyphCopy, "复制"
			if h.ui.copied == i {
				glyph, tip = mallow.GlyphCheck, "已复制"
			}
			copy := mallow.GhostIconButton(c, "复制回答", glyph, mallow.IconButtonStyle{})
			mallow.Tooltip(c, copy, tip)
			if copy.Clicked() {
				c.WriteClipboard(e.Text)
				h.ui.copied = i
			}
			if i == len(h.ui.entries)-1 && h.CanRegenerate() {
				again := mallow.GhostIconButton(c, "重新生成", mallow.GlyphRepeat, mallow.IconButtonStyle{})
				mallow.Tooltip(c, again, "重新生成")
				if again.Clicked() {
					h.Regenerate()
				}
			}
		})
	})
}
