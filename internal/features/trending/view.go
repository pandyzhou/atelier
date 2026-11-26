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
	// open is the repository whose page shows, "" for the list; readmes
	// are the READMEs read, by repository, and doc what shows one.
	open    string
	readmes map[string]*readme
	// docs are what show the READMEs, one for a repository: each holds
	// the pictures it loaded, by addresses that are the README's own.
	docs map[string]*mallow.Document
	// translating is the translation under way, nil for none, and runs
	// counts those begun; original is whether the README shows as it was
	// written though there is a translation of it; translateProblem is why
	// the last translation failed.
	translating      *translating
	runs             int
	original         bool
	translateProblem string
	// chinese are the parts of the translation that shows, made of so
	// much text of a repository's, for a dark palette or a light one.
	chinese struct {
		name  string
		of    int
		dark  bool
		parts []part
	}
}

// View builds the page: one card, a heading over the list. It reads the
// list anew by itself when what it has is old.
func (t *Trending) View(c *ui.Context) {
	if t.due() {
		t.Refresh()
	}
	ui.Row(c).Fill().Padding(0, 28, 0, 16).AlignItems(ui.Stretch).Children(func() {
		mallow.Card(c, mallow.Medium).Grow(1).MinWidth(0).Gap(12).Padding(24, 4, 0, 4).Children(func() {
			if r, ok := t.opened(); ok {
				t.page(c, r)
				return
			}
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

// list is the repositories, in the page's order: a click opens one's page.
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
			facts(c, r, false)
		})
	})
	if b.Clicked() {
		t.Open(r.Name)
	}
}

// translateButton is what puts a repository's README into Chinese: it
// asks the model, stops it while it writes, and once there is a
// translation goes between it and the README as it was written.
func (t *Trending) translateButton(c *ui.Context, name string) {
	about := t.ui.readmes[name]
	if !t.CanTranslate(name) {
		return
	}
	switch job := t.ui.translating; {
	case job != nil && job.name == name:
		if mallow.Button(c, "停止翻译").Clicked() {
			t.StopTranslating()
		}
	case t.translated(name, about) == "":
		if mallow.Button(c, "翻译成中文").Clicked() {
			t.Translate(name)
		}
	case t.ui.original:
		if mallow.Button(c, "看中文").Clicked() {
			t.ui.original = false
		}
	default:
		if mallow.Button(c, "看原文").Clicked() {
			t.ui.original = true
		}
	}
}

// showing returns the parts of a repository's README that show: of its
// translation as far as it has come, or as it was kept, unless the README
// as it was written is asked for or there is none.
func (t *Trending) showing(name string, about *readme) []part {
	dark := mallow.IsDark()
	text := ""
	if job := t.ui.translating; job != nil && job.name == name {
		if text = job.text; strings.TrimSpace(text) == "" {
			return []part{{text: "*正在翻译…*"}}
		}
	} else if !t.ui.original {
		text = t.translated(name, about)
	}
	if text == "" {
		return about.shown(dark)
	}
	if k := &t.ui.chinese; k.name != name || k.of != len(text) || k.dark != dark || k.parts == nil {
		k.name, k.of, k.dark, k.parts = name, len(text), dark, Parts(Tidy(text, dark))
	}
	return t.ui.chinese.parts
}

// facts is a repository's language, its stars, its forks, and how many of
// its stars are this week's, on a line.
func facts(c *ui.Context, r Repo, forks bool) {
	ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
		if r.Language != "" {
			ui.Text(c, r.Language).FontSize(12).TextColor(mallow.Muted).SingleLine()
		}
		ui.Text(c, "★ "+grouped(r.Stars)).FontSize(12).TextColor(mallow.Muted).SingleLine()
		if forks {
			ui.Text(c, "复刻 "+grouped(r.Forks)).FontSize(12).TextColor(mallow.Muted).SingleLine()
		}
		if r.Gained > 0 {
			ui.Text(c, "本周 +"+grouped(r.Gained)).FontSize(12).FontWeight(500).TextColor(mallow.Accent).SingleLine()
		}
	})
}

// page is a repository's own page: the way back, what the list says of
// it, the button that puts its README into Chinese, and its README.
func (t *Trending) page(c *ui.Context, r Repo) {
	owner, name, _ := strings.Cut(r.Name, "/")
	services := c.Services()
	ui.Row(c).Shrink(0).Padding(0, 28, 0, 24).Gap(14).AlignItems(ui.Start).Children(func() {
		if mallow.IconButton(c, "返回热门", mallow.GlyphChevronLeft, mallow.IconButtonStyle{Size: 36}).Clicked() {
			t.Close()
		}
		// Whose it is and its name, beside the button, which goes under
		// them where the page is too narrow for both; then what it is.
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			ui.Row(c).Wrap().Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Row(c).Grow(1).Basis(280).MinWidth(0).Gap(6).AlignItems(ui.Baseline).Children(func() {
					ui.Text(c, owner+" /").FontSize(18).TextColor(mallow.Muted).SingleLine().Shrink(1).MinWidth(0)
					ui.Text(c, name).FontSize(22).FontWeight(600).TextColor(mallow.Text).SingleLine().Shrink(1).MinWidth(0)
				})
				ui.Row(c).Shrink(0).Gap(12).Children(func() { t.translateButton(c, r.Name) })
			})
			if r.Description != "" {
				ui.Text(c, r.Description).FontSize(13).TextColor(mallow.Text).MaxLines(3)
			}
			facts(c, r, true)
		})
	})
	if t.ui.translateProblem != "" {
		ui.Text(c, t.ui.translateProblem).FontSize(13).TextColor(mallow.Danger).Padding(0, 28, 0, 74)
	}
	mallow.Groove(c).Margin(0, 24)
	about := t.ui.readmes[r.Name]
	word := ""
	switch {
	case about == nil:
		word = "这里读不了 README。"
	case about.loading:
		word = "正在读取 README…"
	case about.problem != "":
		word = about.problem
	case about.missing:
		word = "这个仓库没有 README。"
	}
	if word != "" {
		ui.Column(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Gap(12).Padding(0, 28, 48, 28).Children(func() {
			ui.Text(c, word).FontSize(14).TextColor(mallow.Muted)
			if about != nil && about.problem != "" && mallow.Button(c, "再试一次").Clicked() {
				t.Open(r.Name)
			}
		})
		return
	}
	// Its pictures are loaded from where the repository's files are, and
	// its links go to the browser: to the repository's pages on GitHub
	// when they are written as paths within it.
	file := about.file
	if t.ui.docs == nil {
		t.ui.docs = map[string]*mallow.Document{}
	}
	doc := t.ui.docs[r.Name]
	if doc == nil {
		doc = &mallow.Document{}
		t.ui.docs[r.Name] = doc
	}
	doc.LoadImage = func(address string) (ui.ImageSource, error) { return t.loadPicture(r.Name, file, address) }
	// They show at the size their README gives them, where it does.
	doc.Size = pictureSize
	doc.OnLink = func(address string) {
		if strings.HasPrefix(address, "#") {
			return
		}
		if to := resolve("https://github.com/", r.Name+"/blob/HEAD", file, address); ui.SafeToOpen(to) {
			services.OpenURL(to)
		}
	}
	// The room at its right is the scroll bar's.
	ui.Scroll(c.Key("readme:"+r.Name)).Label("README").Grow(1).MinHeight(0).Padding(4, 24, 24, 28).Children(func() {
		ui.Row(c).Justify(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).MaxWidth(860).Gap(12).Children(func() {
				for i, p := range t.showing(r.Name, about) {
					if !p.centered {
						doc.View(c.Key(i), p.text)
						continue
					}
					// In the middle, as its README has it.
					ui.Column(c.Key(i)).AlignItems(ui.Center).TextAlign(ui.Center).Children(func() {
						doc.View(c, p.text)
					})
				}
			})
		})
	})
}

// grouped writes a count with its thousands apart, as 14,814.
func grouped(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
