package main

import (
	"context"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"

	"github.com/pandyzhou/atelier/internal/llm"
)

// modelConfig is a language model of a service, as the settings keep it.
type modelConfig struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	// Kind is an llm.Kind: the interface the service speaks.
	Kind string `json:"kind"`
	// BaseURL is the service's address, "" for the one the kind is named
	// after.
	BaseURL string `json:"baseURL,omitempty"`
	// Key is the API key, sealed by the app's secret.Box: never the key
	// itself.
	Key   string `json:"key,omitempty"`
	Model string `json:"model"`
}

// modelsState is what the models' part of the settings shows.
type modelsState struct {
	open int    // the ID of the model whose card is open, 0 for none
	key  string // what is typed as its key: sealed and saved as it changes
	// tests are the outcomes of testing a model's connection, by its ID.
	tests map[int]modelTest
}

// modelTest is how a test of a model's connection stands.
type modelTest struct {
	running, ok bool
	message     string
}

var kindLabels = []struct {
	kind  llm.Kind
	label string
}{
	{llm.Anthropic, "Anthropic"},
	{llm.Chat, "Chat Completions"},
	{llm.Responses, "Responses"},
}

func kindLabel(kind string) string {
	for _, k := range kindLabels {
		if string(k.kind) == kind {
			return k.label
		}
	}
	return kind
}

// model returns the model of an ID, nil if there is none.
func (a *app) model(id int) *modelConfig {
	for i := range a.settings.Models {
		if a.settings.Models[i].ID == id {
			return &a.settings.Models[i]
		}
	}
	return nil
}

// DefaultModel returns the model the app's features use, ready to call,
// and whether one is set: its key opened.
func (a *app) DefaultModel() (llm.Config, bool) {
	m := a.model(a.settings.Model)
	if m == nil {
		return llm.Config{}, false
	}
	return a.config(m)
}

// config makes a model ready to call, opening its key; it reports whether
// that worked.
func (a *app) config(m *modelConfig) (llm.Config, bool) {
	key, err := a.box.Open(m.Key)
	if err != nil {
		log.Printf("models: opening the key of %q: %v", m.Name, err)
		return llm.Config{}, false
	}
	return llm.Config{Kind: llm.Kind(m.Kind), BaseURL: m.BaseURL, Key: key, Model: m.Model}, true
}

// addModel adds a model to fill in, and opens it. The first is the default.
func (a *app) addModel() {
	a.settings.NextModel++
	m := modelConfig{ID: a.settings.NextModel, Name: "Claude", Kind: string(llm.Anthropic), Model: llm.DefaultModel(llm.Anthropic)}
	a.settings.Models = append(a.settings.Models, m)
	if a.model(a.settings.Model) == nil {
		a.settings.Model = m.ID
	}
	a.models.open, a.models.key = m.ID, ""
	a.saveSettings()
}

// removeModel deletes a model; if it was the default, the first left is.
func (a *app) removeModel(id int) {
	a.settings.Models = slices.DeleteFunc(a.settings.Models, func(m modelConfig) bool { return m.ID == id })
	if a.settings.Model == id {
		a.settings.Model = 0
		if len(a.settings.Models) > 0 {
			a.settings.Model = a.settings.Models[0].ID
		}
	}
	if a.models.open == id {
		a.models.open, a.models.key = 0, ""
	}
	delete(a.models.tests, id)
	a.saveSettings()
}

// testModel sends the model one short message, off the UI's thread, and
// shows how it went.
func (a *app) testModel(m *modelConfig) {
	id := m.ID
	if a.models.tests == nil {
		a.models.tests = map[int]modelTest{}
	}
	cfg, ok := a.config(m)
	if !ok {
		a.models.tests[id] = modelTest{message: "保存的 Key 打不开，请重新填一次"}
		return
	}
	a.models.tests[id] = modelTest{running: true}
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := a.ping(ctx, cfg)
		return func() {
			if err != nil {
				a.models.tests[id] = modelTest{message: err.Error()}
			} else {
				a.models.tests[id] = modelTest{ok: true, message: "连接正常，模型有回应"}
			}
		}
	})
}

// background runs work off the UI's thread, then what it returns on it,
// and shows the window anew. In tests, which have no such window, both run
// at once.
func (a *app) background(work func() func()) {
	win, ok := a.win.(interface{ Update(func()) })
	if !ok {
		work()()
		return
	}
	go func() {
		done := work()
		win.Update(done)
	}()
}

// modelSettings are the models the app's features can use.
func (a *app) modelSettings(c *ui.Context, _ func(name, note string, width float32, control func())) {
	ui.Text(c, "AI 功能会用这里的默认模型。Key 加密后保存在这台电脑上，不会明文写进数据文件。").
		FontSize(13).TextColor(mallow.Muted).Padding(4, 4, 12, 4)
	remove := 0
	for i := range a.settings.Models {
		m := &a.settings.Models[i]
		if m.ID == a.models.open {
			if a.modelCard(c.Key(m.ID), m) {
				remove = m.ID
			}
		} else {
			a.modelRow(c.Key(m.ID), m)
		}
	}
	if remove != 0 {
		a.removeModel(remove)
	}
	if len(a.settings.Models) == 0 {
		ui.Text(c, "还没有配置模型。").FontSize(14).TextColor(mallow.Muted).Padding(8, 4)
	}
	ui.Row(c).Padding(16, 4, 0, 4).Children(func() {
		if mallow.Button(c, "添加模型").Clicked() {
			a.addModel()
		}
	})
}

// modelRow is a model in the list: its name, what it is, and whether it is
// the default. It opens the model's card.
func (a *app) modelRow(c *ui.Context, m *modelConfig) {
	mallow.ListRow(c, false).Children(func() {
		body := mallow.Plain(c, "打开"+m.Name).Grow(1).MinWidth(0).Padding(10, 8).Gap(12)
		body.Children(func() {
			ui.Text(c, m.Name).FontSize(14).FontWeight(500).TextColor(mallow.Text).SingleLine()
			what := kindLabel(m.Kind)
			if m.Model != "" {
				what += " · " + m.Model
			}
			ui.Text(c, what).Grow(1).MinWidth(0).FontSize(12).TextColor(mallow.Muted).SingleLine()
			if m.ID == a.settings.Model {
				ui.Text(c, "默认").FontSize(12).FontWeight(500).TextColor(mallow.Accent)
			}
		})
		if body.Clicked() {
			a.models.open, a.models.key = m.ID, ""
		}
	})
}

// modelCard is the model that is open: a card that edits it. It reports
// whether the model is to be deleted.
func (a *app) modelCard(c *ui.Context, m *modelConfig) (remove bool) {
	line := func(name string, width float32, control func()) {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			mallow.Label(c, name).Width(48).Shrink(0)
			ui.Column(c).Grow(1).MinWidth(0).MaxWidth(width).Gap(6).Children(control)
		})
	}
	changed := false
	card := mallow.Card(c, mallow.Small).Margin(10, 4).Padding(12, 16, 16, 16).Gap(14)
	card.OnShortcut(0, ui.KeyEscape, func() { a.models.open, a.models.key = 0, "" })
	card.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			if mallow.TextInput(c, &m.Name).Placeholder("给它起个名字").Label("模型名称").Grow(1).MinWidth(0).Margin(0, 8, 0, 0).Changed() {
				changed = true
			}
			if mallow.GhostIconButton(c, "删除模型", mallow.GlyphTrash, mallow.IconButtonStyle{Danger: true}).Clicked() {
				remove = true
			}
			if mallow.GhostIconButton(c, "收起", mallow.GlyphClose, mallow.IconButtonStyle{}).Clicked() {
				a.models.open, a.models.key = 0, ""
			}
		})
		line("接口", 440, func() {
			kind := m.Kind
			options := make([]mallow.Option, len(kindLabels))
			for i, k := range kindLabels {
				options[i] = mallow.Option{Value: string(k.kind), Label: k.label}
			}
			if mallow.Segmented(c, "接口类型", &kind, options) {
				// The model's name follows, where it was the kind's own.
				if m.Model == llm.DefaultModel(llm.Kind(m.Kind)) {
					m.Model = llm.DefaultModel(llm.Kind(kind))
				}
				m.Kind, changed = kind, true
			}
		})
		cfg := llm.Config{Kind: llm.Kind(m.Kind), BaseURL: m.BaseURL}
		line("地址", 560, func() {
			if mallow.TextInput(c, &m.BaseURL).Placeholder(llm.DefaultBaseURL(cfg.Kind)).Label("接口地址").Changed() {
				m.BaseURL, changed = strings.TrimSpace(m.BaseURL), true
			}
			ui.Text(c, "请求会发到 "+cfg.Endpoint()).FontSize(12).TextColor(mallow.Muted)
		})
		line("Key", 560, func() {
			hint := "粘贴 API Key"
			if m.Key != "" {
				hint = "已保存。要换一个，就在这里重新粘贴"
			}
			if mallow.TextInput(c, &a.models.key).Password().Placeholder(hint).Label("API Key").Changed() {
				// Emptying the field keeps the key that is saved.
				if key := strings.TrimSpace(a.models.key); key != "" {
					sealed, err := a.box.Seal(key)
					if err != nil {
						log.Printf("models: sealing a key: %v", err)
					} else {
						m.Key, changed = sealed, true
					}
				}
			}
		})
		line("模型", 560, func() {
			hint := "服务给模型起的名字，如 deepseek-chat"
			if d := llm.DefaultModel(cfg.Kind); d != "" {
				hint = d
			}
			if mallow.TextInput(c, &m.Model).Placeholder(hint).Label("模型名").Changed() {
				m.Model, changed = strings.TrimSpace(m.Model), true
			}
			if cfg.Kind == llm.Anthropic {
				ui.Row(c).Gap(8).Wrap().Children(func() {
					for _, name := range llm.AnthropicModels {
						if mallow.Chip(c, name, m.Model == name).Clicked() {
							m.Model, changed = name, true
						}
					}
				})
			}
		})
		test := a.models.tests[m.ID]
		ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(4, 0, 0, 60).Children(func() {
			if mallow.Button(c, "测试连接").Clicked() && !test.running {
				a.testModel(m)
				test = a.models.tests[m.ID]
			}
			if m.ID == a.settings.Model {
				ui.Text(c, "默认模型").FontSize(12).FontWeight(500).TextColor(mallow.Accent).Padding(0, 4)
			} else if mallow.Button(c, "设为默认").Clicked() {
				a.settings.Model, changed = m.ID, true
			}
			switch {
			case test.running:
				ui.Text(c, "正在测试…").Grow(1).MinWidth(0).FontSize(13).TextColor(mallow.Muted)
			case test.ok:
				ui.Text(c, test.message).Grow(1).MinWidth(0).FontSize(13).TextColor(mallow.Success)
			case test.message != "":
				ui.Text(c, test.message).Grow(1).MinWidth(0).FontSize(13).TextColor(mallow.Danger)
			}
		})
	})
	if changed {
		// What was tested is no longer what is set.
		delete(a.models.tests, m.ID)
		a.saveSettings()
	}
	return remove
}
