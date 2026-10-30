// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
)

// maximumStorageNodes bounds the element tree built from one storage body.
const maximumStorageNodes = 200000

var (
	errStorageUnreadable = errors.New("body is not readable Confluence storage format")
	// storageVoidElements omits link, which xml.HTMLAutoClose would match against ac:link because prefixes are ignored.
	storageVoidElements = []string{"br", "hr", "img", "col", "area", "base", "meta", "input", "wbr"}
)

// storageNode is one element or text node of a storage-format body.
type storageNode struct {
	name       string
	attributes map[string]string
	children   []*storageNode
	text       string
	isText     bool
}

// storageDocumentReader converts a storage-format element tree to the shared document model.
type storageDocumentReader struct{}

// parseStorageDocument reads Confluence storage format, XHTML with ac: and ri: elements, into blocks.
func parseStorageDocument(storage string) ([]documentBlock, error) {
	root, err := parseStorageTree(storage)
	if err != nil {
		return nil, err
	}
	return storageDocumentReader{}.readBlocks(root.children, 0), nil
}

// parseStorageTree parses leniently: undeclared ac: and ri: prefixes, HTML entities, and void tags are accepted.
func parseStorageTree(storage string) (*storageNode, error) {
	decoder := xml.NewDecoder(strings.NewReader("<storage-root>" + storage + "</storage-root>"))
	decoder.Strict = false
	decoder.AutoClose = storageVoidElements
	decoder.Entity = xml.HTMLEntity
	// The document node holds the synthetic storage-root element, which holds the body's nodes.
	document := &storageNode{}
	stack := []*storageNode{document}
	nodeCount := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errStorageUnreadable
		}
		nodeCount++
		if nodeCount > maximumStorageNodes {
			return nil, errStorageUnreadable
		}
		parent := stack[len(stack)-1]
		switch typed := token.(type) {
		case xml.StartElement:
			node := &storageNode{name: storageName(typed.Name), attributes: map[string]string{}}
			for _, attribute := range typed.Attr {
				node.attributes[storageName(attribute.Name)] = attribute.Value
			}
			parent.children = append(parent.children, node)
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			parent.children = append(parent.children, &storageNode{text: string(typed), isText: true})
		}
	}
	if len(document.children) != 1 || document.children[0].name != "storage-root" {
		return nil, errStorageUnreadable
	}
	return document.children[0], nil
}

func storageName(name xml.Name) string {
	if name.Space == "" {
		return strings.ToLower(name.Local)
	}
	return strings.ToLower(name.Space + ":" + name.Local)
}

// readBlocks groups inline runs into paragraphs and converts block elements.
func (reader storageDocumentReader) readBlocks(nodes []*storageNode, depth int) []documentBlock {
	var blocks []documentBlock
	var pendingInlines []documentInline
	for _, node := range nodes {
		if !node.isText && isStorageBlockElement(node) {
			blocks = appendParagraphBlock(blocks, pendingInlines)
			pendingInlines = nil
			blocks = append(blocks, reader.readBlock(node, depth+1)...)
			continue
		}
		pendingInlines = reader.appendInlines(pendingInlines, node, documentInline{}, depth+1)
	}
	return appendParagraphBlock(blocks, pendingInlines)
}

func (reader storageDocumentReader) readBlock(node *storageNode, depth int) []documentBlock {
	if depth > maximumDocumentDepth {
		return nil
	}
	switch node.name {
	case "p":
		inlines := trimInlineEdges(reader.readInlines(node.children, documentInline{}, depth))
		if !hasInlineText(inlines) {
			return nil
		}
		return []documentBlock{{kind: blockParagraph, inlines: inlines}}
	case "h1", "h2", "h3", "h4", "h5", "h6":
		level, _ := strconv.Atoi(node.name[1:])
		return []documentBlock{{kind: blockHeading, headingLevel: level, inlines: trimInlineEdges(reader.readInlines(node.children, documentInline{}, depth))}}
	case "ul", "ol":
		return []documentBlock{reader.readList(node, depth)}
	case "ac:task-list":
		return []documentBlock{reader.readTaskList(node, depth)}
	case "blockquote":
		return []documentBlock{{kind: blockQuote, children: reader.readBlocks(node.children, depth)}}
	case "pre":
		return []documentBlock{{kind: blockCode, codeText: strings.Trim(storageText(node), "\n")}}
	case "hr":
		return []documentBlock{{kind: blockRule}}
	case "table":
		return []documentBlock{{kind: blockTable, tableRows: reader.readTableRows(node, depth)}}
	case "ac:structured-macro", "ac:macro":
		return reader.readMacro(node, depth)
	case "ac:adf-extension":
		// Newer editor content stores a readable fallback beside attributes that are not text.
		for _, child := range node.children {
			if child.name == "ac:adf-fallback" {
				return reader.readBlocks(child.children, depth)
			}
		}
		return nil
	default:
		return reader.readBlocks(node.children, depth)
	}
}

func (reader storageDocumentReader) readList(node *storageNode, depth int) documentBlock {
	block := documentBlock{kind: blockBulletList}
	if node.name == "ol" {
		block.kind = blockOrderedList
		block.orderedStart, _ = strconv.Atoi(node.attributes["start"])
	}
	for _, child := range node.children {
		if child.name == "li" {
			block.listItems = append(block.listItems, documentListItem{blocks: reader.readBlocks(child.children, depth)})
		}
	}
	return block
}

func (reader storageDocumentReader) readTaskList(node *storageNode, depth int) documentBlock {
	block := documentBlock{kind: blockTaskList}
	for _, task := range node.children {
		if task.name != "ac:task" {
			continue
		}
		item := documentListItem{}
		for _, part := range task.children {
			switch part.name {
			case "ac:task-status":
				item.isTaskDone = strings.TrimSpace(storageText(part)) == "complete"
			case "ac:task-body":
				item.blocks = reader.readBlocks(part.children, depth)
			}
		}
		block.listItems = append(block.listItems, item)
	}
	return block
}

// readTableRows collects rows directly below the table or inside its thead, tbody, and tfoot.
func (reader storageDocumentReader) readTableRows(node *storageNode, depth int) []documentTableRow {
	var rows []documentTableRow
	for _, child := range node.children {
		switch child.name {
		case "tr":
			rows = append(rows, reader.readTableRow(child, depth))
		case "thead", "tbody", "tfoot":
			for _, row := range child.children {
				if row.name == "tr" {
					rows = append(rows, reader.readTableRow(row, depth))
				}
			}
		}
	}
	return rows
}

func (reader storageDocumentReader) readTableRow(node *storageNode, depth int) documentTableRow {
	row := documentTableRow{isHeader: true}
	for _, cell := range node.children {
		if cell.name != "th" && cell.name != "td" {
			continue
		}
		if cell.name == "td" {
			row.isHeader = false
		}
		row.cells = append(row.cells, flattenBlocksToInlines(reader.readBlocks(cell.children, depth)))
	}
	if len(row.cells) == 0 {
		row.isHeader = false
	}
	return row
}

// readMacro keeps a macro's readable content: code bodies, panel bodies, and other rich-text bodies.
func (reader storageDocumentReader) readMacro(node *storageNode, depth int) []documentBlock {
	macroName := node.attributes["ac:name"]
	parameters := map[string]string{}
	var richTextBody, plainTextBody *storageNode
	for _, child := range node.children {
		switch child.name {
		case "ac:parameter":
			parameters[child.attributes["ac:name"]] = strings.TrimSpace(storageText(child))
		case "ac:rich-text-body":
			richTextBody = child
		case "ac:plain-text-body":
			plainTextBody = child
		}
	}
	switch {
	case macroName == "code" || macroName == "noformat":
		code := ""
		if plainTextBody != nil {
			code = strings.Trim(storageText(plainTextBody), "\n")
		}
		return []documentBlock{{kind: blockCode, codeLanguage: markdownFenceLanguage(parameters["language"]), codeText: code}}
	case richTextBody != nil && (macroName == "info" || macroName == "note" || macroName == "warning" || macroName == "tip" || macroName == "panel"):
		return []documentBlock{{kind: blockQuote, children: reader.readBlocks(richTextBody.children, depth)}}
	case richTextBody != nil:
		return reader.readBlocks(richTextBody.children, depth)
	case plainTextBody != nil:
		return []documentBlock{{kind: blockParagraph, inlines: []documentInline{{text: strings.TrimSpace(storageText(plainTextBody))}}}}
	default:
		return nil
	}
}

func (reader storageDocumentReader) readInlines(nodes []*storageNode, style documentInline, depth int) []documentInline {
	var inlines []documentInline
	for _, node := range nodes {
		inlines = reader.appendInlines(inlines, node, style, depth+1)
	}
	return inlines
}

// appendInlines adds one node's spans; block elements met inside inline content become line breaks.
func (reader storageDocumentReader) appendInlines(inlines []documentInline, node *storageNode, style documentInline, depth int) []documentInline {
	if depth > maximumDocumentDepth {
		return inlines
	}
	if node.isText {
		span := style
		span.text = collapseStorageWhitespace(node.text)
		return appendInline(inlines, span)
	}
	switch node.name {
	case "br":
		return appendInline(inlines, documentInline{isLineBreak: true})
	case "strong", "b":
		style.isStrong = true
	case "em", "i":
		style.isEmphasis = true
	case "code", "tt":
		style.isCode = true
	case "s", "del", "strike":
		style.isStrikethrough = true
	case "span":
		if strings.Contains(strings.ToLower(node.attributes["style"]), "line-through") {
			style.isStrikethrough = true
		}
	case "a":
		style.linkHref = safeLinkHref(node.attributes["href"])
	case "ac:link":
		return reader.appendLinkInlines(inlines, node, style, depth)
	case "ac:emoticon":
		span := style
		span.text = node.attributes["ac:emoji-fallback"]
		return appendInline(inlines, span)
	case "time":
		span := style
		span.text = node.attributes["datetime"]
		return appendInline(inlines, span)
	case "ac:structured-macro":
		return reader.appendInlineMacro(inlines, node, style)
	case "ac:image", "ac:placeholder", "ac:parameter", "ac:adf-attribute", "ri:user", "ri:attachment", "ri:page":
		return inlines
	case "p", "div", "li", "h1", "h2", "h3", "h4", "h5", "h6", "tr", "blockquote", "pre":
		if len(inlines) > 0 {
			inlines = appendInline(inlines, documentInline{isLineBreak: true})
		}
	}
	for _, child := range node.children {
		inlines = reader.appendInlines(inlines, child, style, depth+1)
	}
	return inlines
}

// appendLinkInlines renders an ac:link by its body text, or the linked page title or URL.
func (reader storageDocumentReader) appendLinkInlines(inlines []documentInline, node *storageNode, style documentInline, depth int) []documentInline {
	var linkBody *storageNode
	fallbackText := ""
	for _, child := range node.children {
		switch child.name {
		case "ac:link-body":
			linkBody = child
		case "ac:plain-text-link-body":
			fallbackText = strings.TrimSpace(storageText(child))
		case "ri:page":
			if fallbackText == "" {
				fallbackText = child.attributes["ri:content-title"]
			}
		case "ri:attachment":
			if fallbackText == "" {
				fallbackText = child.attributes["ri:filename"]
			}
		case "ri:url":
			style.linkHref = safeLinkHref(child.attributes["ri:value"])
			if fallbackText == "" {
				fallbackText = child.attributes["ri:value"]
			}
		}
	}
	if linkBody != nil {
		for _, child := range linkBody.children {
			inlines = reader.appendInlines(inlines, child, style, depth+1)
		}
		return inlines
	}
	span := style
	span.text = fallbackText
	return appendInline(inlines, span)
}

// appendInlineMacro keeps the visible title of an inline status macro and drops other inline macros.
func (storageDocumentReader) appendInlineMacro(inlines []documentInline, node *storageNode, style documentInline) []documentInline {
	if node.attributes["ac:name"] != "status" {
		return inlines
	}
	for _, child := range node.children {
		if child.name == "ac:parameter" && child.attributes["ac:name"] == "title" {
			span := style
			span.text = strings.TrimSpace(storageText(child))
			return appendInline(inlines, span)
		}
	}
	return inlines
}

func isStorageBlockElement(node *storageNode) bool {
	switch node.name {
	case "p", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "ac:task-list", "blockquote", "pre", "hr", "table",
		"div", "section", "ac:layout", "ac:layout-section", "ac:layout-cell", "ac:rich-text-body", "ac:adf-extension":
		return true
	case "ac:structured-macro", "ac:macro":
		return node.attributes["ac:name"] != "status"
	default:
		return false
	}
}

// storageText concatenates every descendant text node, keeping whitespace for code bodies.
func storageText(node *storageNode) string {
	var builder strings.Builder
	writeStorageText(&builder, node, 0)
	return builder.String()
}

func writeStorageText(builder *strings.Builder, node *storageNode, depth int) {
	if depth > maximumDocumentDepth {
		return
	}
	if node.isText {
		builder.WriteString(node.text)
		return
	}
	for _, child := range node.children {
		writeStorageText(builder, child, depth+1)
	}
}

// appendParagraphBlock adds the trimmed spans as a paragraph when they hold any text.
func appendParagraphBlock(blocks []documentBlock, inlines []documentInline) []documentBlock {
	if trimmed := trimInlineEdges(inlines); hasInlineText(trimmed) {
		return append(blocks, documentBlock{kind: blockParagraph, inlines: trimmed})
	}
	return blocks
}

// collapseStorageWhitespace applies HTML whitespace rules: each run of spaces, tabs, line breaks, and
// no-break spaces becomes one space.
func collapseStorageWhitespace(text string) string {
	var builder strings.Builder
	isPreviousSpace := false
	for _, character := range text {
		switch character {
		case ' ', '\t', '\n', '\r', '\u00a0':
			if !isPreviousSpace {
				builder.WriteByte(' ')
			}
			isPreviousSpace = true
		default:
			isPreviousSpace = false
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

// flattenBlocksToInlines joins a table cell's blocks with line breaks.
func flattenBlocksToInlines(blocks []documentBlock) []documentInline {
	var inlines []documentInline
	renderer := documentTextRenderer{}
	for _, block := range blocks {
		if len(inlines) > 0 {
			inlines = appendInline(inlines, documentInline{isLineBreak: true})
		}
		switch block.kind {
		case blockParagraph, blockHeading:
			for _, inline := range block.inlines {
				inlines = appendInline(inlines, inline)
			}
		default:
			inlines = appendInline(inlines, documentInline{text: renderer.renderBlock(block)})
		}
	}
	return inlines
}

// safeLinkHref keeps only absolute http, https, and mailto links, which cannot run script when rendered.
func safeLinkHref(href string) string {
	href = strings.TrimSpace(href)
	lower := strings.ToLower(href)
	if len(href) > 2048 || strings.ContainsAny(href, " \t\r\n<>\"") {
		return ""
	}
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "mailto:") {
		return href
	}
	return ""
}

// markdownFenceLanguage keeps a code language that is safe on a fence line, such as go or c++.
func markdownFenceLanguage(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" || len(language) > 32 {
		return ""
	}
	for _, character := range language {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && !strings.ContainsRune("+#-_.", character) {
			return ""
		}
	}
	return language
}
