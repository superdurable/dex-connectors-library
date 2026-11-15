// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"bytes"
	"io"
	"net/url"
	"strings"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/net/html/charset"
)

const (
	pageAcceptHeader     = "text/html, application/xhtml+xml;q=0.9, */*;q=0.1"
	maxHTMLLinkElements  = 200
	maxHTMLMetaElements  = 400
	maxStructuredScripts = 20
	maxStructuredBytes   = 256 << 10
)

// htmlLinkElement is one link element of a page.
type htmlLinkElement struct {
	relations []string
	mediaType string
	href      string
	title     string
}

// htmlDocumentHead is what one tokenizer pass collects from the bounded page prefix.
type htmlDocumentHead struct {
	baseHref          string
	title             string
	links             []htmlLinkElement
	metaByName        map[string]string
	structuredScripts []string
}

// scanHTMLHead tokenizes the page prefix once; the first value of each element wins, even in the body.
func scanHTMLHead(body []byte, contentType string) htmlDocumentHead {
	head := htmlDocumentHead{metaByName: map[string]string{}}
	var reader io.Reader = bytes.NewReader(body)
	if converted, err := charset.NewReader(reader, contentType); err == nil {
		reader = converted
	}
	tokenizer := nethtml.NewTokenizer(reader)
	foreignDepth := 0
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case nethtml.ErrorToken:
			return head
		case nethtml.EndTagToken:
			name, _ := tokenizer.TagName()
			if tag := atom.Lookup(name); (tag == atom.Svg || tag == atom.Math) && foreignDepth > 0 {
				foreignDepth--
			}
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			token := tokenizer.Token()
			switch token.DataAtom {
			case atom.Svg, atom.Math:
				if tokenType == nethtml.StartTagToken {
					foreignDepth++
				}
			case atom.Base:
				if head.baseHref == "" {
					head.baseHref = attributeValue(token, "href")
				}
			case atom.Title:
				if tokenType == nethtml.StartTagToken && foreignDepth == 0 && head.title == "" {
					if tokenizer.Next() == nethtml.TextToken {
						head.title = boundedText(string(tokenizer.Text()), maxTitleCharacters)
					}
				}
			case atom.Link:
				if len(head.links) < maxHTMLLinkElements {
					head.links = append(head.links, htmlLinkElement{
						relations: strings.Fields(strings.ToLower(attributeValue(token, "rel"))),
						mediaType: strings.ToLower(strings.TrimSpace(strings.SplitN(attributeValue(token, "type"), ";", 2)[0])),
						href:      attributeValue(token, "href"),
						title:     attributeValue(token, "title"),
					})
				}
			case atom.Meta:
				head.recordMeta(token)
			case atom.Script:
				if tokenType == nethtml.StartTagToken && isStructuredDataScript(token) && len(head.structuredScripts) < maxStructuredScripts {
					if tokenizer.Next() == nethtml.TextToken {
						if text := tokenizer.Text(); len(text) <= maxStructuredBytes {
							head.structuredScripts = append(head.structuredScripts, string(text))
						}
					}
				}
			}
		}
	}
}

func (head *htmlDocumentHead) recordMeta(token nethtml.Token) {
	if len(head.metaByName) >= maxHTMLMetaElements {
		return
	}
	content := attributeValue(token, "content")
	for _, attributeName := range []string{"property", "name", "itemprop"} {
		key := strings.ToLower(strings.TrimSpace(attributeValue(token, attributeName)))
		if key == "" || content == "" {
			continue
		}
		if _, exists := head.metaByName[key]; !exists {
			head.metaByName[key] = content
		}
	}
}

// baseURL applies the page's base element, when it holds a usable URL, to the URL that served the page.
func (head *htmlDocumentHead) baseURL(pageURL *url.URL) *url.URL {
	if head.baseHref == "" {
		return pageURL
	}
	parsed, err := url.Parse(strings.TrimSpace(head.baseHref))
	if err != nil {
		return pageURL
	}
	resolved := pageURL.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return pageURL
	}
	return resolved
}

func isStructuredDataScript(token nethtml.Token) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(attributeValue(token, "type"), ";", 2)[0]))
	return mediaType == "application/ld+json"
}

func attributeValue(token nethtml.Token, name string) string {
	for _, attribute := range token.Attr {
		if attribute.Namespace == "" && strings.EqualFold(attribute.Key, name) {
			return attribute.Val
		}
	}
	return ""
}

func hasRelation(relations []string, relation string) bool {
	for _, each := range relations {
		if each == relation {
			return true
		}
	}
	return false
}

// isHTMLMediaType accepts HTML pages and a missing media type; another type is not a page.
func isHTMLMediaType(mediaType string) bool {
	return mediaType == "" || mediaType == "text/html" || mediaType == "application/xhtml+xml"
}
