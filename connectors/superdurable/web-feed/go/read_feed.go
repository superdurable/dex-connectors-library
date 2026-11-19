// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"bytes"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	defaultFeedItemLimit        = 50
	maxFeedItemLimit            = 200
	defaultSummaryCharacters    = 500
	maxSummaryCharactersAllowed = 4000
	feedAcceptHeader            = "application/feed+json, application/atom+xml, application/rss+xml, application/rdf+xml;q=0.9, application/xml;q=0.8, text/xml;q=0.8, application/json;q=0.7, */*;q=0.5"
)

// FeedFormat names the syndication format of a feed document. Unknown values may appear in later releases.
type FeedFormat string

const (
	// FeedFormatRSS is an rss root element: RSS 0.91, 0.92, or 2.0.
	FeedFormatRSS FeedFormat = "rss"
	// FeedFormatRDF is an rdf:RDF root element: RSS 1.0 or RSS 0.90.
	FeedFormatRDF FeedFormat = "rdf"
	// FeedFormatAtom is an Atom 1.0 feed root element; Atom 0.3 element names are also read.
	FeedFormatAtom FeedFormat = "atom"
	// FeedFormatJSON is a JSON Feed 1.0 or 1.1 document.
	FeedFormatJSON FeedFormat = "json"
)

// ReadFeedInput selects one feed document and, optionally, the validators of a previous read of it.
type ReadFeedInput struct {
	// URL is the absolute http or https URL of the feed document, such as https://go.dev/blog/feed.atom. An
	// http URL is requested over https at the same host; a fragment is ignored.
	URL string `json:"url"`
	// ETag is the etag a previous read of this feed returned. The connector sends it as If-None-Match.
	// Empty sends no If-None-Match header.
	ETag string `json:"etag,omitempty"`
	// LastModified is the lastModified a previous read of this feed returned, an HTTP date. The connector
	// sends it as If-Modified-Since. Empty sends no If-Modified-Since header.
	LastModified string `json:"lastModified,omitempty"`
	// Limit bounds the returned items, from 1 through 200, in document order. Zero selects 50.
	Limit int `json:"limit,omitempty"`
	// MaxSummaryCharacters bounds each item summary in Unicode code points, from 1 through 4000. Zero
	// selects 500.
	MaxSummaryCharacters int `json:"maxSummaryCharacters,omitempty"`
}

// Feed is one parsed feed document, or only the refreshed validators when the branch is notModified.
// Text fields are untrusted provider text with HTML removed; escape them before rendering as HTML.
type Feed struct {
	// Format is the syndication format of the document. It is empty for notModified.
	Format FeedFormat `json:"format,omitempty"`
	// Title is the feed title as plain text.
	Title string `json:"title,omitempty"`
	// SiteURL is the absolute URL of the site the feed belongs to, when the feed names one.
	SiteURL string `json:"siteUrl,omitempty"`
	// Description is the feed description or subtitle as plain text, at most 1000 code points.
	Description string `json:"description,omitempty"`
	// UpdatedAt is the feed-level update time in UTC: Atom updated, or RSS lastBuildDate or pubDate.
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	// FinalURL is the https URL that served the response after redirects. A poller may store it in place
	// of the requested URL.
	FinalURL string `json:"finalUrl"`
	// ETag is the response's ETag header. Pass it as the next read's etag. Empty when the site sent none.
	ETag string `json:"etag,omitempty"`
	// LastModified is the response's Last-Modified header. Pass it as the next read's lastModified. Empty
	// when the site sent none.
	LastModified string `json:"lastModified,omitempty"`
	// FetchedAt is the UTC time the Worker read the response.
	FetchedAt time.Time `json:"fetchedAt"`
	// Items holds at most the requested limit of entries in document order, which is usually newest first.
	Items []FeedItem `json:"items"`
	// ItemCount is the number of entries in the whole document.
	ItemCount int `json:"itemCount"`
	// HasMore reports that the document held more entries than Items.
	HasMore bool `json:"hasMore"`
}

// FeedItem is one RSS item, Atom entry, or JSON Feed item.
type FeedItem struct {
	// ID is the entry's stable identity: the RSS guid, Atom id, RDF rdf:about, or JSON Feed id; else its
	// link; else a sha256: digest of its title and dates. An identity longer than 2048 bytes is replaced by
	// its sha256: digest.
	ID string `json:"id"`
	// Title is the entry title as plain text, at most 500 code points.
	Title string `json:"title,omitempty"`
	// Link is the absolute http or https URL of the entry's page, resolved against xml:base and the feed URL.
	Link string `json:"link,omitempty"`
	// PublishedAt is the entry's publication time in UTC, when the feed states a readable one.
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	// UpdatedAt is the entry's last update time in UTC, when the feed states a readable one.
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	// Summary is the entry summary, or its content when there is no summary, as plain text within the
	// requested bound. A cut summary ends with an ellipsis.
	Summary string `json:"summary,omitempty"`
	// Authors holds up to 20 author names, each at most 200 code points.
	Authors []string `json:"authors,omitempty"`
	// Categories holds up to 20 distinct categories or tags, each at most 200 code points.
	Categories []string `json:"categories,omitempty"`
}

var readFeedBranches = operationBranches{
	operationID: "readFeed", notFound: ReadFeedBranchNotFound, providerRejected: ReadFeedBranchProviderRejected,
	unreachable: ReadFeedBranchUnreachable, invalidResponse: ReadFeedBranchInvalidResponse, defect: ReadFeedBranchDefect,
}

// feedParseOptions carries the per-call bounds and base URL into the format parsers.
type feedParseOptions struct {
	limit                int
	maxSummaryCharacters int
	documentURL          string
}

// Definition returns the manifest-generated readFeed contract.
func (ReadFeedOperation) Definition() sdkgo.QueryDefinition { return ReadFeedDefinition }

// Invoke reads and parses one feed with one GET plus at most five redirect hops. A 304 answer to a
// conditional GET selects notModified; transient failures return Retry.
func (operation ReadFeedOperation) Invoke(call sdkgo.Call, input ReadFeedInput) sdkgo.QueryAttempt[Feed] {
	target, err := parseRequestURL(input.URL)
	if err != nil {
		return attemptFromFailure[Feed](readFeedBranches, defectFailure(err))
	}
	previous, err := validatorsFromInput(input.ETag, input.LastModified)
	if err != nil {
		return attemptFromFailure[Feed](readFeedBranches, defectFailure(err))
	}
	options := feedParseOptions{limit: input.Limit, maxSummaryCharacters: input.MaxSummaryCharacters}
	if options.limit == 0 {
		options.limit = defaultFeedItemLimit
	}
	if options.maxSummaryCharacters == 0 {
		options.maxSummaryCharacters = defaultSummaryCharacters
	}
	if options.limit < 1 || options.limit > maxFeedItemLimit {
		return failedBranch[Feed]("readFeed", ReadFeedBranchDefect, sdkgo.FailureValidation, "limit must be from 1 through 200")
	}
	if options.maxSummaryCharacters < 1 || options.maxSummaryCharacters > maxSummaryCharactersAllowed {
		return failedBranch[Feed]("readFeed", ReadFeedBranchDefect, sdkgo.FailureValidation, "maxSummaryCharacters must be from 1 through 4000")
	}
	client := operation.client
	document, failure := client.fetchDocument(call.Context, documentRequest{
		target: target, accept: feedAcceptHeader, validators: previous, maxBytes: client.maxDocumentBytes,
		mayBeGzip: true, acceptsMediaType: isFeedMediaType, expectedContent: "a feed",
	})
	if failure != nil {
		return attemptFromFailure[Feed](readFeedBranches, failure)
	}
	feed := Feed{
		FinalURL: document.finalURL.String(), ETag: document.validators.etag,
		LastModified: document.validators.lastModified, FetchedAt: client.clock().UTC(), Items: []FeedItem{},
	}
	if document.isNotModified {
		return sdkgo.NewQueryBranch(ReadFeedBranchNotModified, feed, nil, sdkgo.Receipt{})
	}
	options.documentURL = feed.FinalURL
	parsed, failure := parseFeedDocument(document, options)
	if failure != nil {
		return attemptFromFailure[Feed](readFeedBranches, failure)
	}
	parsed.FinalURL, parsed.ETag, parsed.LastModified, parsed.FetchedAt = feed.FinalURL, feed.ETag, feed.LastModified, feed.FetchedAt
	return sdkgo.NewQueryBranch(ReadFeedBranchRead, parsed, nil, sdkgo.Receipt{})
}

// parseFeedDocument detects JSON Feed by its first byte and parses every other document as XML.
func parseFeedDocument(document fetchedDocument, options feedParseOptions) (Feed, *fetchFailure) {
	body := bytes.TrimLeft(bytes.TrimPrefix(document.body, []byte("\xef\xbb\xbf")), " \t\r\n")
	if len(body) == 0 {
		return Feed{}, invalidResponse("the feed document is empty")
	}
	if body[0] == '{' {
		return parseJSONFeed(body, options)
	}
	return parseXMLFeed(body, document.charsetLabel, options)
}

// setFeedItems stores the parsed items, at most limit, and the number of entries in the whole document.
func setFeedItems(feed *Feed, items []FeedItem, totalItemCount int, limit int) {
	feed.Items = items[:min(limit, len(items))]
	if feed.Items == nil {
		feed.Items = []FeedItem{}
	}
	feed.ItemCount = totalItemCount
	feed.HasMore = totalItemCount > len(feed.Items)
}

// finishFeedItem fills the fallback identity and bounds the free-text fields of one parsed item.
func finishFeedItem(item FeedItem) FeedItem {
	item.ID = boundedIdentifier(item.ID)
	if item.ID == "" {
		item.ID = boundedIdentifier(item.Link)
	}
	if item.ID == "" {
		item.ID = digestIdentifier(item.Title, formatOptionalTimestamp(item.PublishedAt), formatOptionalTimestamp(item.UpdatedAt), item.Summary)
	}
	item.Title = boundedText(item.Title, maxTitleCharacters)
	return item
}

func formatOptionalTimestamp(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

// isFeedMediaType accepts feed, XML, JSON, and text types; mislabeled text/html feeds still parse.
func isFeedMediaType(mediaType string) bool {
	switch {
	case mediaType == "", mediaType == "application/octet-stream", mediaType == "application/gzip", mediaType == "application/x-gzip":
		return true
	case strings.HasPrefix(mediaType, "text/"):
		return mediaType != "text/css" && mediaType != "text/javascript"
	case mediaType == "application/xml", mediaType == "application/json":
		return true
	case strings.HasPrefix(mediaType, "application/"):
		return strings.HasSuffix(mediaType, "+xml") || strings.HasSuffix(mediaType, "+json")
	}
	return false
}
