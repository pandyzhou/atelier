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
