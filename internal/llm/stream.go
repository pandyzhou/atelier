package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Stream is Complete with the answer's text given to onText piece by piece
// as it comes, off the caller's thread; the message returned is the whole
// of it. A service that answers all at once all the same gives its text in
// one piece. What the model thinks goes to the request's OnThinking the
// same way. The error is an *Error; onText may have had text before it.
func Stream(ctx context.Context, c Config, r Request, onText func(string)) (Message, error) {
	c, body, read, err := prepare(c, r)
	if err != nil {
		return Message{}, err
	}
	onThinking := r.OnThinking
	if onThinking == nil {
		onThinking = func(string) {}
	}
	var events stream
	switch c.Kind {
	case Anthropic:
		events = &anthropicStream{}
	case Chat:
		events = &chatStream{}
	default:
		events = &responsesStream{}
	}
	body["stream"] = true
	data, err := json.Marshal(body)
	if err != nil {
		return Message{}, &Error{Message: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint(), bytes.NewReader(data))
	if err != nil {
		return Message{}, &Error{Message: "地址写得不对：" + err.Error()}
	}
	req.Header = header(c)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Message{}, netError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return Message{}, statusError(resp.StatusCode, errorMessage(text))
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "event-stream") {
		// The service does not stream: the answer is here whole.
		text, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return Message{}, netError(err)
		}
		m, err := finish(c, read, text, resp.StatusCode)
		if err == nil && m.Thinking != "" {
			onThinking(m.Thinking)
		}
		if err == nil && m.Text != "" {
			onText(m.Text)
		}
		return m, err
	}
	lines := bufio.NewReader(resp.Body)
	for {
		line, err := lines.ReadString('\n')
		if event, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok {
			if event = strings.TrimSpace(event); event != "" && event != "[DONE]" {
				if failed := events.add([]byte(event), onText, onThinking); failed != nil {
					return Message{}, failed
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return Message{}, netError(err)
		}
	}
	return finish(c, read, events.whole(), resp.StatusCode)
}

// stream puts the events of a streamed answer together.
type stream interface {
	// add takes an event's data, giving onText the text it brings and
	// onThinking the thinking; the error is what the service says went
	// wrong.
	add(event []byte, onText, onThinking func(string)) *Error
	// whole returns the answer as the service would have sent it at once.
	whole() []byte
}

// streamError is what an event says went wrong, nil when it says nothing
// of the kind: {"type":"error", …} or one with an "error" in it.
func streamError(event []byte) *Error {
	var v struct {
		Type     string          `json:"type"`
		Error    json.RawMessage `json:"error"`
		Message  string          `json:"message"`
		Response struct {
			Error json.RawMessage `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(event, &v) != nil {
		return nil
	}
	said := func(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "null" }
	switch {
	case said(v.Error):
	case v.Type == "response.failed" && said(v.Response.Error):
		event, _ = json.Marshal(map[string]any{"error": v.Response.Error})
	case v.Type == "error":
	default:
		return nil
	}
	msg := "服务那边出错了"
	if detail := errorMessage(event); detail != "" {
		msg += "：" + detail
	}
	return &Error{Message: msg}
}

// Anthropic's Messages API: the content's blocks start, grow and stop.

type anthropicStream struct {
	blocks []map[string]any
	inputs map[int]*strings.Builder // the JSON of tools' inputs, as it comes
}

func (s *anthropicStream) add(event []byte, onText, onThinking func(string)) *Error {
	if err := streamError(event); err != nil {
		return err
	}
	var v struct {
		Type  string         `json:"type"`
		Index int            `json:"index"`
		Block map[string]any `json:"content_block"`
		Delta struct {
			Type      string `json:"type"`
			Text      string `json:"text"`
			JSON      string `json:"partial_json"`
			Thinking  string `json:"thinking"`
			Signature string `json:"signature"`
		} `json:"delta"`
	}
	if json.Unmarshal(event, &v) != nil || v.Index < 0 || v.Index > 1000 {
		return nil
	}
	text := func(b map[string]any, key string) string { s, _ := b[key].(string); return s }
	switch v.Type {
	case "content_block_start":
		for len(s.blocks) <= v.Index {
			s.blocks = append(s.blocks, nil)
		}
		s.blocks[v.Index] = v.Block
	case "content_block_delta":
		if v.Index >= len(s.blocks) || s.blocks[v.Index] == nil {
			return nil
		}
		b := s.blocks[v.Index]
		switch v.Delta.Type {
		case "text_delta":
			b["text"] = text(b, "text") + v.Delta.Text
			if v.Delta.Text != "" {
				onText(v.Delta.Text)
			}
		case "input_json_delta":
			if s.inputs == nil {
				s.inputs = map[int]*strings.Builder{}
			}
			if s.inputs[v.Index] == nil {
				s.inputs[v.Index] = &strings.Builder{}
			}
			s.inputs[v.Index].WriteString(v.Delta.JSON)
		case "thinking_delta":
			b["thinking"] = text(b, "thinking") + v.Delta.Thinking
			if v.Delta.Thinking != "" {
				onThinking(v.Delta.Thinking)
			}
		case "signature_delta":
			b["signature"] = v.Delta.Signature
		}
	}
	return nil
}

func (s *anthropicStream) whole() []byte {
	var blocks []map[string]any
	for i, b := range s.blocks {
		if b == nil {
			continue
		}
		if b["type"] == "tool_use" {
			input := ""
			if s.inputs[i] != nil {
				input = s.inputs[i].String()
			}
			b["input"] = object(json.RawMessage(input))
		}
		blocks = append(blocks, b)
	}
	out, _ := json.Marshal(map[string]any{"content": blocks})
	return out
}

// OpenAI's Chat Completions: each chunk adds to the one message.

type chatStream struct {
	content, reasoning strings.Builder
	// reasoned is the name the service gives the model's thinking under,
	// "" until it gave some.
	reasoned string
	calls    []*chatCall
}

type chatCall struct {
	id, name  string
	arguments strings.Builder
}

func (s *chatStream) add(event []byte, onText, onThinking func(string)) *Error {
	if err := streamError(event); err != nil {
		return err
	}
	var v struct {
		Choices []struct {
			Delta struct {
				Content   *string `json:"content"`
				Reasoning *string `json:"reasoning_content"`
				Reasoned  *string `json:"reasoning"`
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal(event, &v) != nil || len(v.Choices) == 0 {
		return nil
	}
	d := v.Choices[0].Delta
	thought, name := d.Reasoning, "reasoning_content"
	if thought == nil {
		thought, name = d.Reasoned, "reasoning"
	}
	if thought != nil {
		s.reasoned = name
		s.reasoning.WriteString(*thought)
		if *thought != "" {
			onThinking(*thought)
		}
	}
	if d.Content != nil && *d.Content != "" {
		s.content.WriteString(*d.Content)
		onText(*d.Content)
	}
	for _, call := range d.ToolCalls {
		if call.Index < 0 || call.Index > 1000 {
			continue
		}
		for len(s.calls) <= call.Index {
			s.calls = append(s.calls, &chatCall{})
		}
		into := s.calls[call.Index]
		if call.ID != "" {
			into.id = call.ID
		}
		into.name += call.Function.Name
		into.arguments.WriteString(call.Function.Arguments)
	}
	return nil
}

func (s *chatStream) whole() []byte {
	msg := map[string]any{"role": "assistant", "content": s.content.String()}
	if s.reasoned != "" {
		msg[s.reasoned] = s.reasoning.String()
	}
	var calls []map[string]any
	for _, call := range s.calls {
		if call.name == "" {
			continue
		}
		calls = append(calls, map[string]any{"id": call.id, "type": "function", "function": map[string]any{
			"name": call.name, "arguments": call.arguments.String(),
		}})
	}
	if len(calls) > 0 {
		msg["tool_calls"] = calls
	}
	out, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": msg}}})
	return out
}

// OpenAI's Responses: the text comes in deltas, and each item of the
// output whole as it is done; the last event has all of them.

type responsesStream struct {
	items  []json.RawMessage
	output json.RawMessage
}

func (s *responsesStream) add(event []byte, onText, onThinking func(string)) *Error {
	if err := streamError(event); err != nil {
		return err
	}
	var v struct {
		Type     string          `json:"type"`
		Delta    string          `json:"delta"`
		Item     json.RawMessage `json:"item"`
		Response struct {
			Output json.RawMessage `json:"output"`
		} `json:"response"`
	}
	if json.Unmarshal(event, &v) != nil {
		return nil
	}
	switch v.Type {
	case "response.output_text.delta":
		if v.Delta != "" {
			onText(v.Delta)
		}
	case "response.reasoning_summary_text.delta":
		if v.Delta != "" {
			onThinking(v.Delta)
		}
	case "response.reasoning_summary_part.done":
		onThinking("\n\n")
	case "response.output_item.done":
		if len(v.Item) > 0 {
			s.items = append(s.items, v.Item)
		}
	case "response.completed":
		var items []json.RawMessage
		if json.Unmarshal(v.Response.Output, &items) == nil && len(items) > 0 {
			s.output = v.Response.Output
		}
	}
	return nil
}

func (s *responsesStream) whole() []byte {
	var output any = s.output
	if len(s.output) == 0 {
		output = s.items
		if s.items == nil {
			output = []any{}
		}
	}
	out, _ := json.Marshal(map[string]any{"output": output})
	return out
}
