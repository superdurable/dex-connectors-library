// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"
)

// readableParagraph is one block of page text and the outermost article and main elements around it.
// A region ordinal of zero means the paragraph is outside every such element.
type readableParagraph struct {
	text           string
	articleOrdinal int
	mainOrdinal    int
}

// readableTextCollector splits balanced element events into paragraphs and remembers their regions.
type readableTextCollector struct {
	paragraphs     []readableParagraph
	lines          []string
	line           strings.Builder
	skippedDepth   int
	articleDepth   int
	articleOrdinal int
	mainDepth      int
	mainOrdinal    int
}

// extractReadableText returns the paragraphs of the article with the most text, else of the main element
// with the most text, else of the whole page, separated by one blank line.
func extractReadableText(body []byte, contentType string) string {
	collector := &readableTextCollector{}
	elements := &htmlElementStack{collector: collector, foreignContext: []bool{false}}
	tokenizer := nethtml.NewTokenizer(strings.NewReader(decodePageText(body, contentType)))
	for {
		switch tokenizer.Next() {
		case nethtml.ErrorToken:
			elements.closeAll()
			collector.finishParagraph()
			return joinParagraphs(selectReadableParagraphs(collector.paragraphs))
		case nethtml.TextToken:
			collector.addText(string(tokenizer.Text()))
		case nethtml.StartTagToken:
			name, _ := tokenizer.TagName()
			elements.open(string(name), false)
		case nethtml.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			elements.open(string(name), true)
		case nethtml.EndTagToken:
			name, _ := tokenizer.TagName()
			elements.close(string(name))
		}
	}
}

// decodePageText decodes body with the golang.org/x/net/html/charset rules and drops a leading byte order mark.
func decodePageText(body []byte, contentType string) string {
	reader, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return strings.TrimPrefix(string(body), "\uFEFF")
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return strings.TrimPrefix(string(body), "\uFEFF")
	}
	return strings.TrimPrefix(string(decoded), "\uFEFF")
}

func (collector *readableTextCollector) addText(text string) {
	if collector.skippedDepth == 0 {
		collector.line.WriteString(text)
	}
}

// openElement and closeElement receive balanced events: every opened element is closed exactly once.
func (collector *readableTextCollector) openElement(name string) {
	tag := atom.Lookup([]byte(name))
	switch {
	case isBoilerplateElement(tag):
		collector.finishParagraph()
		collector.skippedDepth++
	case collector.skippedDepth > 0:
	case tag == atom.Br:
		collector.finishLine()
	case tag == atom.Article:
		collector.finishParagraph()
		if collector.articleDepth == 0 {
			collector.articleOrdinal++
		}
		collector.articleDepth++
	case tag == atom.Main:
		collector.finishParagraph()
		if collector.mainDepth == 0 {
			collector.mainOrdinal++
		}
		collector.mainDepth++
	case isBlockElement(tag):
		collector.finishParagraph()
	}
}

func (collector *readableTextCollector) closeElement(name string) {
	tag := atom.Lookup([]byte(name))
	switch {
	case isBoilerplateElement(tag):
		collector.skippedDepth = max(collector.skippedDepth-1, 0)
	case collector.skippedDepth > 0:
	case tag == atom.Br:
		collector.finishLine()
	case tag == atom.Article:
		collector.finishParagraph()
		collector.articleDepth = max(collector.articleDepth-1, 0)
	case tag == atom.Main:
		collector.finishParagraph()
		collector.mainDepth = max(collector.mainDepth-1, 0)
	case isBlockElement(tag):
		collector.finishParagraph()
	}
}

func (collector *readableTextCollector) finishLine() {
	if line := collapseWhitespace(collector.line.String()); line != "" {
		collector.lines = append(collector.lines, line)
	}
	collector.line.Reset()
}

func (collector *readableTextCollector) finishParagraph() {
	collector.finishLine()
	if len(collector.lines) == 0 {
		return
	}
	paragraph := readableParagraph{text: strings.Join(collector.lines, "\n")}
	if collector.articleDepth > 0 {
		paragraph.articleOrdinal = collector.articleOrdinal
	}
	if collector.mainDepth > 0 {
		paragraph.mainOrdinal = collector.mainOrdinal
	}
	collector.paragraphs = append(collector.paragraphs, paragraph)
	collector.lines = nil
}

// htmlElementStack turns tokens into balanced open and close events with the htmlparser2 rules: implied
// closes, void elements, closing every element above a matched end tag, and self-closing tags only in svg
// or math. The TypeScript package receives the same events from htmlparser2, so both extract equal text,
// and an unclosed element such as a nav ends with its parent instead of hiding the rest of the page.
type htmlElementStack struct {
	collector      *readableTextCollector
	openElements   []string
	foreignContext []bool
}

func (stack *htmlElementStack) open(name string, isSelfClosing bool) {
	if impliedCloses, exists := openImpliesClose[name]; exists {
		for len(stack.openElements) > 0 && slices.Contains(impliedCloses, stack.openElements[len(stack.openElements)-1]) {
			stack.popTop()
		}
	}
	stack.collector.openElement(name)
	if voidElements[name] {
		stack.collector.closeElement(name)
		return
	}
	stack.openElements = append(stack.openElements, name)
	if foreignContextElements[name] {
		stack.foreignContext = append(stack.foreignContext, true)
	} else if htmlIntegrationElements[name] {
		stack.foreignContext = append(stack.foreignContext, false)
	}
	if isSelfClosing && stack.foreignContext[len(stack.foreignContext)-1] {
		stack.popTop()
	}
}

func (stack *htmlElementStack) close(name string) {
	if (foreignContextElements[name] || htmlIntegrationElements[name]) && len(stack.foreignContext) > 1 {
		stack.foreignContext = stack.foreignContext[:len(stack.foreignContext)-1]
	}
	if voidElements[name] {
		if name == "br" {
			stack.collector.openElement(name)
			stack.collector.closeElement(name)
		}
		return
	}
	if position := lastIndex(stack.openElements, name); position >= 0 {
		for len(stack.openElements) > position {
			stack.popTop()
		}
		return
	}
	if name == "p" {
		stack.open("p", false)
		stack.popTop()
	}
}

func (stack *htmlElementStack) closeAll() {
	for len(stack.openElements) > 0 {
		stack.popTop()
	}
}

func (stack *htmlElementStack) popTop() {
	name := stack.openElements[len(stack.openElements)-1]
	stack.openElements = stack.openElements[:len(stack.openElements)-1]
	stack.collector.closeElement(name)
}

func lastIndex(values []string, value string) int {
	for index := len(values) - 1; index >= 0; index-- {
		if values[index] == value {
			return index
		}
	}
	return -1
}

// selectReadableParagraphs prefers the article, then the main element, holding the most text.
func selectReadableParagraphs(paragraphs []readableParagraph) []readableParagraph {
	if selected := selectLargestRegion(paragraphs, func(paragraph readableParagraph) int { return paragraph.articleOrdinal }); selected != nil {
		return selected
	}
	if selected := selectLargestRegion(paragraphs, func(paragraph readableParagraph) int { return paragraph.mainOrdinal }); selected != nil {
		return selected
	}
	return paragraphs
}

// selectLargestRegion returns the paragraphs of the region with the most code points; the earliest wins a tie.
func selectLargestRegion(paragraphs []readableParagraph, regionOrdinal func(readableParagraph) int) []readableParagraph {
	sizes := map[int]int{}
	largestOrdinal := 0
	for _, paragraph := range paragraphs {
		ordinal := regionOrdinal(paragraph)
		if ordinal == 0 {
			continue
		}
		sizes[ordinal] += utf8.RuneCountInString(paragraph.text)
		if largestOrdinal == 0 || sizes[ordinal] > sizes[largestOrdinal] || (sizes[ordinal] == sizes[largestOrdinal] && ordinal < largestOrdinal) {
			largestOrdinal = ordinal
		}
	}
	if largestOrdinal == 0 {
		return nil
	}
	var selected []readableParagraph
	for _, paragraph := range paragraphs {
		if regionOrdinal(paragraph) == largestOrdinal {
			selected = append(selected, paragraph)
		}
	}
	return selected
}

func joinParagraphs(paragraphs []readableParagraph) string {
	texts := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		texts = append(texts, paragraph.text)
	}
	return strings.Join(texts, "\n\n")
}

// truncateText cuts text to at most maxCharacters code points at a late space or line break and ends a
// cut text with an ellipsis that counts toward the bound.
func truncateText(text string, maxCharacters int) (string, bool) {
	if utf8.RuneCountInString(text) <= maxCharacters {
		return text, false
	}
	cut := string([]rune(text)[:maxCharacters-1])
	if boundary := strings.LastIndexAny(cut, " \n"); boundary > len(cut)/2 {
		cut = cut[:boundary]
	}
	return strings.TrimRightFunc(cut, unicode.IsSpace) + "…", true
}

// isBoilerplateElement reports elements whose content is never readable article text.
func isBoilerplateElement(tag atom.Atom) bool {
	switch tag {
	case atom.Script, atom.Style, atom.Noscript, atom.Template, atom.Svg, atom.Math, atom.Iframe, atom.Title,
		atom.Nav, atom.Header, atom.Footer, atom.Aside, atom.Button, atom.Select, atom.Textarea:
		return true
	}
	return false
}

var (
	paragraphElements   = []string{"p"}
	formControlElements = []string{"input", "option", "optgroup", "select", "button", "datalist", "textarea"}
	// openImpliesClose lists, per opened element, the open elements it closes first, as htmlparser2 does.
	openImpliesClose = map[string][]string{
		"tr": {"tr", "th", "td"}, "th": {"th"}, "td": {"thead", "th", "td"}, "body": {"head", "link", "script"},
		"li": {"li"}, "p": paragraphElements, "h1": paragraphElements, "h2": paragraphElements, "h3": paragraphElements,
		"h4": paragraphElements, "h5": paragraphElements, "h6": paragraphElements,
		"select": formControlElements, "input": formControlElements, "output": formControlElements,
		"button": formControlElements, "datalist": formControlElements, "textarea": formControlElements,
		"option": {"option"}, "optgroup": {"optgroup", "option"}, "dd": {"dd", "dt"}, "dt": {"dd", "dt"},
		"address": paragraphElements, "article": paragraphElements, "aside": paragraphElements,
		"blockquote": paragraphElements, "details": paragraphElements, "div": paragraphElements,
		"dl": paragraphElements, "fieldset": paragraphElements, "figcaption": paragraphElements,
		"figure": paragraphElements, "footer": paragraphElements, "form": paragraphElements,
		"header": paragraphElements, "hr": paragraphElements, "main": paragraphElements, "nav": paragraphElements,
		"ol": paragraphElements, "pre": paragraphElements, "section": paragraphElements,
		"table": paragraphElements, "ul": paragraphElements, "rt": {"rt", "rp"}, "rp": {"rt", "rp"},
		"tbody": {"thead", "tbody"}, "tfoot": {"thead", "tbody"},
	}
	voidElements = map[string]bool{
		"area": true, "base": true, "basefont": true, "br": true, "col": true, "command": true, "embed": true,
		"frame": true, "hr": true, "img": true, "input": true, "isindex": true, "keygen": true, "link": true,
		"meta": true, "param": true, "source": true, "track": true, "wbr": true,
	}
	foreignContextElements  = map[string]bool{"math": true, "svg": true}
	htmlIntegrationElements = map[string]bool{
		"mi": true, "mo": true, "mn": true, "ms": true, "mtext": true, "annotation-xml": true,
		"foreignobject": true, "desc": true, "title": true,
	}
)
