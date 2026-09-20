package documentparser

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf16"
)

var pdfJPEGImage = regexp.MustCompile(`(?s)/Subtype\s*/Image.*?/Filter\s*/DCTDecode.*?stream\r?\n(.*?)\r?\nendstream`)
var pdfObject = regexp.MustCompile(`(?s)(\d+)\s+\d+\s+obj\b(.*?)\bendobj\b`)
var pdfFontReference = regexp.MustCompile(`/([A-Za-z0-9_.+-]+)\s+(\d+)\s+\d+\s+R\b`)
var pdfUnicodeCIDFont = regexp.MustCompile(`/Encoding\s*/Uni(?:GB|CNS|JIS|KS)-UCS2-[HV]\b`)

type pdfTextEncoding uint8

const (
	pdfTextEncodingRaw pdfTextEncoding = iota
	pdfTextEncodingUTF16BE
)

type pdfOperand struct {
	name string
	text []byte
}

// parsePDF extracts literal text from normal and Flate-compressed PDF content streams.
// Encrypted, scanned and custom-font PDFs have no usable text layer; their page images are returned when embedded as JPEG.
func parsePDF(data []byte) (Result, error) {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return Result{}, fmt.Errorf("invalid PDF header")
	}
	streams := pdfStreams(data)
	fontEncodings := pdfFontEncodings(data)
	var builder strings.Builder
	for _, stream := range streams {
		builder.WriteString(pdfText(stream, fontEncodings))
		builder.WriteByte('\n')
	}
	text := NormalizeMarkdownForChunker(builder.String())
	if text != "" {
		text = "# PDF 文档\n\n" + text
	}
	return Result{Text: text, Images: pdfImages(data)}, nil
}

// pdfFontEncodings resolves the resource aliases used by page content streams.  A
// Type0 font with a Uni*-UCS2-* CMap stores its glyph codes as UTF-16BE, usually
// without a BOM.  Treating those bytes as UTF-8 is what produced the mojibake in
// knowledge-base PDF chunks.
func pdfFontEncodings(data []byte) map[string]pdfTextEncoding {
	objects := make(map[string][]byte)
	for _, match := range pdfObject.FindAllSubmatch(data, -1) {
		if len(match) == 3 {
			objects[string(match[1])] = match[2]
		}
	}

	encodings := make(map[string]pdfTextEncoding)
	for _, object := range objects {
		for _, reference := range pdfFontReference.FindAllSubmatch(object, -1) {
			if len(reference) != 3 || !pdfUnicodeCIDFont.Match(objects[string(reference[2])]) {
				continue
			}
			encodings[string(reference[1])] = pdfTextEncodingUTF16BE
		}
	}
	return encodings
}

func pdfStreams(data []byte) [][]byte {
	result := make([][]byte, 0)
	for offset := 0; offset < len(data); {
		start := bytes.Index(data[offset:], []byte("\nstream"))
		if start < 0 {
			break
		}
		streamStart := offset + start + len("\nstream")
		end := bytes.Index(data[streamStart:], []byte("endstream"))
		if end < 0 {
			break
		}
		stream := bytes.TrimLeft(data[streamStart:streamStart+end], "\r\n")
		headerStart := bytes.LastIndex(data[:streamStart], []byte("<<"))
		header := []byte(nil)
		if headerStart >= 0 {
			header = data[headerStart:streamStart]
		}
		if bytes.Contains(header, []byte("ASCII85Decode")) {
			decoded := make([]byte, len(stream))
			encoded := bytes.TrimSpace(stream)
			encoded = bytes.TrimPrefix(encoded, []byte("<~"))
			encoded = bytes.TrimSuffix(encoded, []byte("~>"))
			if size, _, decodeErr := ascii85.Decode(decoded, encoded, true); decodeErr == nil {
				stream = decoded[:size]
			}
		}
		if bytes.Contains(header, []byte("FlateDecode")) {
			if decoded, decodeErr := decodeFlate(stream); decodeErr == nil {
				stream = decoded
			}
		}
		result = append(result, stream)
		offset = streamStart + end + len("endstream")
	}
	return result
}

func decodeFlate(data []byte) ([]byte, error) {
	if reader, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
		decoded, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr == nil {
			return decoded, nil
		}
	}
	reader := flate.NewReader(bytes.NewReader(data))
	decoded, err := io.ReadAll(reader)
	_ = reader.Close()
	return decoded, err
}

// pdfText extracts strings only while the content stream is inside a BT/ET text
// object.  Font files, image data, and metadata streams can legally contain byte
// sequences that look like PDF strings, but none of those should become a
// knowledge-base chunk.
func pdfText(stream []byte, fontEncodings map[string]pdfTextEncoding) string {
	var builder strings.Builder
	inTextObject := false
	encoding := pdfTextEncodingRaw
	operands := make([]pdfOperand, 0, 8)

	for index := 0; index < len(stream); {
		index = skipPDFWhitespaceAndComments(stream, index)
		if index >= len(stream) {
			break
		}

		switch stream[index] {
		case '(':
			text, next := pdfLiteralBytes(stream, index)
			operands = append(operands, pdfOperand{text: text})
			index = next + 1
		case '<':
			if index+1 >= len(stream) || stream[index+1] == '<' {
				index++
				continue
			}
			text, next := pdfHexBytes(stream, index)
			operands = append(operands, pdfOperand{text: text})
			index = next
		case '/':
			name, next := pdfName(stream, index)
			operands = append(operands, pdfOperand{name: name})
			index = next
		}
		if index >= len(stream) {
			break
		}
		word, next := pdfWord(stream, index)
		if word == "" {
			index++
			continue
		}
		index = next

		switch word {
		case "BT":
			inTextObject = true
			encoding = pdfTextEncodingRaw
		case "ET":
			inTextObject = false
		case "Tf":
			if inTextObject {
				for operandIndex := len(operands) - 1; operandIndex >= 0; operandIndex-- {
					if operands[operandIndex].name != "" {
						encoding = fontEncodings[operands[operandIndex].name]
						break
					}
				}
			}
		case "Tj", "'", "\"":
			if inTextObject {
				pdfAppendLastText(&builder, operands, encoding)
			}
		case "TJ":
			if inTextObject {
				pdfAppendAllText(&builder, operands, encoding)
			}
		case "Td", "TD", "T*":
			if inTextObject && builder.Len() > 0 {
				builder.WriteByte('\n')
			}
		}

		if !pdfNumber(word) {
			operands = operands[:0]
		}
	}
	return builder.String()
}

func pdfAppendLastText(builder *strings.Builder, operands []pdfOperand, encoding pdfTextEncoding) {
	for index := len(operands) - 1; index >= 0; index-- {
		if operands[index].text == nil {
			continue
		}
		text := pdfDecodedText(operands[index].text, encoding)
		builder.WriteString(text)
		pdfAppendSeparator(builder, text)
		return
	}
}

func pdfAppendAllText(builder *strings.Builder, operands []pdfOperand, encoding pdfTextEncoding) {
	lastText := ""
	for _, operand := range operands {
		if operand.text != nil {
			lastText = pdfDecodedText(operand.text, encoding)
			builder.WriteString(lastText)
		}
	}
	pdfAppendSeparator(builder, lastText)
}

func pdfAppendSeparator(builder *strings.Builder, text string) {
	if text != "" && !strings.HasSuffix(text, " ") && !strings.HasSuffix(text, "\n") && !strings.HasSuffix(text, "\r") && !strings.HasSuffix(text, "\t") {
		builder.WriteByte(' ')
	}
}

func pdfLiteralBytes(stream []byte, start int) ([]byte, int) {
	data := make([]byte, 0, 32)
	depth := 1
	for index := start + 1; index < len(stream); index++ {
		switch stream[index] {
		case '\\':
			index++
			if index >= len(stream) {
				break
			}
			if stream[index] >= '0' && stream[index] <= '7' {
				value := byte(0)
				for count := 0; count < 3 && index < len(stream) && stream[index] >= '0' && stream[index] <= '7'; count++ {
					value = value*8 + stream[index] - '0'
					index++
				}
				data = append(data, value)
				index--
				continue
			}
			data = append(data, stream[index])
		case '(':
			depth++
			data = append(data, '(')
		case ')':
			depth--
			if depth == 0 {
				return data, index
			}
			data = append(data, ')')
		default:
			data = append(data, stream[index])
		}
	}
	return data, len(stream) - 1
}

func pdfHexBytes(stream []byte, start int) ([]byte, int) {
	end := bytes.IndexByte(stream[start+1:], '>')
	if end < 0 {
		return nil, len(stream)
	}
	end += start + 1
	encoded := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\r' || r == '\n' || r == '\t' {
			return -1
		}
		return r
	}, string(stream[start+1:end]))
	if len(encoded)%2 != 0 {
		encoded += "0"
	}
	decoded, _ := hex.DecodeString(encoded)
	return decoded, end + 1
}

func pdfName(stream []byte, start int) (string, int) {
	end := start + 1
	for end < len(stream) && !pdfDelimiter(stream[end]) {
		end++
	}
	return string(stream[start+1 : end]), end
}

func pdfWord(stream []byte, start int) (string, int) {
	end := start
	for end < len(stream) && !pdfDelimiter(stream[end]) {
		end++
	}
	return string(stream[start:end]), end
}

func skipPDFWhitespaceAndComments(stream []byte, start int) int {
	for start < len(stream) {
		if stream[start] == '%' {
			for start < len(stream) && stream[start] != '\n' && stream[start] != '\r' {
				start++
			}
			continue
		}
		if !pdfWhitespace(stream[start]) {
			return start
		}
		start++
	}
	return start
}

func pdfWhitespace(value byte) bool {
	return value == 0 || value == '\t' || value == '\n' || value == '\f' || value == '\r' || value == ' '
}

func pdfDelimiter(value byte) bool {
	return pdfWhitespace(value) || bytes.ContainsRune([]byte("()<>[]{}/%"), rune(value))
}

func pdfNumber(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if (character < '0' || character > '9') && character != '.' && character != '+' && character != '-' {
			return false
		}
		if index > 0 && (character == '+' || character == '-') {
			return false
		}
	}
	return true
}

func pdfDecodedText(data []byte, encoding pdfTextEncoding) string {
	hasUTF16BOM := len(data) > 2 && bytes.HasPrefix(data, []byte{0xfe, 0xff})
	if hasUTF16BOM {
		data = data[2:]
	}
	if len(data)%2 == 0 && len(data) > 1 && (hasUTF16BOM || encoding == pdfTextEncodingUTF16BE || utf16Likely(data)) {
		units := make([]uint16, 0, len(data)/2)
		for index := 0; index < len(data); index += 2 {
			units = append(units, uint16(data[index])<<8|uint16(data[index+1]))
		}
		return string(utf16.Decode(units))
	}
	return string(data)
}

func utf16Likely(data []byte) bool {
	zeroBytes := 0
	for index := 0; index < len(data); index += 2 {
		if data[index] == 0 {
			zeroBytes++
		}
	}
	return zeroBytes >= len(data)/4
}
func pdfImages(data []byte) []Image {
	matches := pdfJPEGImage.FindAllSubmatch(data, -1)
	images := make([]Image, 0, len(matches))
	for index, match := range matches {
		if len(match) != 2 || len(match[1]) == 0 {
			continue
		}
		images = append(images, Image{Name: fmt.Sprintf("page-image-%03d.jpg", index+1), MIME: "image/jpeg", Data: append([]byte(nil), match[1]...)})
	}
	return images
}
