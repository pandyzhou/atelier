package home

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	_ "image/gif" // pictures that are read
	_ "image/png"

	"github.com/egoist/mygo/ui"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/pandyzhou/atelier/internal/llm"
)

// attached is a file a message of the user's came with, as the page shows
// it: a picture, kept in the files' folder, or a file of text, whose text
// went into what the model is sent.
type attached struct {
	Name string `json:"name"`
	// Image is what a picture is kept under, "" for a file of text.
	Image string `json:"image,omitempty"`
}

// pending is a file to send with the next message.
type pending struct {
	Name string
	// Type is a picture's media type, and Data the picture; a file of text
	// has neither, and its Text.
	Type string
	Data []byte
	Text string
	// shown is the picture as the field shows it, once it has.
	shown *ui.Bitmap
}

const (
	maxFile    = 20 << 20  // the largest file that is read
	maxText    = 200 << 10 // the most text of a file that is sent
	maxSide    = 1568      // the longest side of a picture that is sent
	maxPicture = 1 << 20   // the largest picture sent as it is
	maxPixels  = 80e6      // the largest picture that is read
	maxPending = 8         // the most files a message comes with
)

// load reads the file at path, to send.
func load(path string) (pending, error) {
	name := filepath.Base(path)
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return pending{}, fmt.Errorf("“%s”打不开", name)
	case info.IsDir():
		return pending{}, fmt.Errorf("“%s”是文件夹，只能添加文件", name)
	case info.Size() > maxFile:
		return pending{}, fmt.Errorf("“%s”太大了，最多 20 MB", name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return pending{}, fmt.Errorf("“%s”打不开", name)
	}
	return read(name, data)
}

// read makes of a file's data what is sent of it: a picture, no larger
// than a model looks at, or the text it holds, a PDF's and a Word
// document's too.
func read(name string, data []byte) (pending, error) {
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		if float64(cfg.Width)*float64(cfg.Height) > maxPixels {
			return pending{}, fmt.Errorf("“%s”这张图片太大了", name)
		}
		kind, data, err := picture(data, cfg, format)
		if err != nil {
			return pending{}, fmt.Errorf("“%s”这张图片读不出来", name)
		}
		return pending{Name: name, Type: kind, Data: data}, nil
	}
	text, ok, err := document(name, data)
	if err != nil {
		return pending{}, err
	}
	if !ok {
		text, ok = decodeText(data)
	}
	switch {
	case !ok:
		return pending{}, fmt.Errorf("“%s”不是图片、文档，也不是文本文件", name)
	case strings.TrimSpace(text) == "":
		return pending{}, fmt.Errorf("“%s”是空的", name)
	case len(text) > maxText:
		return pending{}, fmt.Errorf("“%s”太长了，文字最多 200 KB", name)
	}
	return pending{Name: name, Text: text}, nil
}

// picture returns a picture to send, and its media type: the file as it
// is when it is a small enough JPEG or PNG, else drawn again as a JPEG no
// longer than maxSide, the way up its file says it is.
func picture(data []byte, cfg image.Config, format string) (string, []byte, error) {
	turned := 1
	if format == "jpeg" {
		turned = orientation(data)
	}
	if (format == "jpeg" || format == "png") && turned == 1 && max(cfg.Width, cfg.Height) <= maxSide && len(data) <= maxPicture {
		return "image/" + format, data, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", nil, err
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	if long := max(w, h); long > maxSide {
		w, h = max(1, w*maxSide/long), max(1, h*maxSide/long)
	}
	// On white, which a JPEG has in place of what is seen through.
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, turn(dst, turned), &jpeg.Options{Quality: 85}); err != nil {
		return "", nil, err
	}
	return "image/jpeg", out.Bytes(), nil
}

// orientation reads which way up a JPEG says it is to be shown, from its
// Exif data: 1, as it is, when it says nothing, else 2 to 8.
func orientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data) && data[i] == 0xFF; {
		marker, n := data[i+1], int(data[i+2])<<8|int(data[i+3])
		if marker == 0xDA || n < 2 || i+2+n > len(data) {
			break
		}
		seg := data[i+4 : i+2+n]
		i += 2 + n
		if marker != 0xE1 || len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
			continue
		}
		tiff := seg[6:]
		var order binary.ByteOrder
		switch string(tiff[:2]) {
		case "II":
			order = binary.LittleEndian
		case "MM":
			order = binary.BigEndian
		default:
			return 1
		}
		at := int(order.Uint32(tiff[4:8]))
		if at < 8 || at+2 > len(tiff) {
			return 1
		}
		for k := range int(order.Uint16(tiff[at:])) {
			e := at + 2 + 12*k
			if e+12 > len(tiff) {
				break
			}
			if order.Uint16(tiff[e:]) == 0x0112 {
				if o := int(order.Uint16(tiff[e+8:])); o >= 1 && o <= 8 {
					return o
				}
				return 1
			}
		}
		return 1
	}
	return 1
}

// turn returns src the way up an Exif orientation says it is shown.
func turn(src *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		for x := range dw {
			sx, sy := x, y
			switch o {
			case 2:
				sx = w - 1 - x
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sy = h - 1 - y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			dst.SetRGBA(x, y, src.RGBAAt(sx, sy))
		}
	}
	return dst
}

// decodeText returns the text a file holds, and whether it is one of
// text: in UTF-8, UTF-16 with its mark, or the Chinese encoding of older
// Windows programs.
func decodeText(data []byte) (string, bool) {
	clean := func(s string) (string, bool) {
		return s, !strings.ContainsAny(s, "\x00�")
	}
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		data = data[3:]
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}), bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		var order binary.ByteOrder = binary.LittleEndian
		if data[0] == 0xFE {
			order = binary.BigEndian
		}
		units := make([]uint16, 0, len(data)/2)
		for i := 2; i+1 < len(data); i += 2 {
			units = append(units, order.Uint16(data[i:]))
		}
		return clean(string(utf16.Decode(units)))
	}
	if utf8.Valid(data) {
		return clean(string(data))
	}
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data); err == nil {
		return clean(string(out))
	}
	return "", false
}

// Attach reads files to send with the next message; what cannot be sent
// of them is said in the field.
func (h *Home) Attach(paths ...string) {
	if len(paths) == 0 {
		return
	}
	over := false
	if room := maxPending - len(h.ui.pending) - h.ui.loading; len(paths) > room {
		paths, over = paths[:max(room, 0)], true
	}
	h.ui.loading += len(paths)
	h.Background(func() func() {
		var got []pending
		var problems []string
		for _, path := range paths {
			if p, err := load(path); err != nil {
				problems = append(problems, err.Error())
			} else {
				got = append(got, p)
			}
		}
		return func() { h.attach(len(paths), got, problems, over) }
	})
}

// AttachPicture takes a picture that is not a file, as one pasted, to send
// with the next message.
func (h *Home) AttachPicture(data []byte) {
	if len(h.ui.pending)+h.ui.loading >= maxPending {
		h.attach(0, nil, nil, true)
		return
	}
	h.ui.loading++
	h.Background(func() func() {
		p, err := read("粘贴的图片", data)
		if err != nil {
			return func() { h.attach(1, nil, []string{err.Error()}, false) }
		}
		return func() { h.attach(1, []pending{p}, nil, false) }
	})
}

// attach takes what was read of n files into the field.
func (h *Home) attach(n int, got []pending, problems []string, over bool) {
	h.ui.loading = max(h.ui.loading-n, 0)
	h.ui.pending = append(h.ui.pending, got...)
	if over {
		problems = append(problems, fmt.Sprintf("一次最多带 %d 个附件", maxPending))
	}
	h.ui.note = strings.Join(problems, "；")
}

// Detach takes a file out of those to send.
func (h *Home) Detach(i int) {
	if i >= 0 && i < len(h.ui.pending) {
		h.ui.pending = append(h.ui.pending[:i:i], h.ui.pending[i+1:]...)
	}
	h.ui.note = ""
}

// filesDir is where the pictures of the conversations are kept.
func (h *Home) filesDir() string { return filepath.Join(h.store.Dir(), "home-files") }

// keepFile keeps a picture that is sent, and returns what it is kept
// under: in the files' folder, or for this run where nothing is saved.
func (h *Home) keepFile(p pending) string {
	h.data.NextFile++
	ext := ".jpg"
	if p.Type == "image/png" {
		ext = ".png"
	}
	name := fmt.Sprintf("%d%s", h.data.NextFile, ext)
	if h.store.Dir() == "" {
		if h.blobs == nil {
			h.blobs = map[string][]byte{}
		}
		h.blobs[name] = p.Data
		return name
	}
	err := os.MkdirAll(h.filesDir(), 0o755)
	if err == nil {
		err = os.WriteFile(filepath.Join(h.filesDir(), name), p.Data, 0o644)
	}
	if err != nil {
		log.Printf("home: keep %s: %v", name, err)
	}
	return name
}

// blob returns a picture that was kept, nil when it is no longer there.
func (h *Home) blob(name string) []byte {
	if b, ok := h.blobs[name]; ok {
		return b
	}
	if h.store.Dir() == "" || name != filepath.Base(name) {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(h.filesDir(), name))
	if err != nil {
		return nil
	}
	return b
}

// forget drops the pictures of a conversation that is no longer saved.
func (h *Home) forget(k *conversation) {
	for _, e := range k.Entries {
		for _, f := range e.Files {
			if f.Image == "" || f.Image != filepath.Base(f.Image) {
				continue
			}
			delete(h.blobs, f.Image)
			if h.store.Dir() != "" {
				os.Remove(filepath.Join(h.filesDir(), f.Image))
			}
		}
	}
}

// pictures returns the conversation as the model is sent it: with the
// data of the pictures that were kept.
func (h *Home) pictures(history []llm.Message) []llm.Message {
	out := make([]llm.Message, len(history))
	copy(out, history)
	for i, m := range out {
		if len(m.Images) == 0 {
			continue
		}
		images := make([]llm.Image, len(m.Images))
		copy(images, m.Images)
		for j := range images {
			images[j].Data = h.blob(images[j].Name)
		}
		out[i].Images = images
	}
	return out
}
