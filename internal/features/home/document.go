package home

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/ledongthuc/pdf"
)

// Documents are sent as the text they hold: a PDF's as its pages show it,
// line by line, and a Word document's paragraph by paragraph. What a page
// only shows as a picture, as one that was scanned, is not read.

var (
	errNoText    = errors.New("no text")
	errNotOffice = errors.New("not an Office document")
	errUnreadble = errors.New("unreadable")
)

// maxPages is how many pages of a PDF are read.
const maxPages = 300

// pdfText returns the text of a PDF, its pages one after another.
func pdfText(data []byte) (text string, err error) {
	// The reader panics at what it does not expect of a file.
	defer func() {
		if recover() != nil {
			text, err = "", errUnreadble
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", errUnreadble
	}
	var b strings.Builder
	for i := 1; i <= min(r.NumPage(), maxPages); i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		if lines := pageLines(page.Content().Text); lines != "" {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(lines)
		}
		if b.Len() > maxText {
			break // more than is sent: said by whoever asked
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		return "", errNoText
	}
	return b.String(), nil
}

// wide reports whether r is a character as wide as it is tall, as the
// Chinese ones, between which no space is written.
func wide(r rune) bool { return r >= 0x2E80 }

// narrow is about how wide a character that is not a wide one is drawn, in
// the size of its font: as the common fonts of Latin text have it.
func narrow(r rune) float64 {
	switch {
	case strings.ContainsRune("ijl.,;:'|!It ", r):
		return 0.28
	case strings.ContainsRune("fr()[]-\"/", r):
		return 0.34
	case strings.ContainsRune("mwMW@%", r):
		return 0.86
	case r >= 'A' && r <= 'Z':
		return 0.68
	}
	return 0.52
}

// pageLines puts the characters of a page, each where it is drawn, into
// lines from the top down, with spaces where the page leaves room.
func pageLines(glyphs []pdf.Text) string {
	type line struct {
		y      float64
		glyphs []pdf.Text
	}
	var lines []*line
	var last *line
	for _, g := range glyphs {
		if g.S == "" {
			continue
		}
		near := func(l *line) bool { return l != nil && math.Abs(l.y-g.Y) < max(2, 0.4*math.Abs(g.FontSize)) }
		if !near(last) {
			last = nil
			for _, l := range lines {
				if near(l) {
					last = l
					break
				}
			}
			if last == nil {
				last = &line{y: g.Y}
				lines = append(lines, last)
			}
		}
		last.glyphs = append(last.glyphs, g)
	}
	slices.SortStableFunc(lines, func(a, b *line) int {
		switch {
		case a.y > b.y:
			return -1
		case a.y < b.y:
			return 1
		}
		return 0
	})
	var b strings.Builder
	for _, l := range lines {
		// The characters go as the page draws them, which is as they
		// read. Many files do not say how wide their characters are, and
		// place only the first of a run of them: where a run ends is
		// then reckoned from its characters, and how sure that is, from
		// how many of them are of a script whose widths vary.
		var row strings.Builder
		var start, end, unsure float64 // of the run that is being drawn
		for i, g := range l.glyphs {
			size := math.Abs(g.FontSize)
			if i == 0 {
				start, end, unsure = g.X, g.X, 0
			} else if g.X != l.glyphs[i-1].X {
				// A run begins: after room for a space, or back before
				// the last one, it is apart from it.
				if g.X-end > 0.2*size+0.1*unsure || g.X < start-0.5*size {
					row.WriteString(" ")
				}
				start, end, unsure = g.X, g.X, 0
			}
			row.WriteString(g.S)
			switch r := []rune(g.S)[0]; {
			case g.W > 0:
				end = g.X + g.W
			case wide(r):
				end += size
			default:
				w := size * narrow(r)
				end, unsure = end+w, unsure+w
			}
		}
		if text := strings.TrimSpace(strings.Join(strings.Fields(row.String()), " ")); text != "" {
			b.WriteString(text + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// maxDocument is the most of a Word document's text, as it is written in
// the file, that is read.
const maxDocument = 64 << 20

// wordText returns the text of a Word document, a .docx: its paragraphs
// each on a line, and the rows of its tables with their cells apart.
func wordText(data []byte) (string, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", errNotOffice
	}
	var body *zip.File
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			body = f
		}
	}
	if body == nil {
		return "", errNotOffice
	}
	f, err := body.Open()
	if err != nil {
		return "", errUnreadble
	}
	defer f.Close()
	var b strings.Builder
	d := xml.NewDecoder(io.LimitReader(f, maxDocument))
	inText, cells := false, 0 // within a run of text; how deep in cells of tables
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", errUnreadble
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				b.WriteString("\t")
			case "br", "cr":
				b.WriteString("\n")
			case "tc":
				cells++
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				// A paragraph in a cell stays on the row's line.
				if cells > 0 {
					b.WriteString(" ")
				} else {
					b.WriteString("\n")
				}
			case "tc":
				cells = max(cells-1, 0)
				b.WriteString("\t")
			case "tr":
				b.WriteString("\n")
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
	// The lines, without the room at their ends and the empty ones in a row.
	var out strings.Builder
	empty := true
	for _, l := range strings.Split(b.String(), "\n") {
		l = strings.TrimSpace(strings.ReplaceAll(l, " \t", "\t"))
		if l == "" {
			if !empty {
				out.WriteString("\n")
			}
			empty = true
			continue
		}
		out.WriteString(l + "\n")
		empty = false
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", errNoText
	}
	return strings.TrimSpace(out.String()), nil
}

// document reads the text of a file that is a document of a kind known by
// how it begins, and reports whether it is one.
func document(name string, data []byte) (text string, is bool, err error) {
	switch {
	case bytes.HasPrefix(data, []byte("%PDF-")):
		text, err = pdfText(data)
		switch err {
		case errNoText:
			err = fmt.Errorf("“%s”里没有能读出来的文字，可能是扫描件", name)
		case nil:
		default:
			err = fmt.Errorf("“%s”这份 PDF 读不出来，可能加了密", name)
		}
		return text, true, err
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		text, err = officeText(data)
		switch err {
		case errNotOffice:
			err = fmt.Errorf("“%s”这种文件还读不了，文档只支持 PDF、Word、Excel 和 PPT", name)
		case errNoText:
			err = fmt.Errorf("“%s”里没有文字", name)
		case nil:
		default:
			err = fmt.Errorf("“%s”这份文档读不出来", name)
		}
		return text, true, err
	case bytes.HasPrefix(data, []byte{0xD0, 0xCF, 0x11, 0xE0}):
		return "", true, fmt.Errorf("“%s”是旧版的 Office 文件，请另存为新版格式（.docx、.xlsx、.pptx）再添加", name)
	}
	return "", false, nil
}
