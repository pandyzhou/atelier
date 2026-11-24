package home

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
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
		{"a program", []byte{0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0xFF, 0xFF}, "不是图片、文档，也不是文本文件"},
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

// madePDF is a PDF of one page that draws what content says, in Helvetica.
func madePDF(content string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, o := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, start)
	return b.Bytes()
}

// madeWord is a .docx, or another file of the kind, with body as the XML
// of the part named part.
func madeWord(t *testing.T, part, body string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, err := z.Create(part)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>%s</w:body></w:document>`, body)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestReadDocuments checks that a PDF and a Word document are sent as the
// text they hold, line by line, and what is said of those that have none
// or are of a kind that is not read.
func TestReadDocuments(t *testing.T) {
	// Two lines, the first of two pieces far apart, as columns are; drawn
	// the lower line first.
	page := "BT /F1 12 Tf 72 700 Td (Tuesday English) Tj 0 20 Td (Monday Math) Tj 200 0 Td (Room A301) Tj ET"
	p, err := read("课表.pdf", madePDF(page))
	if err != nil || p.Type != "" || p.Text != "Monday Math Room A301\nTuesday English" {
		t.Errorf("a PDF: %q, %v", p.Text, err)
	}
	if _, err := read("扫描.pdf", madePDF("0 0 m 10 10 l S")); err == nil || !strings.Contains(err.Error(), "可能是扫描件") {
		t.Errorf("a PDF without text: %v", err)
	}
	if _, err := read("坏的.pdf", []byte("%PDF-1.4\nnothing of a PDF")); err == nil || !strings.Contains(err.Error(), "读不出来") {
		t.Errorf("a broken PDF: %v", err)
	}

	body := `<w:p><w:r><w:t>第一周</w:t></w:r><w:r><w:t xml:space="preserve"> 安排</w:t></w:r></w:p>` +
		`<w:p><w:r><w:instrText>PAGE</w:instrText></w:r><w:r><w:t>交报告</w:t><w:tab/><w:t>周三</w:t><w:br/><w:t>地点待定</w:t></w:r></w:p>` +
		`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>周一</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>高数</w:t></w:r></w:p></w:tc></w:tr>` +
		`<w:tr><w:tc><w:p><w:r><w:t>周二</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>英语</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`
	p, err = read("安排.docx", madeWord(t, "word/document.xml", body))
	if want := "第一周 安排\n交报告\t周三\n地点待定\n周一\t高数\n周二\t英语"; err != nil || p.Text != want {
		t.Errorf("a Word document: %q, %v", p.Text, err)
	}
	if _, err := read("空的.docx", madeWord(t, "word/document.xml", "<w:p/>")); err == nil || !strings.Contains(err.Error(), "里没有文字") {
		t.Errorf("a Word document without text: %v", err)
	}
	if _, err := read("旧的.doc", []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}); err == nil || !strings.Contains(err.Error(), "另存为新版格式") {
		t.Errorf("an old Word document: %v", err)
	}
}

// madeOffice is a zip file of the parts given, as the newer Office files
// are: each a name and what is in it.
func madeOffice(t *testing.T, parts ...string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for i := 0; i+1 < len(parts); i += 2 {
		w, err := z.Create(parts[i])
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, parts[i+1])
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestReadWorkbooksAndSlides checks that a workbook is sent as the rows of
// its sheets, with dates and numbers as its sheets show them, and a
// presentation as the text of its slides, in their order.
func TestReadWorkbooksAndSlides(t *testing.T) {
	book := madeOffice(t,
		"xl/workbook.xml", `<workbook xmlns:r="r"><sheets><sheet name="课表" sheetId="1" r:id="rId2"/><sheet name="空的" sheetId="2" r:id="rId3"/><sheet name="成绩" sheetId="3" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels", `<Relationships><Relationship Id="rId1" Target="worksheets/sheet3.xml"/><Relationship Id="rId2" Target="/xl/worksheets/sheet1.xml"/><Relationship Id="rId3" Target="worksheets/sheet2.xml"/></Relationships>`,
		"xl/sharedStrings.xml", `<sst><si><t>星期</t></si><si><r><t>高等</t></r><r><t>数学</t></r><rPh><t>こうとう</t></rPh></si><si><t>交报告</t></si></sst>`,
		"xl/styles.xml", `<styleSheet><numFmts><numFmt numFmtId="176" formatCode="yyyy&quot;年&quot;m&quot;月&quot;d&quot;日&quot;"/><numFmt numFmtId="177" formatCode="0.00&quot;d&quot;"/></numFmts><cellStyleXfs><xf numFmtId="14"/></cellStyleXfs><cellXfs><xf numFmtId="0"/><xf numFmtId="176"/><xf numFmtId="20"/><xf numFmtId="177"/><xf numFmtId="22"/></cellXfs></styleSheet>`,
		"xl/worksheets/sheet1.xml", `<worksheet><sheetData>`+
			`<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="D1" t="inlineStr"><is><t>A301</t></is></c></row>`+
			`<row r="2"></row>`+
			`<row r="3"><c r="A3" t="s"><v>2</v></c><c r="B3" s="1"><v>46359</v></c><c r="C3" s="2"><v>0.375</v></c><c r="D3" s="4"><v>46359.75</v></c></row>`+
			`</sheetData></worksheet>`,
		"xl/worksheets/sheet2.xml", `<worksheet><sheetData/></worksheet>`,
		"xl/worksheets/sheet3.xml", `<worksheet><sheetData><row><c><v>0.30000000000000004</v></c><c s="3"><v>46359</v></c><c t="b"><v>1</v></c><c t="str"><f>A1</f><v>合计</v></c><c><v>1200000</v></c></row></sheetData></worksheet>`,
	)
	want := "【工作表：课表】\n星期\t高等数学\t\tA301\n交报告\t2026-12-03\t09:00\t2026-12-03 18:00\n\n【工作表：成绩】\n0.3\t46359\t是\t合计\t1200000"
	if p, err := read("课表.xlsx", book); err != nil || p.Text != want {
		t.Errorf("a workbook: %q, %v", p.Text, err)
	}
	if _, err := read("空.xlsx", madeOffice(t, "xl/workbook.xml", `<workbook/>`)); err == nil || !strings.Contains(err.Error(), "里没有文字") {
		t.Errorf("a workbook of nothing: %v", err)
	}

	slide := func(body string) string {
		return `<p:sld xmlns:p="p" xmlns:a="a"><p:cSld>` + body + `</p:cSld></p:sld>`
	}
	deck := madeOffice(t,
		"ppt/presentation.xml", `<p:presentation xmlns:p="p" xmlns:r="r"><p:sldMasterIdLst><p:sldMasterId id="1" r:id="rId9"/></p:sldMasterIdLst><p:sldIdLst><p:sldId id="256" r:id="rId3"/><p:sldId id="257" r:id="rId2"/><p:sldId id="258" r:id="rId4"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels", `<Relationships><Relationship Id="rId2" Target="slides/slide1.xml"/><Relationship Id="rId3" Target="slides/slide2.xml"/><Relationship Id="rId4" Target="slides/slide3.xml"/><Relationship Id="rId9" Target="slideMasters/slideMaster1.xml"/></Relationships>`,
		"ppt/slides/slide1.xml", slide(`<a:p><a:r><a:t>作业要求</a:t></a:r></a:p><a:p><a:r><a:t>周三前</a:t></a:r><a:br/><a:r><a:t>交到教务处</a:t></a:r></a:p>`),
		"ppt/slides/slide2.xml", slide(`<a:p><a:r><a:t>第三周</a:t></a:r><a:r><a:t> 安排</a:t></a:r></a:p><a:tbl><a:tr><a:tc><a:txBody><a:p><a:r><a:t>周一</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>高数</a:t></a:r></a:p></a:txBody></a:tc></a:tr></a:tbl>`),
		"ppt/slides/slide3.xml", slide(`<a:p/>`),
		"ppt/slideMasters/slideMaster1.xml", slide(`<a:p><a:r><a:t>单击此处编辑母版标题</a:t></a:r></a:p>`),
	)
	want = "【第 1 页】\n第三周 安排\n周一\t高数\n\n【第 2 页】\n作业要求\n周三前\n交到教务处"
	if p, err := read("安排.pptx", deck); err != nil || p.Text != want {
		t.Errorf("a presentation: %q, %v", p.Text, err)
	}
	if _, err := read("别的.zip", madeOffice(t, "a.txt", "x")); err == nil || !strings.Contains(err.Error(), "还读不了") {
		t.Errorf("another zip file: %v", err)
	}
}
