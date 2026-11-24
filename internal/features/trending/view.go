package trending

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"
)

// viewState is what the page shows besides the list itself.
type viewState struct {
	loading bool      // the list is being read
	problem string    // why the last reading failed, "" if it did not
	tried   time.Time // when a reading was last begun
}

// View builds the page: one card, a heading over the list. It reads the
// list anew by itself when what it has is old.
func (t *Trending) View(c *ui.Context) {
	if t.due() {
		t.Refresh()
	}
	ui.Row(c).Fill().Padding(0, 28, 0, 16).AlignItems(ui.Stretch).Children(func() {
		mallow.Card(c, mallow.Medium).Grow(1).MinWidth(0).Gap(12).Padding(24, 4, 0, 4).Children(func() {
			ui.Column(c).PaddingX(28).Children(func() { t.heading(c) })
			t.list(c)
		})
	})
}

// heading is the page's title, when the list was read, and the button that
// reads it anew.
func (t *Trending) heading(c *ui.Context) {
	note := "还没有读过"
	switch at := t.fetched(); {
	case t.ui.loading:
		note = "正在刷新…"
	case !at.IsZero():
		note = "更新于 " + t.when(at) + " · 来自 github.com/trending"
	}
	ui.Row(c).Shrink(0).PaddingX(4).Gap(16).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			ui.Text(c, "GitHub 每周热门").FontSize(28).FontWeight(600).TextColor(mallow.Text).SingleLine()
			ui.Text(c, note).FontSize(14).TextColor(mallow.Muted).SingleLine()
			if t.ui.problem != "" {
				ui.Text(c, t.ui.problem).FontSize(13).TextColor(mallow.Danger)
			}
		})
		if t.Fetch != nil && mallow.IconButton(c, "刷新热门", mallow.GlyphRepeat, mallow.IconButtonStyle{Size: 36}).Disabled(t.ui.loading).Clicked() {
			t.Refresh()
		}
	})
}

// when writes a time for the heading: the time of day if it is of today,
// else the day too.
func (t *Trending) when(at time.Time) string {
	at = at.In(t.now().Location())
	if at.Format("20060102") == t.now().Format("20060102") {
		return "今天 " + at.Format("15:04")
	}
	return fmt.Sprintf("%d月%d日 %s", int(at.Month()), at.Day(), at.Format("15:04"))
}

// list is the repositories, in the page's order: a click opens one in the
// browser.
func (t *Trending) list(c *ui.Context) {
	if len(t.data.Repos) == 0 {
		note := "这里会列出 GitHub 这一周最受关注的仓库。"
		if t.ui.loading {
			note = "正在读取 GitHub 的热门页面…"
		}
		ui.Column(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Padding(0, 28, 48, 28).Children(func() {
			ui.Text(c, note).FontSize(14).TextColor(mallow.Muted)
		})
		return
	}
	// The room at its right is the scroll bar's.
	ui.Scroll(c).Label("热门仓库").Grow(1).MinHeight(0).Padding(0, 24, 16, 20).Gap(2).Children(func() {
		for i, r := range t.data.Repos {
			t.row(c.Key(r.Name), i+1, r)
		}
	})
}

// row is a repository: its place, whose it is and its name, what it is,
// and its language, its stars, and how many of them are this week's.
func (t *Trending) row(c *ui.Context, place int, r Repo) {
	owner, name, _ := strings.Cut(r.Name, "/")
	b := mallow.Plain(c, "打开 "+r.Name).Padding(12, 12).Gap(14).AlignItems(ui.Start).Radius(mallow.RadiusSmall)
	if b.Hovered() {
		b.Background(mallow.Text.Alpha(0.04))
	}
	b.Children(func() {
		ui.Text(c, strconv.Itoa(place)).Width(24).Shrink(0).FontSize(14).FontWeight(600).TextColor(mallow.Muted).TextAlign(ui.End)
		ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
			ui.Row(c).Gap(5).AlignItems(ui.Baseline).Children(func() {
				ui.Text(c, owner+" /").FontSize(14).TextColor(mallow.Muted).SingleLine().Shrink(1).MinWidth(0)
				ui.Text(c, name).FontSize(15).FontWeight(600).TextColor(mallow.Accent).SingleLine().Shrink(1).MinWidth(0)
			})
			if r.Description != "" {
				ui.Text(c, r.Description).FontSize(13).TextColor(mallow.Text).MaxLines(2)
			}
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				if r.Language != "" {
					ui.Text(c, r.Language).FontSize(12).TextColor(mallow.Muted).SingleLine()
				}
				ui.Text(c, "★ "+grouped(r.Stars)).FontSize(12).TextColor(mallow.Muted).SingleLine()
				if r.Gained > 0 {
					ui.Text(c, "本周 +"+grouped(r.Gained)).FontSize(12).FontWeight(500).TextColor(mallow.Accent).SingleLine()
				}
			})
		})
	})
	if b.Clicked() {
		c.Services().OpenURL(r.URL())
	}
}

// grouped writes a count with its thousands apart, as 14,814.
func grouped(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
