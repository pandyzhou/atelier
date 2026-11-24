package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo/ui"
)

// Fonts the system does not have can still be chosen: the free ones are
// downloaded the first time, into "fonts" in the app's data directory, and
// registered with the UI from there. They are not part of the app, which
// they would make many times larger.

// webFont is a font the app can download: one file, of a version that does
// not change, so that what arrives can be checked against sum.
type webFont struct {
	// family is the name the font is registered under, and the one saved
	// as the font chosen.
	family string
	file   string // its name in the fonts' directory
	url    string
	sum    string // the SHA-256 of the file, in hex
	size   int64  // of the file, in bytes
}

// webFonts are the fonts to download, by their label in knownFonts. All are
// under the SIL Open Font License, which lets anyone pass them on.
var webFonts = map[string]*webFont{
	"霞鹜文楷": {
		family: "LXGW WenKai",
		file:   "LXGWWenKai-Regular-1.530.ttf",
		url:    "https://github.com/lxgw/LxgwWenKai/releases/download/v1.530/LXGWWenKai-Regular.ttf",
		sum:    "2d73eea94ad808999d90c0000c239a84a33b09d4bc6ab6c5b8bbf1eda8a7fc8b",
		size:   25928940,
	},
	"霞鹜新晰黑": {
		family: "LXGW Neo XiHei",
		file:   "LXGWNeoXiHei-1.309.ttf",
		url:    "https://github.com/lxgw/LxgwNeoXiHei/releases/download/v1.309/LXGWNeoXiHei.ttf",
		sum:    "2e938517d1bb2d23c452829bac6c2830a4577d9eebbad22445c5c4191eb4308f",
		size:   7475784,
	},
	"思源黑体": {
		family: "Source Han Sans CN",
		file:   "SourceHanSansCN-VF-2.005.ttf",
		url:    "https://github.com/adobe-fonts/source-han-sans/raw/2.005R/Variable/TTF/Subset/SourceHanSansCN-VF.ttf",
		sum:    "25a01e41b5cc99893eb35a6cd2cc7611841dc19eb03cbaf7f0c1de8210f2ba0b",
		size:   17749860,
	},
	"思源宋体": {
		family: "Source Han Serif CN",
		file:   "SourceHanSerifCN-VF-2.003.ttf",
		url:    "https://github.com/adobe-fonts/source-han-serif/raw/2.003R/Variable/TTF/Subset/SourceHanSerifCN-VF.ttf",
		sum:    "8e052cbcdbd0f03496c9ad05da7d57901549286d5efd30f3caf66a393f6c389b",
		size:   25125104,
	},
}

// fontsState is what the app knows of the fonts it downloads.
type fontsState struct {
	// dir is where they are kept, "" to keep none: in tests.
	dir string
	// registered are the families the UI has been given.
	registered map[string]bool
	// loading is the label of the font being downloaded, "" for none, and
	// progress how far that got, from 0 to 1.
	loading  string
	progress float64
	// message says what went wrong with the last download.
	message string
	// fetch downloads a font and register gives one to the UI: the
	// network and ui.RegisterFont, but in tests.
	fetch    func(ctx context.Context, f *webFont, progress func(float64)) ([]byte, error)
	register func(data []byte, family string) error
}

func newFontsState(dir string) fontsState {
	s := fontsState{registered: map[string]bool{}, fetch: fetchFont, register: ui.RegisterFont}
	if dir != "" {
		s.dir = filepath.Join(dir, "fonts")
	}
	return s
}

// path is where a font is kept, "" when none are.
func (s *fontsState) path(f *webFont) string {
	if s.dir == "" {
		return ""
	}
	return filepath.Join(s.dir, f.file)
}

// has reports whether a font was downloaded already.
func (s *fontsState) has(f *webFont) bool {
	if s.registered[f.family] {
		return true
	}
	if p := s.path(f); p != "" {
		info, err := os.Stat(p)
		return err == nil && info.Size() == f.size
	}
	return false
}

// use gives the UI a font that was downloaded, read from the disk unless
// data is it. A font the UI has already is not given again.
func (s *fontsState) use(f *webFont, data []byte) error {
	if s.registered[f.family] {
		return nil
	}
	if data == nil {
		var err error
		if data, err = os.ReadFile(s.path(f)); err != nil {
			return err
		}
	}
	if err := s.register(data, f.family); err != nil {
		return err
	}
	s.registered[f.family] = true
	return nil
}

// webFontOf returns the downloaded font of a family, nil if it is none.
func webFontOf(family string) *webFont {
	for _, f := range webFonts {
		if f.family == family {
			return f
		}
	}
	return nil
}

// restoreFont gives the UI the font chosen, as the app starts, when it is
// one that was downloaded. The others wait until they are chosen: each is
// tens of megabytes to read.
func (a *app) restoreFont() {
	f := webFontOf(a.settings.Font)
	if f == nil || !a.webFonts.has(f) {
		return
	}
	if err := a.webFonts.use(f, nil); err != nil {
		log.Printf("fonts: %s: %v", f.family, err)
	}
}

// chooseWebFont sets the text in a font to download: at once if it is here
// already, else once it has arrived.
func (a *app) chooseWebFont(label string, f *webFont) {
	s := &a.webFonts
	if s.loading != "" {
		return
	}
	s.message = ""
	if s.has(f) {
		if err := s.use(f, nil); err != nil {
			s.message = label + " 用不了：" + err.Error()
			return
		}
		a.settings.Font = f.family
		a.saveSettings()
		return
	}
	s.loading, s.progress = label, 0
	a.background(func() func() {
		// No deadline: a slow line is slow, not stuck.
		last := -1
		data, err := s.fetch(context.Background(), f, func(p float64) {
			// A frame for each percent, not for each packet.
			if pct := int(p * 100); pct != last {
				last = pct
				a.onUI(func() { s.progress = p })
			}
		})
		if err == nil {
			err = keepFont(s.path(f), data)
		}
		return func() {
			s.loading = ""
			if err == nil {
				err = s.use(f, data)
			}
			if err != nil {
				s.message = label + " 没有下载成功：" + fontError(err)
				return
			}
			a.settings.Font = f.family
			a.saveSettings()
		}
	})
}

// keepFont writes a downloaded font where it is kept; nowhere for "". It
// writes a temporary file first, so a font half written is never read.
func keepFont(path string, data []byte) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// errFontChanged is a download that is not the file expected.
var errFontChanged = errors.New("下载到的文件和预期的不一样")

// fontError says what went wrong with a download in the user's words.
func fontError(err error) string {
	var status *fontStatusError
	switch {
	case errors.Is(err, errFontChanged):
		return errFontChanged.Error()
	case errors.As(err, &status):
		return fmt.Sprintf("下载地址回应 %d", status.code)
	}
	return err.Error()
}

// fontStatusError is an answer to a download that is not the file.
type fontStatusError struct{ code int }

func (e *fontStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

// fontClient downloads fonts. It has no deadline for the whole of one,
// which a slow line would pass, but gives up on a server that says nothing.
var fontClient = &http.Client{Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ResponseHeaderTimeout: 30 * time.Second,
}}

// fetchFont downloads a font and checks it is the file expected, telling
// how far it got, from 0 to 1.
func fetchFont(ctx context.Context, f *webFont, progress func(float64)) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := fontClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &fontStatusError{resp.StatusCode}
	}
	var buf bytes.Buffer
	buf.Grow(int(f.size))
	chunk := make([]byte, 64<<10)
	// One byte more than the file has is enough to know it is another.
	body := io.LimitReader(resp.Body, f.size+1)
	for {
		n, err := body.Read(chunk)
		buf.Write(chunk[:n])
		progress(min(float64(buf.Len())/float64(f.size), 1))
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if sum := sha256.Sum256(buf.Bytes()); int64(buf.Len()) != f.size || hex.EncodeToString(sum[:]) != f.sum {
		return nil, errFontChanged
	}
	return buf.Bytes(), nil
}
