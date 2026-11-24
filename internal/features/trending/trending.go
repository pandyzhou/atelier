// Package trending shows the repositories GitHub lists as trending this
// week: read from its page of them, which is all there is to read them
// from, and kept until read again.
package trending

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pandyzhou/atelier/internal/llm"
	"github.com/pandyzhou/atelier/internal/store"
)

// Page is where the week's trending repositories are listed.
const Page = "https://github.com/trending?since=weekly"

// Repo is a repository of the list.
type Repo struct {
	Name        string `json:"name"` // its owner and its name, as "owner/name"
	Description string `json:"description,omitempty"`
	Language    string `json:"language,omitempty"`
	Stars       int    `json:"stars"`
	Forks       int    `json:"forks"`
	Gained      int    `json:"gained"` // the stars it got this week
}

// URL is where the repository is.
func (r Repo) URL() string { return "https://github.com/" + r.Name }

// Options are what the page needs of the app around it.
type Options struct {
	// Fetch returns what is at an address of GitHub's, and ErrNotFound for
	// one there is nothing at: Get, but in tests. Without it nothing is
	// read anew.
	Fetch func(ctx context.Context, url string) ([]byte, error)
	// Background runs work off the UI's thread, then what it returns on it.
	Background func(work func() func())
	// Model returns the model to ask, and whether one is set, and Complete
	// asks it, giving onText the answer's text as it comes, off the UI's
	// thread; OnUI runs fn on the UI's thread, and shows the window anew.
	// They are for translating READMEs: without them none is.
	Model    func() (llm.Config, bool)
	Complete func(ctx context.Context, c llm.Config, r llm.Request, onText func(string)) (llm.Message, error)
	OnUI     func(fn func())
}

// data is what is saved: the list as it was last read, and when.
type data struct {
	Repos   []Repo `json:"repos"`
	Fetched string `json:"fetched,omitempty"` // RFC 3339
	// Translations are the READMEs put into Chinese, the newest first.
	Translations []translation `json:"translations,omitempty"`
}

// Trending is the feature.
type Trending struct {
	Options
	store *store.Store
	data  data
	now   func() time.Time
	ui    viewState
}

// New returns the page of the week's trending repositories, with the list
// last read saved in s.
func New(s *store.Store, o Options) *Trending {
	t := &Trending{Options: o, store: s, now: time.Now}
	if err := s.Load(t.ID(), &t.data); err != nil {
		log.Printf("trending: load: %v", err)
	}
	return t
}

func (t *Trending) ID() string    { return "trending" }
func (t *Trending) Title() string { return "热门" }

// Repos returns the list as it was last read.
func (t *Trending) Repos() []Repo { return t.data.Repos }

// stale is how old the list may be before it is read anew when the page
// shows, and retry how long after a reading that failed the next is tried
// by itself.
const (
	stale = 6 * time.Hour
	retry = 10 * time.Minute
)

// fetched is when the list was last read, zero for never.
func (t *Trending) fetched() time.Time {
	at, _ := time.Parse(time.RFC3339, t.data.Fetched)
	return at
}

// due reports whether the list is to be read anew by itself: it is old, or
// was never read, and no reading is under way or failed just now.
func (t *Trending) due() bool {
	if t.Fetch == nil || t.ui.loading {
		return false
	}
	now := t.now()
	if !t.ui.tried.IsZero() && now.Sub(t.ui.tried) < retry {
		return false
	}
	return now.Sub(t.fetched()) > stale
}

// Refresh reads the list anew; what was read before stays if that fails,
// with a word of why.
func (t *Trending) Refresh() {
	if t.Fetch == nil || t.ui.loading {
		return
	}
	t.ui.loading, t.ui.problem, t.ui.tried = true, "", t.now()
	t.Background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		page, err := t.Fetch(ctx, Page)
		var repos []Repo
		if err == nil {
			if repos = Parse(page); len(repos) == 0 {
				err = errors.New("GitHub 的热门页面读不出仓库，页面可能改版了")
			}
		}
		return func() {
			t.ui.loading = false
			if err != nil {
				t.ui.problem = "没有刷新成功：" + err.Error()
				return
			}
			t.data.Repos, t.data.Fetched = repos, t.now().Format(time.RFC3339)
			if err := t.store.Save(t.ID(), &t.data); err != nil {
				log.Printf("trending: save: %v", err)
			}
		}
	})
}

// Get reads what is at an address, as any browser without an account
// would: GitHub's page of trending repositories, a file of a repository, a
// picture of its README.
func Get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// GitHub's pages are not served to a client that does not say what it is.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; desktop-app)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("连不上，检查一下网络")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("对方回了 %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPicture+1))
	if err != nil {
		return nil, errors.New("读到一半断了")
	}
	return data, nil
}

// What is read of a repository's part of the page.
var (
	article  = regexp.MustCompile(`(?s)<article[^>]*class="[^"]*Box-row[^"]*"[^>]*>(.*?)</article>`)
	heading  = regexp.MustCompile(`(?s)<h2[^>]*>.*?<a[^>]*href="/([^"/?#]+/[^"/?#]+)"`)
	about    = regexp.MustCompile(`(?s)<p(?:\s[^>]*)?>(.*?)</p>`)
	language = regexp.MustCompile(`(?s)itemprop="programmingLanguage"[^>]*>(.*?)<`)
	gained   = regexp.MustCompile(`([\d,]+)\s+stars?\s+this\s+week`)
	tag      = regexp.MustCompile(`(?s)<svg.*?</svg>|<[^>]*>`)
)

// text is the text of a piece of HTML, on one line.
func text(piece string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tag.ReplaceAllString(piece, " "))), " ")
}

// number reads a count as the page writes it, as "14,814": 0 for none.
func number(s string) int {
	n, _ := strconv.Atoi(strings.ReplaceAll(strings.TrimSpace(s), ",", ""))
	return n
}

// Parse reads the repositories listed in the HTML of Page, in its order:
// none of a page that is not laid out as it was.
func Parse(page []byte) []Repo {
	var repos []Repo
	for _, m := range article.FindAllSubmatch(page, -1) {
		part := string(m[1])
		name := heading.FindStringSubmatch(part)
		if name == nil {
			continue
		}
		r := Repo{Name: name[1]}
		if p := about.FindStringSubmatch(part); p != nil {
			r.Description = text(p[1])
		}
		if l := language.FindStringSubmatch(part); l != nil {
			r.Language = text(l[1])
		}
		// The counts are the texts of the links to who starred and forked it.
		count := func(what string) int {
			link := regexp.MustCompile(`(?s)href="/` + regexp.QuoteMeta(r.Name) + `/` + what + `"[^>]*>(.*?)</a>`)
			if c := link.FindStringSubmatch(part); c != nil {
				return number(text(c[1]))
			}
			return 0
		}
		r.Stars, r.Forks = count("stargazers"), count("forks")
		if g := gained.FindStringSubmatch(part); g != nil {
			r.Gained = number(g[1])
		}
		repos = append(repos, r)
	}
	return repos
}
