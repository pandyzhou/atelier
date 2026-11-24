package home

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
	"time"
)

// A workbook is sent as its sheets, each row on a line with its cells
// apart, and a presentation as its slides, each with its text line by
// line: what they show of pictures and charts is not read.

// officeText returns the text of a Word document, a workbook or a
// presentation, of the kinds that are zip files of XML.
func officeText(data []byte) (string, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", errNotOffice
	}
	var text string
	switch {
	case member(z, "word/document.xml") != nil:
		return wordText(data)
	case member(z, "xl/workbook.xml") != nil:
		text, err = sheetsText(z)
	case member(z, "ppt/presentation.xml") != nil:
		text, err = slidesText(z)
	default:
		return "", errNotOffice
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", errNoText
	}
	return strings.TrimSpace(text), nil
}

// member returns the file of a name in an archive, nil if it has none.
func member(z *zip.Reader, name string) *zip.File {
	name = strings.TrimPrefix(name, "/")
	for _, f := range z.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// walk reads the XML of a file of an archive, giving each of its tokens
// to visit: nothing of a file that is not there.
func walk(z *zip.Reader, name string, visit func(xml.Token)) error {
	file := member(z, name)
	if file == nil {
		return nil
	}
	f, err := file.Open()
	if err != nil {
		return errUnreadble
	}
	defer f.Close()
	d := xml.NewDecoder(io.LimitReader(f, maxDocument))
	for {
		token, err := d.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errUnreadble
		}
		visit(token)
	}
}

// attr returns an attribute of an element by its name, whatever its
// namespace, "" if it has none.
func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// partsOf returns the files a part of an archive names, in the order it
// lists them by element: those of the relations of its elements' r:id.
func partsOf(z *zip.Reader, part, element string) (names, files []string, err error) {
	dir, base := path.Split(part)
	targets := map[string]string{}
	err = walk(z, dir+"_rels/"+base+".rels", func(t xml.Token) {
		if e, ok := t.(xml.StartElement); ok && e.Name.Local == "Relationship" {
			target := attr(e, "Target")
			if !strings.HasPrefix(target, "/") {
				target = path.Join(dir, target)
			}
			targets[attr(e, "Id")] = target
		}
	})
	if err != nil {
		return nil, nil, err
	}
	err = walk(z, part, func(t xml.Token) {
		if e, ok := t.(xml.StartElement); ok && e.Name.Local == element {
			// The relation's id, which is of a namespace: not the
			// element's own.
			id := ""
			for _, a := range e.Attr {
				if a.Name.Local == "id" && a.Name.Space != "" {
					id = a.Value
				}
			}
			if target := targets[id]; target != "" {
				names, files = append(names, attr(e, "name")), append(files, target)
			}
		}
	})
	return names, files, err
}

// maxColumns is how many cells of a row of a sheet are read.
const maxColumns = 200

// sheetsText returns the text of a workbook: its sheets, each under its
// name, a row on a line with its cells apart.
func sheetsText(z *zip.Reader) (string, error) {
	// The texts that cells share, each the runs of one put together,
	// without how it is read aloud.
	var shared []string
	var one strings.Builder
	in, phonetic := false, 0
	err := walk(z, "xl/sharedStrings.xml", func(t xml.Token) {
		switch t := t.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				one.Reset()
			case "rPh":
				phonetic++
			case "t":
				in = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				shared = append(shared, one.String())
			case "rPh":
				phonetic--
			case "t":
				in = false
			}
		case xml.CharData:
			if in && phonetic == 0 {
				one.Write(t)
			}
		}
	})
	if err != nil {
		return "", err
	}
	dates, err := dateStyles(z)
	if err != nil {
		return "", err
	}
	names, files, err := partsOf(z, "xl/workbook.xml", "sheet")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, file := range files {
		rows, err := sheetRows(z, file, shared, dates)
		if err != nil {
			return "", err
		}
		if rows == "" {
			continue
		}
		fmt.Fprintf(&b, "【工作表：%s】\n%s\n\n", names[i], rows)
		if b.Len() > maxText {
			break // more than is sent: said by whoever asked
		}
	}
	return b.String(), nil
}

// dateStyles reports, for each of a workbook's cell styles, whether the
// number of a cell of that style is a date or a time.
func dateStyles(z *zip.Reader) ([]bool, error) {
	custom := map[int]bool{} // the formats the workbook defines that are of dates
	var styles []bool
	inStyles := false
	err := walk(z, "xl/styles.xml", func(t xml.Token) {
		switch t := t.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "numFmt":
				id, _ := strconv.Atoi(attr(t, "numFmtId"))
				custom[id] = dateFormat(attr(t, "formatCode"))
			case "cellXfs":
				inStyles = true
			case "xf":
				if inStyles {
					id, _ := strconv.Atoi(attr(t, "numFmtId"))
					// The formats every workbook has that are of dates.
					builtin := (id >= 14 && id <= 22) || (id >= 27 && id <= 36) || (id >= 45 && id <= 47) || (id >= 50 && id <= 58)
					styles = append(styles, custom[id] || (builtin && !hasKey(custom, id)))
				}
			}
		case xml.EndElement:
			if t.Name.Local == "cellXfs" {
				inStyles = false
			}
		}
	})
	return styles, err
}

func hasKey(m map[int]bool, k int) bool { _, ok := m[k]; return ok }

// dateFormat reports whether a number format writes a date or a time: it
// has a letter of one outside what it quotes.
func dateFormat(code string) bool {
	quoted, bracket := false, false
	for i := 0; i < len(code); i++ {
		switch c := code[i]; {
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '\\':
			i++
		case c == '[':
			bracket = true
		case c == ']':
			bracket = false
		case bracket:
		case strings.IndexByte("ymdhsYMDHS", c) >= 0:
			return true
		}
	}
	return false
}

// column returns which column a cell's reference, as "AB12", is in, from
// 0, and -1 for none.
func column(ref string) int {
	n := 0
	for _, c := range ref {
		if c < 'A' || c > 'Z' {
			break
		}
		n = n*26 + int(c-'A') + 1
	}
	return n - 1
}

// sheetRows returns the rows of a sheet that have something in them.
func sheetRows(z *zip.Reader, file string, shared []string, dates []bool) (string, error) {
	var b strings.Builder
	var row []string
	var value strings.Builder
	var kind string
	col, style := -1, 0
	inValue := false
	err := walk(z, file, func(t xml.Token) {
		switch t := t.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row, col = row[:0], -1
			case "c":
				if at := column(attr(t, "r")); at >= 0 {
					col = at
				} else {
					col++
				}
				kind = attr(t, "t")
				style, _ = strconv.Atoi(attr(t, "s"))
				value.Reset()
			case "v", "t":
				inValue = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v", "t":
				inValue = false
			case "c":
				text := cellText(value.String(), kind, shared, style < len(dates) && dates[style])
				if text == "" || col < 0 || col >= maxColumns {
					return
				}
				for len(row) <= col {
					row = append(row, "")
				}
				row[col] = text
			case "row":
				if len(row) > 0 {
					b.WriteString(strings.Join(row, "\t") + "\n")
				}
			}
		case xml.CharData:
			if inValue {
				value.Write(t)
			}
		}
	})
	return strings.TrimRight(b.String(), "\n"), err
}

// cellText writes what a cell holds as the sheet shows it, near enough: a
// shared text, a date for the number of one, a number without the noise
// of how it is kept.
func cellText(value, kind string, shared []string, date bool) string {
	clean := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	switch kind {
	case "s":
		if i, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && i >= 0 && i < len(shared) {
			return clean(shared[i])
		}
		return ""
	case "b":
		if strings.TrimSpace(value) == "1" {
			return "是"
		}
		return "否"
	case "inlineStr", "str", "e", "d":
		return clean(value)
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return clean(value)
	}
	if date && n >= 0 && n < 2958466 {
		// Days since the end of 1899, and the part of a day that is gone.
		days, part := math.Floor(n), n-math.Floor(n)
		at := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(days)).Add(time.Duration(math.Round(part*86400)) * time.Second)
		switch {
		case part == 0:
			return at.Format(dateLayout)
		case days == 0:
			return at.Format("15:04")
		}
		return at.Format(dateLayout + " 15:04")
	}
	return strconv.FormatFloat(roundTo(n, 12), 'f', -1, 64)
}

// roundTo returns n to so many digits that count.
func roundTo(n float64, digits int) float64 {
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(n, 'g', digits, 64), 64)
	if err != nil {
		return n
	}
	return rounded
}

// slidesText returns the text of a presentation: its slides in order,
// each under its number, a paragraph on a line.
func slidesText(z *zip.Reader) (string, error) {
	_, files, err := partsOf(z, "ppt/presentation.xml", "sldId")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, file := range files {
		var page strings.Builder
		in, cells := false, 0
		err := walk(z, file, func(t xml.Token) {
			switch t := t.(type) {
			case xml.StartElement:
				switch t.Name.Local {
				case "t":
					in = true
				case "br":
					page.WriteString("\n")
				case "tc":
					cells++
				}
			case xml.EndElement:
				switch t.Name.Local {
				case "t":
					in = false
				case "p":
					if cells > 0 {
						page.WriteString(" ")
					} else {
						page.WriteString("\n")
					}
				case "tc":
					cells = max(cells-1, 0)
					page.WriteString("\t")
				case "tr":
					page.WriteString("\n")
				}
			case xml.CharData:
				if in {
					page.Write(t)
				}
			}
		})
		if err != nil {
			return "", err
		}
		var lines []string
		for _, l := range strings.Split(page.String(), "\n") {
			if l = strings.TrimSpace(strings.ReplaceAll(l, " \t", "\t")); l != "" {
				lines = append(lines, l)
			}
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "【第 %d 页】\n%s\n\n", i+1, strings.Join(lines, "\n"))
		if b.Len() > maxText {
			break
		}
	}
	return b.String(), nil
}
