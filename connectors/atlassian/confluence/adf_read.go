// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

var errADFUnreadable = errors.New("body is not readable Atlassian Document Format")

// adfNode is the subset of an Atlassian Document Format node the connector reads.
type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Marks   []adfMark      `json:"marks,omitempty"`
	Content []adfNode      `json:"content,omitempty"`
}

type adfMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// adfDocumentReader converts an Atlassian Document Format tree to the shared document model.
type adfDocumentReader struct{}

// parseADFDocument reads the atlas_doc_format body value, a JSON-encoded document, into blocks.
func parseADFDocument(value string) ([]documentBlock, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var document adfNode
	if err := json.Unmarshal([]byte(value), &document); err != nil || document.Type != "doc" {
		return nil, errADFUnreadable
	}
	return adfDocumentReader{}.readBlocks(document.Content, 0), nil
}

func (reader adfDocumentReader) readBlocks(nodes []adfNode, depth int) []documentBlock {
	var blocks []documentBlock
	var pendingInlines []documentInline
	for _, node := range nodes {
		if isADFInlineNode(node.Type) {
			pendingInlines = reader.appendInlines(pendingInlines, node)
			continue
		}
		blocks = appendParagraphBlock(blocks, pendingInlines)
		pendingInlines = nil
		blocks = append(blocks, reader.readBlock(node, depth+1)...)
	}
	return appendParagraphBlock(blocks, pendingInlines)
}

func (reader adfDocumentReader) readBlock(node adfNode, depth int) []documentBlock {
	if depth > maximumDocumentDepth {
		return nil
	}
	switch node.Type {
	case "paragraph":
		return appendParagraphBlock(nil, reader.readInlines(node.Content))
	case "heading":
		level := int(adfNumberAttribute(node.Attrs, "level"))
		return []documentBlock{{kind: blockHeading, headingLevel: level, inlines: trimInlineEdges(reader.readInlines(node.Content))}}
	case "bulletList", "orderedList", "taskList", "decisionList":
		return []documentBlock{reader.readList(node, depth)}
	case "blockquote", "panel":
		return []documentBlock{{kind: blockQuote, children: reader.readBlocks(node.Content, depth)}}
	case "codeBlock":
		var code strings.Builder
		for _, child := range node.Content {
			code.WriteString(child.Text)
		}
		return []documentBlock{{kind: blockCode, codeLanguage: markdownFenceLanguage(adfStringAttribute(node.Attrs, "language")), codeText: code.String()}}
	case "rule":
		return []documentBlock{{kind: blockRule}}
	case "table":
		return []documentBlock{{kind: blockTable, tableRows: reader.readTableRows(node, depth)}}
	case "expand", "nestedExpand":
		blocks := []documentBlock{}
		if title := strings.TrimSpace(adfStringAttribute(node.Attrs, "title")); title != "" {
			blocks = append(blocks, documentBlock{kind: blockParagraph, inlines: []documentInline{{text: title, isStrong: true}}})
		}
		return append(blocks, reader.readBlocks(node.Content, depth)...)
	case "blockCard", "embedCard":
		href := safeLinkHref(adfStringAttribute(node.Attrs, "url"))
		if href == "" {
			return nil
		}
		return []documentBlock{{kind: blockParagraph, inlines: []documentInline{{text: href, linkHref: href}}}}
	case "mediaSingle", "mediaGroup", "media", "extension":
		return nil
	default:
		return reader.readBlocks(node.Content, depth)
	}
}

func (reader adfDocumentReader) readList(node adfNode, depth int) documentBlock {
	block := documentBlock{kind: blockBulletList}
	switch node.Type {
	case "orderedList":
		block.kind = blockOrderedList
		block.orderedStart = int(adfNumberAttribute(node.Attrs, "order"))
	case "taskList":
		block.kind = blockTaskList
	}
	for _, item := range node.Content {
		listItem := documentListItem{isTaskDone: adfStringAttribute(item.Attrs, "state") == "DONE"}
		if item.Type == "taskItem" || item.Type == "decisionItem" {
			listItem.blocks = appendParagraphBlock(nil, reader.readInlines(item.Content))
		} else {
			listItem.blocks = reader.readBlocks(item.Content, depth)
		}
		block.listItems = append(block.listItems, listItem)
	}
	return block
}

func (reader adfDocumentReader) readTableRows(node adfNode, depth int) []documentTableRow {
	var rows []documentTableRow
	for _, rowNode := range node.Content {
		if rowNode.Type != "tableRow" {
			continue
		}
		row := documentTableRow{isHeader: len(rowNode.Content) > 0}
		for _, cell := range rowNode.Content {
			if cell.Type != "tableHeader" {
				row.isHeader = false
			}
			row.cells = append(row.cells, flattenBlocksToInlines(reader.readBlocks(cell.Content, depth)))
		}
		rows = append(rows, row)
	}
	return rows
}

func (reader adfDocumentReader) readInlines(nodes []adfNode) []documentInline {
	var inlines []documentInline
	for _, node := range nodes {
		inlines = reader.appendInlines(inlines, node)
	}
	return inlines
}

// appendInlines converts one inline node; marks become span styles and a link mark becomes the href.
func (adfDocumentReader) appendInlines(inlines []documentInline, node adfNode) []documentInline {
	span := documentInline{}
	for _, mark := range node.Marks {
		switch mark.Type {
		case "strong":
			span.isStrong = true
		case "em":
			span.isEmphasis = true
		case "code":
			span.isCode = true
		case "strike":
			span.isStrikethrough = true
		case "link":
			span.linkHref = safeLinkHref(adfStringAttribute(mark.Attrs, "href"))
		}
	}
	switch node.Type {
	case "text":
		span.text = node.Text
	case "hardBreak":
		return appendInline(inlines, documentInline{isLineBreak: true})
	case "mention", "status":
		span.text = adfStringAttribute(node.Attrs, "text")
	case "emoji":
		span.text = adfStringAttribute(node.Attrs, "text")
		if span.text == "" {
			span.text = adfStringAttribute(node.Attrs, "shortName")
		}
	case "inlineCard":
		span.linkHref = safeLinkHref(adfStringAttribute(node.Attrs, "url"))
		span.text = span.linkHref
	case "date":
		span.text = formatADFDate(adfStringAttribute(node.Attrs, "timestamp"))
	}
	return appendInline(inlines, span)
}

func isADFInlineNode(nodeType string) bool {
	switch nodeType {
	case "text", "hardBreak", "mention", "status", "emoji", "inlineCard", "date", "placeholder", "inlineExtension", "mediaInline":
		return true
	default:
		return false
	}
}

func adfStringAttribute(attributes map[string]any, name string) string {
	value, _ := attributes[name].(string)
	return value
}

func adfNumberAttribute(attributes map[string]any, name string) float64 {
	value, _ := attributes[name].(float64)
	return value
}

// formatADFDate renders a date node's millisecond Unix timestamp as YYYY-MM-DD in UTC.
func formatADFDate(timestamp string) string {
	milliseconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ""
	}
	return time.UnixMilli(milliseconds).UTC().Format(time.DateOnly)
}
