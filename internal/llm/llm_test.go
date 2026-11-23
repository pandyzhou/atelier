package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// service is a stand-in for a model service: it records what it was sent,
// and answers as told.
type service struct {
	*httptest.Server
	path, auth, key, version string
	body                     map[string]any
	status                   int
	answer                   string
}

func newService(t *testing.T, status int, answer string) *service {
	s := &service{status: status, answer: answer}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.path, s.auth, s.key = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Api-Key")
		s.version = r.Header.Get("Anthropic-Version")
		data, _ := io.ReadAll(r.Body)
		s.body = nil
		json.Unmarshal(data, &s.body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		io.WriteString(w, s.answer)
	}))
	t.Cleanup(s.Close)
	return s
}

const anthropicOK = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":9,"output_tokens":1}}`

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

// Each kind sends its request to its path, with the key where its service
// reads it and the model's name in the body.
func TestPing(t *testing.T) {
	for _, c := range []struct {
		kind         Kind
		answer, path string
	}{
		{Anthropic, anthropicOK, "/v1/messages"},
		{Chat, `{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`, "/v1/chat/completions"},
		{Responses, `{"output":[{"type":"message","content":[{"type":"output_text","text":"OK"}]}]}`, "/v1/responses"},
	} {
		s := newService(t, 200, c.answer)
		base := s.URL + "/v1"
		if c.kind == Anthropic {
			base = s.URL
		}
		if err := Ping(ctx(t), Config{Kind: c.kind, BaseURL: base, Key: " sk-test ", Model: "the-model"}); err != nil {
			t.Errorf("%s: %v", c.kind, err)
			continue
		}
		if s.path != c.path || s.body["model"] != "the-model" {
			t.Errorf("%s: sent to %s with model %v, want %s", c.kind, s.path, s.body["model"], c.path)
		}
		// The key goes out trimmed of the spaces pasted around it: as
		// Anthropic reads it, and to a service that is not Anthropic's
		// own, as here, also as OpenAI's services do.
		if s.auth != "Bearer sk-test" {
			t.Errorf("%s: authorization sent as %q", c.kind, s.auth)
		}
		if c.kind == Anthropic && (s.key != "sk-test" || s.version != anthropicVersion || s.body["max_tokens"] == nil) {
			t.Errorf("anthropic: x-api-key %q, version %q, body %v", s.key, s.version, s.body)
		}
		if c.kind != Anthropic && s.key != "" {
			t.Errorf("%s: sent an x-api-key", c.kind)
		}
	}
}

// Anthropic's own service gets the key in its header alone; another that
// speaks its interface gets it as OpenAI's services send it too.
func TestAnthropicHeader(t *testing.T) {
	for _, c := range []struct {
		base string
		both bool
	}{
		{"", false},
		{"https://api.anthropic.com/v1", false},
		{"https://gw.example/anthropic", true},
	} {
		h := anthropicHeader(Config{Kind: Anthropic, BaseURL: c.base, Key: "sk-test"})
		if h.Get("X-Api-Key") != "sk-test" || (h.Get("Authorization") == "Bearer sk-test") != c.both {
			t.Errorf("%q: x-api-key %q, authorization %q", c.base, h.Get("X-Api-Key"), h.Get("Authorization"))
		}
	}
}

// The address may be pasted with the path of the call, or with a slash.
func TestBaseURL(t *testing.T) {
	for _, c := range []struct {
		kind       Kind
		base, want string
	}{
		{Anthropic, "", "https://api.anthropic.com/v1/messages"},
		{Anthropic, "https://gw.example/anthropic/", "https://gw.example/anthropic/v1/messages"},
		{Anthropic, "https://gw.example/anthropic/v1/messages", "https://gw.example/anthropic/v1/messages"},
		{Chat, "", "https://api.openai.com/v1/chat/completions"},
		{Chat, "https://api.deepseek.com/v1/chat/completions", "https://api.deepseek.com/v1/chat/completions"},
		{Chat, " https://open.bigmodel.cn/api/paas/v4/ ", "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
		{Responses, "https://gw.example/v1/responses", "https://gw.example/v1/responses"},
	} {
		if got := (Config{Kind: c.kind, BaseURL: c.base}).Endpoint(); got != c.want {
			t.Errorf("%s %q: %s, want %s", c.kind, c.base, got, c.want)
		}
	}
}

// What goes wrong is said so that the user can act on it.
func TestPingErrors(t *testing.T) {
	for _, c := range []struct {
		kind         Kind
		status       int
		answer, want string
	}{
		{Anthropic, 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, "Key 不对"},
		{Anthropic, 404, `{"type":"error","error":{"type":"not_found_error","message":"model: nope"}}`, "model: nope"},
		{Chat, 401, `{"error":{"message":"Incorrect API key provided"}}`, "Incorrect API key"},
		{Chat, 404, `{"error":"model not found"}`, "地址或模型名不对"},
		{Responses, 429, `{"message":"quota exceeded"}`, "quota exceeded"},
		{Chat, 500, `oops`, "服务那边出错了"},
		{Chat, 200, `<html>login</html>`, "不是接口的数据"},
	} {
		s := newService(t, c.status, c.answer)
		err := Ping(ctx(t), Config{Kind: c.kind, BaseURL: s.URL, Key: "k", Model: "m"})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %d: %v, want it to say %q", c.kind, c.status, err, c.want)
		}
		if strings.Contains(err.Error(), "POST") {
			t.Errorf("%s %d: the message shows the request: %v", c.kind, c.status, err)
		}
	}
	for _, c := range []Config{{Kind: Chat, Model: "m"}, {Kind: Chat, Key: "k"}} {
		if err := Ping(ctx(t), c); err == nil {
			t.Errorf("a ping without a key or a model went out: %+v", c)
		}
	}
	// Nothing listens there.
	if err := Ping(ctx(t), Config{Kind: Chat, BaseURL: "http://127.0.0.1:1", Key: "k", Model: "m"}); err == nil || !strings.Contains(err.Error(), "连不上") {
		t.Errorf("a ping to a closed port: %v", err)
	}
}
