package main

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"

	"github.com/pandyzhou/atelier/internal/llm"
)

// modelConfig is a provider of language models, as the settings keep it: a
// service, how it is reached, and the models of it the user added. No
// provider and no model is there until the user adds it.
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
	Key string `json:"key,omitempty"`
	// Models are the names of the provider's models, as the service calls
	// them, in the order they were added.
	Models []string `json:"models,omitempty"`
	// Model is the one model an entry had when each was a model of its
	// own: migrateModels moves it into Models.
	Model string `json:"model,omitempty"`
}

// modelsState is what the models' part of the settings shows.
type modelsState struct {
	open int    // the ID of the provider that shows, 0 for the first
	key  string // what is typed as its key: sealed and saved as it changes
	// adding is whether the dialog that adds models to it shows, and draft
	// what is typed there: a model's name, or a part of one to look for.
	adding bool
	draft  string
	// tests are the outcomes of testing a provider's connection, and
	// lists what its service said its models are, by its ID.
	tests map[int]modelTest
	lists map[int]modelList
}

// modelTest is how a test of a provider's connection stands.
type modelTest struct {
	running, ok bool
	message     string
}

// modelList is how asking a provider for its models stands.
type modelList struct {
	running bool
	names   []string
	message string // what went wrong
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

// migrateModels brings settings saved when each entry was one model to
// providers with models: the entry's model becomes its provider's first,
// and the default entry's the default model.
func (a *app) migrateModels() {
	for i := range a.settings.Models {
		m := &a.settings.Models[i]
		if m.Model == "" {
			continue
		}
		if !slices.Contains(m.Models, m.Model) {
			m.Models = append(m.Models, m.Model)
		}
		if m.ID == a.settings.Model && a.settings.ModelName == "" {
			a.settings.ModelName = m.Model
		}
		m.Model = ""
	}
}

// model returns the provider of an ID, nil if there is none.
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
	if m == nil || !slices.Contains(m.Models, a.settings.ModelName) {
		return llm.Config{}, false
	}
	return a.config(m, a.settings.ModelName)
}

// config makes a model of a provider ready to call, opening the key; it
// reports whether that worked.
func (a *app) config(m *modelConfig, model string) (llm.Config, bool) {
	key, err := a.box.Open(m.Key)
	if err != nil {
		log.Printf("models: opening the key of %q: %v", m.Name, err)
		return llm.Config{}, false
	}
	return llm.Config{Kind: llm.Kind(m.Kind), BaseURL: m.BaseURL, Key: key, Model: model}, true
}

// isDefault reports whether a model of a provider is the default one.
func (a *app) isDefault(m *modelConfig, model string) bool {
	return a.settings.Model == m.ID && a.settings.ModelName == model
}

// settleDefault keeps a default model while there is any model: the one
// set, or else the first there is.
func (a *app) settleDefault() {
	if m := a.model(a.settings.Model); m != nil && slices.Contains(m.Models, a.settings.ModelName) {
		return
	}
	a.settings.Model, a.settings.ModelName = 0, ""
	for _, m := range a.settings.Models {
		if len(m.Models) > 0 {
			a.settings.Model, a.settings.ModelName = m.ID, m.Models[0]
			return
		}
	}
}

// addProvider adds a provider to fill in, and opens it: nothing of it is
// set but the interface, the one most services speak.
func (a *app) addProvider() {
	a.settings.NextModel++
	m := modelConfig{ID: a.settings.NextModel, Kind: string(llm.Chat)}
	a.settings.Models = append(a.settings.Models, m)
	a.openProvider(m.ID)
	a.saveSettings()
}

// openProvider shows a provider, 0 for the first there is.
func (a *app) openProvider(id int) {
	a.models.open, a.models.key, a.models.draft, a.models.adding = id, "", "", false
}

// startAddingModels shows the dialog that adds models to a provider, and
// asks its service for the models it has when there is a key to ask with.
func (a *app) startAddingModels(m *modelConfig) {
	a.models.adding, a.models.draft = true, ""
	if list := a.models.lists[m.ID]; m.Key != "" && !list.running && len(list.names) == 0 {
		a.fetchModels(m)
	}
}

// removeProvider deletes a provider and its models.
func (a *app) removeProvider(id int) {
	a.settings.Models = slices.DeleteFunc(a.settings.Models, func(m modelConfig) bool { return m.ID == id })
	if a.models.open == id {
		a.openProvider(0)
	}
	delete(a.models.tests, id)
	delete(a.models.lists, id)
	a.settleDefault()
	a.saveSettings()
}

// addModelName adds a model to a provider, by the name its service has for
// it; the first model there is becomes the default.
func (a *app) addModelName(m *modelConfig, name string) {
	name = strings.TrimSpace(name)
	if name == "" || slices.Contains(m.Models, name) {
		return
	}
	m.Models = append(m.Models, name)
	a.settleDefault()
	a.saveSettings()
}

// removeModelName takes a model from a provider.
func (a *app) removeModelName(m *modelConfig, name string) {
	m.Models = slices.DeleteFunc(m.Models, func(s string) bool { return s == name })
	a.settleDefault()
	a.saveSettings()
}

// testProvider sends one of the provider's models a short message, off the
// UI's thread, and shows how it went: the default model if it is the
// provider's, else its first.
func (a *app) testProvider(m *modelConfig) {
	id := m.ID
	if a.models.tests == nil {
		a.models.tests = map[int]modelTest{}
	}
	if len(m.Models) == 0 {
		a.models.tests[id] = modelTest{message: "先添加一个模型，测试要用它发一句话"}
		return
	}
	model := m.Models[0]
	if a.settings.Model == id && slices.Contains(m.Models, a.settings.ModelName) {
		model = a.settings.ModelName
	}
	cfg, ok := a.config(m, model)
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
				a.models.tests[id] = modelTest{ok: true, message: "连接正常，" + model + " 有回应"}
			}
		}
	})
}

// fetchModels asks the provider's service which models it has, off the
// UI's thread, to choose from.
func (a *app) fetchModels(m *modelConfig) {
	id := m.ID
	if a.models.lists == nil {
		a.models.lists = map[int]modelList{}
	}
	cfg, ok := a.config(m, "")
	if !ok {
		a.models.lists[id] = modelList{message: "保存的 Key 打不开，请重新填一次"}
		return
	}
	a.models.lists[id] = modelList{running: true}
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		names, err := a.listModels(ctx, cfg)
		return func() {
			if err != nil {
				a.models.lists[id] = modelList{message: err.Error()}
			} else {
				a.models.lists[id] = modelList{names: names}
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

// providersBeside is the least width of the window that has room for the
// providers beside the one that shows; narrower, they go above it.
const providersBeside = 1120

// modelSettings are the providers, and their models, the app's features
// can use: the providers down the left, and the one chosen beside them.
func (a *app) modelSettings(c *ui.Context, _ func(name, note string, width float32, control func())) {
	ui.Text(c, "AI 功能会用标了“默认”的那个模型。Key 加密后保存在这台电脑上，不会明文写进数据文件。").
		FontSize(13).TextColor(mallow.Muted).Padding(4, 4, 16, 4)
	if len(a.settings.Models) == 0 {
		ui.Text(c, "还没有供应商。").FontSize(14).TextColor(mallow.Muted).Padding(8, 4)
		ui.Row(c).Padding(8, 4, 0, 4).Children(func() {
			if mallow.Button(c, "添加供应商").Clicked() {
				a.addProvider()
			}
		})
		return
	}
	if a.model(a.models.open) == nil {
		a.openProvider(a.settings.Models[0].ID)
	}
	providers := func() {
		items := make([]mallow.Item, len(a.settings.Models))
		for i := range a.settings.Models {
			m := &a.settings.Models[i]
			items[i] = mallow.Item{ID: strconv.Itoa(m.ID), Label: providerName(m)}
			if m.ID == a.settings.Model && a.settings.ModelName != "" {
				items[i].Note = "默认"
			}
		}
		chosen := strconv.Itoa(a.models.open)
		if mallow.SourceList(c, "供应商", &chosen, items) {
			id, _ := strconv.Atoi(chosen)
			a.openProvider(id)
		}
		ui.Row(c).Padding(8, 0, 0, 0).Children(func() {
			if mallow.Button(c, "添加供应商").Clicked() {
				a.addProvider()
			}
		})
	}
	remove := false
	detail := func(e ui.Element) {
		// Built after the list, which may have chosen another.
		if m := a.model(a.models.open); m != nil {
			remove = a.providerDetail(c.Key(m.ID), e, m)
		}
	}
	if w, _ := c.Size(); w >= providersBeside {
		ui.Row(c).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Width(176).Shrink(0).Gap(4).Children(providers)
			mallow.GrooveV(c).Margin(0, 20)
			detail(ui.Column(c).Grow(1).MinWidth(0))
		})
	} else {
		ui.Column(c).Gap(4).Children(providers)
		mallow.Groove(c).Margin(16, 0)
		detail(ui.Column(c))
	}
	if remove {
		a.removeProvider(a.models.open)
	}
}

// providerName is what a provider is called, one without a name too.
func providerName(m *modelConfig) string {
	if name := strings.TrimSpace(m.Name); name != "" {
		return name
	}
	return "未命名的供应商"
}

// providerDetail fills e with the provider that shows: what edits it, and
// its models. It reports whether the provider is to be deleted.
func (a *app) providerDetail(c *ui.Context, e ui.Element, m *modelConfig) (remove bool) {
	line := func(name string, width float32, control func()) {
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			mallow.Label(c, name).Width(48).Shrink(0).Padding(9, 0, 0, 0)
			ui.Column(c).Grow(1).MinWidth(0).MaxWidth(width).Gap(6).Children(control)
		})
	}
	changed := false
	e.Gap(14).Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			if mallow.TextInput(c, &m.Name).Placeholder("供应商的名字，随便起").Label("供应商名称").FontSize(16).Grow(1).MinWidth(0).Margin(0, 8, 0, 0).Changed() {
				changed = true
			}
			if mallow.GhostIconButton(c, "删除供应商", mallow.GlyphTrash, mallow.IconButtonStyle{Danger: true}).Clicked() {
				remove = true
			}
		})
		line("接口", 440, func() {
			kind := m.Kind
			options := make([]mallow.Option, len(kindLabels))
			for i, k := range kindLabels {
				options[i] = mallow.Option{Value: string(k.kind), Label: k.label}
			}
			if mallow.Segmented(c, "接口类型", &kind, options) {
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
			test := a.models.tests[m.ID]
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				hint := "粘贴 API Key"
				if m.Key != "" {
					hint = "已保存。要换一个，就在这里重新粘贴"
				}
				if mallow.TextInput(c, &a.models.key).Password().Placeholder(hint).Label("API Key").Grow(1).MinWidth(0).Changed() {
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
				if mallow.Button(c, "测试连接").Clicked() && !test.running {
					a.testProvider(m)
					test = a.models.tests[m.ID]
				}
			})
			switch {
			case test.running:
				ui.Text(c, "正在测试…").FontSize(12).TextColor(mallow.Muted)
			case test.ok:
				ui.Text(c, test.message).FontSize(12).TextColor(mallow.Success)
			case test.message != "":
				ui.Text(c, test.message).FontSize(12).TextColor(mallow.Danger)
			}
		})
		mallow.Groove(c).Margin(4, 0)
		a.providerModels(c, m)
	})
	if changed {
		// What was tested, and what was listed, is no longer what is set.
		delete(a.models.tests, m.ID)
		delete(a.models.lists, m.ID)
		a.saveSettings()
	}
	a.modelAdder(c, m)
	return remove
}

// providerModels are a provider's models: each to make the default or to
// take away, under a button that opens the dialog adding more.
func (a *app) providerModels(c *ui.Context, m *modelConfig) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		mallow.Label(c, "模型").Grow(1)
		if mallow.Button(c, "添加模型").Clicked() {
			a.startAddingModels(m)
		}
	})
	if len(m.Models) == 0 {
		ui.Text(c, "还没有模型。").FontSize(13).TextColor(mallow.Muted)
		return
	}
	var drop string
	ui.Column(c).Gap(2).Children(func() {
		for _, name := range m.Models {
			ui.Row(c.Key("model:" + name)).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).Grow(1).MinWidth(0).FontSize(14).TextColor(mallow.Text).SingleLine()
				if a.isDefault(m, name) {
					ui.Text(c, "默认").FontSize(12).FontWeight(500).TextColor(mallow.Accent).Padding(0, 8)
				} else if mallow.Plain(c, "把"+name+"设为默认").Padding(4, 8).Children(func() {
					ui.Text(c, "设为默认").FontSize(12).TextColor(mallow.Muted)
				}).Clicked() {
					a.settings.Model, a.settings.ModelName = m.ID, name
					a.saveSettings()
				}
				if mallow.GhostIconButton(c, "移除"+name, mallow.GlyphClose, mallow.IconButtonStyle{Danger: true}).Clicked() {
					drop = name
				}
			})
		}
	})
	if drop != "" {
		a.removeModelName(m, drop)
	}
}

// listShown is how many of the models a service lists show at most: some
// services list hundreds, and what is typed narrows them.
const listShown = 60

// modelAdder is the dialog that adds models to a provider. What is typed
// looks through the models its service lists, each added with its plus;
// and it is a model's name itself, which Enter adds, for the services that
// list none. It stays open, to add several.
func (a *app) modelAdder(c *ui.Context, m *modelConfig) {
	mallow.Dialog(c, &a.models.adding, mallow.DialogOptions{Title: "添加模型", Width: 480}, func() {
		field := mallow.TextInput(c, &a.models.draft).Placeholder("搜索，或者直接填模型名").Label("搜索或填写模型名").AutoFocus()
		typed := strings.TrimSpace(a.models.draft)
		if field.Submitted() {
			a.addModelName(m, typed)
			a.models.draft, typed = "", ""
		}
		list := a.models.lists[m.ID]
		offer := func(name, note string) {
			ui.Row(c.Key("offer:" + name)).Gap(8).MinHeight(36).AlignItems(ui.Center).Children(func() {
				ui.Text(c, name).Grow(1).MinWidth(0).FontSize(14).TextColor(mallow.Text).SingleLine()
				if slices.Contains(m.Models, name) {
					ui.Text(c, "已添加").FontSize(12).TextColor(mallow.Muted).Padding(0, 8)
					return
				}
				if note != "" {
					ui.Text(c, note).FontSize(12).TextColor(mallow.Muted)
				}
				if mallow.GhostIconButton(c, "添加"+name, mallow.GlyphPlus, mallow.IconButtonStyle{}).Clicked() {
					a.addModelName(m, name)
				}
			})
		}
		// As tall whatever it holds: the dialog does not jump as the list
		// comes in or is narrowed.
		ui.Scroll(c).Label("可添加的模型").Height(300).Shrink(1).MinHeight(0).Gap(2).Children(func() {
			if typed != "" && !slices.Contains(list.names, typed) {
				offer(typed, "按这个名字添加")
			}
			switch {
			case list.running:
				ui.Text(c, "正在向服务要模型列表…").FontSize(13).TextColor(mallow.Muted).Padding(8, 0)
			case list.message != "":
				ui.Text(c, "没拿到模型列表："+list.message).FontSize(13).TextColor(mallow.Danger).Padding(8, 0)
				ui.Text(c, "也可以在上面直接填模型名，按回车添加。").FontSize(13).TextColor(mallow.Muted)
			case len(list.names) == 0:
				ui.Text(c, "填好地址和 Key 后，这里会列出服务有的模型。现在可以在上面直接填模型名，按回车添加。").
					FontSize(13).TextColor(mallow.Muted).Padding(8, 0)
			default:
				shown, more := 0, 0
				for _, name := range list.names {
					if !strings.Contains(strings.ToLower(name), strings.ToLower(typed)) {
						continue
					}
					if shown == listShown {
						more++
						continue
					}
					shown++
					offer(name, "")
				}
				switch {
				case shown == 0:
					ui.Text(c, "服务的列表里没有名字带这几个字的模型。").FontSize(13).TextColor(mallow.Muted).Padding(8, 0)
				case more > 0:
					ui.Text(c, fmt.Sprintf("还有 %d 个没显示，在上面输入关键字来找。", more)).FontSize(12).TextColor(mallow.Muted).Padding(8, 0)
				}
			}
		})
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			if mallow.Button(c, "刷新列表").Disabled(list.running).Clicked() {
				a.fetchModels(m)
			}
			ui.Box(c).Grow(1)
			if mallow.PrimaryButton(c, "完成").Clicked() {
				a.models.adding = false
			}
		})
	})
}
