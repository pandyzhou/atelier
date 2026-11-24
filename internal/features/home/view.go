package home

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/transfer"
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
	pending []pending          // the files to send with the next message
	loading int                // how many more of them are being read
	note    string             // what could not be attached, and why
	over    bool               // files are dragged over the page
	// thumbs are the conversation's pictures as they show, by what they
	// are kept under: nil for one that is no longer there.
	thumbs map[string]*ui.Bitmap
}

func fresh() viewState { return viewState{growing: -1, copied: -1} }

// suggestions are things to ask, to start from.
var suggestions = []string{"今天有什么安排？", "明天有哪些课？", "这周还有哪些任务没做？"}

const (
	width       = 720 // how wide the conversation and the field are at most
	panelWidth  = 280 // how wide the saved conversations' panel is
	panelHeight = 420 // how high its list is at most
)

// View builds the page: before anything is said, the field in the middle
// with things to ask under it; then the conversation over the field. The
// conversations saved are in a panel that floats over the page, from a button
// at the right, shut until asked for.
func (h *Home) View(c *ui.Context) {
	ui.Row(c).Fill().Padding(0, 28, 0, 16).AlignItems(ui.Stretch).Children(func() {
		mallow.Card(c, mallow.Medium).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				// Files dropped anywhere on the page go with the next message.
				page := ui.Column(c).Grow(1).MinWidth(0).Padding(20, 28, 24, 28)
				if files := page.DroppedFiles(); len(files) > 0 {
					h.Attach(files...)
				}
				h.ui.over = page.FileDragOver()
				page.Children(func() {
					h.bar(c)
					if len(h.ui.entries) == 0 {
						h.start(c)
					} else {
						h.conversation(c)
					}
				})
			})
		})
	})
}

// bar is the row over the page: at the left the model asked, to choose
// another; at the right a new conversation, once there is one, and the
// panel of those saved.
func (h *Home) bar(c *ui.Context) {
	ui.Row(c).Shrink(0).Gap(12).AlignItems(ui.Center).Children(func() {
		h.picker(c)
		ui.Box(c).Grow(1)
		if len(h.ui.entries) > 0 && mallow.IconButton(c, "新对话", mallow.GlyphCompose, mallow.IconButtonStyle{Size: 36}).Clicked() {
			h.Reset()
		}
		button := mallow.IconButton(c, "历史对话", mallow.GlyphHistory, mallow.IconButtonStyle{Size: 36, Active: h.ui.panel})
		if button.Clicked() {
			h.ui.panel = !h.ui.panel
		}
		mallow.Popover(c, button, &h.ui.panel, func() { h.saved(c) })
	})
}

// saved is the panel of the conversations saved, the newest first: a click
// opens one to go on with.
func (h *Home) saved(c *ui.Context) {
	ui.Column(c).Width(panelWidth).Padding(6, 4, 4, 4).Gap(8).Children(func() {
		mallow.Label(c, "历史对话").PaddingX(8)
		if len(h.data.Conversations) == 0 {
			ui.Text(c, "还没有保存的对话。").FontSize(13).TextColor(mallow.Muted).Padding(8)
			return
		}
		open, drop := 0, 0
		ui.Scroll(c).Label("历史对话列表").MaxHeight(panelHeight).Gap(2).Children(func() {
			group := ""
			for _, k := range h.data.Conversations {
				if g := h.when(k.Updated); g != group {
					group = g
					ui.Text(c.Key("group:"+g), g).FontSize(11).TextColor(mallow.Muted).Padding(10, 8, 4, 8)
				}
				mallow.ListRow(c.Key(k.ID), k.ID == h.ui.id).Children(func() {
					body := mallow.Plain(c, "打开对话："+k.Title).Grow(1).MinWidth(0).Padding(8, 6)
					body.Children(func() {
						ui.Text(c, k.Title).Grow(1).MinWidth(0).FontSize(13).TextColor(mallow.Text).SingleLine()
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
			h.ui.panel = false
		}
	})
}

// when names how long ago a conversation last went on, as what those of
// about then are listed under.
func (h *Home) when(updated string) string {
	t, err := time.Parse(time.RFC3339, updated)
	if err != nil {
		return "更早"
	}
	now := h.now()
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
	switch days := int(day(now).Sub(day(t.In(now.Location()))).Hours() / 24); {
	case days <= 0:
		return "今天"
	case days == 1:
		return "昨天"
	case days <= 7:
		return "近 7 天"
	case days <= 30:
		return "近 30 天"
	}
	return "更早"
}

// start is the page before anything is said.
func (h *Home) start(c *ui.Context) {
	_, ok := h.Model()
	ui.Column(c).Grow(1).Justify(ui.Center).Padding(0, 0, 48, 0).Children(func() {
		centered(c, 20, func() {
			ui.Column(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "今天想安排点什么？").FontSize(28).FontWeight(600).TextColor(mallow.Text)
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
			ui.Column(c).Gap(2).PaddingX(8).Children(func() {
				ui.Text(c, "试试这样问").FontSize(12).TextColor(mallow.Muted).Padding(0, 8, 4, 8)
				for _, s := range suggestions {
					item := mallow.Plain(c.Key(s), s).Padding(8, 8)
					item.Children(func() {
						ui.Text(c, s).Grow(1).MinWidth(0).FontSize(14).TextColor(mallow.Text).SingleLine()
					})
					if item.Clicked() {
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
// Shift and Enter starts a new line. Over what is written, the files that
// go with it; under it, the button that attaches them at the left, and at
// the right the one that sends, or stops the answer that is coming.
func (h *Home) field(c *ui.Context, usable bool) {
	can := func() bool { return usable && !h.ui.busy }
	services := c.Services()
	mallow.Tray(c, mallow.Small).Padding(12, 12, 10, 16).Gap(8).Children(func() {
		h.attachments(c)
		in := ui.TextAreaBase(c, &h.ui.draft).Lines(1, 6).MinHeight(0).Placeholder("问日程，或者让我添加、修改任务").Label("给助手的话").
			FontSize(14).TextColor(mallow.Text).PlaceholderColor(mallow.Muted).FocusRing(false).Padding(4, 0).AutoFocus()
		in.OnTextKey(func(k ui.TextKey) bool {
			if k.Key != ui.KeyEnter || k.Mods != 0 {
				return false // Shift and Enter breaks the line
			}
			if text := strings.TrimSpace(k.Text()); (text != "" || len(h.ui.pending) > 0) && can() {
				k.SetText("")
				h.Send(text)
			}
			return true
		})
		// A paste of no text may be of files, or of a picture, as a
		// screenshot: those are attached.
		in.OnPaste(func(text string) bool {
			if text != "" {
				return false
			}
			data := services.ReadClipboardData(transfer.FileList, transfer.PNG)
			if files, err := data.Files(); err == nil && len(files) > 0 {
				h.Attach(files...)
				return true
			}
			if picture, err := data.Read(transfer.PNG); err == nil && len(picture) > 0 {
				h.AttachPicture(picture)
				return true
			}
			return false
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Margin(0, 0, 0, -6).Children(func() {
			if h.PickFiles != nil {
				attach := mallow.GhostIconButton(c, "添加附件", mallow.GlyphPlus, mallow.IconButtonStyle{Size: 30})
				mallow.Tooltip(c, attach, "添加图片或文件")
				if attach.Clicked() {
					h.Background(func() func() {
						paths := h.PickFiles()
						return func() { h.Attach(paths...) }
					})
				}
			}
			ui.Box(c).Grow(1)
			if h.ui.busy {
				if mallow.IconButton(c, "停止", mallow.GlyphStop, mallow.IconButtonStyle{Size: 34}).Clicked() {
					h.Stop()
				}
			} else if mallow.IconButton(c, "发送", mallow.GlyphArrowUp, mallow.IconButtonStyle{Size: 34, Primary: true}).Disabled(!usable || (strings.TrimSpace(h.ui.draft) == "" && len(h.ui.pending) == 0)).Clicked() {
				text := h.ui.draft
				h.ui.draft = ""
				h.Send(text)
			}
		})
	})
}

// attachments are the files that go with the next message, each with a
// button that takes it out again, and a word of those that are still being
// read, of what could not be attached, or of files dragged over the page.
func (h *Home) attachments(c *ui.Context) {
	if len(h.ui.pending) > 0 || h.ui.loading > 0 {
		drop := -1
		ui.Row(c).Gap(8).Wrap().AlignItems(ui.Center).Padding(2, 0, 4, 0).Children(func() {
			for i := range h.ui.pending {
				p := &h.ui.pending[i]
				if p.Type != "" && p.shown == nil {
					p.shown, _ = ui.DecodeBitmapScaled(p.Data, 96)
				}
				if fileChip(c.Key(i), p.Name, p.shown, true) {
					drop = i
				}
			}
			if h.ui.loading > 0 {
				ui.Text(c, "正在读取…").FontSize(12).TextColor(mallow.Muted)
			}
		})
		if drop >= 0 {
			h.Detach(drop)
		}
	}
	switch {
	case h.ui.over:
		ui.Text(c, "松开鼠标，把文件添加为附件").FontSize(12).TextColor(mallow.Accent)
	case h.ui.note != "":
		ui.Text(c, h.ui.note).FontSize(12).TextColor(mallow.Danger)
	}
}

// fileChip shows a file by its name, after its picture if it is one, and
// when removable a button that takes it out, of which it reports a click.
func fileChip(c *ui.Context, name string, picture *ui.Bitmap, removable bool) (removed bool) {
	chip := ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(4, 6, 4, 6).Radius(mallow.RadiusSmall).Shrink(1).MinWidth(0)
	mallow.Raised(chip, mallow.Tiny)
	chip.Children(func() {
		if picture != nil {
			ui.Box(c).Size(28, 28).Shrink(0).Radius(6).Clip().Children(func() {
				ui.Image(c, picture).Size(28, 28).Fit(ui.Cover)
			})
		} else {
			ui.Row(c).Size(20, 28).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				mallow.GlyphFile(c, mallow.Muted)
			})
		}
		ui.Text(c, name).FontSize(12).TextColor(mallow.Text).SingleLine().MaxWidth(180).Shrink(1).MinWidth(0)
		if removable {
			removed = mallow.GhostIconButton(c, "移除附件："+name, mallow.GlyphClose, mallow.IconButtonStyle{Size: 22}).Clicked()
		} else {
			ui.Box(c).Width(2)
		}
	})
	return removed
}

// thumb returns a picture of the conversation as it shows, nil when it is
// no longer there.
func (h *Home) thumb(name string) *ui.Bitmap {
	if b, ok := h.ui.thumbs[name]; ok {
		return b
	}
	var b *ui.Bitmap
	if data := h.blob(name); data != nil {
		b, _ = ui.DecodeBitmapScaled(data, 640)
	}
	if h.ui.thumbs == nil {
		h.ui.thumbs = map[string]*ui.Bitmap{}
	}
	h.ui.thumbs[name] = b
	return b
}

// The most room a picture of the conversation takes.
const pictureWidth, pictureHeight = 280, 200

// files shows what a message of the user's came with: its pictures, then
// its other files by their names.
func (h *Home) files(c *ui.Context, files []attached) {
	for i, f := range files {
		b := (*ui.Bitmap)(nil)
		if f.Image != "" {
			b = h.thumb(f.Image)
		}
		if b == nil {
			continue
		}
		w, ht := ui.ImageSize(b)
		if w <= 0 || ht <= 0 {
			continue
		}
		scale := min(pictureWidth/w, pictureHeight/ht, 1)
		ui.Box(c.Key(fmt.Sprint("picture:", i))).Size(w*scale, ht*scale).Shrink(0).Radius(mallow.RadiusSmall).Clip().Label("图片：" + f.Name).Children(func() {
			ui.Image(c, b).Size(w*scale, ht*scale)
		})
	}
	for i, f := range files {
		switch {
		case f.Image == "":
			fileChip(c.Key(fmt.Sprint("file:", i)), f.Name, nil, false)
		case h.thumb(f.Image) == nil:
			fileChip(c.Key(fmt.Sprint("file:", i)), f.Name+"（图片已不在）", nil, false)
		}
	}
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
	button := mallow.Plain(c, "选择模型").Padding(6, 10).Gap(4).MinWidth(0).Shrink(1)
	button.Children(func() {
		ui.Text(c, current).FontSize(16).FontWeight(500).TextColor(mallow.Text).SingleLine().Shrink(1).MinWidth(0)
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
			ui.Column(c).MaxWidth(520).Shrink(1).MinWidth(0).Gap(8).AlignItems(ui.End).Children(func() {
				h.files(c, e.Files)
				if e.Text == "" {
					return
				}
				bubble := ui.Column(c).Shrink(1).Padding(8, 14).Radius(mallow.RadiusSmall)
				mallow.Raised(bubble, mallow.Tiny)
				bubble.Children(func() {
					ui.Text(c, e.Text).FontSize(14).TextColor(mallow.Text).Selectable()
				})
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
		if e.Model != "" {
			ui.Text(c, e.Model).FontSize(12).FontWeight(500).TextColor(mallow.Muted).SingleLine().Padding(0, 0, 4, 0)
		}
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
