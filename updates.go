package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/pandyzhou/mallow"
)

// Updates come from the releases of the app's repository on GitHub: each
// build publishes a signed archive and a manifest there, and MyGo's updater
// downloads the newer version, checks it against the public key the app
// was built with, and replaces the app's files. See mygo.json's "updates"
// and the release workflow.

// newVersion is a version newer than the running one.
type newVersion struct {
	version, notes string
	// install downloads it and replaces the app's files, telling how far
	// the download got, from 0 to 1. The new version runs after a relaunch.
	install func(ctx context.Context, progress func(float64)) error
}

// updater is where new versions come from: MyGo's, but in tests.
type updater struct {
	// enabled reports whether this build can update itself: a released
	// build, in a place it can write to.
	enabled func() bool
	// check returns a newer version, nil when the app is up to date.
	check    func(ctx context.Context) (*newVersion, error)
	relaunch func()
}

// mygoUpdater is the updater of released builds.
func mygoUpdater() updater {
	return updater{
		enabled: mygo.Updater.Enabled,
		check: func(ctx context.Context) (*newVersion, error) {
			up, err := mygo.Updater.Check(ctx)
			if err != nil || up == nil {
				return nil, err
			}
			return &newVersion{
				version: up.Version,
				notes:   up.Notes,
				install: func(ctx context.Context, progress func(float64)) error {
					return up.Install(ctx, func(downloaded, total int64) {
						if total > 0 {
							progress(float64(downloaded) / float64(total))
						}
					})
				},
			}, nil
		},
		relaunch: func() { mygo.App.Relaunch() },
	}
}

// How the update stands.
const (
	updateIdle        = iota // not checked yet
	updateChecking           // asking for a newer version
	updateCurrent            // the app is the newest
	updateAvailable          // found is newer
	updateDownloading        // found is being installed
	updateReady              // found is installed: a relaunch runs it
	updateFailed             // message says what went wrong
)

// updateState is what the app knows of a newer version.
type updateState struct {
	phase    int
	found    *newVersion
	progress float64 // of the download, 0 to 1
	message  string  // what went wrong, for updateFailed
}

// onUI runs fn on the UI's thread and shows the window anew; in tests,
// which have no such window, at once.
func (a *app) onUI(fn func()) {
	if win, ok := a.win.(interface{ Update(func()) }); ok {
		win.Update(fn)
		return
	}
	fn()
}

// checkForUpdate asks for a newer version. Quiet, as at launch, it says
// nothing when there is none or the check fails: nobody asked.
func (a *app) checkForUpdate(quiet bool) {
	if !a.updater.enabled() || a.update.phase == updateChecking || a.update.phase == updateDownloading || a.update.phase == updateReady {
		return
	}
	if !quiet {
		a.update = updateState{phase: updateChecking}
	}
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		found, err := a.updater.check(ctx)
		return func() {
			switch {
			case err != nil && quiet:
			case err != nil:
				a.update = updateState{phase: updateFailed, message: updateError(err)}
			case found != nil:
				a.update = updateState{phase: updateAvailable, found: found}
			case !quiet:
				a.update = updateState{phase: updateCurrent}
			}
		}
	})
}

// installUpdate downloads the version found and puts it in the app's place.
func (a *app) installUpdate() {
	found := a.update.found
	if found == nil || a.update.phase != updateAvailable {
		return
	}
	a.update.phase, a.update.progress = updateDownloading, 0
	a.background(func() func() {
		// No deadline: a slow line is slow, not stuck; MyGo's download
		// gives up on one that stalls.
		last := -1
		err := found.install(context.Background(), func(p float64) {
			// A frame for each percent, not for each packet.
			if pct := int(p * 100); pct != last {
				last = pct
				a.onUI(func() { a.update.progress = p })
			}
		})
		return func() {
			if err != nil {
				a.update = updateState{phase: updateFailed, found: found, message: updateError(err)}
				return
			}
			a.update = updateState{phase: updateReady, found: found}
		}
	})
}

// updateError says what went wrong with an update in the user's words.
func updateError(err error) string {
	switch {
	case errors.Is(err, mygo.ErrUpdatesDisabled):
		return "这个版本不能自动更新"
	case errors.Is(err, context.DeadlineExceeded):
		return "超时了，没有连上 GitHub"
	}
	return "没有成功：" + err.Error()
}

// updateRows are the rows about updating, in the part of the settings
// about the app.
func (a *app) updateRows(c *ui.Context, row func(name, note string, width float32, control func())) {
	u := a.update
	note := "新版本发布在 GitHub，下载后会校验签名"
	if !a.updater.enabled() {
		// A build run from the source, or one installed where it cannot
		// write, as under Program Files.
		row("更新", "这个版本不能自动更新：它不是发布版，或者装在了没有写入权限的位置", 0, func() {})
		return
	}
	switch u.phase {
	case updateChecking:
		note = "正在检查…"
	case updateCurrent:
		note = "已经是最新版本"
	case updateAvailable:
		note = "发现新版本 v" + u.found.version
	case updateDownloading:
		note = fmt.Sprintf("正在下载 v%s：%d%%", u.found.version, int(u.progress*100))
	case updateReady:
		note = "v" + u.found.version + " 已经装好，重启后生效"
	case updateFailed:
		note = u.message
	}
	row("更新", note, 0, func() {
		switch u.phase {
		case updateAvailable:
			if mallow.PrimaryButton(c, "下载并安装").Clicked() {
				a.installUpdate()
			}
		case updateReady:
			if mallow.PrimaryButton(c, "立即重启").Clicked() {
				a.updater.relaunch()
			}
		case updateChecking, updateDownloading:
		case updateFailed:
			if mallow.Button(c, "再试一次").Clicked() {
				if u.found != nil {
					a.update = updateState{phase: updateAvailable, found: u.found}
					a.installUpdate()
				} else {
					a.checkForUpdate(false)
				}
			}
		default:
			if mallow.Button(c, "检查更新").Clicked() {
				a.checkForUpdate(false)
			}
		}
	})
	// What the new version brings, as its release says.
	if u.found != nil && (u.phase == updateAvailable || u.phase == updateDownloading || u.phase == updateReady) {
		if notes := plainNotes(u.found.notes); notes != "" {
			mallow.Card(c, mallow.Small).Margin(4, 4, 12, 4).Padding(14, 18).Gap(4).Children(func() {
				mallow.Label(c, "v"+u.found.version+" 的更新内容")
				ui.Text(c, notes).FontSize(13).TextColor(mallow.Text).LineHeight(1.6)
			})
		}
	}
}

// plainNotes turns a release's notes, Markdown, into text to read as it
// is: headings lose their marks, the rest stays.
func plainNotes(md string) string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(md), "\n") {
		line = strings.TrimRight(line, " \r")
		if h := strings.TrimLeft(line, "#"); h != line {
			line = "· " + strings.TrimSpace(h)
		}
		// One blank line at most.
		if line == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
