// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	maxStructuredDataNodes = 500
	maxStructuredDataDepth = 8
)

// articleTypes are the schema.org types whose headline and dates describe the page's article.
var articleTypes = map[string]bool{
	"Article": true, "BlogPosting": true, "NewsArticle": true, "TechArticle": true, "ScholarlyArticle": true,
	"Report": true, "LiveBlogPosting": true, "SocialMediaPosting": true, "DiscussionForumPosting": true,
	"AnalysisNewsArticle": true, "OpinionNewsArticle": true, "ReportageNewsArticle": true,
	"ReviewNewsArticle": true, "BackgroundNewsArticle": true, "Posting": true,
}

// ReadPageMetadataInput selects the article page whose head metadata is read.
type ReadPageMetadataInput struct {
	// URL is the absolute http or https URL of one HTML page, such as a post URL from readSitemap. An http
	// URL is requested over https at the same host.
	URL string `json:"url"`
}

// PageMetadata is the head metadata of one page. Every field the page does not declare is empty, and
// text fields are untrusted provider text with HTML removed.
type PageMetadata struct {
	// FinalURL is the https URL that served the page after redirects.
	FinalURL string `json:"finalUrl"`
	// Title is the page's title element as plain text, at most 500 code points.
	Title string `json:"title,omitempty"`
	// Description is the meta name=description content, at most 1000 code points.
	Description string `json:"description,omitempty"`
	// CanonicalURL is the absolute link rel=canonical URL.
	CanonicalURL string `json:"canonicalUrl,omitempty"`
	// OpenGraph holds the page's og: properties.
	OpenGraph OpenGraphMetadata `json:"openGraph"`
	// ArticlePublishedAt is the article:published_time property in UTC.
	ArticlePublishedAt *time.Time `json:"articlePublishedAt,omitempty"`
	// ArticleModifiedAt is the article:modified_time property in UTC.
	ArticleModifiedAt *time.Time `json:"articleModifiedAt,omitempty"`
	// StructuredData holds the headline and dates of the page's first JSON-LD article, or of its first
	// JSON-LD node with a headline or date when it declares no article type.
	StructuredData StructuredDataMetadata `json:"structuredData"`
	// PublishedAt is the best publication time: ArticlePublishedAt, else StructuredData.DatePublished.
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
	// ModifiedAt is the best modification time: ArticleModifiedAt, else StructuredData.DateModified, else
	// the og:updated_time property.
	ModifiedAt *time.Time `json:"modifiedAt,omitempty"`
	// IsTruncated reports that the page was longer than maxPageBytes and only its prefix was read.
	IsTruncated bool `json:"isTruncated"`
	// FetchedAt is the UTC time the Worker read the page.
	FetchedAt time.Time `json:"fetchedAt"`
}

// OpenGraphMetadata holds the Open Graph properties of one page.
type OpenGraphMetadata struct {
	// Title is og:title.
	Title string `json:"title,omitempty"`
	// Description is og:description.
	Description string `json:"description,omitempty"`
	// ImageURL is the absolute og:image URL, or og:image:secure_url or og:image:url when og:image is missing.
	ImageURL string `json:"imageUrl,omitempty"`
	// Type is og:type, such as article or website.
	Type string `json:"type,omitempty"`
	// SiteName is og:site_name.
	SiteName string `json:"siteName,omitempty"`
}

// StructuredDataMetadata holds the fields of one JSON-LD node.
type StructuredDataMetadata struct {
	// Type is the node's first @type, such as BlogPosting.
	Type string `json:"type,omitempty"`
	// Headline is the node's headline, or its name when it has no headline.
	Headline string `json:"headline,omitempty"`
	// DatePublished is the node's datePublished in UTC.
	DatePublished *time.Time `json:"datePublished,omitempty"`
	// DateModified is the node's dateModified in UTC.
	DateModified *time.Time `json:"dateModified,omitempty"`
}

var readPageMetadataBranches = operationBranches{
	operationID: "readPageMetadata", notFound: ReadPageMetadataBranchNotFound, providerRejected: ReadPageMetadataBranchProviderRejected,
	unreachable: ReadPageMetadataBranchUnreachable, invalidResponse: ReadPageMetadataBranchInvalidResponse, defect: ReadPageMetadataBranchDefect,
}

// Definition returns the manifest-generated readPageMetadata contract.
func (ReadPageMetadataOperation) Definition() sdkgo.QueryDefinition {
	return ReadPageMetadataDefinition
}

// Invoke reads the first maxPageBytes of one HTML page with one GET plus at most five redirect hops and
// returns its head metadata. It never runs scripts or requests linked resources.
func (operation ReadPageMetadataOperation) Invoke(call sdkgo.Call, input ReadPageMetadataInput) sdkgo.QueryAttempt[PageMetadata] {
	target, err := parseRequestURL(input.URL)
	if err != nil {
		return attemptFromFailure[PageMetadata](readPageMetadataBranches, defectFailure(err))
	}
	client := operation.client
	document, failure := client.fetchDocument(call.Context, documentRequest{
		target: target, accept: pageAcceptHeader, maxBytes: client.maxPageBytes, isPrefixRead: true,
		acceptsMediaType: isHTMLMediaType, expectedContent: "an HTML page",
	})
	if failure != nil {
		return attemptFromFailure[PageMetadata](readPageMetadataBranches, failure)
	}
	head := scanHTMLHead(document.body, document.htmlContentType())
	base := head.baseURL(document.finalURL)
	meta := head.metaByName
	metadata := PageMetadata{
		FinalURL:    document.finalURL.String(),
		Title:       head.title,
		Description: boundedText(meta["description"], maxDescriptionCharacters),
		OpenGraph: OpenGraphMetadata{
			Title:       boundedText(meta["og:title"], maxTitleCharacters),
			Description: boundedText(meta["og:description"], maxDescriptionCharacters),
			ImageURL:    resolveLink(base, firstNonEmpty(meta["og:image"], meta["og:image:secure_url"], meta["og:image:url"])),
			Type:        boundedText(meta["og:type"], maxNameCharacters),
			SiteName:    boundedText(meta["og:site_name"], maxNameCharacters),
		},
		ArticlePublishedAt: optionalTimestamp(meta["article:published_time"]),
		ArticleModifiedAt:  optionalTimestamp(meta["article:modified_time"]),
		StructuredData:     readStructuredData(head.structuredScripts),
		IsTruncated:        document.isTruncated,
		FetchedAt:          client.clock().UTC(),
	}
	for _, link := range head.links {
		if hasRelation(link.relations, "canonical") {
			if metadata.CanonicalURL = resolveLink(base, link.href); metadata.CanonicalURL != "" {
				break
			}
		}
	}
	metadata.PublishedAt = firstTimestamp(metadata.ArticlePublishedAt, metadata.StructuredData.DatePublished)
	metadata.ModifiedAt = firstTimestamp(metadata.ArticleModifiedAt, metadata.StructuredData.DateModified, optionalTimestamp(meta["og:updated_time"]))
	return sdkgo.NewQueryBranch(ReadPageMetadataBranchRead, metadata, nil, sdkgo.Receipt{})
}

// readStructuredData returns the first JSON-LD article node, else the first node with a headline or date.
// Scripts that are not valid JSON are skipped.
func readStructuredData(scripts []string) StructuredDataMetadata {
	var fallback *StructuredDataMetadata
	visited := 0
	for _, script := range scripts {
		var document any
		if err := json.Unmarshal([]byte(strings.TrimSpace(script)), &document); err != nil {
			continue
		}
		var found *StructuredDataMetadata
		found, fallback = searchStructuredData(document, 0, &visited, fallback)
		if found != nil {
			return *found
		}
	}
	if fallback != nil {
		return *fallback
	}
	return StructuredDataMetadata{}
}

// searchStructuredData walks objects, arrays, and @graph lists within the depth and node bounds.
func searchStructuredData(value any, depth int, visited *int, fallback *StructuredDataMetadata) (*StructuredDataMetadata, *StructuredDataMetadata) {
	if depth > maxStructuredDataDepth || *visited >= maxStructuredDataNodes {
		return nil, fallback
	}
	*visited++
	switch typed := value.(type) {
	case []any:
		for _, element := range typed {
			var found *StructuredDataMetadata
			if found, fallback = searchStructuredData(element, depth+1, visited, fallback); found != nil {
				return found, fallback
			}
		}
	case map[string]any:
		candidate := StructuredDataMetadata{
			Type:          firstJSONString(typed["@type"]),
			Headline:      boundedText(firstNonEmpty(firstJSONString(typed["headline"]), firstJSONString(typed["name"])), maxTitleCharacters),
			DatePublished: optionalTimestamp(firstJSONString(typed["datePublished"])),
			DateModified:  optionalTimestamp(firstJSONString(typed["dateModified"])),
		}
		if isArticleType(typed["@type"]) {
			return &candidate, fallback
		}
		if fallback == nil && (candidate.DatePublished != nil || firstJSONString(typed["headline"]) != "") {
			fallback = &candidate
		}
		for _, key := range []string{"@graph", "mainEntity", "mainEntityOfPage"} {
			if nested, exists := typed[key]; exists {
				var found *StructuredDataMetadata
				if found, fallback = searchStructuredData(nested, depth+1, visited, fallback); found != nil {
					return found, fallback
				}
			}
		}
	}
	return nil, fallback
}

func isArticleType(value any) bool {
	switch typed := value.(type) {
	case string:
		return articleTypes[strings.TrimPrefix(strings.TrimPrefix(typed, "https://schema.org/"), "http://schema.org/")]
	case []any:
		for _, element := range typed {
			if isArticleType(element) {
				return true
			}
		}
	}
	return false
}

// firstJSONString reads a JSON-LD value that is a string, a list starting with a string, or a value object.
func firstJSONString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		for _, element := range typed {
			if text := firstJSONString(element); text != "" {
				return text
			}
		}
	case map[string]any:
		return firstJSONString(typed["@value"])
	}
	return ""
}

func firstTimestamp(values ...*time.Time) *time.Time {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
