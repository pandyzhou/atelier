package home

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// painted is a picture w by h: red in its left half, blue in its right.
func painted(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if x < w/2 {
				img.SetRGBA(x, y, color.RGBA{220, 30, 30, 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{30, 30, 220, 255})
			}
		}
	}
	return img
}

func encoded(t *testing.T, encode func(*bytes.Buffer) error) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := encode(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// oriented puts Exif data that says orientation o into a JPEG.
func oriented(data []byte, o uint16) []byte {
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1}
	tiff = binary.BigEndian.AppendUint16(tiff, 0x0112) // the tag
	tiff = binary.BigEndian.AppendUint16(tiff, 3)      // a short
	tiff = binary.BigEndian.AppendUint32(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, o)
	tiff = append(tiff, 0, 0, 0, 0, 0, 0)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(seg)+2))
	out = append(out, seg...)
	return append(out, data[2:]...)
}

// TestReadPictures checks what is sent of a picture: a small JPEG or PNG
// as it is, a large one or one of another format as a JPEG no longer than
// maxSide, and one its file says is turned, the way up it is shown.
func TestReadPictures(t *testing.T) {
	small := encoded(t, func(b *bytes.Buffer) error { return png.Encode(b, painted(40, 20)) })
	p, err := read("a.png", small)
	if err != nil || p.Type != "image/png" || !bytes.Equal(p.Data, small) || p.Text != "" {
		t.Fatalf("a small PNG: %q, %d bytes, %v", p.Type, len(p.Data), err)
	}

	large := encoded(t, func(b *bytes.Buffer) error { return png.Encode(b, painted(4000, 1000)) })
	p, err = read("b.png", large)
	if err != nil || p.Type != "image/jpeg" {
		t.Fatalf("a large PNG: %q, %v", p.Type, err)
	}
	if cfg, err := jpeg.DecodeConfig(bytes.NewReader(p.Data)); err != nil || cfg.Width != maxSide || cfg.Height != maxSide/4 {
		t.Errorf("a large PNG is sent as %+v, %v", cfg, err)
	}

	moving := encoded(t, func(b *bytes.Buffer) error { return gif.Encode(b, painted(30, 10), nil) })
	if p, err = read("c.gif", moving); err != nil || p.Type != "image/jpeg" {
		t.Errorf("a GIF: %q, %v", p.Type, err)
	}

	// Orientation 6 is shown turned a quarter clockwise: what is at the
	// left of the file is at the top.
	photo := oriented(encoded(t, func(b *bytes.Buffer) error { return jpeg.Encode(b, painted(40, 20), nil) }), 6)
	if o := orientation(photo); o != 6 {
		t.Fatalf("orientation reads %d", o)
	}
	p, err = read("d.jpg", photo)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(p.Data))
	if err != nil || img.Bounds().Dx() != 20 || img.Bounds().Dy() != 40 {
		t.Fatalf("a turned photo is sent as %v, %v", img.Bounds(), err)
	}
	top, _, bottom, _ := img.At(10, 5).RGBA()
	_, _, low, _ := img.At(10, 35).RGBA()
	if top>>8 < 150 || low>>8 < 150 || bottom>>8 > 100 {
		t.Errorf("a turned photo: red %d at the top, blue %d there and %d at the bottom", top>>8, bottom>>8, low>>8)
	}
	// A JPEG that is as it is shown goes as it is.
	plain := encoded(t, func(b *bytes.Buffer) error { return jpeg.Encode(b, painted(40, 20), nil) })
	if p, err = read("e.jpg", oriented(plain, 1)); err != nil || p.Type != "image/jpeg" || len(p.Data) != len(oriented(plain, 1)) {
		t.Errorf("an upright JPEG: %q, %d bytes, %v", p.Type, len(p.Data), err)
	}
}

// TestReadText checks which files are read as text, in which encodings,
// and what is said of those that are not sent.
func TestReadText(t *testing.T) {
	units := utf16.Encode([]rune("课表"))
	wide := []byte{0xFF, 0xFE}
	for _, u := range units {
		wide = binary.LittleEndian.AppendUint16(wide, u)
	}
	for _, c := range []struct {
		name string
		data []byte
		want string
	}{
		{"UTF-8", []byte("周一 高数"), "周一 高数"},
		{"UTF-8 with a mark", append([]byte{0xEF, 0xBB, 0xBF}, "周一"...), "周一"},
		{"UTF-16", wide, "课表"},
		{"GBK", []byte{0xD6, 0xDC, 0xD2, 0xBB}, "周一"},
	} {
		if p, err := read("a.txt", c.data); err != nil || p.Text != c.want || p.Type != "" {
			t.Errorf("%s: read %q, %v", c.name, p.Text, err)
		}
	}
	for _, c := range []struct {
		name string
		data []byte
		want string
	}{
		{"a program", []byte{0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0xFF, 0xFF}, "不是图片，也不是文本文件"},
		{"nothing", []byte("  \n"), "是空的"},
		{"too much", bytes.Repeat([]byte("字"), maxText), "太长了"},
	} {
		if _, err := read("a.bin", c.data); err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "a.bin") {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	dir := t.TempDir()
	if _, err := load(dir); err == nil || !strings.Contains(err.Error(), "是文件夹") {
		t.Errorf("a folder: %v", err)
	}
	if _, err := load(filepath.Join(dir, "none.txt")); err == nil || !strings.Contains(err.Error(), "打不开") {
		t.Errorf("a file that is not there: %v", err)
	}
	path := filepath.Join(dir, "笔记.md")
	if err := os.WriteFile(path, []byte("# 笔记"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, err := load(path); err != nil || p.Name != "笔记.md" || p.Text != "# 笔记" {
		t.Errorf("a file of text: %+v, %v", p, err)
	}
}
