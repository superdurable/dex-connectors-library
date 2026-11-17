// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/mail"
	"net/url"
	"strings"

	"golang.org/x/net/html/charset"
)

const (
	namespaceAtom    = "http://www.w3.org/2005/Atom"
	namespaceAtom03  = "http://purl.org/atom/ns#"
	namespaceRSS1    = "http://purl.org/rss/1.0/"
	namespaceRSS090  = "http://my.netscape.com/rdf/simple/0.9/"
	namespaceRDF     = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	namespaceDublin  = "http://purl.org/dc/elements/1.1/"
	namespaceContent = "http://purl.org/rss/1.0/modules/content/"
	namespaceXML     = "http://www.w3.org/XML/1998/namespace"
)

// maxXMLDepth bounds element nesting, which also bounds the decoder's recursion.
const maxXMLDepth = 128

// xmlNode is a generic element tree, so each format reads elements by exact namespace and local name.
type xmlNode struct {
	name       xml.Name
	attributes []xml.Attr
	children   []xmlNode
	// segments keeps character data and child elements in document order.
	segments []xmlSegment
}

// xmlSegment is either character data or, when childIndex is not negative, a child element.
type xmlSegment struct {
	text       string
	childIndex int
}

// parseXMLFeed decodes an RSS, RDF, or Atom document. Unknown HTML entities and missing end tags are
// tolerated, because many feeds contain them.
func parseXMLFeed(body []byte, httpCharset string, options feedParseOptions) (Feed, *fetchFailure) {
	decoder := newTolerantXMLDecoder(body, httpCharset)
	start, err := readRootElement(decoder)
	if err != nil {
		return Feed{}, invalidResponse("the document is neither XML nor a JSON Feed")
	}
	switch {
	case strings.EqualFold(start.Name.Local, "html"):
		return Feed{}, invalidResponse("the URL serves an HTML page, not a feed; find its feeds with discoverFeeds")
	case start.Name.Local != "rss" && start.Name.Local != "RDF" && start.Name.Local != "feed":
		return Feed{}, invalidResponse("the XML document is not an RSS, RDF, or Atom feed")
	}
	root, err := decodeXMLNode(decoder, start, 1)
	if err != nil {
		return Feed{}, invalidResponse("the feed XML is malformed or nested too deeply")
	}
	documentURL, err := url.Parse(options.documentURL)
	if err != nil {
		return Feed{}, invalidResponse("the feed URL cannot be used as a base URL")
	}
	switch root.name.Local {
	case "rss":
		return parseRSS(root, documentURL, options), nil
	case "RDF":
		return parseRDF(root, documentURL, options), nil
	default:
		if root.name.Space != namespaceAtom && root.name.Space != namespaceAtom03 {
			return Feed{}, invalidResponse("the feed element is not in the Atom namespace")
		}
		return parseAtom(root, documentURL, options), nil
	}
}

func newTolerantXMLDecoder(body []byte, httpCharset string) *xml.Decoder {
	var reader io.Reader = bytes.NewReader(body)
	// An HTTP charset applies only when the XML declaration names none, which encoding/xml then reads as UTF-8.
	if httpCharset != "" && !strings.EqualFold(httpCharset, "utf-8") && !declaresEncoding(body) {
		if converted, err := charset.NewReaderLabel(httpCharset, reader); err == nil {
			reader = converted
		}
	}
	decoder := xml.NewDecoder(reader)
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity
	decoder.CharsetReader = charset.NewReaderLabel
	return decoder
}

func declaresEncoding(body []byte) bool {
	prefix := body[:min(len(body), 256)]
	if !bytes.HasPrefix(prefix, []byte("<?xml")) {
		return false
	}
	end := bytes.Index(prefix, []byte("?>"))
	return end > 0 && bytes.Contains(prefix[:end], []byte("encoding"))
}

// decodeXMLNode reads the element that start opened, through its end element.
func decodeXMLNode(decoder *xml.Decoder, start xml.StartElement, depth int) (*xmlNode, error) {
	if depth > maxXMLDepth {
		return nil, errors.New("XML nesting is too deep")
	}
	node := &xmlNode{name: start.Name, attributes: start.Attr}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			child, err := decodeXMLNode(decoder, typed, depth+1)
			if err != nil {
				return nil, err
			}
			node.children = append(node.children, *child)
			node.segments = append(node.segments, xmlSegment{childIndex: len(node.children) - 1})
		case xml.CharData:
			node.segments = append(node.segments, xmlSegment{text: string(typed), childIndex: -1})
		case xml.EndElement:
			return node, nil
		}
	}
}

func readRootElement(decoder *xml.Decoder) (xml.StartElement, error) {
	for {
		token, err := decoder.Token()
		if err != nil {
			return xml.StartElement{}, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			return typed, nil
		case xml.CharData:
			if len(bytes.TrimSpace(typed)) > 0 {
				return xml.StartElement{}, errors.New("text before the root element")
			}
		}
	}
}

func parseRSS(root *xmlNode, documentURL *url.URL, options feedParseOptions) Feed {
	// RSS has no namespace, but some RSS 2.0 feeds declare a default one; core elements share the root's.
	namespace := root.name.Space
	channel := root.firstChild(namespace, "channel")
	if channel == nil {
		channel = &xmlNode{}
	}
	base := channel.baseURL(root.baseURL(documentURL))
	feed := Feed{
		Format:      FeedFormatRSS,
		Title:       plainTextFromEscapedTitle(channel.childText(namespace, "title")),
		SiteURL:     resolveLink(base, channel.childText(namespace, "link")),
		Description: boundedText(plainTextFromHTML(channel.childText(namespace, "description")), maxDescriptionCharacters),
		UpdatedAt:   optionalTimestamp(firstNonEmpty(channel.childText(namespace, "lastBuildDate"), channel.childText(namespace, "pubDate"), channel.childText(namespaceDublin, "date"))),
	}
	itemNodes := channel.childrenNamed(namespace, "item")
	if len(itemNodes) == 0 {
		itemNodes = root.childrenNamed(namespace, "item")
	}
	items := make([]FeedItem, 0, min(len(itemNodes), options.limit))
	for index := range itemNodes {
		if index >= options.limit {
			break
		}
		items = append(items, parseRSSItem(itemNodes[index], namespace, base, options))
	}
	setFeedItems(&feed, items, len(itemNodes), options.limit)
	return feed
}

func parseRSSItem(node *xmlNode, namespace string, base *url.URL, options feedParseOptions) FeedItem {
	base = node.baseURL(base)
	item := FeedItem{
		Title:       plainTextFromEscapedTitle(node.childText(namespace, "title")),
		Link:        resolveLink(base, node.childText(namespace, "link")),
		PublishedAt: optionalTimestamp(firstNonEmpty(node.childText(namespace, "pubDate"), node.childText(namespaceDublin, "date"))),
		UpdatedAt:   optionalTimestamp(node.childText(namespaceAtom, "updated")),
	}
	if guid := node.firstChild(namespace, "guid"); guid != nil {
		item.ID = strings.TrimSpace(guid.text())
		isPermaLink := !strings.EqualFold(strings.TrimSpace(guid.attribute("", "isPermaLink")), "false")
		if item.Link == "" && isPermaLink {
			item.Link = resolveLink(base, guid.text())
		}
	}
	summary := firstNonEmpty(node.childText(namespace, "description"), node.childText(namespaceContent, "encoded"))
	item.Summary = truncateCharacters(plainTextFromHTML(summary), options.maxSummaryCharacters)
	for _, author := range node.childrenNamed(namespace, "author") {
		item.Authors = appendBoundedValue(item.Authors, rssAuthorName(author.text()), maxNameCharacters)
	}
	for _, creator := range node.childrenNamed(namespaceDublin, "creator") {
		item.Authors = appendBoundedValue(item.Authors, creator.text(), maxNameCharacters)
	}
	for _, category := range node.childrenNamed(namespace, "category") {
		item.Categories = appendBoundedValue(item.Categories, category.text(), maxNameCharacters)
	}
	for _, subject := range node.childrenNamed(namespaceDublin, "subject") {
		item.Categories = appendBoundedValue(item.Categories, subject.text(), maxNameCharacters)
	}
	return finishFeedItem(item)
}

// rssAuthorName reads the "address (Name)" form RSS uses and keeps only the name when one is given.
func rssAuthorName(value string) string {
	value = strings.TrimSpace(value)
	if address, err := mail.ParseAddress(value); err == nil {
		if address.Name != "" {
			return address.Name
		}
		return address.Address
	}
	if open, close := strings.IndexByte(value, '('), strings.LastIndexByte(value, ')'); open > 0 && close > open {
		return value[open+1 : close]
	}
	return value
}

func parseRDF(root *xmlNode, documentURL *url.URL, options feedParseOptions) Feed {
	namespace := namespaceRSS1
	channel := root.firstChild(namespace, "channel")
	if channel == nil {
		namespace = namespaceRSS090
		channel = root.firstChild(namespace, "channel")
	}
	if channel == nil {
		channel = &xmlNode{}
	}
	base := root.baseURL(documentURL)
	feed := Feed{
		Format:      FeedFormatRDF,
		Title:       plainTextFromEscapedTitle(channel.childText(namespace, "title")),
		SiteURL:     resolveLink(base, channel.childText(namespace, "link")),
		Description: boundedText(plainTextFromHTML(channel.childText(namespace, "description")), maxDescriptionCharacters),
		UpdatedAt:   optionalTimestamp(channel.childText(namespaceDublin, "date")),
	}
	itemNodes := root.childrenNamed(namespace, "item")
	items := make([]FeedItem, 0, min(len(itemNodes), options.limit))
	for index := range itemNodes {
		if index >= options.limit {
			break
		}
		node := itemNodes[index]
		item := FeedItem{
			ID:          node.attribute(namespaceRDF, "about"),
			Title:       plainTextFromEscapedTitle(node.childText(namespace, "title")),
			Link:        resolveLink(base, node.childText(namespace, "link")),
			PublishedAt: optionalTimestamp(node.childText(namespaceDublin, "date")),
		}
		summary := firstNonEmpty(node.childText(namespace, "description"), node.childText(namespaceContent, "encoded"))
		item.Summary = truncateCharacters(plainTextFromHTML(summary), options.maxSummaryCharacters)
		for _, creator := range node.childrenNamed(namespaceDublin, "creator") {
			item.Authors = appendBoundedValue(item.Authors, creator.text(), maxNameCharacters)
		}
		for _, subject := range node.childrenNamed(namespaceDublin, "subject") {
			item.Categories = appendBoundedValue(item.Categories, subject.text(), maxNameCharacters)
		}
		items = append(items, finishFeedItem(item))
	}
	setFeedItems(&feed, items, len(itemNodes), options.limit)
	return feed
}

func parseAtom(root *xmlNode, documentURL *url.URL, options feedParseOptions) Feed {
	namespace := root.name.Space
	base := root.baseURL(documentURL)
	feed := Feed{
		Format:      FeedFormatAtom,
		Title:       boundedText(atomText(root.firstChild(namespace, "title")), maxTitleCharacters),
		SiteURL:     atomAlternateLink(root, namespace, base),
		Description: boundedText(atomText(firstNonNilNode(root.firstChild(namespace, "subtitle"), root.firstChild(namespace, "tagline"))), maxDescriptionCharacters),
		UpdatedAt:   optionalTimestamp(firstNonEmpty(root.childText(namespace, "updated"), root.childText(namespace, "modified"))),
	}
	feedAuthors := atomAuthors(root, namespace)
	entryNodes := root.childrenNamed(namespace, "entry")
	items := make([]FeedItem, 0, min(len(entryNodes), options.limit))
	for index := range entryNodes {
		if index >= options.limit {
			break
		}
		node := entryNodes[index]
		entryBase := node.baseURL(base)
		item := FeedItem{
			ID:          strings.TrimSpace(node.childText(namespace, "id")),
			Title:       atomText(node.firstChild(namespace, "title")),
			Link:        atomAlternateLink(node, namespace, entryBase),
			PublishedAt: optionalTimestamp(firstNonEmpty(node.childText(namespace, "published"), node.childText(namespace, "issued"), node.childText(namespace, "created"))),
			UpdatedAt:   optionalTimestamp(firstNonEmpty(node.childText(namespace, "updated"), node.childText(namespace, "modified"))),
			Authors:     atomAuthors(node, namespace),
		}
		summary := atomText(node.firstChild(namespace, "summary"))
		if summary == "" {
			summary = atomText(node.firstChild(namespace, "content"))
		}
		item.Summary = truncateCharacters(summary, options.maxSummaryCharacters)
		if len(item.Authors) == 0 {
			item.Authors = feedAuthors
		}
		for _, category := range node.childrenNamed(namespace, "category") {
			item.Categories = appendBoundedValue(item.Categories, firstNonEmpty(category.attribute("", "term"), category.attribute("", "label")), maxNameCharacters)
		}
		items = append(items, finishFeedItem(item))
	}
	setFeedItems(&feed, items, len(entryNodes), options.limit)
	return feed
}

// atomText reads an Atom text construct by its type: text as is, html with markup removed, and xhtml
// from the text of its child elements.
func atomText(node *xmlNode) string {
	if node == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(node.attribute("", "type"))) {
	case "html", "text/html", "escaped":
		return plainTextFromHTML(node.text())
	case "xhtml", "application/xhtml+xml":
		var text strings.Builder
		node.writeAllText(&text)
		return collapseWhitespace(text.String())
	}
	if node.attribute("", "mode") == "escaped" {
		return plainTextFromHTML(node.text())
	}
	return collapseWhitespace(node.text())
}

// atomAlternateLink picks the entry's page: rel alternate or no rel, preferring an HTML type.
func atomAlternateLink(node *xmlNode, namespace string, base *url.URL) string {
	var fallback string
	for _, link := range node.childrenNamed(namespace, "link") {
		rel := strings.TrimSpace(link.attribute("", "rel"))
		if rel != "" && rel != "alternate" {
			continue
		}
		href := resolveLink(link.baseURL(base), link.attribute("", "href"))
		if href == "" {
			continue
		}
		mediaType := strings.ToLower(link.attribute("", "type"))
		if mediaType == "" || mediaType == "text/html" || mediaType == "application/xhtml+xml" {
			return href
		}
		if fallback == "" {
			fallback = href
		}
	}
	return fallback
}

func atomAuthors(node *xmlNode, namespace string) []string {
	var authors []string
	for _, author := range node.childrenNamed(namespace, "author") {
		authors = appendBoundedValue(authors, firstNonEmpty(author.childText(namespace, "name"), author.childText(namespace, "email")), maxNameCharacters)
	}
	return authors
}

func (node *xmlNode) firstChild(namespace, localName string) *xmlNode {
	for index := range node.children {
		child := &node.children[index]
		if child.name.Local == localName && child.name.Space == namespace {
			return child
		}
	}
	return nil
}

func (node *xmlNode) childrenNamed(namespace, localName string) []*xmlNode {
	var matches []*xmlNode
	for index := range node.children {
		child := &node.children[index]
		if child.name.Local == localName && child.name.Space == namespace {
			matches = append(matches, child)
		}
	}
	return matches
}

func (node *xmlNode) childText(namespace, localName string) string {
	if child := node.firstChild(namespace, localName); child != nil {
		return strings.TrimSpace(child.text())
	}
	return ""
}

// text returns the node's own character data, without that of its children.
func (node *xmlNode) text() string {
	var text strings.Builder
	for _, segment := range node.segments {
		if segment.childIndex < 0 {
			text.WriteString(segment.text)
		}
	}
	return text.String()
}

func (node *xmlNode) attribute(namespace, localName string) string {
	for _, attribute := range node.attributes {
		if attribute.Name.Local == localName && attribute.Name.Space == namespace {
			return attribute.Value
		}
	}
	return ""
}

// baseURL applies the node's xml:base to the inherited base URL.
func (node *xmlNode) baseURL(inherited *url.URL) *url.URL {
	value := strings.TrimSpace(node.attribute(namespaceXML, "base"))
	if value == "" {
		return inherited
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return inherited
	}
	if inherited != nil {
		return inherited.ResolveReference(parsed)
	}
	return parsed
}

// writeAllText writes the character data of the node and its descendants in document order.
func (node *xmlNode) writeAllText(text *strings.Builder) {
	for _, segment := range node.segments {
		if segment.childIndex < 0 {
			text.WriteString(segment.text)
			continue
		}
		text.WriteByte(' ')
		node.children[segment.childIndex].writeAllText(text)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstNonNilNode(nodes ...*xmlNode) *xmlNode {
	for _, node := range nodes {
		if node != nil {
			return node
		}
	}
	return nil
}
