package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// pingOpenAI sends one short message to a service that speaks OpenAI's Chat
// Completions or Responses. No limit on the answer is sent: services
// disagree on its name (max_tokens, max_completion_tokens,
// max_output_tokens) and some refuse the ones they do not know.
func pingOpenAI(ctx context.Context, c Config) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.Key)
	if c.Kind == Chat {
		return post(ctx, c.Endpoint(), header, map[string]any{
			"model":    c.Model,
			"messages": []map[string]string{{"role": "user", "content": pingText}},
		})
	}
	return post(ctx, c.Endpoint(), header, map[string]any{"model": c.Model, "input": pingText})
}

// post sends body as JSON to endpoint and reports whether the service
// answered with JSON and a status that says it worked. The error is an
// *Error.
func post(ctx context.Context, endpoint string, header http.Header, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return &Error{Message: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return &Error{Message: "地址写得不对：" + err.Error()}
	}
	req.Header = header
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return netError(err)
	}
	defer resp.Body.Close()
	// Enough to hold an error's text; an answer is not read further.
	text, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusError(resp.StatusCode, errorMessage(text))
	}
	// A page that is not the API's, as a login or a proxy's, also answers 200.
	if !json.Valid(bytes.TrimSpace(text)) {
		return &Error{Message: "这个地址回的不是接口的数据，地址可能不对", Status: resp.StatusCode}
	}
	return nil
}

// errorMessage finds what a service said went wrong in the body of its
// answer: {"error": {"message": …}}, {"error": "…"} or {"message": …}, the
// shapes these services use. It is "" when the body has none.
func errorMessage(body []byte) string {
	var v struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(body, &v) != nil {
		return ""
	}
	var inner struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(v.Error, &inner) == nil && inner.Message != "" {
		return inner.Message
	}
	var text string
	if json.Unmarshal(v.Error, &text) == nil && text != "" {
		return text
	}
	return v.Message
}
