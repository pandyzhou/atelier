// Package llm reaches the language models the user configured, through the
// three interfaces their services speak: Anthropic's Messages API, and
// OpenAI's Chat Completions and Responses, which many other services
// speak too.
package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// Kind is the interface a service speaks.
type Kind string

const (
	Anthropic Kind = "anthropic" // POST <base>/v1/messages
	Chat      Kind = "chat"      // POST <base>/chat/completions
	Responses Kind = "responses" // POST <base>/responses
)

// Config is one model of one service.
type Config struct {
	Kind Kind
	// BaseURL is the service's address, DefaultBaseURL(Kind) when it is "".
	BaseURL string
	Key     string
	Model   string
}

// DefaultBaseURL is the address of the service a Kind is named after.
func DefaultBaseURL(k Kind) string {
	if k == Anthropic {
		return "https://api.anthropic.com"
	}
	return "https://api.openai.com/v1"
}

// DefaultModel is the model to start from: Anthropic's has a name here, the
// others are whatever the user's service calls its own.
func DefaultModel(k Kind) string {
	if k == Anthropic {
		return "claude-opus-5-5"
	}
	return ""
}

// AnthropicModels are the current Claude models, to choose from.
var AnthropicModels = []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-2"}

// base returns the address requests go to, without the path of the call:
// the user may have pasted the whole of it, as "…/v1/chat/completions".
func (c Config) base() string {
	u := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if u == "" {
		return DefaultBaseURL(c.Kind)
	}
	switch c.Kind {
	case Anthropic:
		// Endpoint adds "/v1/messages".
		u = strings.TrimSuffix(u, "/messages")
		u = strings.TrimSuffix(u, "/v1")
	case Chat:
		u = strings.TrimSuffix(u, "/chat/completions")
	case Responses:
		u = strings.TrimSuffix(u, "/responses")
	}
	return strings.TrimRight(u, "/")
}

// Endpoint is the address a request of c is sent to, to show the user.
func (c Config) Endpoint() string {
	switch c.Kind {
	case Anthropic:
		return c.base() + "/v1/messages"
	case Chat:
		return c.base() + "/chat/completions"
	}
	return c.base() + "/responses"
}

// Error is a failure the user can act on: what went wrong, in their words.
type Error struct {
	Message string
	// Status is the HTTP status, 0 when no answer came.
	Status int
}

func (e *Error) Error() string { return e.Message }

// statusError explains an HTTP status; detail is what the service said.
func statusError(status int, detail string) *Error {
	var what string
	switch {
	case status == 401 || status == 403:
		what = "Key 不对，或者没有权限"
	case status == 404:
		what = "地址或模型名不对"
	case status == 429:
		what = "请求太频繁，或者额度用完了"
	case status >= 500:
		what = "服务那边出错了"
	default:
		what = "请求被拒绝"
	}
	msg := fmt.Sprintf("%s（%d）", what, status)
	if detail = strings.TrimSpace(detail); detail != "" {
		if r := []rune(detail); len(r) > 160 {
			detail = string(r[:160]) + "…"
		}
		msg += "：" + detail
	}
	return &Error{Message: msg, Status: status}
}

// netError explains a request that got no answer.
func netError(err error) *Error {
	var dns *net.DNSError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{Message: "超时了，没有等到回应"}
	case errors.As(err, &dns):
		return &Error{Message: "找不到这个地址：" + dns.Name}
	}
	return &Error{Message: "连不上：" + err.Error()}
}

// Ping sends the model one short message and reports whether an answer
// came: the address, the key and the model's name are right. It costs a
// few tokens. The error is an *Error.
func Ping(ctx context.Context, c Config) error {
	switch {
	case strings.TrimSpace(c.Key) == "":
		return &Error{Message: "还没有填 Key"}
	case strings.TrimSpace(c.Model) == "":
		return &Error{Message: "还没有填模型名"}
	}
	c.Key, c.Model = strings.TrimSpace(c.Key), strings.TrimSpace(c.Model)
	switch c.Kind {
	case Anthropic:
		return pingAnthropic(ctx, c)
	case Chat, Responses:
		return pingOpenAI(ctx, c)
	}
	return &Error{Message: "不认识的接口类型：" + string(c.Kind)}
}

// pingText is what Ping asks: little to read, and little to answer.
const pingText = "Reply with the single word: OK"
