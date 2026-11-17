// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	defaultPageTextCharacters = 20000
	maxPageTextCharacters     = 100000
	readablePageAcceptHeader  = "text/html, application/xhtml+xml;q=0.9, text/plain;q=0.8, */*;q=0.1"
	plainTextMediaType        = "text/plain"
)

// ReadPageInput selects one article page and, optionally, the validators of a previous read of it.
type ReadPageInput struct {
	// URL is the absolute http or https URL of one article or blog post page, such as a post URL from
	// readFeed or readSitemap. An http URL is requested over https at the same host; a fragment is ignored.
	URL string `json:"url"`
	// ETag is the etag a previous read of this page returned. The connector sends it as If-None-Match.
	// Empty sends no If-None-Match header.
	ETag string `json:"etag,omitempty"`
	// LastModified is the lastModified a previous read of this page returned, an HTTP date. The connector
	// sends it as If-Modified-Since. Empty sends no If-Modified-Since header.
	LastModified string `json:"lastModified,omitempty"`
	// MaxTextCharacters bounds Text in Unicode code points, from 1 through 100000. Zero selects 20000.
	MaxTextCharacters int `json:"maxTextCharacters,omitempty"`
}

// Page is the readable text and head metadata of one page, or only the refreshed validators when the
// branch is notModified. Text fields are untrusted provider text with HTML removed; escape them before
// rendering as HTML.
type Page struct {
	// FinalURL is the https URL that served the page after redirects.
	FinalURL string `json:"finalUrl"`
	// StatusCode is the HTTP status of the final response: a 2xx status, or 304 for notModified.
	StatusCode int `json:"statusCode"`
	// ContentType is the media type the page was read as: text/html, application/xhtml+xml, or text/plain.
	// A response without a Content-Type header is read as text/html. It is empty for notModified.
	ContentType string `json:"contentType,omitempty"`
	// Title is the page's title element as plain text, else its og:title, at most 500 code points.
	Title string `json:"title,omitempty"`
	// Description is the meta name=description content, else og:description, at most 1000 code points.
	Description string `json:"description,omitempty"`
	// SiteName is the og:site_name property, at most 200 code points.
	SiteName string `json:"siteName,omitempty"`
	// CanonicalURL is the absolute link rel=canonical URL.
	CanonicalURL string `json:"canonicalUrl,omitempty"`
	// PublishedAt is the article:published_time property in UTC, else the JSON-LD article datePublished.
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	// Text is the page's readable text with paragraphs separated by one blank line and a line break inside
	// a paragraph kept as one newline. HTML text comes from the article element with the most text, else
	// the main element with the most text, else the whole page, without script, style, noscript, template,
	// svg, math, iframe, title, nav, header, footer, aside, button, select, and textarea content; other
	// whitespace runs collapse to one space. A plain-text page is returned with only its surrounding
	// whitespace trimmed. Text is cut to maxTextCharacters, preferring a word boundary, and a cut text ends
	// with an ellipsis that counts toward the bound.
	Text string `json:"text"`
	// IsTextTruncated reports that Text was cut to maxTextCharacters.
	IsTextTruncated bool `json:"isTextTruncated"`
	// ETag is the response's ETag header. Pass it as the next read's etag. Empty when the site sent none.
	ETag string `json:"etag,omitempty"`
	// LastModified is the response's Last-Modified header. Pass it as the next read's lastModified. Empty
	// when the site sent none.
	LastModified string `json:"lastModified,omitempty"`
	// FetchedAt is the UTC time the Worker read the page.
	FetchedAt time.Time `json:"fetchedAt"`
}

var readPageBranches = operationBranches{
	operationID: "readPage", notFound: ReadPageBranchNotFound, providerRejected: ReadPageBranchProviderRejected,
	unreachable: ReadPageBranchUnreachable, invalidResponse: ReadPageBranchInvalidResponse, defect: ReadPageBranchDefect,
}

// Definition returns the manifest-generated readPage contract.
func (ReadPageOperation) Definition() sdkgo.QueryDefinition { return ReadPageDefinition }

// Invoke reads one whole page, at most maxDocumentBytes, with one GET plus at most five redirect hops
// and returns its readable text. It never runs scripts or requests linked resources. A 304 answer to a
// conditional GET selects notModified; transient failures return Retry.
func (operation ReadPageOperation) Invoke(call sdkgo.Call, input ReadPageInput) sdkgo.QueryAttempt[Page] {
	target, err := parseRequestURL(input.URL)
	if err != nil {
		return attemptFromFailure[Page](readPageBranches, defectFailure(err))
	}
	previous, err := validatorsFromInput(input.ETag, input.LastModified)
	if err != nil {
		return attemptFromFailure[Page](readPageBranches, defectFailure(err))
	}
	maxTextCharacters := input.MaxTextCharacters
	if maxTextCharacters == 0 {
		maxTextCharacters = defaultPageTextCharacters
	}
	if maxTextCharacters < 1 || maxTextCharacters > maxPageTextCharacters {
		return failedBranch[Page]("readPage", ReadPageBranchDefect, sdkgo.FailureValidation, "maxTextCharacters must be from 1 through 100000")
	}
	client := operation.client
	document, failure := client.fetchDocument(call.Context, documentRequest{
		target: target, accept: readablePageAcceptHeader, validators: previous, maxBytes: client.maxDocumentBytes,
		acceptsMediaType: isReadablePageMediaType, expectedContent: "an HTML or plain-text page",
	})
	if failure != nil {
		return attemptFromFailure[Page](readPageBranches, failure)
	}
	page := Page{
		FinalURL: document.finalURL.String(), StatusCode: document.statusCode, ETag: document.validators.etag,
		LastModified: document.validators.lastModified, FetchedAt: client.clock().UTC(),
	}
	if document.isNotModified {
		return sdkgo.NewQueryBranch(ReadPageBranchNotModified, page, nil, sdkgo.Receipt{})
	}
	var text string
	if document.mediaType == plainTextMediaType {
		page.ContentType = plainTextMediaType
		text = strings.TrimSpace(decodePageText(document.body, document.plainTextContentType()))
	} else {
		page.ContentType = document.mediaType
		if page.ContentType == "" {
			page.ContentType = "text/html"
		}
		text = extractReadableText(document.body, document.htmlContentType())
		page.setHeadMetadata(scanHTMLHead(document.body, document.htmlContentType()), document)
	}
	page.Text, page.IsTextTruncated = truncateText(text, maxTextCharacters)
	return sdkgo.NewQueryBranch(ReadPageBranchRead, page, nil, sdkgo.Receipt{})
}

// setHeadMetadata copies the title, description, site name, canonical URL, and publication time.
func (page *Page) setHeadMetadata(head htmlDocumentHead, document fetchedDocument) {
	meta := head.metaByName
	page.Title = firstNonEmpty(head.title, boundedText(meta["og:title"], maxTitleCharacters))
	page.Description = boundedText(firstNonEmpty(meta["description"], meta["og:description"]), maxDescriptionCharacters)
	page.SiteName = boundedText(meta["og:site_name"], maxNameCharacters)
	page.CanonicalURL = head.canonicalURL(head.baseURL(document.finalURL))
	page.PublishedAt = firstTimestamp(optionalTimestamp(meta["article:published_time"]), readStructuredData(head.structuredScripts).DatePublished)
}

// isReadablePageMediaType accepts HTML, XHTML, and plain text, and reads a missing media type as HTML.
func isReadablePageMediaType(mediaType string) bool {
	return isHTMLMediaType(mediaType) || mediaType == plainTextMediaType
}
