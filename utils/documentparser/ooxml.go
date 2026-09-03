package documentparser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

func parseDOCX(data []byte) (Result, error) {
	files, err := openOOXML(data)
	if err != nil {
		return Result{}, err
	}
	text, err := docxMarkdown(files["word/document.xml"])
	if err != nil {
		return Result{}, fmt.Errorf("read DOCX document: %w", err)
	}
	return Result{Text: NormalizeMarkdownForChunker(text), Images: media(files, "word/media/")}, nil
}

func parsePPTX(data []byte) (Result, error) {
	files, err := openOOXML(data)
	if err != nil {
		return Result{}, err
	}
	var names []string
	for name := range files {
		if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })
	var builder strings.Builder
	for index, name := range names {
		text, parseErr := xmlText(files[name])
		if parseErr != nil {
			return Result{}, fmt.Errorf("read PPTX %s: %w", name, parseErr)
		}
		if strings.TrimSpace(text) != "" {
			fmt.Fprintf(&builder, "# 幻灯片 %d\n\n%s\n\n", index+1, text)
		}
	}
	return Result{Text: NormalizeMarkdownForChunker(builder.String()), Images: media(files, "ppt/media/")}, nil
}

func parseXLSX(data []byte) (Result, error) {
	files, err := openOOXML(data)
	if err != nil {
		return Result{}, err
	}
	sharedStrings := xmlStringItems(files["xl/sharedStrings.xml"])
	var names []string
	for name := range files {
		if strings.HasPrefix(name, "xl/worksheets/sheet") && strings.HasSuffix(name, ".xml") {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })
	var builder strings.Builder
	for index, name := range names {
		rows, parseErr := worksheetRows(files[name], sharedStrings)
		if parseErr != nil {
			return Result{}, parseErr
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&builder, "# 工作表 %d\n\n", index+1)
		appendMarkdownTable(&builder, rows)
		builder.WriteString("\n")
	}
	return Result{Text: NormalizeMarkdownForChunker(builder.String()), Images: media(files, "xl/media/")}, nil
}

func xmlStringItems(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var items []string
	var current strings.Builder
	inItem, inText := false, false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return items
		}
		switch item := token.(type) {
		case xml.StartElement:
			if item.Name.Local == "si" {
				current.Reset()
				inItem = true
			}
			if item.Name.Local == "t" && inItem {
				inText = true
			}
		case xml.CharData:
			if inText {
				current.Write([]byte(item))
			}
		case xml.EndElement:
			if item.Name.Local == "t" {
				inText = false
			}
			if item.Name.Local == "si" {
				items = append(items, current.String())
				inItem = false
			}
		}
	}
	return items
}

func docxMarkdown(data []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var builder strings.Builder
	var paragraph strings.Builder
	var cell strings.Builder
	var row []string
	var table [][]string
	headingLevel := 0
	var inParagraph, inCell bool
	flushParagraph := func() {
		value := strings.TrimSpace(paragraph.String())
		paragraph.Reset()
		if value == "" {
			return
		}
		if inCell {
			if cell.Len() > 0 {
				cell.WriteString("<br>")
			}
			cell.WriteString(value)
			headingLevel = 0
			return
		}
		if headingLevel > 0 {
			builder.WriteString(strings.Repeat("#", headingLevel) + " " + value + "\n\n")
		} else {
			builder.WriteString(value + "\n\n")
		}
		headingLevel = 0
	}
	flushCell := func() {
		if !inCell {
			return
		}
		row = append(row, strings.TrimSpace(cell.String()))
		cell.Reset()
	}
	flushRow := func() {
		if len(row) > 0 {
			table = append(table, row)
		}
		row = nil
	}
	flushTable := func() {
		if len(table) > 0 {
			appendMarkdownTable(&builder, table)
			builder.WriteByte('\n')
		}
		table = nil
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "tbl":
				table = nil
			case "tr":
				row = nil
			case "tc":
				inCell = true
				cell.Reset()
			case "p":
				inParagraph = true
				paragraph.Reset()
			case "pStyle":
				for _, attribute := range item.Attr {
					if attribute.Name.Local == "val" {
						value := strings.ToLower(attribute.Value)
						if strings.HasPrefix(value, "heading") {
							fmt.Sscanf(strings.TrimPrefix(value, "heading"), "%d", &headingLevel)
							if headingLevel < 1 || headingLevel > 6 {
								headingLevel = 0
							}
						}
					}
				}
			}
		case xml.CharData:
			if inParagraph {
				paragraph.Write([]byte(item))
			}
		case xml.EndElement:
			switch item.Name.Local {
			case "p":
				flushParagraph()
				inParagraph = false
			case "tc":
				flushCell()
				inCell = false
			case "tr":
				flushRow()
			case "tbl":
				flushTable()
			}
		}
	}
	return builder.String(), nil
}

func openOOXML(data []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(newReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open Office document: %w", err)
	}
	files := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		stream, openErr := file.Open()
		if openErr != nil {
			return nil, openErr
		}
		content, readErr := io.ReadAll(stream)
		_ = stream.Close()
		if readErr != nil {
			return nil, readErr
		}
		files[file.Name] = content
	}
	return files, nil
}

func xmlText(data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var builder strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch value := token.(type) {
		case xml.CharData:
			builder.Write([]byte(value))
		case xml.EndElement:
			if value.Name.Local == "p" || value.Name.Local == "tr" {
				builder.WriteByte('\n')
			}
		}
	}
	return builder.String(), nil
}

func worksheetRows(data []byte, shared []string) ([][]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var rows [][]string
	var row []string
	var cellType string
	var inValue bool
	var value strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch item := token.(type) {
		case xml.StartElement:
			if item.Name.Local == "row" {
				row = nil
			}
			if item.Name.Local == "c" {
				cellType = ""
				for _, attr := range item.Attr {
					if attr.Name.Local == "t" {
						cellType = attr.Value
					}
				}
			}
			if item.Name.Local == "v" || item.Name.Local == "t" {
				inValue = true
				value.Reset()
			}
		case xml.CharData:
			if inValue {
				value.Write([]byte(item))
			}
		case xml.EndElement:
			if item.Name.Local == "v" || item.Name.Local == "t" {
				inValue = false
			}
			if item.Name.Local == "c" {
				cell := strings.TrimSpace(value.String())
				if cellType == "s" {
					var index int
					_, _ = fmt.Sscanf(cell, "%d", &index)
					if index >= 0 && index < len(shared) {
						cell = shared[index]
					}
				}
				row = append(row, cell)
			}
			if item.Name.Local == "row" && len(row) > 0 {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

func media(files map[string][]byte, prefix string) []Image {
	var names []string
	for name := range files {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	images := make([]Image, 0, len(names))
	for _, name := range names {
		images = append(images, Image{Name: path.Base(name), MIME: mediaMIME(name), Data: append([]byte(nil), files[name]...)})
	}
	return images
}
func mediaMIME(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}
func naturalLess(left, right string) bool { return left < right }
