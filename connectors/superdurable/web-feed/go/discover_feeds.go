// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// maxDiscoveredFeeds bounds the feeds one page can advertise in a result.
const maxDiscoveredFeeds = 20

// DiscoverFeedsInput selects the HTML page whose advertised feeds are listed.
type DiscoverFeedsInput struct {
	// URL is the absolute http or https URL of an HTML page, usually a site's home page or blog index, such
	// as https://go.dev/blog/. An http URL is requested over https at the same host.
	URL string `json:"url"`
}

// DiscoveredFeeds lists the feeds one page advertises. Titles are untrusted provider text.
type DiscoveredFeeds struct {
	// FinalURL is the https URL that served the page after redirects.
	FinalURL string `json:"finalUrl"`
	// PageTitle is the page's title element as plain text.
	PageTitle string `json:"pageTitle,omitempty"`
	// Feeds holds up to 20 advertised feeds in page order without duplicates. Empty means the page
	// advertises none; a sitemap or a known feed path may still exist.
	Feeds []DiscoveredFeed `json:"feeds"`
	// IsTruncated reports that the page was longer than maxPageBytes and only its prefix was searched.
	IsTruncated bool `json:"isTruncated"`
	// FetchedAt is the UTC time the Worker read the page.
	FetchedAt time.Time `json:"fetchedAt"`
}

// DiscoveredFeed is one link rel=alternate element that names a feed media type.
type DiscoveredFeed struct {
	// URL is the absolute feed URL without a fragment, resolved against the page's base element and final
	// URL. Read it with readFeed.
	URL string `json:"url"`
	// Title is the link element's title attribute, when present.
	Title string `json:"title,omitempty"`
	// MediaType is application/rss+xml, application/atom+xml, or application/feed+json.
	MediaType string `json:"mediaType"`
}

var discoverFeedsBranches = operationBranches{
	operationID: "discoverFeeds", notFound: DiscoverFeedsBranchNotFound, providerRejected: DiscoverFeedsBranchProviderRejected,
	unreachable: DiscoverFeedsBranchUnreachable, invalidResponse: DiscoverFeedsBranchInvalidResponse, defect: DiscoverFeedsBranchDefect,
}

// Definition returns the manifest-generated discoverFeeds contract.
func (DiscoverFeedsOperation) Definition() sdkgo.QueryDefinition { return DiscoverFeedsDefinition }

// Invoke reads the first maxPageBytes of one HTML page and lists its rel=alternate feed links. It never
// requests the feeds themselves or guesses feed paths.
func (operation DiscoverFeedsOperation) Invoke(call sdkgo.Call, input DiscoverFeedsInput) sdkgo.QueryAttempt[DiscoveredFeeds] {
	target, err := parseRequestURL(input.URL)
	if err != nil {
		return attemptFromFailure[DiscoveredFeeds](discoverFeedsBranches, defectFailure(err))
	}
	client := operation.client
	document, failure := client.fetchDocument(call.Context, documentRequest{
		target: target, accept: pageAcceptHeader, maxBytes: client.maxPageBytes, isPrefixRead: true,
		acceptsMediaType: isHTMLMediaType, expectedContent: "an HTML page",
	})
	if failure != nil {
		return attemptFromFailure[DiscoveredFeeds](discoverFeedsBranches, failure)
	}
	head := scanHTMLHead(document.body, document.htmlContentType())
	base := head.baseURL(document.finalURL)
	result := DiscoveredFeeds{
		FinalURL: document.finalURL.String(), PageTitle: head.title, Feeds: []DiscoveredFeed{},
		IsTruncated: document.isTruncated, FetchedAt: client.clock().UTC(),
	}
	seen := map[string]bool{}
	for _, link := range head.links {
		if !hasRelation(link.relations, "alternate") || hasRelation(link.relations, "stylesheet") || !isAdvertisedFeedMediaType(link.mediaType) {
			continue
		}
		// A fragment never changes the fetched document, so it neither distinguishes nor follows a feed URL.
		feedURL, _, _ := strings.Cut(resolveLink(base, link.href), "#")
		if feedURL == "" || seen[feedURL] || len(result.Feeds) == maxDiscoveredFeeds {
			continue
		}
		seen[feedURL] = true
		result.Feeds = append(result.Feeds, DiscoveredFeed{URL: feedURL, Title: boundedText(link.title, maxTitleCharacters), MediaType: link.mediaType})
	}
	return sdkgo.NewQueryBranch(DiscoverFeedsBranchDiscovered, result, nil, sdkgo.Receipt{})
}

// isAdvertisedFeedMediaType accepts the feed types sites advertise. application/json is excluded because
// sites also advertise API documents, such as a CMS's REST resource, with it.
func isAdvertisedFeedMediaType(mediaType string) bool {
	switch mediaType {
	case "application/rss+xml", "application/atom+xml", "application/feed+json":
		return true
	}
	return false
}
