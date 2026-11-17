// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"crypto/sha256"
	"encoding/hex"
	"html"
	"net/url"
	"strings"
	"unicode/utf8"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	maxTitleCharacters       = 500
	maxDescriptionCharacters = 1000
	maxNameCharacters        = 200
	maxListValues            = 20
	maxIdentifierBytes       = 2048
)

// collapseWhitespace joins the whitespace-separated words of value with single spaces.
func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

// boundedText collapses whitespace and cuts the result to at most maxCharacters Unicode code points.
func boundedText(value string, maxCharacters int) string {
	return truncateCharacters(collapseWhitespace(value), maxCharacters)
}

// truncateCharacters cuts value to at most maxCharacters code points, preferring a word boundary and
// ending a cut value with an ellipsis that counts toward the bound.
func truncateCharacters(value string, maxCharacters int) string {
	if maxCharacters <= 0 || utf8.RuneCountInString(value) <= maxCharacters {
		return value
	}
	runes := []rune(value)[:maxCharacters-1]
	cut := string(runes)
	if lastSpace := strings.LastIndexByte(cut, ' '); lastSpace > len(cut)/2 {
		cut = cut[:lastSpace]
	}
	return strings.TrimRight(cut, " ") + "…"
}

// plainTextFromHTML returns the visible text of an HTML fragment with entities decoded, block boundaries
// as spaces, and script and style content dropped.
func plainTextFromHTML(fragment string) string {
	if !strings.ContainsAny(fragment, "<&") {
		return collapseWhitespace(fragment)
	}
	tokenizer := nethtml.NewTokenizer(strings.NewReader(fragment))
	var text strings.Builder
	skippedDepth := 0
	for {
		switch tokenizer.Next() {
		case nethtml.ErrorToken:
			return collapseWhitespace(text.String())
		case nethtml.TextToken:
			if skippedDepth == 0 {
				text.Write(tokenizer.Text())
			}
		case nethtml.StartTagToken:
			name, _ := tokenizer.TagName()
			switch tag := atom.Lookup(name); {
			case isSkippedElement(tag):
				skippedDepth++
			case isBlockElement(tag):
				text.WriteByte(' ')
			}
		case nethtml.EndTagToken:
			name, _ := tokenizer.TagName()
			switch tag := atom.Lookup(name); {
			case isSkippedElement(tag):
				skippedDepth = max(skippedDepth-1, 0)
			case isBlockElement(tag):
				text.WriteByte(' ')
			}
		case nethtml.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			if isBlockElement(atom.Lookup(name)) {
				text.WriteByte(' ')
			}
		}
	}
}

func isSkippedElement(tag atom.Atom) bool {
	switch tag {
	case atom.Script, atom.Style, atom.Noscript, atom.Template, atom.Svg:
		return true
	}
	return false
}

// isBlockElement reports elements whose boundaries separate words; inline elements join their text.
func isBlockElement(tag atom.Atom) bool {
	switch tag {
	case atom.Address, atom.Article, atom.Aside, atom.Blockquote, atom.Br, atom.Dd, atom.Details, atom.Div,
		atom.Dl, atom.Dt, atom.Figcaption, atom.Figure, atom.Footer, atom.H1, atom.H2, atom.H3, atom.H4,
		atom.H5, atom.H6, atom.Header, atom.Hr, atom.Img, atom.Li, atom.Main, atom.Nav, atom.Ol, atom.P,
		atom.Pre, atom.Section, atom.Summary, atom.Table, atom.Td, atom.Th, atom.Tr, atom.Ul:
		return true
	}
	return false
}

// plainTextFromEscapedTitle decodes HTML entities that feeds often double-escape in plain-text titles.
func plainTextFromEscapedTitle(title string) string {
	return boundedText(html.UnescapeString(title), maxTitleCharacters)
}

// resolveLink resolves href against base and returns it only when it is an absolute http or https URL
// within the URL bound.
func resolveLink(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base != nil {
		parsed = base.ResolveReference(parsed)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	resolved := parsed.String()
	if len(resolved) > maxURLBytes {
		return ""
	}
	return resolved
}

// boundedIdentifier keeps an identifier within the bound by replacing a longer one with its digest.
func boundedIdentifier(identifier string) string {
	identifier = strings.TrimSpace(identifier)
	if len(identifier) <= maxIdentifierBytes {
		return identifier
	}
	return digestIdentifier(identifier)
}

func digestIdentifier(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// appendBoundedValue appends a trimmed, bounded, nonempty value that values does not already contain.
func appendBoundedValue(values []string, value string, maxCharacters int) []string {
	value = boundedText(value, maxCharacters)
	if value == "" || len(values) >= maxListValues {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
