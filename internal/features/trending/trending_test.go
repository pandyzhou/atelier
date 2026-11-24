package trending

import (
	"reflect"
	"testing"
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
		"<details><summary>More</summary>\n\n- one\n\n</details>\n"
	want := "# ![Logo](docs/logo%20dark.png#shown-at=120x0) Tiny\n\n" +
		"[![CI](https://img.example.com/ci.svg)](https://example.com/ci)\n[Read the **guide**](docs/guide.md)\n\n" +
		"Use `Map<K, V>` and `Ctrl`, not this.\n\n" +
		"```html\n<p align=\"center\"><img src=\"x.png\"></p>\n```\n\n" +
		"More\n\n- one"
	if got := Tidy(src); got != want {
		t.Errorf("tidied to:\n%s\n\nwant:\n%s", got, want)
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
