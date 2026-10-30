// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs

import (
	"cmp"
	"regexp"
	"strconv"
	"strings"
)

// TextFormat selects how getDocumentText renders a document and how
// createDocument asks Google to import its initial text.
type TextFormat string

const (
	// TextFormatMarkdown renders headings as #, lists as - or 1., and tables as
	// pipe tables. Inline styling, images, and links inside text runs are omitted.
	TextFormatMarkdown TextFormat = "markdown"
	// TextFormatPlainText renders paragraphs as lines, lists with - or 1. and
	// two-space indentation, and table cells separated by tabs.
	TextFormatPlainText TextFormat = "plainText"
)

const (
	markdownListIndent  = "    "
	plainTextListIndent = "  "
)

// headingLevels maps Docs named paragraph styles to Markdown heading depth.
var headingLevels = map[string]int{
	"TITLE": 1, "SUBTITLE": 2, "HEADING_1": 1, "HEADING_2": 2, "HEADING_3": 3,
	"HEADING_4": 4, "HEADING_5": 5, "HEADING_6": 6,
}

// orderedGlyphTypes are the Docs glyph types of numbered or lettered list levels.
var orderedGlyphTypes = map[string]bool{
	"DECIMAL": true, "ZERO_DECIMAL": true, "UPPER_ALPHA": true, "ALPHA": true, "UPPER_ROMAN": true, "ROMAN": true,
}

// markdownBlockStartPattern matches line starts that Markdown would read as structure.
var markdownBlockStartPattern = regexp.MustCompile("^(?:#{1,6}(?:\\s|$)|[-+*](?:\\s|$)|>|```|~~~|(?:-\\s*){3,}$|(?:\\*\\s*){3,}$|(?:_\\s*){3,}$|=+\\s*$)")

// markdownOrderedStartPattern matches a line that Markdown would read as an ordered list item.
var markdownOrderedStartPattern = regexp.MustCompile(`^(\d{1,9})([.)])(\s|$)`)

// renderedBlock is one rendered paragraph or table.
type renderedBlock struct {
	text       string
	isListItem bool
}

// documentTextRenderer renders one tab body; list counters persist across the body.
type documentTextRenderer struct {
	format          TextFormat
	lists           map[string]listResource
	orderedCounters map[string][]int
}

// renderDocumentText renders the tab's body without headers, footers,
// footnote text, tables of contents, images, or equations.
func renderDocumentText(tab tabResource, format TextFormat) (string, error) {
	renderer := documentTextRenderer{format: format, lists: tab.DocumentTab.Lists, orderedCounters: map[string][]int{}}
	blocks, err := renderer.renderContent(tab.DocumentTab.Body.Content, 0)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for index, block := range blocks {
		if index > 0 {
			text.WriteString(renderer.blockSeparator(blocks[index-1], block))
		}
		text.WriteString(block.text)
	}
	return strings.Trim(text.String(), "\n"), nil
}

func (renderer *documentTextRenderer) renderContent(content []structuralElementResource, depth int) ([]renderedBlock, error) {
	if depth > maxStructureDepth {
		return nil, errDocumentStructureTooDeep
	}
	var blocks []renderedBlock
	for _, element := range content {
		switch {
		case element.Paragraph != nil:
			if block, isRendered := renderer.renderParagraph(element.Paragraph); isRendered {
				blocks = append(blocks, block)
			}
		case element.Table != nil:
			block, isRendered, err := renderer.renderTable(element.Table, depth)
			if err != nil {
				return nil, err
			}
			if isRendered {
				blocks = append(blocks, block)
			}
		}
	}
	return blocks, nil
}

func (renderer *documentTextRenderer) blockSeparator(previous renderedBlock, next renderedBlock) string {
	if renderer.format == TextFormatPlainText || (previous.isListItem && next.isListItem) {
		return "\n"
	}
	return "\n\n"
}

func (renderer *documentTextRenderer) renderParagraph(paragraph *paragraphResource) (renderedBlock, bool) {
	text, hasHorizontalRule := renderer.inlineText(paragraph)
	isMarkdown := renderer.format == TextFormatMarkdown
	if paragraph.Bullet != nil {
		return renderedBlock{text: renderer.listItemText(paragraph.Bullet, strings.ReplaceAll(text, "\n", " ")), isListItem: true}, true
	}
	if level, isHeading := headingLevels[paragraph.ParagraphStyle.NamedStyleType]; isHeading && strings.TrimSpace(text) != "" {
		text = strings.ReplaceAll(text, "\n", " ")
		if isMarkdown {
			return renderedBlock{text: strings.Repeat("#", level) + " " + text}, true
		}
		return renderedBlock{text: text}, true
	}
	if strings.TrimSpace(text) == "" {
		switch {
		case hasHorizontalRule && isMarkdown:
			return renderedBlock{text: "---"}, true
		case isMarkdown:
			return renderedBlock{}, false
		default:
			return renderedBlock{text: ""}, true
		}
	}
	if isMarkdown {
		return renderedBlock{text: escapeMarkdownLineStarts(text)}, true
	}
	return renderedBlock{text: text}, true
}

// inlineText joins a paragraph's elements without its final newline. Soft line
// breaks become newlines, and chips become their display text.
func (renderer *documentTextRenderer) inlineText(paragraph *paragraphResource) (string, bool) {
	var text strings.Builder
	hasHorizontalRule := false
	isMarkdown := renderer.format == TextFormatMarkdown
	for _, element := range paragraph.Elements {
		switch {
		case element.TextRun != nil:
			content := strings.ReplaceAll(element.TextRun.Content, string(nonTextElementMarker), "")
			text.WriteString(strings.ReplaceAll(content, "\v", "\n"))
		case element.Person != nil:
			text.WriteString(cmp.Or(element.Person.PersonProperties.Name, element.Person.PersonProperties.Email))
		case element.RichLink != nil:
			properties := element.RichLink.RichLinkProperties
			if isMarkdown && properties.URI != "" {
				text.WriteString("[" + escapeMarkdownLinkText(cmp.Or(properties.Title, properties.URI)) + "](" + escapeMarkdownLinkDestination(properties.URI) + ")")
			} else {
				text.WriteString(cmp.Or(properties.Title, properties.URI))
			}
		case element.DateElement != nil:
			text.WriteString(element.DateElement.DateElementProperties.DisplayText)
		case element.FootnoteReference != nil:
			if isMarkdown {
				text.WriteString("[^" + element.FootnoteReference.FootnoteNumber + "]")
			} else {
				text.WriteString("[" + element.FootnoteReference.FootnoteNumber + "]")
			}
		case element.HorizontalRule != nil:
			hasHorizontalRule = true
		}
	}
	return strings.TrimSuffix(text.String(), "\n"), hasHorizontalRule
}

// listItemText numbers ordered items per list and nesting level, as Docs continues
// a list's numbering across interrupting paragraphs.
func (renderer *documentTextRenderer) listItemText(bullet *bulletResource, text string) string {
	level := min(max(bullet.NestingLevel, 0), 8)
	indent := plainTextListIndent
	if renderer.format == TextFormatMarkdown {
		indent = markdownListIndent
	}
	marker := "- "
	if renderer.isOrderedLevel(bullet.ListID, level) {
		counters := renderer.orderedCounters[bullet.ListID]
		if len(counters) < level+1 {
			counters = append(counters, make([]int, level+1-len(counters))...)
		}
		counters[level]++
		for deeper := level + 1; deeper < len(counters); deeper++ {
			counters[deeper] = 0
		}
		renderer.orderedCounters[bullet.ListID] = counters
		marker = strconv.Itoa(counters[level]) + ". "
	}
	return strings.Repeat(indent, level) + marker + text
}

func (renderer *documentTextRenderer) isOrderedLevel(listID string, level int) bool {
	list, isListKnown := renderer.lists[listID]
	if !isListKnown || level >= len(list.ListProperties.NestingLevels) {
		return false
	}
	return orderedGlyphTypes[list.ListProperties.NestingLevels[level].GlyphType]
}

func (renderer *documentTextRenderer) renderTable(table *tableResource, depth int) (renderedBlock, bool, error) {
	rows := make([][]string, 0, len(table.TableRows))
	columnCount := 0
	for _, row := range table.TableRows {
		cells := make([]string, 0, len(row.TableCells))
		for _, cell := range row.TableCells {
			cellText, err := renderer.cellText(cell.Content, depth+1)
			if err != nil {
				return renderedBlock{}, false, err
			}
			cells = append(cells, cellText)
		}
		columnCount = max(columnCount, len(cells))
		rows = append(rows, cells)
	}
	if columnCount == 0 {
		return renderedBlock{}, false, nil
	}
	lines := make([]string, 0, len(rows)+1)
	for rowIndex, cells := range rows {
		for len(cells) < columnCount {
			cells = append(cells, "")
		}
		if renderer.format == TextFormatPlainText {
			lines = append(lines, strings.Join(cells, "\t"))
			continue
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
		if rowIndex == 0 {
			lines = append(lines, "|"+strings.Repeat(" --- |", columnCount))
		}
	}
	return renderedBlock{text: strings.Join(lines, "\n")}, true, nil
}

// cellText flattens a cell's paragraphs and nested tables into one line.
func (renderer *documentTextRenderer) cellText(content []structuralElementResource, depth int) (string, error) {
	if depth > maxStructureDepth {
		return "", errDocumentStructureTooDeep
	}
	var parts []string
	for _, element := range content {
		switch {
		case element.Paragraph != nil:
			text, _ := renderer.inlineText(element.Paragraph)
			if strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		case element.Table != nil:
			for _, row := range element.Table.TableRows {
				for _, cell := range row.TableCells {
					text, err := renderer.cellText(cell.Content, depth+1)
					if err != nil {
						return "", err
					}
					if text != "" {
						parts = append(parts, text)
					}
				}
			}
		}
	}
	text := strings.Join(parts, " ")
	if renderer.format == TextFormatPlainText {
		return strings.NewReplacer("\n", " ", "\t", " ").Replace(text), nil
	}
	return strings.NewReplacer("\n", " ", "|", "\\|").Replace(text), nil
}

// escapeMarkdownLineStarts keeps an ordinary paragraph from reading as a
// heading, list, quote, rule, or fence. Inline characters are not escaped.
func escapeMarkdownLineStarts(text string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if match := markdownOrderedStartPattern.FindStringSubmatchIndex(line); match != nil {
			delimiterIndex := match[4]
			lines[index] = line[:delimiterIndex] + "\\" + line[delimiterIndex:]
			continue
		}
		if markdownBlockStartPattern.MatchString(line) {
			lines[index] = "\\" + line
		}
	}
	return strings.Join(lines, "\n")
}

func escapeMarkdownLinkText(text string) string {
	return strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]").Replace(text)
}

func escapeMarkdownLinkDestination(uri string) string {
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E").Replace(uri)
}
