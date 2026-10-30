// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// maximumWrittenTextCharacters bounds a page or comment body before conversion.
	maximumWrittenTextCharacters = 262144
	maximumInlineDepth           = 32
)

var (
	paragraphSeparatorPattern = regexp.MustCompile(`\n[ \t]*\n+`)
	headingPattern            = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$`)
	thematicBreakPattern      = regexp.MustCompile(`^ {0,3}(?:(?:\*[ \t]*){3,}|(?:-[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	fenceOpeningPattern       = regexp.MustCompile("^( {0,3})(`{3,}|~{3,})[ \t]*([^`]*?)[ \t]*$")
	listMarkerPattern         = regexp.MustCompile(`^( {0,3})([-*+]|[0-9]{1,9}[.)])([ \t]+|$)`)
	tableSeparatorPattern     = regexp.MustCompile(`^ {0,3}\|?[ \t]*:?-+:?[ \t]*(\|[ \t]*:?-+:?[ \t]*)*\|?[ \t]*$`)
	taskMarkerPattern         = regexp.MustCompile(`^\[([ xX])\][ \t]+`)
)

// markdownDocumentReader parses the Markdown subset documented on TextFormatMarkdown.
type markdownDocumentReader struct{}

// markdownInlineBuilder accumulates plain text in the current style between parsed spans.
type markdownInlineBuilder struct {
	style       documentInline
	inlines     []documentInline
	pendingText strings.Builder
}

// parseWrittenText validates caller text and converts it to blocks in the requested format.
func parseWrittenText(text string, format TextFormat, fieldName string) ([]documentBlock, error) {
	if err := validateWrittenText(text, fieldName); err != nil {
		return nil, err
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	switch format {
	case "", TextFormatMarkdown:
		return markdownDocumentReader{}.readBlocks(strings.Split(strings.Trim(text, "\n"), "\n"), 0), nil
	case TextFormatPlainText:
		return parsePlainText(text), nil
	default:
		return nil, fmt.Errorf("%sFormat must be markdown or plainText", fieldName)
	}
}

func validateWrittenText(text string, fieldName string) error {
	if !utf8.ValidString(text) {
		return errors.New(fieldName + " must be valid UTF-8")
	}
	if strings.TrimSpace(text) == "" {
		return errors.New(fieldName + " is required")
	}
	if utf8.RuneCountInString(text) > maximumWrittenTextCharacters {
		return fmt.Errorf("%s cannot exceed %d characters", fieldName, maximumWrittenTextCharacters)
	}
	for _, character := range text {
		if character == 0x7f || (character < ' ' && character != '\n' && character != '\r' && character != '\t') {
			return errors.New(fieldName + " cannot contain control characters")
		}
	}
	return nil
}

// parsePlainText splits paragraphs on blank lines and keeps other line breaks; nothing is markup.
func parsePlainText(text string) []documentBlock {
	var blocks []documentBlock
	for _, paragraph := range paragraphSeparatorPattern.Split(strings.Trim(text, "\n"), -1) {
		var inlines []documentInline
		for index, line := range strings.Split(paragraph, "\n") {
			if index > 0 {
				inlines = appendInline(inlines, documentInline{isLineBreak: true})
			}
			inlines = appendInline(inlines, documentInline{text: line})
		}
		blocks = appendParagraphBlock(blocks, inlines)
	}
	return blocks
}

func (reader markdownDocumentReader) readBlocks(lines []string, depth int) []documentBlock {
	var blocks []documentBlock
	for index := 0; index < len(lines); {
		line := lines[index]
		if depth > maximumDocumentDepth {
			return append(blocks, documentBlock{kind: blockParagraph, inlines: []documentInline{{text: strings.Join(lines[index:], " ")}}})
		}
		switch {
		case strings.TrimSpace(line) == "":
			index++
		case fenceOpeningPattern.MatchString(line):
			var block documentBlock
			block, index = reader.readFencedCode(lines, index)
			blocks = append(blocks, block)
		case headingPattern.MatchString(line):
			match := headingPattern.FindStringSubmatch(line)
			blocks = append(blocks, documentBlock{kind: blockHeading, headingLevel: len(match[1]), inlines: reader.readInlines(match[2], documentInline{}, 0)})
			index++
		case thematicBreakPattern.MatchString(line):
			blocks = append(blocks, documentBlock{kind: blockRule})
			index++
		case isBlockQuoteLine(line):
			var quoted []string
			for index < len(lines) && isBlockQuoteLine(lines[index]) {
				quoted = append(quoted, stripBlockQuoteMarker(lines[index]))
				index++
			}
			blocks = append(blocks, documentBlock{kind: blockQuote, children: reader.readBlocks(quoted, depth+1)})
		case listMarkerPattern.MatchString(line):
			var block documentBlock
			block, index = reader.readList(lines, index, depth)
			blocks = append(blocks, block)
		case index+1 < len(lines) && strings.Contains(line, "|") && strings.Contains(lines[index+1], "|") && tableSeparatorPattern.MatchString(lines[index+1]):
			var block documentBlock
			block, index = reader.readTable(lines, index)
			blocks = append(blocks, block)
		default:
			var paragraphLines []string
			for index < len(lines) && strings.TrimSpace(lines[index]) != "" && (len(paragraphLines) == 0 || !startsMarkdownBlock(lines[index])) {
				paragraphLines = append(paragraphLines, strings.TrimSpace(lines[index]))
				index++
			}
			blocks = appendParagraphBlock(blocks, reader.readInlines(strings.Join(paragraphLines, "\n"), documentInline{}, 0))
		}
	}
	return blocks
}

func (markdownDocumentReader) readFencedCode(lines []string, start int) (documentBlock, int) {
	match := fenceOpeningPattern.FindStringSubmatch(lines[start])
	indentation, fence := len(match[1]), match[2]
	language, _, _ := strings.Cut(strings.TrimSpace(match[3]), " ")
	block := documentBlock{kind: blockCode, codeLanguage: markdownFenceLanguage(language)}
	var code []string
	index := start + 1
	for ; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if strings.HasPrefix(trimmed, fence[:1]) && strings.Trim(trimmed, fence[:1]) == "" && len(trimmed) >= len(fence) {
			index++
			break
		}
		code = append(code, removeIndentation(lines[index], indentation))
	}
	block.codeText = strings.Join(code, "\n")
	return block, index
}

// readList reads consecutive items of one list type; lines indented to the item content belong to the item.
func (reader markdownDocumentReader) readList(lines []string, start int, depth int) (documentBlock, int) {
	firstMarker := listMarkerPattern.FindStringSubmatch(lines[start])
	isOrdered := isOrderedListMarker(firstMarker[2])
	block := documentBlock{kind: blockBulletList}
	if isOrdered {
		block.kind = blockOrderedList
		_, _ = fmt.Sscanf(firstMarker[2], "%d", &block.orderedStart)
	}
	index := start
	var itemLineGroups [][]string
	for index < len(lines) {
		marker := listMarkerPattern.FindStringSubmatch(lines[index])
		if marker == nil || isOrderedListMarker(marker[2]) != isOrdered || (!isOrdered && marker[2] != firstMarker[2]) {
			break
		}
		contentIndentation := len(marker[0])
		if strings.TrimSpace(marker[3]) == "" && len(marker[3]) > 4 {
			contentIndentation = len(marker[1]) + len(marker[2]) + 1
		}
		itemLines := []string{lines[index][len(marker[0]):]}
		index++
		for index < len(lines) {
			line := lines[index]
			if strings.TrimSpace(line) == "" {
				if index+1 < len(lines) && leadingSpaces(lines[index+1]) >= contentIndentation {
					itemLines = append(itemLines, "")
					index++
					continue
				}
				break
			}
			if leadingSpaces(line) < contentIndentation {
				break
			}
			itemLines = append(itemLines, removeIndentation(line, contentIndentation))
			index++
		}
		itemLineGroups = append(itemLineGroups, itemLines)
		for index < len(lines) && strings.TrimSpace(lines[index]) == "" && index+1 < len(lines) && listMarkerPattern.MatchString(lines[index+1]) {
			index++
		}
	}
	isTaskList := !isOrdered
	for _, itemLines := range itemLineGroups {
		isTaskList = isTaskList && taskMarkerPattern.MatchString(itemLines[0])
	}
	for _, itemLines := range itemLineGroups {
		item := documentListItem{}
		if isTaskList {
			taskMatch := taskMarkerPattern.FindStringSubmatch(itemLines[0])
			item.isTaskDone = taskMatch[1] != " "
			itemLines[0] = itemLines[0][len(taskMatch[0]):]
		}
		item.blocks = reader.readBlocks(itemLines, depth+1)
		block.listItems = append(block.listItems, item)
	}
	if isTaskList {
		block.kind = blockTaskList
	}
	return block, index
}

func (reader markdownDocumentReader) readTable(lines []string, start int) (documentBlock, int) {
	block := documentBlock{kind: blockTable}
	block.tableRows = append(block.tableRows, documentTableRow{isHeader: true, cells: reader.readTableCells(lines[start])})
	index := start + 2
	for ; index < len(lines) && strings.Contains(lines[index], "|") && strings.TrimSpace(lines[index]) != ""; index++ {
		block.tableRows = append(block.tableRows, documentTableRow{cells: reader.readTableCells(lines[index])})
	}
	return block, index
}

func (reader markdownDocumentReader) readTableCells(line string) [][]documentInline {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	if strings.HasSuffix(trimmed, "|") && !strings.HasSuffix(trimmed, `\|`) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	var cells [][]documentInline
	var cell strings.Builder
	isEscaped := false
	for _, character := range trimmed {
		switch {
		case isEscaped:
			if character != '|' {
				cell.WriteRune('\\')
			}
			cell.WriteRune(character)
			isEscaped = false
		case character == '\\':
			isEscaped = true
		case character == '|':
			cells = append(cells, reader.readInlines(strings.TrimSpace(cell.String()), documentInline{}, 0))
			cell.Reset()
		default:
			cell.WriteRune(character)
		}
	}
	return append(cells, reader.readInlines(strings.TrimSpace(cell.String()), documentInline{}, 0))
}

// readInlines parses span markup; a line break in the text is kept as a line break.
func (reader markdownDocumentReader) readInlines(text string, style documentInline, depth int) []documentInline {
	runes := []rune(text)
	builder := &markdownInlineBuilder{style: style}
	for index := 0; index < len(runes); {
		character := runes[index]
		if depth >= maximumInlineDepth {
			builder.pendingText.WriteRune(character)
			index++
			continue
		}
		switch {
		case character == '\\' && index+1 < len(runes) && isMarkdownPunctuation(runes[index+1]):
			builder.pendingText.WriteRune(runes[index+1])
			index += 2
		case character == '\n':
			builder.appendSpans(documentInline{isLineBreak: true})
			index++
		case character == '`':
			if code, end, isFound := readCodeSpan(runes, index); isFound {
				span := style
				span.text, span.isCode = code, true
				builder.appendSpans(span)
				index = end
				continue
			}
			length := delimiterRunLength(runes, index, '`')
			builder.pendingText.WriteString(string(runes[index : index+length]))
			index += length
		case character == '[':
			if label, href, end, isFound := readMarkdownLink(runes, index); isFound {
				linkStyle := style
				linkStyle.linkHref = safeLinkHref(href)
				builder.appendSpans(reader.readInlines(label, linkStyle, depth+1)...)
				index = end
				continue
			}
			builder.pendingText.WriteRune(character)
			index++
		case character == '<':
			if href, end, isFound := readAutolink(runes, index); isFound {
				span := style
				span.text, span.linkHref = href, href
				builder.appendSpans(span)
				index = end
				continue
			}
			builder.pendingText.WriteRune(character)
			index++
		case character == '*' || character == '_' || character == '~':
			if inner, spanStyle, end, isFound := readEmphasis(runes, index, style); isFound {
				builder.appendSpans(reader.readInlines(inner, spanStyle, depth+1)...)
				index = end
				continue
			}
			length := delimiterRunLength(runes, index, character)
			builder.pendingText.WriteString(string(runes[index : index+length]))
			index += length
		default:
			builder.pendingText.WriteRune(character)
			index++
		}
	}
	return trimInlineEdges(builder.finish())
}

// appendSpans ends the pending plain text and appends parsed spans after it.
func (builder *markdownInlineBuilder) appendSpans(spans ...documentInline) {
	builder.flushPendingText()
	for _, span := range spans {
		builder.inlines = appendInline(builder.inlines, span)
	}
}

func (builder *markdownInlineBuilder) finish() []documentInline {
	builder.flushPendingText()
	return builder.inlines
}

func (builder *markdownInlineBuilder) flushPendingText() {
	span := builder.style
	span.text = builder.pendingText.String()
	builder.inlines = appendInline(builder.inlines, span)
	builder.pendingText.Reset()
}

// readCodeSpan finds the closing backtick run of the same length; one surrounding space is stripped.
func readCodeSpan(runes []rune, start int) (string, int, bool) {
	length := delimiterRunLength(runes, start, '`')
	for index := start + length; index < len(runes); {
		if runes[index] != '`' {
			index++
			continue
		}
		closing := delimiterRunLength(runes, index, '`')
		if closing == length {
			code := string(runes[start+length : index])
			if len(code) >= 2 && strings.HasPrefix(code, " ") && strings.HasSuffix(code, " ") && strings.TrimSpace(code) != "" {
				code = code[1 : len(code)-1]
			}
			return strings.ReplaceAll(code, "\n", " "), index + closing, true
		}
		index += closing
	}
	return "", 0, false
}

// readMarkdownLink reads [label](href), honoring nested brackets and escapes in the label.
func readMarkdownLink(runes []rune, start int) (string, string, int, bool) {
	depth := 0
	for index := start; index < len(runes); index++ {
		switch runes[index] {
		case '\\':
			index++
		case '[':
			depth++
		case ']':
			depth--
			if depth > 0 {
				continue
			}
			if index+1 >= len(runes) || runes[index+1] != '(' {
				return "", "", 0, false
			}
			closing := indexOfBalancedClosingParenthesis(runes, index+2)
			if closing < 0 {
				return "", "", 0, false
			}
			href := strings.TrimSpace(string(runes[index+2 : closing]))
			href = strings.TrimSuffix(strings.TrimPrefix(href, "<"), ">")
			if href == "" || strings.ContainsAny(href, " \t\n") {
				return "", "", 0, false
			}
			return string(runes[start+1 : index]), href, closing + 1, true
		}
	}
	return "", "", 0, false
}

// readAutolink reads <https://...> and <mailto:...> autolinks whose target is safe.
func readAutolink(runes []rune, start int) (string, int, bool) {
	closing := indexOfRune(runes, start+1, '>')
	if closing < 0 {
		return "", 0, false
	}
	href := string(runes[start+1 : closing])
	if safeLinkHref(href) != href || href == "" {
		return "", 0, false
	}
	return href, closing + 1, true
}

// readEmphasis matches **strong**, __strong__, *emphasis*, _emphasis_, and ~~strikethrough~~ spans.
func readEmphasis(runes []rune, start int, style documentInline) (string, documentInline, int, bool) {
	character := runes[start]
	runLength := delimiterRunLength(runes, start, character)
	delimiterLength := min(runLength, 2)
	if character == '~' {
		if runLength < 2 {
			return "", style, 0, false
		}
		delimiterLength = 2
	}
	contentStart := start + delimiterLength
	if contentStart >= len(runes) || unicode.IsSpace(runes[contentStart]) {
		return "", style, 0, false
	}
	if character == '_' && start > 0 && isWordRune(runes[start-1]) {
		return "", style, 0, false
	}
	for index := contentStart + 1; index < len(runes); index++ {
		if runes[index] == '\\' {
			index++
			continue
		}
		if runes[index] != character {
			continue
		}
		closingRun := delimiterRunLength(runes, index, character)
		if closingRun < delimiterLength || unicode.IsSpace(runes[index-1]) {
			index += closingRun - 1
			continue
		}
		closing := index + closingRun - delimiterLength
		end := closing + delimiterLength
		if character == '_' && end < len(runes) && isWordRune(runes[end]) {
			index += closingRun - 1
			continue
		}
		spanStyle := style
		switch {
		case character == '~':
			spanStyle.isStrikethrough = true
		case delimiterLength == 2:
			spanStyle.isStrong = true
		default:
			spanStyle.isEmphasis = true
		}
		return string(runes[contentStart:closing]), spanStyle, end, true
	}
	return "", style, 0, false
}

func delimiterRunLength(runes []rune, start int, character rune) int {
	length := 0
	for start+length < len(runes) && runes[start+length] == character {
		length++
	}
	return length
}

// indexOfBalancedClosingParenthesis finds the ")" that closes a link destination, as CommonMark balances them.
func indexOfBalancedClosingParenthesis(runes []rune, start int) int {
	depth := 0
	for index := start; index < len(runes); index++ {
		switch runes[index] {
		case '\\':
			index++
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return index
			}
			depth--
		case '\n':
			return -1
		}
	}
	return -1
}

func indexOfRune(runes []rune, start int, target rune) int {
	for index := start; index < len(runes); index++ {
		if runes[index] == target {
			return index
		}
		if runes[index] == '\n' {
			return -1
		}
	}
	return -1
}

func isMarkdownPunctuation(character rune) bool {
	return character < 0x80 && unicode.IsPunct(character) || strings.ContainsRune("`$^+<=>|~", character)
}

func isOrderedListMarker(marker string) bool {
	return marker[0] >= '0' && marker[0] <= '9'
}

func isBlockQuoteLine(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " "), ">") && leadingSpaces(line) <= 3
}

func stripBlockQuoteMarker(line string) string {
	stripped := strings.TrimPrefix(strings.TrimLeft(line, " "), ">")
	return strings.TrimPrefix(stripped, " ")
}

// startsMarkdownBlock reports a line that interrupts a paragraph.
func startsMarkdownBlock(line string) bool {
	return fenceOpeningPattern.MatchString(line) || headingPattern.MatchString(line) || thematicBreakPattern.MatchString(line) ||
		isBlockQuoteLine(line) || listMarkerPattern.MatchString(line)
}

func leadingSpaces(line string) int {
	count := 0
	for count < len(line) && line[count] == ' ' {
		count++
	}
	return count
}

func removeIndentation(line string, indentation int) string {
	return line[min(leadingSpaces(line), indentation):]
}
