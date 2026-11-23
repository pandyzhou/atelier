package llm

import (
	"context"
	"net/http"
	"net/url"
)

// anthropicVersion is the version of the Messages API the requests are
// written for: the one every service that speaks this interface knows.
const anthropicVersion = "2023-06-01"

// pingAnthropic sends one short message to a service that speaks
// Anthropic's Messages API. Any answer will do, one cut off by the few
// tokens allowed too. The request is written by hand, as the other two
// kinds' are: Anthropic's SDK would bring every other API of theirs along
// for this one call.
func pingAnthropic(ctx context.Context, c Config) error {
	return post(ctx, c.Endpoint(), anthropicHeader(c), map[string]any{
		"model":      c.Model,
		"max_tokens": 64,
		"messages":   []map[string]string{{"role": "user", "content": pingText}},
	})
}

// anthropicHeader is what a request of c carries besides its body: the key
// and the version of the API.
func anthropicHeader(c Config) http.Header {
	header := http.Header{}
	header.Set("X-Api-Key", c.Key)
	header.Set("Anthropic-Version", anthropicVersion)
	// Gateways and relays of other makers often read the key as OpenAI's
	// services send it. Anthropic's own takes an Authorization header for
	// another kind of credential, so it gets the key once.
	if u, err := url.Parse(c.base()); err != nil || u.Hostname() != "api.anthropic.com" {
		header.Set("Authorization", "Bearer "+c.Key)
	}
	return header
}
