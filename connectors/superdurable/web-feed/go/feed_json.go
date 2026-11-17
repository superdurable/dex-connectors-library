// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
)

// jsonFeedDocument holds the JSON Feed 1.0 and 1.1 fields the connector reads.
type jsonFeedDocument struct {
	Version     string           `json:"version"`
	Title       string           `json:"title"`
	HomePageURL string           `json:"home_page_url"`
	Description string           `json:"description"`
	Author      *jsonFeedAuthor  `json:"author"`
	Authors     []jsonFeedAuthor `json:"authors"`
	Items       []jsonFeedItem   `json:"items"`
}

type jsonFeedItem struct {
	// ID is a string in the specification; some generators write a number.
	ID            json.RawMessage  `json:"id"`
	URL           string           `json:"url"`
	ExternalURL   string           `json:"external_url"`
	Title         string           `json:"title"`
	ContentHTML   string           `json:"content_html"`
	ContentText   string           `json:"content_text"`
	Summary       string           `json:"summary"`
	DatePublished string           `json:"date_published"`
	DateModified  string           `json:"date_modified"`
	Author        *jsonFeedAuthor  `json:"author"`
	Authors       []jsonFeedAuthor `json:"authors"`
	Tags          []string         `json:"tags"`
}

type jsonFeedAuthor struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// parseJSONFeed reads a JSON Feed whose version names https://jsonfeed.org/version/1 or a 1.x revision.
func parseJSONFeed(body []byte, options feedParseOptions) (Feed, *fetchFailure) {
	var document jsonFeedDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return Feed{}, invalidResponse("the JSON document is not a valid JSON Feed")
	}
	version := strings.TrimPrefix(strings.TrimPrefix(document.Version, "https://"), "http://")
	if !strings.HasPrefix(version, "jsonfeed.org/version/1") {
		return Feed{}, invalidResponse("the JSON document does not declare a JSON Feed 1.x version")
	}
	base, err := url.Parse(options.documentURL)
	if err != nil {
		return Feed{}, invalidResponse("the feed URL cannot be used as a base URL")
	}
	feed := Feed{
		Format:      FeedFormatJSON,
		Title:       boundedText(document.Title, maxTitleCharacters),
		SiteURL:     resolveLink(base, document.HomePageURL),
		Description: boundedText(document.Description, maxDescriptionCharacters),
	}
	feedAuthors := jsonFeedAuthorNames(document.Author, document.Authors)
	items := make([]FeedItem, 0, min(len(document.Items), options.limit))
	for index := range document.Items {
		if index >= options.limit {
			break
		}
		source := &document.Items[index]
		item := FeedItem{
			ID:          jsonFeedIdentifier(source.ID),
			Title:       source.Title,
			Link:        firstNonEmpty(resolveLink(base, source.URL), resolveLink(base, source.ExternalURL)),
			PublishedAt: optionalTimestamp(source.DatePublished),
			UpdatedAt:   optionalTimestamp(source.DateModified),
			Authors:     jsonFeedAuthorNames(source.Author, source.Authors),
		}
		summary := collapseWhitespace(firstNonEmpty(source.Summary, source.ContentText))
		if summary == "" {
			summary = plainTextFromHTML(source.ContentHTML)
		}
		item.Summary = truncateCharacters(summary, options.maxSummaryCharacters)
		if len(item.Authors) == 0 {
			item.Authors = feedAuthors
		}
		for _, tag := range source.Tags {
			item.Categories = appendBoundedValue(item.Categories, tag, maxNameCharacters)
		}
		items = append(items, finishFeedItem(item))
	}
	setFeedItems(&feed, items, len(document.Items), options.limit)
	return feed, nil
}

func jsonFeedIdentifier(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String()
	}
	return ""
}

// jsonFeedAuthorNames reads JSON Feed 1.1 authors, falling back to the 1.0 author object.
func jsonFeedAuthorNames(author *jsonFeedAuthor, authors []jsonFeedAuthor) []string {
	if len(authors) == 0 && author != nil {
		authors = []jsonFeedAuthor{*author}
	}
	var names []string
	for _, each := range authors {
		names = appendBoundedValue(names, firstNonEmpty(each.Name, each.URL), maxNameCharacters)
	}
	return names
}
