package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Role is who a message of a conversation is from.
type Role string

const (
	User      Role = "user"
	Assistant Role = "assistant"
	// Result is what a tool the model called came to: the answer to one
	// of the Calls of the assistant's message before it.
	Result Role = "tool"
)

// Tool is something the model may ask to have done.
type Tool struct {
	Name, Description string
	// Parameters is the JSON Schema of what it takes, an object's.
	Parameters map[string]any
}

// Call is the model asking for a tool: which, and with what.
type Call struct {
	ID, Name string
	Input    json.RawMessage // a JSON object
}

// Message is one turn of a conversation.
type Message struct {
	Role Role
	Text string
	// Calls are the tools an assistant's message asks for; each is to be
	// answered by a Result message.
	Calls []Call
	// CallID is the ID of the Call a Result answers.
	CallID string
	// raw is an assistant's message as its service sent it, of kind: sent
	// back so to a service of that kind, which keeps what only it knows
	// to read, as the model's reasoning.
	raw  json.RawMessage
	kind Kind
}

// Request is a conversation to continue.
type Request struct {
	// System tells the model what it is here for.
	System   string
	Messages []Message
	Tools    []Tool
}

// maxAnswer is the most tokens asked of a service that wants a limit said.
const maxAnswer = 4096

// Complete sends the conversation to the model and returns its next
// message: text, calls of tools, or both. The error is an *Error.
func Complete(ctx context.Context, c Config, r Request) (Message, error) {
	c, body, read, err := prepare(c, r)
	if err != nil {
		return Message{}, err
	}
	text, status, err := send(ctx, c.Endpoint(), header(c), body)
	if err != nil {
		return Message{}, err
	}
	return finish(c, read, text, status)
}

// prepare checks c, tidies it, and writes the request of its kind for r,
// with what reads the answer to it.
func prepare(c Config, r Request) (Config, map[string]any, func([]byte) (Message, bool), error) {
	switch {
	case strings.TrimSpace(c.Key) == "":
		return c, nil, nil, &Error{Message: "还没有填 Key"}
	case strings.TrimSpace(c.Model) == "":
		return c, nil, nil, &Error{Message: "还没有填模型名"}
	}
	c.Key, c.Model = strings.TrimSpace(c.Key), strings.TrimSpace(c.Model)
	switch c.Kind {
	case Anthropic:
		return c, anthropicRequest(c, r), anthropicAnswer, nil
	case Chat:
		return c, chatRequest(c, r), chatAnswer, nil
	case Responses:
		return c, responsesRequest(c, r), responsesAnswer, nil
	}
	return c, nil, nil, &Error{Message: "不认识的接口类型：" + string(c.Kind)}
}

// finish reads the model's message from the body of an answer.
func finish(c Config, read func([]byte) (Message, bool), text []byte, status int) (Message, error) {
	m, ok := read(text)
	if !ok {
		return Message{}, &Error{Message: "这个地址回的不是接口的数据，地址可能不对", Status: status}
	}
	if m.Text == "" && len(m.Calls) == 0 {
		return Message{}, &Error{Message: "模型没有回答", Status: status}
	}
	m.Role, m.kind = Assistant, c.Kind
	return m, nil
}

// send posts body as JSON and returns the body of the answer, when its
// status says it worked. The error is an *Error.
func send(ctx context.Context, endpoint string, header http.Header, body any) ([]byte, int, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, 0, &Error{Message: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, 0, &Error{Message: "地址写得不对：" + err.Error()}
	}
	req.Header = header
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, netError(err)
	}
	defer resp.Body.Close()
	text, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, statusError(resp.StatusCode, errorMessage(text))
	}
	if err != nil {
		return nil, resp.StatusCode, netError(err)
	}
	return text, resp.StatusCode, nil
}

// object is a Call's input as a JSON object: {} for what is none.
func object(input json.RawMessage) json.RawMessage {
	var v map[string]any
	if json.Unmarshal(input, &v) != nil || v == nil {
		return json.RawMessage("{}")
	}
	return input
}

// Anthropic's Messages API.

func anthropicRequest(c Config, r Request) map[string]any {
	var messages []map[string]any
	results := false // whether the last message is one of tools' results
	for _, m := range r.Messages {
		switch m.Role {
		case User:
			messages, results = append(messages, map[string]any{"role": "user", "content": m.Text}), false
		case Assistant:
			var content any = m.raw
			if m.kind != Anthropic || len(m.raw) == 0 {
				var blocks []map[string]any
				if m.Text != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": m.Text})
				}
				for _, call := range m.Calls {
					blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Name, "input": object(call.Input)})
				}
				content = blocks
			}
			messages, results = append(messages, map[string]any{"role": "assistant", "content": content}), false
		case Result:
			// The results of one message's calls go back as one message.
			block := map[string]any{"type": "tool_result", "tool_use_id": m.CallID, "content": m.Text}
			if results {
				last := messages[len(messages)-1]
				last["content"] = append(last["content"].([]map[string]any), block)
			} else {
				messages, results = append(messages, map[string]any{"role": "user", "content": []map[string]any{block}}), true
			}
		}
	}
	body := map[string]any{"model": c.Model, "max_tokens": maxAnswer, "messages": messages}
	if r.System != "" {
		body["system"] = r.System
	}
	if len(r.Tools) > 0 {
		tools := make([]map[string]any, len(r.Tools))
		for i, t := range r.Tools {
			tools[i] = map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters}
		}
		body["tools"] = tools
	}
	return body
}

func anthropicAnswer(text []byte) (Message, bool) {
	var v struct {
		Content json.RawMessage `json:"content"`
	}
	var blocks []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(text, &v) != nil || json.Unmarshal(v.Content, &blocks) != nil {
		return Message{}, false
	}
	m := Message{raw: v.Content}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			m.Text += b.Text
		case "tool_use":
			m.Calls = append(m.Calls, Call{ID: b.ID, Name: b.Name, Input: object(b.Input)})
		}
	}
	return m, true
}

// OpenAI's Chat Completions.

func chatRequest(c Config, r Request) map[string]any {
	var messages []map[string]any
	if r.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": r.System})
	}
	for _, m := range r.Messages {
		switch m.Role {
		case User:
			messages = append(messages, map[string]any{"role": "user", "content": m.Text})
		case Assistant:
			msg := map[string]any{"role": "assistant", "content": m.Text}
			if len(m.Calls) > 0 {
				calls := make([]map[string]any, len(m.Calls))
				for i, call := range m.Calls {
					calls[i] = map[string]any{"id": call.ID, "type": "function", "function": map[string]any{
						"name": call.Name, "arguments": string(object(call.Input)),
					}}
				}
				msg["tool_calls"] = calls
				// Services whose models reason before they call want that
				// reasoning back with the calls it led to, and only then.
				var was struct {
					Reasoning *string `json:"reasoning_content"`
				}
				if m.kind == Chat && json.Unmarshal(m.raw, &was) == nil && was.Reasoning != nil {
					msg["reasoning_content"] = *was.Reasoning
				}
			}
			messages = append(messages, msg)
		case Result:
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": m.CallID, "content": m.Text})
		}
	}
	body := map[string]any{"model": c.Model, "messages": messages}
	if len(r.Tools) > 0 {
		tools := make([]map[string]any, len(r.Tools))
		for i, t := range r.Tools {
			tools[i] = map[string]any{"type": "function", "function": map[string]any{
				"name": t.Name, "description": t.Description, "parameters": t.Parameters,
			}}
		}
		body["tools"] = tools
	}
	return body
}

func chatAnswer(text []byte) (Message, bool) {
	var v struct {
		Choices []struct {
			Message json.RawMessage `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(text, &v) != nil || len(v.Choices) == 0 {
		return Message{}, false
	}
	var msg struct {
		Content   *string `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if json.Unmarshal(v.Choices[0].Message, &msg) != nil {
		return Message{}, false
	}
	m := Message{raw: v.Choices[0].Message}
	if msg.Content != nil {
		m.Text = *msg.Content
	}
	for _, call := range msg.ToolCalls {
		m.Calls = append(m.Calls, Call{ID: call.ID, Name: call.Function.Name, Input: object(json.RawMessage(call.Function.Arguments))})
	}
	return m, true
}

// OpenAI's Responses.

func responsesRequest(c Config, r Request) map[string]any {
	var input []any
	for _, m := range r.Messages {
		switch m.Role {
		case User:
			input = append(input, map[string]any{"role": "user", "content": m.Text})
		case Assistant:
			// What the service put out goes back as it was, item by item.
			var items []json.RawMessage
			if m.kind == Responses && json.Unmarshal(m.raw, &items) == nil && len(items) > 0 {
				for _, item := range items {
					input = append(input, item)
				}
				continue
			}
			if m.Text != "" {
				input = append(input, map[string]any{"role": "assistant", "content": m.Text})
			}
			for _, call := range m.Calls {
				input = append(input, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Name, "arguments": string(object(call.Input))})
			}
		case Result:
			input = append(input, map[string]any{"type": "function_call_output", "call_id": m.CallID, "output": m.Text})
		}
	}
	body := map[string]any{"model": c.Model, "input": input}
	if r.System != "" {
		body["instructions"] = r.System
	}
	if len(r.Tools) > 0 {
		tools := make([]map[string]any, len(r.Tools))
		for i, t := range r.Tools {
			tools[i] = map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": t.Parameters}
		}
		body["tools"] = tools
	}
	return body
}

func responsesAnswer(text []byte) (Message, bool) {
	var v struct {
		Output json.RawMessage `json:"output"`
	}
	var items []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	if json.Unmarshal(text, &v) != nil || json.Unmarshal(v.Output, &items) != nil {
		return Message{}, false
	}
	m := Message{raw: v.Output}
	for _, item := range items {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					m.Text += part.Text
				}
			}
		case "function_call":
			m.Calls = append(m.Calls, Call{ID: item.CallID, Name: item.Name, Input: object(json.RawMessage(item.Arguments))})
		}
	}
	return m, true
}
