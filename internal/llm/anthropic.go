package llm

import (
	"context"
	"errors"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// pingAnthropic sends one short message through Anthropic's SDK. Any
// answer will do, one cut off by the few tokens allowed too.
func pingAnthropic(ctx context.Context, c Config) error {
	client := anthropic.NewClient(
		option.WithAPIKey(c.Key),
		option.WithBaseURL(c.base()),
		// One try: the user is waiting, and will try again themselves.
		option.WithMaxRetries(0),
	)
	_, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.Model),
		MaxTokens: 64,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(pingText)),
		},
	})
	if err == nil {
		return nil
	}
	var api *anthropic.Error
	if errors.As(err, &api) {
		return statusError(api.StatusCode, apiMessage(api))
	}
	return netError(err)
}

// apiMessage is what the service said went wrong, without the request the
// SDK prints around it.
func apiMessage(e *anthropic.Error) string {
	return errorMessage([]byte(e.RawJSON()))
}
