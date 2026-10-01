// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"strconv"
	"strings"
)

// storageDocumentWriter renders the document model as storage format, escaping all text so none becomes markup.
type storageDocumentWriter struct {
	builder strings.Builder
}

// renderStorageDocument converts blocks to storage-format XHTML.
func renderStorageDocument(blocks []documentBlock) string {
	writer := &storageDocumentWriter{}
	writer.writeBlocks(blocks)
	return writer.builder.String()
}

func (writer *storageDocumentWriter) writeBlocks(blocks []documentBlock) {
	for _, block := range blocks {
		writer.writeBlock(block)
	}
}

func (writer *storageDocumentWriter) writeBlock(block documentBlock) {
	switch block.kind {
	case blockParagraph:
		writer.builder.WriteString("<p>")
		writer.writeInlines(block.inlines)
		writer.builder.WriteString("</p>")
	case blockHeading:
		tag := "h" + strconv.Itoa(clampHeadingLevel(block.headingLevel))
		writer.builder.WriteString("<" + tag + ">")
		writer.writeInlines(block.inlines)
		writer.builder.WriteString("</" + tag + ">")
	case blockBulletList, blockOrderedList:
		writer.writeList(block)
	case blockTaskList:
		writer.builder.WriteString("<ac:task-list>")
		for _, item := range block.listItems {
			status := "incomplete"
			if item.isTaskDone {
				status = "complete"
			}
			writer.builder.WriteString("<ac:task><ac:task-status>" + status + "</ac:task-status><ac:task-body>")
			writer.writeListItemContent(item.blocks)
			writer.builder.WriteString("</ac:task-body></ac:task>")
		}
		writer.builder.WriteString("</ac:task-list>")
	case blockQuote:
		writer.builder.WriteString("<blockquote>")
		writer.writeBlocks(block.children)
		writer.builder.WriteString("</blockquote>")
	case blockCode:
		writer.builder.WriteString("<pre>")
		writer.builder.WriteString(escapeStorageText(block.codeText))
		writer.builder.WriteString("</pre>")
	case blockRule:
		writer.builder.WriteString("<hr />")
	case blockTable:
		writer.writeTable(block.tableRows)
	}
}

func (writer *storageDocumentWriter) writeList(block documentBlock) {
	tag := "ul"
	if block.kind == blockOrderedList {
		tag = "ol"
	}
	writer.builder.WriteString("<" + tag)
	if block.kind == blockOrderedList && block.orderedStart > 1 {
		writer.builder.WriteString(` start="` + strconv.Itoa(block.orderedStart) + `"`)
	}
	writer.builder.WriteString(">")
	for _, item := range block.listItems {
		writer.builder.WriteString("<li>")
		writer.writeListItemContent(item.blocks)
		writer.builder.WriteString("</li>")
	}
	writer.builder.WriteString("</" + tag + ">")
}

// writeListItemContent writes a lone paragraph inline, as the Confluence editor does, and other content as blocks.
func (writer *storageDocumentWriter) writeListItemContent(blocks []documentBlock) {
	if len(blocks) > 0 && blocks[0].kind == blockParagraph {
		writer.writeInlines(blocks[0].inlines)
		writer.writeBlocks(blocks[1:])
		return
	}
	writer.writeBlocks(blocks)
}

func (writer *storageDocumentWriter) writeTable(rows []documentTableRow) {
	writer.builder.WriteString("<table><tbody>")
	for _, row := range rows {
		cellTag := "td"
		if row.isHeader {
			cellTag = "th"
		}
		writer.builder.WriteString("<tr>")
		for _, cell := range row.cells {
			writer.builder.WriteString("<" + cellTag + ">")
			writer.writeInlines(cell)
			writer.builder.WriteString("</" + cellTag + ">")
		}
		writer.builder.WriteString("</tr>")
	}
	writer.builder.WriteString("</tbody></table>")
}

func (writer *storageDocumentWriter) writeInlines(inlines []documentInline) {
	for _, inline := range inlines {
		if inline.isLineBreak {
			writer.builder.WriteString("<br />")
			continue
		}
		var closingTags []string
		if inline.linkHref != "" {
			writer.builder.WriteString(`<a href="` + escapeStorageAttribute(inline.linkHref) + `">`)
			closingTags = append(closingTags, "</a>")
		}
		for _, styled := range []struct {
			isSet   bool
			opening string
			closing string
		}{
			{inline.isStrong, "<strong>", "</strong>"},
			{inline.isEmphasis, "<em>", "</em>"},
			{inline.isStrikethrough, `<span style="text-decoration: line-through;">`, "</span>"},
			{inline.isCode, "<code>", "</code>"},
		} {
			if styled.isSet {
				writer.builder.WriteString(styled.opening)
				closingTags = append(closingTags, styled.closing)
			}
		}
		writer.builder.WriteString(escapeStorageText(inline.text))
		for index := len(closingTags) - 1; index >= 0; index-- {
			writer.builder.WriteString(closingTags[index])
		}
	}
}

func escapeStorageText(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

func escapeStorageAttribute(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;").Replace(text)
}
