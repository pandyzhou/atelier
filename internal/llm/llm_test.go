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

// A service's models are asked for at its own address, with the key as its
// kind sends it, and come back in its order.
func TestListModels(t *testing.T) {
	const list = `{"object":"list","data":[{"id":"model-b"},{"id":"model-a"},{"id":""}]}`
	for _, c := range []struct {
		kind       Kind
		path, auth string
	}{
		{Anthropic, "/v1/models", ""},
		{Chat, "/models", "Bearer k"},
		{Responses, "/models", "Bearer k"},
	} {
		s := newService(t, 200, list)
		got, err := ListModels(context.Background(), Config{Kind: c.kind, BaseURL: s.URL, Key: " k "})
		if err != nil || len(got) != 2 || got[0] != "model-b" || got[1] != "model-a" {
			t.Errorf("%s: models %q, %v", c.kind, got, err)
		}
		if s.path != c.path || (c.auth != "" && s.auth != c.auth) || (c.kind == Anthropic && s.key != "k") {
			t.Errorf("%s: asked at %q with Authorization %q, X-Api-Key %q", c.kind, s.path, s.auth, s.key)
		}
	}
	for _, c := range []struct {
		status int
		answer string
		want   string
	}{
		{401, `{"error":{"message":"bad key"}}`, "Key 不对"},
		{200, `<html>login</html>`, "不是模型列表"},
		{200, `{"data":[]}`, "没有列出模型"},
	} {
		s := newService(t, c.status, c.answer)
		_, err := ListModels(context.Background(), Config{Kind: Chat, BaseURL: s.URL, Key: "k"})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("status %d, %q: error %v, want one saying %q", c.status, c.answer, err, c.want)
		}
	}
	if _, err := ListModels(context.Background(), Config{Kind: Chat}); err == nil {
		t.Errorf("no error without a key")
	}
}

// A conversation with tools goes to each kind as its service reads it, and
// what comes back is read the same from all three: text, and the calls of
// tools; the calls' results go back in the next request.
func TestComplete(t *testing.T) {
	tools := []Tool{{Name: "add_task", Description: "adds", Parameters: map[string]any{"type": "object"}}}
	for _, c := range []struct {
		kind           Kind
		calls, done    string
		toolsAt, sysAt string
	}{
		{Anthropic,
			`{"content":[{"type":"thinking","thinking":"hm","signature":"s"},{"type":"text","text":"好"},{"type":"tool_use","id":"c1","name":"add_task","input":{"title":"x"}}],"stop_reason":"tool_use"}`,
			`{"content":[{"type":"text","text":"完成"}]}`, "input_schema", "system"},
		{Chat,
			`{"choices":[{"message":{"role":"assistant","content":"好","reasoning_content":"hm","tool_calls":[{"id":"c1","type":"function","function":{"name":"add_task","arguments":"{\"title\":\"x\"}"}}]}}]}`,
			`{"choices":[{"message":{"role":"assistant","content":"完成"}}]}`, "function", "messages"},
		{Responses,
			`{"output":[{"type":"reasoning","id":"r1","summary":[]},{"type":"message","content":[{"type":"output_text","text":"好"}]},{"type":"function_call","call_id":"c1","name":"add_task","arguments":"{\"title\":\"x\"}"}]}`,
			`{"output":[{"type":"message","content":[{"type":"output_text","text":"完成"}]}]}`, "parameters", "instructions"},
	} {
		s := newService(t, 200, c.calls)
		cfg := Config{Kind: c.kind, BaseURL: s.URL, Key: "k", Model: "m"}
		req := Request{System: "sys", Messages: []Message{{Role: User, Text: "加个任务"}}, Tools: tools}
		got, err := Complete(ctx(t), cfg, req)
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		if got.Role != Assistant || got.Text != "好" || len(got.Calls) != 1 || got.Calls[0].ID != "c1" || got.Calls[0].Name != "add_task" || string(got.Calls[0].Input) != `{"title":"x"}` {
			t.Errorf("%s: read %+v", c.kind, got)
		}
		sent, _ := json.Marshal(s.body)
		if s.body["model"] != "m" || !strings.Contains(string(sent), c.toolsAt) || !strings.Contains(string(sent), `"sys"`) || !strings.Contains(string(sent), c.sysAt) || !strings.Contains(string(sent), "加个任务") {
			t.Errorf("%s: sent %s", c.kind, sent)
		}

		// The next request carries the model's message as it came, its
		// reasoning too, and the call's result.
		s.answer = c.done
		req.Messages = append(req.Messages, got, Message{Role: Result, CallID: "c1", Text: "已添加"})
		got, err = Complete(ctx(t), cfg, req)
		if err != nil || got.Text != "完成" || len(got.Calls) != 0 {
			t.Errorf("%s: the second answer %+v, %v", c.kind, got, err)
		}
		sent, _ = json.Marshal(s.body)
		for _, want := range []string{"hm", "c1", "add_task", "已添加", "加个任务"} {
			if c.kind == Responses && want == "hm" {
				want = "r1"
			}
			if !strings.Contains(string(sent), want) {
				t.Errorf("%s: the second request lacks %q: %s", c.kind, want, sent)
			}
		}

		// A conversation begun with another kind's model goes on with this
		// one: its message is written anew.
		other := Message{Role: Assistant, Text: "好", Calls: []Call{{ID: "c9", Name: "add_task", Input: json.RawMessage(`{"title":"y"}`)}}}
		req.Messages = []Message{{Role: User, Text: "加个任务"}, other, {Role: Result, CallID: "c9", Text: "已添加"}}
		if _, err = Complete(ctx(t), cfg, req); err != nil {
			t.Errorf("%s: %v", c.kind, err)
		}
		if sent, _ = json.Marshal(s.body); !strings.Contains(string(sent), "c9") || !strings.Contains(string(sent), "title") {
			t.Errorf("%s: another kind's message sent as %s", c.kind, sent)
		}
	}

	s := newService(t, 401, `{"error":{"message":"bad key"}}`)
	if _, err := Complete(ctx(t), Config{Kind: Chat, BaseURL: s.URL, Key: "k", Model: "m"}, Request{}); err == nil || !strings.Contains(err.Error(), "Key 不对") {
		t.Errorf("a refused request: %v", err)
	}
	s = newService(t, 200, `<html>`)
	if _, err := Complete(ctx(t), Config{Kind: Chat, BaseURL: s.URL, Key: "k", Model: "m"}, Request{}); err == nil {
		t.Errorf("no error for an answer that is not the API's")
	}
	if _, err := Complete(ctx(t), Config{Kind: Chat, Model: "m"}, Request{}); err == nil {
		t.Errorf("no error without a key")
	}
}
