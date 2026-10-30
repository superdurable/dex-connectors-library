// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TextFormat selects how page and comment text is written or returned.
type TextFormat string

const (
	// TextFormatMarkdown is a Markdown subset: headings, paragraphs, lists, block quotes, fenced code,
	// tables, rules, and bold, italic, strikethrough, code, and link spans. A single line break inside
	// a paragraph is kept as a line break.
	TextFormatMarkdown TextFormat = "markdown"
	// TextFormatPlainText is text without markup: blank lines separate paragraphs and other line breaks
	// are kept. Written plain text is never read as markup.
	TextFormatPlainText TextFormat = "plainText"
)

// maximumDocumentDepth bounds nesting so a hostile body cannot exhaust the stack.
const maximumDocumentDepth = 64

type documentBlockKind uint8

const (
	blockParagraph documentBlockKind = iota + 1
	blockHeading
	blockBulletList
	blockOrderedList
	blockTaskList
	blockQuote
	blockCode
	blockRule
	blockTable
)

// documentBlock is one block of the format-neutral document both readers and the writer share.
type documentBlock struct {
	kind         documentBlockKind
	headingLevel int
	inlines      []documentInline
	listItems    []documentListItem
	orderedStart int
	children     []documentBlock
	codeLanguage string
	codeText     string
	tableRows    []documentTableRow
}

// documentListItem is one list or task item; isTaskDone applies to task lists only.
type documentListItem struct {
	blocks     []documentBlock
	isTaskDone bool
}

// documentTableRow is one table row; isHeader marks a row of header cells.
type documentTableRow struct {
	cells    [][]documentInline
	isHeader bool
}

// documentInline is one styled text span or a line break.
type documentInline struct {
	text            string
	isLineBreak     bool
	isStrong        bool
	isEmphasis      bool
	isCode          bool
	isStrikethrough bool
	linkHref        string
}

// documentTextRenderer renders a document as Markdown or plain text.
type documentTextRenderer struct {
	isMarkdown bool
}

// renderDocumentText renders blocks and cuts the text at maxCharacters runes, reporting the cut.
func renderDocumentText(blocks []documentBlock, format TextFormat, maxCharacters int) (string, bool) {
	renderer := documentTextRenderer{isMarkdown: format == TextFormatMarkdown}
	text := strings.TrimSpace(renderer.renderBlocks(blocks))
	return truncateCharacters(text, maxCharacters)
}

// documentComparisonText is whitespace-free plain text, so markup and spacing differences do not matter.
func documentComparisonText(blocks []documentBlock) string {
	renderer := documentTextRenderer{}
	return strings.Join(strings.Fields(renderer.renderBlocks(blocks)), "")
}

func (renderer documentTextRenderer) renderBlocks(blocks []documentBlock) string {
	var rendered []string
	for _, block := range blocks {
		if text := renderer.renderBlock(block); strings.TrimSpace(text) != "" {
			rendered = append(rendered, text)
		}
	}
	return strings.Join(rendered, "\n\n")
}

func (renderer documentTextRenderer) renderBlock(block documentBlock) string {
	switch block.kind {
	case blockParagraph:
		return renderer.renderParagraph(block.inlines)
	case blockHeading:
		text := strings.ReplaceAll(renderer.renderInlines(block.inlines), "\n", " ")
		if !renderer.isMarkdown {
			return text
		}
		return strings.Repeat("#", clampHeadingLevel(block.headingLevel)) + " " + text
	case blockBulletList, blockOrderedList, blockTaskList:
		return renderer.renderList(block)
	case blockQuote:
		text := renderer.renderBlocks(block.children)
		if !renderer.isMarkdown {
			return text
		}
		return prefixLines(text, "> ", ">")
	case blockCode:
		if !renderer.isMarkdown {
			return block.codeText
		}
		fence := "```"
		for strings.Contains(block.codeText, fence) {
			fence += "`"
		}
		return fence + block.codeLanguage + "\n" + block.codeText + "\n" + fence
	case blockRule:
		return "---"
	case blockTable:
		return renderer.renderTable(block.tableRows)
	default:
		return ""
	}
}

func (renderer documentTextRenderer) renderParagraph(inlines []documentInline) string {
	text := renderer.renderInlines(inlines)
	if !renderer.isMarkdown {
		return text
	}
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = escapeMarkdownLineStart(line)
	}
	return strings.Join(lines, "\n")
}

func (renderer documentTextRenderer) renderList(block documentBlock) string {
	var items []string
	for index, item := range block.listItems {
		marker := "- "
		switch block.kind {
		case blockOrderedList:
			marker = strconv.Itoa(max(block.orderedStart, 1)+index) + ". "
		case blockTaskList:
			marker = "- [ ] "
			if item.isTaskDone {
				marker = "- [x] "
			}
		}
		content := renderer.renderBlocks(item.blocks)
		items = append(items, marker+indentContinuationLines(content, strings.Repeat(" ", len(marker))))
	}
	return strings.Join(items, "\n")
}

func (renderer documentTextRenderer) renderTable(rows []documentTableRow) string {
	var lines []string
	for index, row := range rows {
		cells := make([]string, 0, len(row.cells))
		for _, cell := range row.cells {
			text := strings.ReplaceAll(renderer.renderInlines(cell), "\n", " ")
			if renderer.isMarkdown {
				text = strings.ReplaceAll(text, "|", `\|`)
			}
			cells = append(cells, text)
		}
		if !renderer.isMarkdown {
			lines = append(lines, strings.Join(cells, " | "))
			continue
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
		if index == 0 {
			separators := make([]string, len(cells))
			for cellIndex := range separators {
				separators[cellIndex] = "---"
			}
			lines = append(lines, "| "+strings.Join(separators, " | ")+" |")
		}
	}
	return strings.Join(lines, "\n")
}

func (renderer documentTextRenderer) renderInlines(inlines []documentInline) string {
	var builder strings.Builder
	for _, inline := range inlines {
		if inline.isLineBreak {
			builder.WriteString("\n")
			continue
		}
		builder.WriteString(renderer.renderInline(inline))
	}
	return builder.String()
}

func (renderer documentTextRenderer) renderInline(inline documentInline) string {
	if !renderer.isMarkdown {
		return inline.text
	}
	if strings.TrimSpace(inline.text) == "" {
		return inline.text
	}
	leading, core, trailing := splitSurroundingSpace(inline.text)
	text := escapeMarkdownText(core)
	if inline.isCode {
		fence := "`"
		for strings.Contains(core, fence) {
			fence += "`"
		}
		text = fence + core + fence
	}
	if inline.isStrikethrough {
		text = "~~" + text + "~~"
	}
	if inline.isEmphasis {
		text = "*" + text + "*"
	}
	if inline.isStrong {
		text = "**" + text + "**"
	}
	if inline.linkHref != "" {
		text = "[" + text + "](" + strings.NewReplacer("(", "%28", ")", "%29", " ", "%20").Replace(inline.linkHref) + ")"
	}
	return leading + text + trailing
}

// appendInline merges a span into the previous one when both carry the same style.
func appendInline(inlines []documentInline, inline documentInline) []documentInline {
	if inline.isLineBreak {
		return append(inlines, inline)
	}
	if inline.text == "" {
		return inlines
	}
	if count := len(inlines); count > 0 {
		previous := &inlines[count-1]
		if !previous.isLineBreak && previous.isStrong == inline.isStrong && previous.isEmphasis == inline.isEmphasis &&
			previous.isCode == inline.isCode && previous.isStrikethrough == inline.isStrikethrough && previous.linkHref == inline.linkHref {
			previous.text += inline.text
			return inlines
		}
	}
	return append(inlines, inline)
}

// trimInlineEdges removes surrounding whitespace and line breaks from a paragraph's spans.
func trimInlineEdges(inlines []documentInline) []documentInline {
	for len(inlines) > 0 && (inlines[0].isLineBreak || strings.TrimSpace(inlines[0].text) == "") {
		inlines = inlines[1:]
	}
	for len(inlines) > 0 {
		last := inlines[len(inlines)-1]
		if !last.isLineBreak && strings.TrimSpace(last.text) != "" {
			break
		}
		inlines = inlines[:len(inlines)-1]
	}
	if len(inlines) == 0 {
		return nil
	}
	trimmed := append([]documentInline(nil), inlines...)
	trimmed[0].text = strings.TrimLeftFunc(trimmed[0].text, unicode.IsSpace)
	trimmed[len(trimmed)-1].text = strings.TrimRightFunc(trimmed[len(trimmed)-1].text, unicode.IsSpace)
	return trimmed
}

func hasInlineText(inlines []documentInline) bool {
	for _, inline := range inlines {
		if !inline.isLineBreak && strings.TrimSpace(inline.text) != "" {
			return true
		}
	}
	return false
}

func clampHeadingLevel(level int) int {
	return min(max(level, 1), 6)
}

// escapeMarkdownText escapes span delimiters so text reads back as text, not markup.
func escapeMarkdownText(text string) string {
	var builder strings.Builder
	runes := []rune(text)
	for index, character := range runes {
		switch character {
		case '\\', '`', '*', '[', ']', '~':
			builder.WriteRune('\\')
		case '_':
			isBeforeWord := index+1 < len(runes) && isWordRune(runes[index+1])
			isAfterWord := index > 0 && isWordRune(runes[index-1])
			if !isBeforeWord || !isAfterWord {
				builder.WriteRune('\\')
			}
		}
		builder.WriteRune(character)
	}
	return builder.String()
}

// escapeMarkdownLineStart escapes a leading block marker such as #, >, -, or 1. in paragraph text.
func escapeMarkdownLineStart(line string) string {
	trimmed := strings.TrimLeft(line, " ")
	if trimmed == "" {
		return line
	}
	indentation := line[:len(line)-len(trimmed)]
	switch trimmed[0] {
	case '#', '>', '-', '+', '=', '|':
		return indentation + `\` + trimmed
	}
	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits > 0 && digits < len(trimmed) && (trimmed[digits] == '.' || trimmed[digits] == ')') {
		return indentation + trimmed[:digits] + `\` + trimmed[digits:]
	}
	return line
}

func isWordRune(character rune) bool {
	return unicode.IsLetter(character) || unicode.IsDigit(character)
}

func splitSurroundingSpace(text string) (string, string, string) {
	core := strings.TrimSpace(text)
	start := strings.Index(text, core)
	return text[:start], core, text[start+len(core):]
}

func prefixLines(text string, prefix string, emptyLinePrefix string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if line == "" {
			lines[index] = emptyLinePrefix
			continue
		}
		lines[index] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func indentContinuationLines(text string, indentation string) string {
	lines := strings.Split(text, "\n")
	for index := 1; index < len(lines); index++ {
		if lines[index] != "" {
			lines[index] = indentation + lines[index]
		}
	}
	return strings.Join(lines, "\n")
}

// truncateCharacters cuts text at maxCharacters runes; a non-positive limit keeps all text.
func truncateCharacters(text string, maxCharacters int) (string, bool) {
	if maxCharacters <= 0 || utf8.RuneCountInString(text) <= maxCharacters {
		return text, false
	}
	count := 0
	for index := range text {
		if count == maxCharacters {
			return text[:index], true
		}
		count++
	}
	return text, false
}
