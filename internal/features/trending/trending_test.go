package trending

import (
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/markdown/parser"
)

// page is GitHub's page of trending repositories, cut down to what is read
// of it: two repositories, the second with no description and no language.
const page = `<html><body><main>
<article class="Box-row">
  <div class="float-right d-flex"><a href="/login?return_to=%2Facme%2Frocket" class="btn-sm btn"><svg><path d="M8 .25a.75.75 0 0 1 .673.418z"></path></svg><span class="d-none d-md-inline">Star</span></a></div>
  <h2 class="h3 lh-condensed">
    <a data-hydro-click="{&quot;event_type&quot;:&quot;explore.click&quot;}" href="/acme/rocket" class="Link"><svg><path d="M2 2.5A2.5"></path></svg>
      <span class="text-normal">
        acme /
</span>
      rocket</a>  </h2>
    <p class="col-9 color-fg-muted my-1 tmp-pr-4">
      Rockets &amp; launchers for <b>agents</b>, with one click.
    </p>
  <div class="f6 color-fg-muted mt-2">
      <span class="d-inline-block ml-0"><span class="repo-language-color" style="background-color: #3178c6"></span>
  <span itemprop="programmingLanguage">TypeScript</span></span>
      <a href="/acme/rocket/stargazers" class="Link Link--muted d-inline-block"><svg><path d="M8"></path></svg>
        14,814</a>
      <a href="/acme/rocket/forks" class="Link Link--muted d-inline-block"><svg><path d="M5"></path></svg>
        1,430</a>
      <span class="d-inline-block float-sm-right"><svg><path d="M8"></path></svg>
        2,395 stars this week</span>
  </div>
</article>
<article class="Box-row">
  <h2 class="h3 lh-condensed"><a href="/solo/tiny.go" class="Link"><span class="text-normal">solo /</span> tiny.go</a></h2>
  <div class="f6 color-fg-muted mt-2">
      <a href="/solo/tiny.go/stargazers" class="Link">987</a>
      <a href="/solo/tiny.go/forks" class="Link">12</a>
      <span class="d-inline-block float-sm-right">1 star this week</span>
  </div>
</article>
</main></body></html>`

// TestParse checks what is read of the page: each repository's name,
// description, language and counts, in the page's order, and nothing of a
// page that is laid out otherwise.
func TestParse(t *testing.T) {
	want := []Repo{
		{Name: "acme/rocket", Description: "Rockets & launchers for agents , with one click.", Language: "TypeScript", Stars: 14814, Forks: 1430, Gained: 2395},
		{Name: "solo/tiny.go", Stars: 987, Forks: 12, Gained: 1},
	}
	if got := Parse([]byte(page)); !reflect.DeepEqual(got, want) {
		t.Errorf("read %+v", got)
	}
	if got := Parse([]byte(`<html><body><h1>Sign in</h1><p>to continue</p></body></html>`)); len(got) != 0 {
		t.Errorf("of another page, read %+v", got)
	}
	if got := grouped(1234567) + " " + grouped(987) + " " + grouped(1000); got != "1,234,567 987 1,000" {
		t.Errorf("counts are written %q", got)
	}
}

// TestTidy checks that the HTML a README is written with becomes Markdown,
// or goes, and that code is left as it is written.
func TestTidy(t *testing.T) {
	src := "<!-- banner -->\n<h1 align=\"center\">\n  <img src=\"docs/logo dark.png\" alt=\"Logo\" width=\"120\"><br>\n  Tiny\n</h1>\n\n" +
		"<p align=\"center\">\n    <a href=\"https://example.com/ci\"><img src='https://img.example.com/ci.svg' alt=\"CI\"></a>\n    <a href=\"docs/guide.md\">Read the <b>guide</b></a>\n</p>\n\n" +
		"Use `Map<K, V>` and <kbd>Ctrl</kbd>, not <span style=\"color:red\">this</span>.\n\n" +
		"```html\n<p align=\"center\"><img src=\"x.png\"></p>\n```\n\n" +
		"<details><summary>More</summary>\n\n- one\n\n</details>\n\n" +
		"<p align=\"center\">\n  <img src=\"a.svg\" height=\"28\" />&nbsp;&nbsp;\n  <picture>\n    <source media=\"(prefers-color-scheme: dark)\" srcset=\"b-dark.svg\" />\n    <img src=\"b.svg\" height=\"28\" />\n  </picture>&nbsp;&nbsp;\n  <img src=\"c.png\" height=\"28\" />\n  <img src=\"sun.png#gh-light-mode-only\"><img src=\"moon.png#gh-dark-mode-only\">\n</p>\n"
	// What was centered is a part for each of its paragraphs, marked; the
	// rest is not.
	show := func(dark bool) string {
		var b strings.Builder
		for _, p := range Parts(Tidy(src, dark)) {
			if p.centered {
				b.WriteString("[centered]\n")
			}
			b.WriteString(p.text + "\n---\n")
		}
		return b.String()
	}
	// A line of pictures is a paragraph apart from the text under it, which
	// would show it as what it is of.
	want := "[centered]\n# ![Logo](docs/logo%20dark.png#shown-at=120x0) Tiny\n---\n" +
		"[centered]\n[![CI](https://img.example.com/ci.svg)](https://example.com/ci)\n---\n[centered]\n[Read the **guide**](docs/guide.md)\n---\n" +
		"Use `Map<K, V>` and `Ctrl`, not this.\n\n" +
		"```html\n<p align=\"center\"><img src=\"x.png\"></p>\n```\n\n" +
		"More\n\n- one\n---\n" +
		"[centered]\n![](a.svg#shown-at=0x28)&nbsp;&nbsp;\n![](b.svg#shown-at=0x28)&nbsp;&nbsp;\n![](c.png#shown-at=0x28)\n![](sun.png)\n---\n"
	if got := show(false); got != want {
		t.Errorf("for a light page, tidied to:\n%s\n\nwant:\n%s", got, want)
	}
	// On a dark page the pictures made for one show in place of the others.
	dark := strings.NewReplacer("b.svg#shown-at=0x28", "b-dark.svg#shown-at=0x28", "sun.png", "moon.png").Replace(want)
	if got := show(true); got != dark {
		t.Errorf("for a dark page, tidied to:\n%s\n\nwant:\n%s", got, dark)
	}
}

// TestTidyLayout checks what is made of the layouts READMEs build with HTML.
// A table written a row to a line, its cells indented, is no code, and each
// cell is a line of the row's paragraph, not run on into the next, as the
// README of n8n-io/nod showed it; a picture over its caption is a paragraph
// apart, which shows it, and pictures with room between them stay a row;
// code indented after a blank line stays code; and the text around code in a
// line keeps its spaces.
func TestTidyLayout(t *testing.T) {
	src := "<table>\n  <tr>\n    <td width=\"50%\"><strong>Every note.</strong><br />See each one.</td>\n" +
		"    <td width=\"50%\"><strong>Drafts.</strong><br />Learns your style.</td>\n  </tr>\n  <tr>\n" +
		"    <td><strong>Tell it once.</strong><br />It uses the new fact.</td>\n  </tr>\n</table>\n\n" +
		"<table><tr>\n  <td><img src=\"logo.svg\" width=\"40\" alt=\"Acme logo\"><br><b>Acme</b><br><a href=\"https://acme.dev\">Docs</a></td>\n" +
		"  <td><img src=\"a.png\" alt=\"A\">&nbsp;<img src=\"b.png\" alt=\"B\"></td>\n</tr></table>\n\n" +
		"Run it:\n\n    <script src=\"x.js\"></script>\n\n" +
		"Use `x` <b>now</b>.\n"
	text := Tidy(src, false)
	for _, want := range []string{
		"See each one.  \n**Drafts.**  \nLearns your style.",
		"**Tell it once.**  \nIt uses the new fact.",
		"Use `x` **now**.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in:\n%s", want, text)
		}
	}
	var code, pictures, captions []string
	doc := parser.Parse(text)
	for b := 0; b < doc.Len(); b++ {
		blk := doc.Block(b)
		switch blk.Kind {
		case parser.CodeBlock:
			code = append(code, strings.TrimSpace(text[blk.Start:blk.End]))
		case parser.Paragraph:
			alone, alts := true, ""
			in := doc.Inlines(b)
			for k := 0; k < len(in); k = in[k].Next {
				switch in[k].Kind {
				case parser.Image:
					alts += parser.PlainText(in[k+1:in[k].Next]) + " "
				case parser.SoftBreak, parser.LineBreak:
				case parser.Text:
					alone = alone && strings.TrimSpace(in[k].Text) == ""
				default:
					alone = false
				}
			}
			if alts != "" && alone {
				pictures = append(pictures, strings.TrimSpace(alts))
			}
			if strings.Contains(text[blk.Start:blk.End], "Acme**") {
				captions = append(captions, text[blk.Start:blk.End])
			}
		}
	}
	if len(code) != 1 || code[0] != `<script src="x.js"></script>` {
		t.Errorf("code: %q", code)
	}
	if len(pictures) != 2 || pictures[0] != "Acme logo" || pictures[1] != "A B" {
		t.Errorf("paragraphs of pictures alone: %q", pictures)
	}
	if len(captions) != 1 || !strings.Contains(captions[0], "[Docs](https://acme.dev)") {
		t.Errorf("the caption: %q", captions)
	}
}

// TestTidyForPage checks that a picture made for dark pages, or for light
// ones, shows on those alone, whether its own address says so or that of the
// link it is in, as the README of n8n-io/nod has it.
func TestTidyForPage(t *testing.T) {
	src := "<p align=\"center\">\n  <a href=\"https://x.dev#gh-dark-mode-only\"><img src=\"hero-dark.png\" alt=\"Hero\"></a>\n" +
		"  <a href=\"https://x.dev#gh-light-mode-only\"><img src=\"hero.png\" alt=\"Hero\"></a>\n</p>\n"
	for dark, want := range map[bool]string{
		true:  "[![Hero](hero-dark.png)](https://x.dev)",
		false: "[![Hero](hero.png)](https://x.dev)",
	} {
		if parts := Parts(Tidy(src, dark)); len(parts) != 1 || parts[0].text != want {
			t.Errorf("on a page dark %v: %+v, want %q", dark, parts, want)
		}
	}
}

// TestAddresses checks where the pictures and the links of a README go:
// relative to where the README is, pictures to the repository's files and
// links to its pages.
func TestAddresses(t *testing.T) {
	for _, c := range []struct{ file, address, picture, link string }{
		{"README.md", "docs/a.png", "https://raw.githubusercontent.com/o/r/HEAD/docs/a.png", "https://github.com/o/r/blob/HEAD/docs/a.png"},
		{"docs/README.md", "../img/a.png?raw=true", "https://raw.githubusercontent.com/o/r/HEAD/img/a.png?raw=true", "https://github.com/o/r/blob/HEAD/img/a.png?raw=true"},
		{"docs/README.md", "/LICENSE", "https://raw.githubusercontent.com/o/r/HEAD/LICENSE", "https://github.com/o/r/blob/HEAD/LICENSE"},
		{"README.md", "https://github.com/o/r/blob/main/shot.png", "https://raw.githubusercontent.com/o/r/main/shot.png", "https://github.com/o/r/blob/main/shot.png"},
		{"README.md", "https://github.com/o/r/raw/main/shot.png?raw=true", "https://raw.githubusercontent.com/o/r/main/shot.png", "https://github.com/o/r/raw/main/shot.png?raw=true"},
		{"README.md", "//cdn.example.com/a.png", "https://cdn.example.com/a.png", "https://cdn.example.com/a.png"},
		{"README.md", "https://example.com/a.png", "https://example.com/a.png", "https://example.com/a.png"},
	} {
		if got := picture("o/r", c.file, c.address); got != c.picture {
			t.Errorf("the picture %q of %s is loaded from %q", c.address, c.file, got)
		}
		if got := resolve("https://github.com/", "o/r/blob/HEAD", c.file, c.address); got != c.link {
			t.Errorf("the link %q of %s goes to %q", c.address, c.file, got)
		}
	}
}

// TestResized checks that an SVG is told another size without what it
// draws changing: its viewBox stays, or is made of the size it had.
func TestResized(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`<svg xmlns="x" width="100" height='50'><path d="M0 0"/></svg>`, `<svg xmlns="x" width="56" height="28" viewBox="0 0 100 50"><path d="M0 0"/></svg>`},
		{`<?xml version="1.0"?><svg viewBox="0 0 24 24" width=24 height=24 fill="none"><g/></svg>`, `<?xml version="1.0"?><svg viewBox="0 0 24 24" fill="none" width="56" height="28"><g/></svg>`},
	} {
		if got := string(resized([]byte(c.in), 100, 50, 56, 28)); got != c.want {
			t.Errorf("resized to %s", got)
		}
	}
}

// TestChunks checks that a README is cut into stretches at the blank lines
// between its paragraphs, sooner before a heading, never within code, and
// that nothing of it is lost.
func TestChunks(t *testing.T) {
	code := "```sh\n" + strings.Repeat("echo one\n\n", 30) + "```\n"
	src := "# Title\n\n" + strings.Repeat("A paragraph of the README.\n\n", 8) + "## Install\n\n" + code + "\n## Use\n\nThe end.\n"
	parts := chunks(src, 200)
	if strings.Join(parts, "") != src {
		t.Fatalf("the stretches do not make the README: %q", parts)
	}
	if len(parts) < 3 {
		t.Fatalf("cut into %d stretches: %q", len(parts), parts)
	}
	for i, p := range parts {
		if strings.Count(p, "```")%2 != 0 {
			t.Errorf("stretch %d cuts a block of code: %q", i, p)
		}
	}
	if !strings.HasPrefix(parts[len(parts)-1], "## Use") {
		t.Errorf("no stretch begins at the heading after the code: %q", parts)
	}
	if got := chunks("one\n\ntwo\n", 1000); len(got) != 1 || got[0] != "one\n\ntwo\n" {
		t.Errorf("a short README is cut into %q", got)
	}
}
