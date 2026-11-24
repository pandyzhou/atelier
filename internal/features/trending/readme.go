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
	text    string // as it was read
	plain   bool   // it is not Markdown: it shows as it is written
	// parts are the text tidied to show on a dark palette or a light one,
	// whichever dark says they were made for.
	parts   []part
	dark    bool
	file    string // where in the repository it is, as "docs/README.md"
	problem string // why it could not be read
	missing bool   // the repository has none
}

// Open shows a repository's page, and reads its README unless that was read.
func (t *Trending) Open(name string) {
	t.ui.open, t.ui.original, t.ui.translateProblem = name, false, ""
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
		got := t.fetchReadme(ctx, name)
		return func() { *r = got }
	})
}

// fetchReadme reads a repository's README from GitHub, wherever it is
// kept. It changes nothing, and can be called off the UI's thread.
func (t *Trending) fetchReadme(ctx context.Context, name string) readme {
	for _, file := range readmes {
		data, err := t.Fetch(ctx, raw+name+"/HEAD/"+file)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return readme{problem: "没有读到 README：" + err.Error()}
		}
		if len(data) > maxReadme {
			data = data[:maxReadme]
		}
		if !utf8.Valid(data) {
			data = []byte(strings.ToValidUTF8(string(data), ""))
		}
		markdown := strings.HasSuffix(strings.ToLower(file), ".md") || strings.HasSuffix(file, ".markdown")
		return readme{text: string(data), file: file, plain: !markdown}
	}
	return readme{missing: true}
}

// maxTold is the most of a README the assistant is given to read.
const maxTold = 24 << 10

// told is a README as the assistant reads it: Markdown without its HTML,
// no longer than a model takes in at once.
func (r readme) told() (string, error) {
	switch {
	case r.problem != "":
		return "", errors.New(r.problem)
	case r.missing:
		return "", errors.New("这个仓库没有 README，或者没有这个仓库")
	}
	text := r.text
	if !r.plain {
		text = strings.NewReplacer(centerStart, "", centerEnd, "").Replace(Tidy(text, false))
	}
	if len(text) > maxTold {
		text = strings.ToValidUTF8(text[:maxTold], "") + "\n\n（README 太长，后面的省略了）"
	}
	return text, nil
}

// named tells the names repositories have, as "owner/name".
var named = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)

// IsRepo reports whether name is written as a repository's is.
func IsRepo(name string) bool { return named.MatchString(name) && len(name) <= 140 }

// Readme returns a repository's README for the assistant to read, when it
// was read already, as by its page: on the UI's thread.
func (t *Trending) Readme(name string) (string, bool) {
	r := t.ui.readmes[name]
	if r == nil || r.loading || r.problem != "" {
		return "", false
	}
	text, err := r.told()
	return text, err == nil
}

// ReadReadme reads a repository's README from GitHub for the assistant.
// It changes nothing, and can be called off the UI's thread.
func (t *Trending) ReadReadme(ctx context.Context, name string) (string, error) {
	if t.Fetch == nil {
		return "", errors.New("这里读不了 GitHub")
	}
	return t.fetchReadme(ctx, name).told()
}

// part is a stretch of a README: Markdown, which its README has in the
// middle of the page or, as most of it, at the left.
type part struct {
	text     string
	centered bool
}

// shown returns the README as it shows on a dark palette or a light one:
// tidied once for each, as the pictures some READMEs have for the two differ.
func (r *readme) shown(dark bool) []part {
	if r.parts != nil && r.dark == dark {
		return r.parts
	}
	r.dark = dark
	if r.plain {
		// Not Markdown: as it is written.
		r.parts = []part{{text: "```\n" + strings.ReplaceAll(r.text, "```", "'''") + "\n```"}}
	} else {
		r.parts = Parts(Tidy(r.text, dark))
	}
	return r.parts
}

// The marks Tidy leaves around what a README has in the middle of the page.
const (
	centerStart = "\uE000"
	centerEnd   = "\uE001"
)

// Parts cuts what Tidy made into its stretches: what is between its marks
// is centered, a paragraph to a part, so that each is in the middle by
// itself; the rest is not.
func Parts(text string) []part {
	var parts []part
	add := func(s string, centered bool) {
		if s = strings.TrimSpace(s); s == "" {
			return
		}
		if !centered {
			parts = append(parts, part{text: s})
			return
		}
		for _, block := range strings.Split(blank.ReplaceAllString(s, "\n\n"), "\n\n") {
			// Its lines without the room they were written after, which
			// would make code of them.
			lines := strings.Split(block, "\n")
			for i := range lines {
				lines[i] = strings.TrimSpace(lines[i])
			}
			if block = strings.TrimSpace(strings.Join(lines, "\n")); block != "" {
				parts = append(parts, part{text: block, centered: true})
			}
		}
	}
	for {
		i := strings.Index(text, centerStart)
		if i < 0 {
			break
		}
		add(text[:i], false)
		text = text[i+len(centerStart):]
		j := strings.Index(text, centerEnd)
		if j < 0 {
			j = len(text)
		}
		add(text[:j], true)
		text = strings.TrimPrefix(text[j:], centerEnd)
	}
	add(strings.ReplaceAll(text, centerEnd, ""), false)
	return parts
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
	switch src.(type) {
	case *ui.SVG:
		// A drawing is as sharp at any size: it is told its own.
		if sized, err := ui.ParseSVG(resized(data, w, h, width, height)); err == nil {
			return sized, nil
		}
	case *ui.Bitmap:
		// A small one, as an icon in a row of them, is made that small.
		// A large one shows as it is, at most as wide as the page: made
		// smaller here it would show blurred where the screen has more
		// pixels than the page counts.
		if float32(width) < w && max(width, height) <= smallPicture {
			return ui.DecodeBitmapScaled(data, max(width, height))
		}
	}
	return src, nil
}

// smallPicture is the longest side, in DIPs, of the pictures that are made
// as small as their README says.
const smallPicture = 160

// root is the tag an SVG begins with.
var root = regexp.MustCompile(`(?is)<svg\b[^>]*>`)

// resized returns an SVG that says it is width by height, of one that says
// it is w by h: what it draws is the same, in the room of the new size.
func resized(data []byte, w, h float32, width, height int) []byte {
	return root.ReplaceAllFunc(data, func(tag []byte) []byte {
		t := string(tag)
		box := ""
		if attribute(t, "viewBox") == "" {
			box = fmt.Sprintf(` viewBox="0 0 %g %g"`, w, h)
		}
		t = regexp.MustCompile(`(?is)\s(width|height)\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`).ReplaceAllString(t, "")
		end := ">"
		if strings.HasSuffix(t, "/>") {
			end = "/>"
		}
		return []byte(strings.TrimSuffix(t, end) + fmt.Sprintf(` width="%d" height="%d"`, width, height) + box + end)
	})
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
	// alone is a tag that goes without a trace, on a line of its own.
	alone = regexp.MustCompile(`(?im)^[ \t]*</?(span|picture|font|sub|sup|small|big|source|nav|main|svg|path|g)\b[^>\n]*>[ \t]*\n`)
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
// they are written. Code is left as it is. What the README has in the
// middle of the page is between marks, for Parts; of pictures it has one of
// for dark pages and one for light ones, it keeps the one for dark's.
func Tidy(src string, dark bool) string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.NewReplacer(centerStart, "", centerEnd, "").Replace(src)
	var out strings.Builder
	// Between the fences of code blocks, and outside the code in a line.
	inCode, at := false, 0
	prose := func(s string) {
		last := 0
		for _, m := range code.FindAllStringIndex(s, -1) {
			out.WriteString(tidy(s[last:m[0]], dark))
			out.WriteString(s[m[0]:m[1]])
			last = m[1]
		}
		out.WriteString(tidy(s[last:], dark))
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

// What tidy reads of pictures made for one palette, and of what is centered.
var (
	pictured = regexp.MustCompile(`(?is)<picture\b[^>]*>(.*?)</picture>`)
	sourced  = regexp.MustCompile(`(?is)<source\b[^>]*>`)
	centered = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<center\b[^>]*>(.*?)</center>`),
		regexp.MustCompile(`(?is)<p\b[^>]*\balign\s*=\s*["']?center\b[^>]*>(.*?)</p>`),
		regexp.MustCompile(`(?is)<div\b[^>]*\balign\s*=\s*["']?center\b[^>]*>(.*?)</div>`),
	}
	centeredTitle = regexp.MustCompile(`(?is)<h([1-6])\b[^>]*\balign\s*=\s*["']?center\b[^>]*>(.*?)</h[1-6]>`)
)

// tidy is Tidy for text that holds no code.
func tidy(s string, dark bool) string {
	s = comment.ReplaceAllString(s, "")
	// Of a picture with sources for dark pages or light ones, the one for
	// the page that shows: its img's, unless a source is for this page.
	s = pictured.ReplaceAllStringFunc(s, func(whole string) string {
		img := image.FindString(whole)
		if img == "" {
			return ""
		}
		for _, source := range sourced.FindAllString(whole, -1) {
			media, set := strings.ToLower(attribute(source, "media")), strings.Fields(attribute(source, "srcset"))
			if len(set) > 0 && ((dark && strings.Contains(media, "dark")) || (!dark && strings.Contains(media, "light"))) {
				at := strings.TrimSuffix(set[0], ",")
				return regexp.MustCompile(`(?is)\bsrc\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)`).ReplaceAllLiteralString(img, `src="`+at+`"`)
			}
		}
		return img
	})
	// What is in the middle of the page, between the marks: a heading
	// there stays one.
	s = centeredTitle.ReplaceAllString(s, "\n\n"+centerStart+"\n<h$1>$2</h$1>\n"+centerEnd+"\n\n")
	for _, c := range centered {
		s = c.ReplaceAllString(s, "\n\n"+centerStart+"\n$1\n"+centerEnd+"\n\n")
	}
	// A line that only held such a tag would be left empty, and part the
	// paragraph around it in two.
	s = alone.ReplaceAllString(s, "")
	s = image.ReplaceAllStringFunc(s, func(tag string) string {
		src := attribute(tag, "src")
		// GitHub shows a picture whose address ends so on dark pages only,
		// or on light ones only.
		for mark, forDark := range map[string]bool{"#gh-dark-mode-only": true, "#gh-light-mode-only": false} {
			if rest, ok := strings.CutSuffix(src, mark); ok {
				if src = rest; forDark != dark {
					return ""
				}
			}
		}
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
