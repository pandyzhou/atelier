package trending

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

// A repository opens on a page of its own: what the list says of it, over
// its README, read from where GitHub serves the files of repositories as
// they are, which needs no account and counts no requests.

// raw is where the files of a repository are served, at its default branch.
const raw = "https://raw.githubusercontent.com/"

// ErrNotFound is what Fetch returns for an address there is nothing at.
var ErrNotFound = errors.New("not found")

// readmes are where a repository's README is looked for, in this order.
var readmes = []string{"README.md", "readme.md", "Readme.md", "README.MD", "README.markdown", "README.rst", "README.txt", "README", "docs/README.md", ".github/README.md"}

// maxReadme is the most of a README that is shown.
const maxReadme = 512 << 10

// readme is a repository's README, as far as it has been read.
type readme struct {
	loading bool
	text    string // as Markdown, tidied to show
	file    string // where in the repository it is, as "docs/README.md"
	problem string // why it could not be read
	missing bool   // the repository has none
}

// Open shows a repository's page, and reads its README unless that was read.
func (t *Trending) Open(name string) {
	t.ui.open = name
	if t.Fetch == nil {
		return
	}
	if r := t.ui.readmes[name]; r != nil && (r.loading || r.problem == "") {
		return
	}
	if t.ui.readmes == nil {
		t.ui.readmes = map[string]*readme{}
	}
	r := &readme{loading: true}
	t.ui.readmes[name] = r
	t.Background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		got := readme{missing: true}
		for _, file := range readmes {
			data, err := t.Fetch(ctx, raw+name+"/HEAD/"+file)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				got = readme{problem: "没有读到 README：" + err.Error()}
				break
			}
			if len(data) > maxReadme {
				data = data[:maxReadme]
			}
			if !utf8.Valid(data) {
				data = []byte(strings.ToValidUTF8(string(data), ""))
			}
			text := string(data)
			if strings.HasSuffix(strings.ToLower(file), ".md") || strings.HasSuffix(file, ".markdown") {
				text = Tidy(text)
			} else {
				// Not Markdown: as it is written.
				text = "```\n" + strings.ReplaceAll(text, "```", "'''") + "\n```"
			}
			got = readme{text: text, file: file}
			break
		}
		return func() { *r = got }
	})
}

// Close goes back from a repository's page to the list.
func (t *Trending) Close() { t.ui.open = "" }

// opened returns the repository whose page shows, and whether one does:
// one that is still on the list.
func (t *Trending) opened() (Repo, bool) {
	for _, r := range t.data.Repos {
		if r.Name == t.ui.open {
			return r, true
		}
	}
	return Repo{}, false
}

// resolve makes an address written in a repository's README, which may be
// relative to where the README is, a whole one: under base, the files as
// they are for pictures, or their pages on GitHub for links.
func resolve(base, name, file, address string) string {
	address = strings.TrimSpace(address)
	switch {
	case address == "", strings.HasPrefix(address, "#"):
		return address
	case strings.HasPrefix(address, "//"):
		return "https:" + address
	case strings.Contains(address, "://"), strings.HasPrefix(address, "mailto:"), strings.HasPrefix(address, "data:"):
		return address
	}
	rest := ""
	if i := strings.IndexAny(address, "?#"); i >= 0 {
		address, rest = address[:i], address[i:]
	}
	dir := path.Dir(file)
	if strings.HasPrefix(address, "/") {
		dir = ""
	}
	at := strings.TrimPrefix(path.Clean("/"+path.Join(dir, address)), "/")
	return base + name + "/" + at + rest
}

// picture is the address a picture of a README is loaded from: that of
// the file itself, where it was written as its page on GitHub.
func picture(name, file, address string) string {
	address = resolve(raw, name+"/HEAD", file, address)
	if rest, ok := strings.CutPrefix(address, "https://github.com/"); ok {
		// owner/name/blob/branch/path, or …/raw/branch/path.
		if parts := strings.SplitN(rest, "/", 4); len(parts) == 4 && (parts[2] == "blob" || parts[2] == "raw") {
			return raw + parts[0] + "/" + parts[1] + "/" + strings.TrimSuffix(parts[3], "?raw=true")
		}
	}
	return address
}

// maxPicture is the largest picture of a README that is loaded.
const maxPicture = 8 << 20

// loadPicture loads a picture of the README of the repository that shows.
func (t *Trending) loadPicture(name, file, address string) (ui.ImageSource, error) {
	width, height := 0, 0
	if i := strings.Index(address, sized); i >= 0 {
		fmt.Sscanf(address[i+len(sized):], "%dx%d", &width, &height)
		address = address[:i]
	}
	address = picture(name, file, address)
	if !strings.HasPrefix(address, "https://") && !strings.HasPrefix(address, "http://") {
		return nil, errors.New("not on the web")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := t.Fetch(ctx, address)
	if err != nil {
		return nil, err
	}
	if len(data) > maxPicture {
		return nil, errors.New("too large")
	}
	src, err := ui.DecodeImage(data)
	if err != nil || (width == 0 && height == 0) {
		return src, err
	}
	// At the size its README gives it, keeping its shape where only one
	// side is given.
	w, h := ui.ImageSize(src)
	if w <= 0 || h <= 0 {
		return src, nil
	}
	switch {
	case width == 0:
		width = max(1, int(w*float32(height)/h))
	case height == 0:
		height = max(1, int(h*float32(width)/w))
	}
	switch src := src.(type) {
	case *ui.SVG:
		return ui.NewBitmap(src.Image(width, height, nil)), nil
	case *ui.Bitmap:
		if float32(width) < w {
			return ui.DecodeBitmapScaled(data, max(width, height))
		}
	}
	return src, nil
}

// What Tidy reads of the HTML that READMEs are written with.
var (
	fence    = regexp.MustCompile("(?m)^[ \t]*(```|~~~)")
	code     = regexp.MustCompile("`[^`\n]*`")
	comment  = regexp.MustCompile(`(?s)<!--.*?-->`)
	image    = regexp.MustCompile(`(?is)<img\b([^>]*)>`)
	anchor   = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	title    = regexp.MustCompile(`(?is)<h([1-6])\b[^>]*>(.*?)</h[1-6]>`)
	strong   = regexp.MustCompile(`(?i)</?(b|strong)\b[^>]*>`)
	emphasis = regexp.MustCompile(`(?i)</?(i|em)\b[^>]*>`)
	typed    = regexp.MustCompile(`(?i)</?(code|kbd|samp|tt)\b[^>]*>`)
	breaks   = regexp.MustCompile(`(?i)<br\b[^>]*>`)
	item     = regexp.MustCompile(`(?i)<li\b[^>]*>`)
	parted   = regexp.MustCompile(`(?i)</?(p|div|center|details|summary|section|article|header|footer|table|thead|tbody|tr|ul|ol|blockquote|figure|figcaption)\b[^>]*>`)
	dropped  = regexp.MustCompile(`(?i)</?(span|picture|font|sub|sup|small|big|u|ins|del|s|strike|source|td|th|li|nav|main|a|video|audio|iframe|hr|wbr|abbr|mark|label|svg|path|g)\b[^>]*>`)
	blank    = regexp.MustCompile(`\n{3,}`)
)

// sized is what follows a picture's address when its README says how
// large it shows: its width and height in DIPs, 0 for one left open.
const sized = "#shown-at="

// pixels reads a size written in a tag, as "28" or "28px": 0 for none, or
// one that is not in pixels.
func pixels(s string) int {
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(s), "px"))
	if err != nil || n <= 0 || n > 4000 {
		return 0
	}
	return n
}

// attribute reads an attribute of a tag from what is written in it.
func attribute(in, name string) string {
	m := regexp.MustCompile(`(?is)\b` + name + `\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`).FindStringSubmatch(in)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1] + m[2] + m[3])
}

// Tidy makes of a README's Markdown, which may be written with HTML, as
// GitHub lets it be, Markdown alone: its pictures, links and headings as
// Markdown's, and the other tags gone, since what shows it shows them as
// they are written. Code is left as it is.
func Tidy(src string) string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var out strings.Builder
	// Between the fences of code blocks, and outside the code in a line.
	inCode, at := false, 0
	prose := func(s string) {
		last := 0
		for _, m := range code.FindAllStringIndex(s, -1) {
			out.WriteString(tidy(s[last:m[0]]))
			out.WriteString(s[m[0]:m[1]])
			last = m[1]
		}
		out.WriteString(tidy(s[last:]))
	}
	for _, m := range fence.FindAllStringIndex(src, -1) {
		if !inCode {
			prose(src[at:m[0]])
		} else {
			out.WriteString(src[at:m[0]])
		}
		at, inCode = m[0], !inCode
	}
	if inCode {
		out.WriteString(src[at:])
	} else {
		prose(src[at:])
	}
	return strings.TrimSpace(blank.ReplaceAllString(out.String(), "\n\n"))
}

// tidy is Tidy for text that holds no code.
func tidy(s string) string {
	s = comment.ReplaceAllString(s, "")
	s = image.ReplaceAllStringFunc(s, func(tag string) string {
		src := attribute(tag, "src")
		if src == "" {
			return ""
		}
		alt := strings.NewReplacer("[", "", "]", "", "\n", " ").Replace(attribute(tag, "alt"))
		// The size it is to show at, which Markdown has no way to say,
		// goes with its address, for loadPicture.
		if w, h := pixels(attribute(tag, "width")), pixels(attribute(tag, "height")); (w > 0 || h > 0) && !strings.Contains(src, "#") {
			src += fmt.Sprintf("%s%dx%d", sized, w, h)
		}
		return "![" + alt + "](" + strings.ReplaceAll(src, " ", "%20") + ")"
	})
	s = anchor.ReplaceAllStringFunc(s, func(tag string) string {
		m := anchor.FindStringSubmatch(tag)
		href, inner := attribute(m[1], "href"), strings.TrimSpace(m[2])
		if href == "" || inner == "" {
			return inner
		}
		return "[" + strings.Join(strings.Fields(inner), " ") + "](" + strings.ReplaceAll(href, " ", "%20") + ")"
	})
	s = title.ReplaceAllStringFunc(s, func(tag string) string {
		m := title.FindStringSubmatch(tag)
		return "\n\n" + strings.Repeat("#", int(m[1][0]-'0')) + " " + strings.Join(strings.Fields(breaks.ReplaceAllString(m[2], " ")), " ") + "\n\n"
	})
	s = strong.ReplaceAllString(s, "**")
	s = emphasis.ReplaceAllString(s, "*")
	s = typed.ReplaceAllString(s, "`")
	s = breaks.ReplaceAllString(s, "  \n")
	s = item.ReplaceAllString(s, "\n- ")
	s = parted.ReplaceAllString(s, "\n\n")
	s = dropped.ReplaceAllString(s, "")
	// What was indented as HTML is not code.
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if trimmed := strings.TrimLeft(l, " \t"); len(l)-len(trimmed) >= 4 && (strings.HasPrefix(trimmed, "![") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "#")) {
			lines[i] = trimmed
		}
	}
	return strings.Join(lines, "\n")
}
