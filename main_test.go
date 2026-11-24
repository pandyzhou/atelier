package main

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/pandyzhou/atelier/internal/features/todo"
	"github.com/pandyzhou/atelier/internal/llm"
	"github.com/pandyzhou/atelier/internal/store"
	"github.com/pandyzhou/mallow"
)

// The view runs without a window in tests, which click and type as a user
// would. An empty store keeps nothing on disk.
func newTodoTester(t *testing.T) (*todo.Todo, *ui.Tester) {
	t.Helper()
	a := newApp(store.Open(""))
	a.win = &fakeWindow{}
	tt := ui.NewTester(a.view, 1280, 800)
	if !tt.HasText("待办") {
		t.Fatalf("sidebar missing the to-do list, texts %q", tt.Texts())
	}
	return a.current().(*todo.Todo), tt
}

func click(t *testing.T, tt *ui.Tester, labels ...string) {
	t.Helper()
	for _, label := range labels {
		if err := tt.Click(label); err != nil {
			t.Fatalf("click %q: %v (texts %q)", label, err, tt.Texts())
		}
	}
}

// add types a task into the adder of the view shown.
func add(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	click(t, tt, "添加任务")
	tt.Type(title)
	tt.Key(0, ui.KeyEnter)
}

func TestTodoAddAndCheck(t *testing.T) {
	d, tt := newTodoTester(t)
	add(t, tt, "写周报")
	tasks := d.Tasks()
	if len(tasks) != 1 || tasks[0].Due != d.Today() || !tt.HasText("写周报") {
		t.Fatalf("after adding in 今天: tasks %+v, texts %q", tasks, tt.Texts())
	}
	click(t, tt, "完成写周报")
	if !tasks[0].Done || !tt.HasText("今天完成的") {
		t.Errorf("after checking off: %+v, texts %q", tasks[0], tt.Texts())
	}
}

func TestTodoDetail(t *testing.T) {
	d, tt := newTodoTester(t)
	add(t, tt, "交水电费")
	k := d.Tasks()[0]
	click(t, tt, "打开交水电费", "下周一", "高", "每月")
	if k.Due <= d.Today() || k.Priority != todo.PriorityHigh || k.Repeat != todo.RepeatMonthly {
		t.Fatalf("after editing: %+v", k)
	}
	due := k.Due
	// Checked off, a repeating task leaves a done copy and moves on.
	click(t, tt, "完成这项任务")
	if n := len(d.Tasks()); n != 2 || k.Done || k.Due <= due || !d.Tasks()[1].Done {
		t.Fatalf("after checking off a monthly task: %d tasks, %+v", n, k)
	}
	click(t, tt, "删除任务")
	if n := len(d.Tasks()); n != 1 || d.Tasks()[0].Repeat != todo.RepeatNone {
		t.Fatalf("after deleting: %+v", d.Tasks())
	}
}

// rightClick opens the context menu of what is labelled so.
func rightClick(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.RightClick(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

// A right click on a task edits it.
func TestTodoContextMenus(t *testing.T) {
	d, tt := newTodoTester(t)
	add(t, tt, "Buy milk")
	rightClick(t, tt, "打开Buy milk")
	click(t, tt, "改到明天")
	if k := d.Tasks()[0]; k.Due == d.Today() {
		t.Fatalf("the task moved to tomorrow is due %s, today", k.Due)
	}
	click(t, tt, "最近 7 天")
	rightClick(t, tt, "打开Buy milk")
	click(t, tt, "删除任务")
	if len(d.Tasks()) != 0 {
		t.Errorf("tasks after deleting from the menu: %+v", d.Tasks())
	}
}

// Tasks are not sorted into lists: nothing of lists shows.
func TestTodoNoLists(t *testing.T) {
	_, tt := newTodoTester(t)
	add(t, tt, "Buy milk")
	click(t, tt, "打开Buy milk")
	for _, text := range []string{"清单", "收集箱", "新建清单"} {
		if tt.HasText(text) {
			t.Errorf("the page shows %q: %q", text, tt.Texts())
		}
	}
}

// The + opens a new task's details at once: what repeats is set there
// before the title is done. One left without a title is not kept.
func TestTodoStart(t *testing.T) {
	d, tt := newTodoTester(t)
	click(t, tt, "添加任务", "每周", "任务标题")
	tt.Type("Check upstream")
	tt.Key(0, ui.KeyEnter)
	if ks := d.Tasks(); len(ks) != 1 || ks[0].Title != "Check upstream" || ks[0].Repeat != todo.RepeatWeekly {
		t.Fatalf("after starting a weekly task: %+v", ks)
	}
	click(t, tt, "添加任务")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if n := len(d.Tasks()); n != 1 {
		t.Fatalf("%d tasks after starting one and leaving it blank", n)
	}
	// Starting another closes the one that was open.
	click(t, tt, "添加任务", "添加任务")
	tt.Type("Buy milk")
	tt.Key(0, ui.KeyEnter)
	if ks := d.Tasks(); len(ks) != 2 || ks[1].Title != "Buy milk" {
		t.Fatalf("after starting twice and writing once: %+v", ks)
	}
}

// Of the chips for the day, one is chosen at a time: 其他日期 brings the
// calendar without looking chosen beside another, and is the day itself
// once one is picked there.
func TestTodoOtherDate(t *testing.T) {
	d, tt := newTodoTester(t)
	add(t, tt, "Buy milk")
	k := d.Tasks()[0]
	click(t, tt, "打开Buy milk", "其他日期")
	if !tt.HasText("其他日期") || k.Due != d.Today() {
		t.Fatalf("opening the calendar changed the day to %q", k.Due)
	}
	day, _ := time.Parse("2006-01-02", d.Today())
	far := day.AddDate(0, 0, 3)
	if far.Month() != day.Month() {
		far = day.AddDate(0, 0, -3)
	}
	click(t, tt, fmt.Sprintf("%d月%d日", int(far.Month()), far.Day()))
	if k.Due != far.Format("2006-01-02") || !tt.HasText(fmt.Sprintf("%d月%d日", int(far.Month()), far.Day())) {
		t.Fatalf("after picking a day in the calendar: due %q, texts %q", k.Due, tt.Texts())
	}
	// A quick day closes the calendar again.
	click(t, tt, "无日期")
	if k.Due != "" || tt.HasText("上个月") {
		t.Errorf("after 无日期: due %q, the calendar still shows: %v", k.Due, tt.HasText("上个月"))
	}
}

// A task is made to repeat weekly from its menu.
func TestTodoWeeklyRepeat(t *testing.T) {
	d, tt := newTodoTester(t)
	add(t, tt, "Check upstream")
	add(t, tt, "Buy milk")
	ks := d.Tasks()
	rightClick(t, tt, "打开Buy milk")
	click(t, tt, "设为每周重复")
	if ks[1].Repeat != todo.RepeatWeekly || ks[1].Due == "" {
		t.Fatalf("after 设为每周重复: repeat %q, due %q", ks[1].Repeat, ks[1].Due)
	}
	rightClick(t, tt, "打开Buy milk")
	click(t, tt, "取消重复")
	if ks[1].Repeat != todo.RepeatNone {
		t.Errorf("after 取消重复: repeat %q", ks[1].Repeat)
	}
}

func TestTodoWeekView(t *testing.T) {
	d, tt := newTodoTester(t)
	// Added in this week, a task is due today; in another, on its Monday.
	click(t, tt, "按周")
	add(t, tt, "大扫除")
	if k := d.Tasks()[0]; k.Due != d.Today() {
		t.Fatalf("task added in this week is due %s, today is %s", k.Due, d.Today())
	}
	click(t, tt, "下一周")
	if tt.HasText("大扫除") {
		t.Errorf("next week shows this week's task")
	}
	add(t, tt, "Water plants")
	day, err := time.Parse("2006-01-02", d.Tasks()[1].Due)
	if err != nil || day.Weekday() != time.Monday || d.Tasks()[1].Due <= d.Today() {
		t.Fatalf("task added in next week: %+v", d.Tasks()[1])
	}
}

// A group's heading folds its tasks away and back; the list's count stays.
func TestTodoFold(t *testing.T) {
	_, tt := newTodoTester(t)
	add(t, tt, "Buy milk")
	if !tt.HasText("1") {
		t.Errorf("the nav shows no count for today's one task")
	}
	click(t, tt, "收起今天")
	if tt.HasText("Buy milk") {
		t.Errorf("the folded group still shows its task")
	}
	click(t, tt, "展开今天")
	if !tt.HasText("Buy milk") {
		t.Errorf("the unfolded group does not show its task")
	}
}

// TestScreenshot renders the window without opening it and writes a PNG to
// the path in ATELIER_SCREENSHOT, to look at the UI from scripts:
//
//	ATELIER_SCREENSHOT=shot.png go test -run Screenshot
//
// ATELIER_SCREENSHOT_CLICK lists what to click first, separated by commas,
// as "按周" for the week view or "打开写周报" for a task's details.
func TestScreenshot(t *testing.T) {
	path := os.Getenv("ATELIER_SCREENSHOT")
	if path == "" {
		t.Skip("set ATELIER_SCREENSHOT to write a screenshot")
	}
	a := newApp(store.Open(""))
	// ATELIER_SCREENSHOT_STYLE picks the style: neumorphism or organic.
	a.settings.Style = os.Getenv("ATELIER_SCREENSHOT_STYLE")
	// ATELIER_SCREENSHOT_THEME picks the appearance: light, dark or system.
	if m := os.Getenv("ATELIER_SCREENSHOT_THEME"); m != "" {
		a.settings.Appearance = m
	}
	a.win = &fakeWindow{}
	d := a.current().(*todo.Todo)
	today, _ := time.Parse("2006-01-02", d.Today())
	day := func(n int) string { return today.AddDate(0, 0, n).Format("2006-01-02") }
	for _, k := range []todo.Task{
		{Title: "交水电费", Due: day(-1), Priority: todo.PriorityHigh},
		{Title: "写周报", Due: day(0), Repeat: todo.RepeatWeekly, Priority: todo.PriorityMid},
		{Title: "修登录页的 bug", Due: day(0), Note: "复现步骤在群里"},
		{Title: "跑步 30 分钟", Due: day(0), Repeat: todo.RepeatDaily},
		{Title: "给家里打电话", Due: day(1)},
		{Title: "读完《置身事内》", Due: day(3), Priority: todo.PriorityLow},
		{Title: "采购日用品", Due: day(5)},
		{Title: "整理书架"},
	} {
		d.Add(k)
	}
	d.SetDone(d.Add(todo.Task{Title: "预约体检", Due: day(0)}), true)
	tt := ui.NewTester(a.view, 1280, 800)
	if os.Getenv("ATELIER_SCREENSHOT_SCALE") == "2" {
		tt.SetScale(2)
	}
	if s := os.Getenv("ATELIER_SCREENSHOT_CLICK"); s != "" {
		click(t, tt, strings.Split(s, ",")...)
	}
	tt.Move(5, 400)
	// ATELIER_SCREENSHOT_RIGHTCLICK opens the context menu of a label.
	if s := os.Getenv("ATELIER_SCREENSHOT_RIGHTCLICK"); s != "" {
		rightClick(t, tt, s)
	}
	// Let the 200 ms transitions finish before taking the picture.
	tt.Advance(time.Second)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

// The appearance follows the system until the user picks one, which is saved.
func TestAppearance(t *testing.T) {
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetDark(true)
	tt.Frame()
	if mallow.Base != mallow.DarkPalette.Base {
		t.Fatalf("following a dark system, base %v", mallow.Base)
	}
	// The switch is in the first part of the settings; the version in
	// another, chosen in the menu down their side.
	click(t, tt, "设置", "浅色")
	if tt.HasText("版本") {
		t.Errorf("the settings show the version beside how the app looks")
	}
	click(t, tt, "关于")
	if !tt.HasText("版本") || !tt.HasText("开发版") || tt.HasText("浅色") {
		t.Errorf("the part about the app: %q", tt.Texts())
	}
	if a.settings.Appearance != string(mallow.ModeLight) || mallow.Base != mallow.LightPalette.Base {
		t.Fatalf("after choosing light: appearance %q, base %v", a.settings.Appearance, mallow.Base)
	}
	// The gear goes back to the feature the settings were opened from.
	click(t, tt, "设置")
	if a.selected != "todo" {
		t.Errorf("after the gear a second time, %q is selected", a.selected)
	}
	// A new start reads the choice back, whatever the system is.
	b := newApp(store.Open(dir))
	tb := ui.NewTester(b.view, 1280, 800)
	tb.SetDark(true)
	tb.Frame()
	if b.settings.Appearance != string(mallow.ModeLight) || mallow.Base != mallow.LightPalette.Base {
		t.Fatalf("after restarting: appearance %q, base %v", b.settings.Appearance, mallow.Base)
	}
}

// The style is chosen on the settings page, and saved.
func TestStyle(t *testing.T) {
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if mallow.CurrentLook() != mallow.Neumorphism {
		t.Fatalf("a first start is not in neumorphism")
	}
	click(t, tt, "设置", "自然有机")
	if a.settings.Style != string(mallow.StyleOrganic) || mallow.CurrentLook() != mallow.Organic {
		t.Fatalf("after choosing 自然有机: style %q", a.settings.Style)
	}
	// The to-do page works as it does in the other look.
	click(t, tt, "待办")
	add(t, tt, "Buy milk")
	click(t, tt, "打开Buy milk", "高", "关闭详情")
	if !tt.HasText("Buy milk") || !tt.HasText("!!!") {
		t.Errorf("the task edited on the organic to-do page: %q", tt.Texts())
	}
	b := newApp(store.Open(dir))
	ui.NewTester(b.view, 1280, 800).Frame()
	if b.settings.Style != string(mallow.StyleOrganic) || mallow.CurrentLook() != mallow.Organic {
		t.Errorf("after restarting: style %q", b.settings.Style)
	}
}

// The size zooms the whole interface, and the font is the theme's: both are
// saved.
func TestTextSettings(t *testing.T) {
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	tt := ui.NewTester(a.view, 1280, 800)
	click(t, tt, "设置", "特大")
	if a.settings.Size != "xlarge" || zoomOf(a.settings.Size) != 1.25 {
		t.Fatalf("after choosing 特大: size %q", a.settings.Size)
	}
	// At the largest size the page still works.
	click(t, tt, "待办")
	add(t, tt, "Buy milk")
	if !tt.HasText("Buy milk") {
		t.Errorf("no task added at the largest size: %q", tt.Texts())
	}
	// The default font is always offered; another where it is installed.
	click(t, tt, "设置", "微软雅黑")
	if a.settings.Font != "" || mallow.Font != defaultFont {
		t.Errorf("after choosing the default font: %q, drawn in %q", a.settings.Font, mallow.Font)
	}
	// Not one to download: this test asks nothing of the network.
	for _, f := range a.fontChoices()[1:] {
		if f.web != nil {
			continue
		}
		click(t, tt, f.label)
		tt.Frame()
		if a.settings.Font != f.family || mallow.Font != f.family {
			t.Errorf("after choosing %s: %q, drawn in %q", f.label, a.settings.Font, mallow.Font)
		}
		break
	}
	b := newApp(store.Open(dir))
	ui.NewTester(b.view, 1280, 800).Frame()
	if b.settings.Size != "xlarge" || b.settings.Font != a.settings.Font {
		t.Errorf("after restarting: size %q, font %q", b.settings.Size, b.settings.Font)
	}
	mallow.Font = defaultFont
}

// Choosing another font moves nothing: every text of the pages is at the
// height it was, and as high, in each font the system has, at each size.
// Only what a font draws in its place differs.
func TestFontsKeepLayout(t *testing.T) {
	defer func() { mallow.Font = defaultFont }()
	a := newApp(store.Open(""))
	st := ui.NewTester(a.view, 1280, 800)
	add(t, st, "Buy milk")
	click(t, st, "打开Buy milk")

	places := func(tt *ui.Tester) map[string]place {
		tt.Frame()
		tt.Frame()
		out := map[string]place{}
		for _, text := range tt.Texts() {
			if r, ok := tt.Find(text); ok {
				out[text] = place{r.Y, r.H}
			}
		}
		return out
	}
	fonts := 0
	for _, page := range []string{"待办", "设置"} {
		click(t, st, page)
		for _, s := range sizes {
			fonts += steady(t, a, s.id, page, func() map[string]place { return places(st) })
		}
	}
	if fonts == 0 {
		t.Skip("the system has none of the fonts to choose from")
	}
}

// place is where a text is down the page, and how high.
type place struct{ y, h float32 }

// steady reports the places that differ, of those places returns, between
// the default font and each other the system has, at a size; and how many
// fonts it looked at.
func steady(t *testing.T, a *app, size, page string, places func() map[string]place) int {
	t.Helper()
	a.settings.Size = size
	var base map[string]place
	fonts := 0
	for _, f := range a.fontChoices() {
		if f.web != nil {
			continue
		}
		a.settings.Font = f.family
		got := places()
		if base == nil {
			base = got
			continue
		}
		fonts++
		for text, want := range base {
			if got[text] != want {
				t.Errorf("%s at size %s in %s: %q is at %+v, in the default font at %+v", page, size, f.label, text, got[text], want)
			}
		}
	}
	return fonts
}

// A font the system does not have is downloaded when chosen, kept, and
// used again after a restart without another download.
func TestWebFont(t *testing.T) {
	dir := t.TempDir()
	font := &webFont{family: "Test Web Font", file: "test.ttf", size: 4}
	webFonts["测试字体"] = font
	knownFonts = append(knownFonts, struct {
		label string
		names []string
	}{"测试字体", []string{"Test Web Font"}})
	defer func() {
		delete(webFonts, "测试字体")
		knownFonts = knownFonts[:len(knownFonts)-1]
		mallow.Font = defaultFont
	}()

	fetched, fetchErr := 0, error(nil)
	var registered []string
	fake := func(a *app) {
		a.webFonts.fetch = func(_ context.Context, _ *webFont, progress func(float64)) ([]byte, error) {
			fetched++
			progress(0.5)
			return []byte("font"), fetchErr
		}
		a.webFonts.register = func(data []byte, family string) error {
			registered = append(registered, family+":"+string(data))
			return nil
		}
	}
	a := newApp(store.Open(dir))
	fake(a)
	tt := ui.NewTester(a.view, 1280, 800)
	click(t, tt, "设置")
	// A download that fails says so and changes nothing.
	fetchErr = errFontChanged
	click(t, tt, "测试字体 ↓")
	tt.Frame()
	if a.settings.Font != "" || !tt.HasText("测试字体 没有下载成功：下载到的文件和预期的不一样") {
		t.Fatalf("after a failed download: font %q, %q", a.settings.Font, tt.Texts())
	}
	fetchErr = nil
	click(t, tt, "测试字体 ↓")
	tt.Frame()
	if a.settings.Font != font.family || mallow.Font != font.family || !slices.Equal(registered, []string{"Test Web Font:font"}) {
		t.Fatalf("after downloading: font %q, drawn in %q, registered %q", a.settings.Font, mallow.Font, registered)
	}
	// Here, it no longer says it is to download, and is not fetched again.
	if !tt.HasText("测试字体") || tt.HasText("测试字体 ↓") {
		t.Errorf("the font downloaded still says it is to download: %q", tt.Texts())
	}
	click(t, tt, "微软雅黑", "测试字体")
	if fetched != 2 || a.settings.Font != font.family || len(registered) != 1 {
		t.Errorf("choosing it again: fetched %d times, font %q, registered %q", fetched, a.settings.Font, registered)
	}

	// After a restart it is read from where it was kept.
	registered = nil
	b := newApp(store.Open(dir))
	fake(b)
	b.restoreFont()
	if !slices.Equal(registered, []string{"Test Web Font:font"}) || fetched != 2 {
		t.Errorf("after restarting: registered %q, fetched %d times", registered, fetched)
	}
}

// A known font is found under the name its foundry gives the version
// installed, and the plain family before its weights.
func TestInstalledFont(t *testing.T) {
	families := []string{"Alibaba PuHuiTi 3.0 Heavy", "Alibaba PuHuiTi 3.0", "SimSun-ExtB", "SimSun", "noto sans sc"}
	for _, c := range []struct {
		names []string
		want  string
	}{
		{[]string{"Alibaba PuHuiTi"}, "Alibaba PuHuiTi 3.0"},
		{[]string{"SimSun"}, "SimSun"},
		{[]string{"Source Han Sans SC", "Noto Sans SC"}, "noto sans sc"},
		{[]string{"LXGW WenKai"}, ""},
	} {
		if got := installed(families, c.names); got != c.want {
			t.Errorf("installed(%q) = %q, want %q", c.names, got, c.want)
		}
	}
}

// A model is added, filled in and tested in the settings; its key is saved
// sealed, and opened again for the features that use the model.
func TestModelSettings(t *testing.T) {
	dir := t.TempDir()
	a := newApp(store.Open(dir))
	var pinged []llm.Config
	var answer error
	a.ping = func(_ context.Context, c llm.Config) error {
		pinged = append(pinged, c)
		return answer
	}
	tt := ui.NewTester(a.view, 1280, 800)
	click(t, tt, "设置", "模型", "添加模型")
	if len(a.settings.Models) != 1 || a.settings.Model != a.settings.Models[0].ID {
		t.Fatalf("after adding a model: %+v, default %d", a.settings.Models, a.settings.Model)
	}
	if _, ok := a.DefaultModel(); !ok {
		t.Errorf("the model added is not ready as the default")
	}
	// Tested without a key, nothing is sent that could work: it says so.
	click(t, tt, "Chat Completions", "接口地址")
	tt.Type("https://api.example.com/v1")
	click(t, tt, "模型名")
	tt.Type("some-model")
	click(t, tt, "API Key")
	tt.Type("sk-secret-123")
	tt.Frame()
	m := a.settings.Models[0]
	if m.Kind != string(llm.Chat) || m.BaseURL != "https://api.example.com/v1" || m.Model != "some-model" {
		t.Fatalf("the model filled in: %+v", m)
	}
	if m.Key == "" || strings.Contains(m.Key, "sk-secret") {
		t.Fatalf("the key is saved as %q", m.Key)
	}
	if tt.HasText("sk-secret-123") {
		t.Errorf("the key shows on the page")
	}
	// The settings file holds the sealed key alone.
	if data, err := os.ReadFile(filepath.Join(dir, "settings.json")); err != nil || strings.Contains(string(data), "sk-secret") {
		t.Fatalf("settings.json: %v, holds the key: %v", err, strings.Contains(string(data), "sk-secret"))
	}

	click(t, tt, "测试连接")
	tt.Frame()
	want := llm.Config{Kind: llm.Chat, BaseURL: "https://api.example.com/v1", Key: "sk-secret-123", Model: "some-model"}
	if len(pinged) != 1 || pinged[0] != want || !tt.HasText("连接正常，模型有回应") {
		t.Fatalf("the test sent %+v; texts %q", pinged, tt.Texts())
	}
	answer = &llm.Error{Message: "Key 不对，或者没有权限（401）"}
	click(t, tt, "测试连接")
	tt.Frame()
	if !tt.HasText("Key 不对，或者没有权限（401）") {
		t.Errorf("a failed test does not say why: %q", tt.Texts())
	}

	// After a restart the model is there, and its key opens.
	b := newApp(store.Open(dir))
	if c, ok := b.DefaultModel(); !ok || c != want {
		t.Fatalf("after restarting, the default model is %+v, %v", c, ok)
	}
	// A second model can be made the default; deleting it gives that back.
	click(t, tt, "添加模型", "设为默认")
	second := a.settings.Models[1].ID
	if a.settings.Model != second {
		t.Fatalf("after 设为默认: default %d, want %d", a.settings.Model, second)
	}
	click(t, tt, "删除模型")
	if len(a.settings.Models) != 1 || a.settings.Model != a.settings.Models[0].ID {
		t.Errorf("after deleting the default model: %+v, default %d", a.settings.Models, a.settings.Model)
	}
}

// A newer version is found in the settings, installed, and run by a
// relaunch; a build that cannot update says so.
func TestUpdate(t *testing.T) {
	a := newApp(store.Open(""))
	tt := ui.NewTester(a.view, 1280, 800)
	click(t, tt, "设置", "关于")
	if !tt.HasText("这个版本不能自动更新：它不是发布版，或者装在了没有写入权限的位置") || tt.HasText("检查更新") {
		t.Fatalf("a build without updates: %q", tt.Texts())
	}

	var found *newVersion
	var checkErr, installErr error
	relaunched := false
	a.updater = updater{
		enabled:  func() bool { return true },
		check:    func(context.Context) (*newVersion, error) { return found, checkErr },
		relaunch: func() { relaunched = true },
	}
	// Quiet, as at launch, a check that finds nothing says nothing.
	a.checkForUpdate(true)
	tt.Frame()
	if a.update.phase != updateIdle || !tt.HasText("检查更新") {
		t.Fatalf("after a quiet check that found nothing: phase %d", a.update.phase)
	}
	click(t, tt, "检查更新")
	tt.Frame()
	if !tt.HasText("已经是最新版本") {
		t.Fatalf("after a check that found nothing: %q", tt.Texts())
	}

	found = &newVersion{
		version: "9.9.9",
		notes:   "### 新功能\n\n可以自动更新了\n",
		install: func(_ context.Context, progress func(float64)) error {
			progress(0.5)
			return installErr
		},
	}
	// Found as the app starts, it waits in the settings, with its notes.
	a.checkForUpdate(true)
	tt.Frame()
	if a.update.phase != updateAvailable || !tt.HasText("发现新版本 v9.9.9") || !tt.HasText("· 新功能\n\n可以自动更新了") {
		t.Fatalf("after finding a version: phase %d, %q", a.update.phase, tt.Texts())
	}
	// A download that fails can be tried again.
	installErr = errors.New("connection reset")
	click(t, tt, "下载并安装")
	tt.Frame()
	if a.update.phase != updateFailed || !tt.HasText("没有成功：connection reset") {
		t.Fatalf("after a failed download: phase %d, %q", a.update.phase, tt.Texts())
	}
	installErr = nil
	click(t, tt, "再试一次")
	tt.Frame()
	if a.update.phase != updateReady || !tt.HasText("v9.9.9 已经装好，重启后生效") {
		t.Fatalf("after installing: phase %d, %q", a.update.phase, tt.Texts())
	}
	click(t, tt, "立即重启")
	if !relaunched {
		t.Errorf("立即重启 did not relaunch the app")
	}
}

func TestPlainNotes(t *testing.T) {
	got := plainNotes("### 标题一\n\n\n正文\n- 一条\n\n## 标题二\r\n")
	if want := "· 标题一\n\n正文\n- 一条\n\n· 标题二"; got != want {
		t.Errorf("plainNotes: %q, want %q", got, want)
	}
}

// fakeWindow records what the title bar's buttons ask of the window.
type fakeWindow struct {
	maximized bool
	calls     []string
}

func (w *fakeWindow) Minimize()         { w.calls = append(w.calls, "minimize") }
func (w *fakeWindow) Maximize()         { w.calls = append(w.calls, "maximize"); w.maximized = true }
func (w *fakeWindow) Unmaximize()       { w.calls = append(w.calls, "unmaximize"); w.maximized = false }
func (w *fakeWindow) Close()            { w.calls = append(w.calls, "close") }
func (w *fakeWindow) IsMaximized() bool { return w.maximized }

// The drawn title bar's buttons drive the window, the middle one maximizing
// or restoring as the window is.
func TestTitleBarButtons(t *testing.T) {
	win := &fakeWindow{}
	a := newApp(store.Open(""))
	a.win = win
	tt := ui.NewTester(a.view, 1280, 800)
	for _, label := range []string{"最小化", "最大化", "还原", "关闭"} {
		if err := tt.Click(label); err != nil {
			t.Fatalf("click %s: %v (texts %q)", label, err, tt.Texts())
		}
	}
	want := "minimize maximize unmaximize close"
	if got := strings.Join(win.calls, " "); got != want {
		t.Errorf("calls %q, want %q", got, want)
	}
}
