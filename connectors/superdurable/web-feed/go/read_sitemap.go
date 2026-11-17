// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	defaultSitemapLimit    = 200
	maxSitemapLimit        = 2000
	maxSitemapPathPrefix   = 512
	maxSitemapScanEntries  = 100000
	sitemapAcceptHeader    = "application/xml, text/xml;q=0.9, application/gzip;q=0.8, */*;q=0.5"
	namespaceGoogleNews    = "http://www.google.com/schemas/sitemap-news/0.9"
	sitemapEntryElement    = "url"
	sitemapIndexEntryName  = "sitemap"
	sitemapURLSetRootName  = "urlset"
	sitemapIndexRootName   = "sitemapindex"
	maxSitemapElementDepth = 8
)

// SitemapKind names the root element of a sitemap document. Unknown values may appear in later releases.
type SitemapKind string

const (
	// SitemapKindURLSet is a urlset that lists pages.
	SitemapKindURLSet SitemapKind = "urlset"
	// SitemapKindIndex is a sitemapindex that lists child sitemaps.
	SitemapKindIndex SitemapKind = "sitemapIndex"
)

// ReadSitemapInput selects one sitemap, the entries to keep, and optionally the validators of a previous read.
type ReadSitemapInput struct {
	// URL is the absolute http or https URL of the sitemap, such as https://www.example.com/sitemap.xml or a
	// .xml.gz file. An http URL is requested over https at the same host.
	URL string `json:"url"`
	// PathPrefix keeps only urlset entries whose URL path starts with it, compared case-sensitively, such as
	// /blog/. It must start with a slash. Empty keeps every path. It does not filter a sitemap index.
	PathPrefix string `json:"pathPrefix,omitempty"`
	// LastModifiedAfter keeps only urlset entries whose lastmod, or Google News publication date when lastmod
	// is missing, is strictly after it. Entries with neither never match. Nil keeps entries of any date. It
	// does not filter a sitemap index.
	LastModifiedAfter *time.Time `json:"lastModifiedAfter,omitempty"`
	// Limit bounds the returned entries or child sitemaps, from 1 through 2000. Zero selects 200.
	Limit int `json:"limit,omitempty"`
	// ETag is the etag a previous read of this sitemap returned. The connector sends it as If-None-Match.
	// Empty sends no If-None-Match header.
	ETag string `json:"etag,omitempty"`
	// LastModified is the lastModified a previous read of this sitemap returned, an HTTP date. The connector
	// sends it as If-Modified-Since. Empty sends no If-Modified-Since header.
	LastModified string `json:"lastModified,omitempty"`
}

// Sitemap is one parsed sitemap, or only the refreshed validators when the branch is notModified.
type Sitemap struct {
	// Kind is urlset or sitemapIndex. It is empty for notModified.
	Kind SitemapKind `json:"kind,omitempty"`
	// FinalURL is the https URL that served the sitemap after redirects.
	FinalURL string `json:"finalUrl"`
	// Entries holds, for a urlset, the matching page entries newest first: entries with a date by that date
	// descending, then undated entries in document order. It is empty for an index.
	Entries []SitemapEntry `json:"entries"`
	// Sitemaps holds, for an index, its child sitemaps ordered the same way. Read each with readSitemap. It
	// is empty for a urlset.
	Sitemaps []ChildSitemap `json:"sitemaps"`
	// TotalEntryCount is the number of url or sitemap entries with a usable URL in the whole document.
	TotalEntryCount int `json:"totalEntryCount"`
	// MatchedEntryCount is the number of entries that pass pathPrefix and lastModifiedAfter; for an index it
	// equals TotalEntryCount.
	MatchedEntryCount int `json:"matchedEntryCount"`
	// HasMore reports that more entries matched than the limit returned; the newest are kept.
	HasMore bool `json:"hasMore"`
	// ETag is the response's ETag header. Pass it as the next read's etag. Empty when the site sent none.
	ETag string `json:"etag,omitempty"`
	// LastModified is the response's Last-Modified header. Pass it as the next read's lastModified. Empty
	// when the site sent none.
	LastModified string `json:"lastModified,omitempty"`
	// FetchedAt is the UTC time the Worker read the response.
	FetchedAt time.Time `json:"fetchedAt"`
}

// SitemapEntry is one url element of a urlset.
type SitemapEntry struct {
	// URL is the absolute http or https page URL from loc.
	URL string `json:"url"`
	// LastModifiedAt is the entry's lastmod in UTC, when present and readable.
	LastModifiedAt *time.Time `json:"lastModifiedAt,omitempty"`
	// NewsTitle is the Google News sitemap title of the article, when present.
	NewsTitle string `json:"newsTitle,omitempty"`
	// NewsPublishedAt is the Google News sitemap publication date in UTC, when present and readable.
	NewsPublishedAt *time.Time `json:"newsPublishedAt,omitempty"`
}

// ChildSitemap is one sitemap element of a sitemap index.
type ChildSitemap struct {
	// URL is the absolute http or https URL of the child sitemap from loc.
	URL string `json:"url"`
	// LastModifiedAt is the child's lastmod in UTC, when present and readable.
	LastModifiedAt *time.Time `json:"lastModifiedAt,omitempty"`
}

var readSitemapBranches = operationBranches{
	operationID: "readSitemap", notFound: ReadSitemapBranchNotFound, providerRejected: ReadSitemapBranchProviderRejected,
	unreachable: ReadSitemapBranchUnreachable, invalidResponse: ReadSitemapBranchInvalidResponse, defect: ReadSitemapBranchDefect,
}

// sitemapFilter is the validated selection of one readSitemap call.
type sitemapFilter struct {
	pathPrefix        string
	lastModifiedAfter *time.Time
	limit             int
}

// Definition returns the manifest-generated readSitemap contract.
func (ReadSitemapOperation) Definition() sdkgo.QueryDefinition { return ReadSitemapDefinition }

// Invoke reads one sitemap with one GET plus at most five redirect hops and decompresses a gzip body. A
// 304 answer to a conditional GET selects notModified; transient failures return Retry.
func (operation ReadSitemapOperation) Invoke(call sdkgo.Call, input ReadSitemapInput) sdkgo.QueryAttempt[Sitemap] {
	target, err := parseRequestURL(input.URL)
	if err != nil {
		return attemptFromFailure[Sitemap](readSitemapBranches, defectFailure(err))
	}
	previous, err := validatorsFromInput(input.ETag, input.LastModified)
	if err != nil {
		return attemptFromFailure[Sitemap](readSitemapBranches, defectFailure(err))
	}
	filter, err := newSitemapFilter(input)
	if err != nil {
		return attemptFromFailure[Sitemap](readSitemapBranches, defectFailure(err))
	}
	client := operation.client
	document, failure := client.fetchDocument(call.Context, documentRequest{
		target: target, accept: sitemapAcceptHeader, validators: previous, maxBytes: client.maxDocumentBytes,
		mayBeGzip: true, acceptsMediaType: isSitemapMediaType, expectedContent: "a sitemap",
	})
	if failure != nil {
		return attemptFromFailure[Sitemap](readSitemapBranches, failure)
	}
	sitemap := Sitemap{
		FinalURL: document.finalURL.String(), Entries: []SitemapEntry{}, Sitemaps: []ChildSitemap{},
		ETag: document.validators.etag, LastModified: document.validators.lastModified, FetchedAt: client.clock().UTC(),
	}
	if document.isNotModified {
		return sdkgo.NewQueryBranch(ReadSitemapBranchNotModified, sitemap, nil, sdkgo.Receipt{})
	}
	if failure := parseSitemapDocument(&sitemap, document, filter); failure != nil {
		return attemptFromFailure[Sitemap](readSitemapBranches, failure)
	}
	return sdkgo.NewQueryBranch(ReadSitemapBranchRead, sitemap, nil, sdkgo.Receipt{})
}

func newSitemapFilter(input ReadSitemapInput) (sitemapFilter, error) {
	filter := sitemapFilter{pathPrefix: input.PathPrefix, lastModifiedAfter: input.LastModifiedAfter, limit: input.Limit}
	if filter.limit == 0 {
		filter.limit = defaultSitemapLimit
	}
	switch {
	case filter.limit < 1 || filter.limit > maxSitemapLimit:
		return sitemapFilter{}, errors.New("limit must be from 1 through 2000")
	case filter.pathPrefix != "" && !strings.HasPrefix(filter.pathPrefix, "/"):
		return sitemapFilter{}, errors.New("pathPrefix must start with a slash, such as /blog/")
	case len(filter.pathPrefix) > maxSitemapPathPrefix || strings.ContainsAny(filter.pathPrefix, "?#"):
		return sitemapFilter{}, errors.New("pathPrefix must be a URL path of at most 512 bytes without a query or fragment")
	}
	return filter, nil
}

// sitemapRecord is one url or sitemap element before filtering.
type sitemapRecord struct {
	location        string
	lastModified    string
	newsTitle       string
	newsPublication string
}

// parseSitemapDocument streams the document so a large urlset never becomes one element tree.
func parseSitemapDocument(sitemap *Sitemap, document fetchedDocument, filter sitemapFilter) *fetchFailure {
	body := bytes.TrimLeft(bytes.TrimPrefix(document.body, []byte("\xef\xbb\xbf")), " \t\r\n")
	decoder := newTolerantXMLDecoder(body, document.charsetLabel)
	start, err := readRootElement(decoder)
	if err != nil {
		return invalidResponse("the document is not XML")
	}
	var entryName string
	switch {
	case start.Name.Local == sitemapURLSetRootName:
		sitemap.Kind, entryName = SitemapKindURLSet, sitemapEntryElement
	case start.Name.Local == sitemapIndexRootName:
		sitemap.Kind, entryName = SitemapKindIndex, sitemapIndexEntryName
	case strings.EqualFold(start.Name.Local, "html"):
		return invalidResponse("the URL serves an HTML page, not a sitemap")
	default:
		return invalidResponse("the XML root element is neither urlset nor sitemapindex")
	}
	base := document.finalURL
	var entries []SitemapEntry
	var children []ChildSitemap
	for scanned := 0; ; {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return invalidResponse("the sitemap XML is malformed")
		}
		element, isStart := token.(xml.StartElement)
		if !isStart {
			continue
		}
		if element.Name.Local != entryName {
			if err := decoder.Skip(); err != nil {
				return invalidResponse("the sitemap XML is malformed")
			}
			continue
		}
		record, err := readSitemapRecord(decoder)
		if err != nil {
			return invalidResponse("the sitemap XML is malformed or nested too deeply")
		}
		if scanned++; scanned > maxSitemapScanEntries {
			return invalidResponse("the sitemap lists more than 100000 entries")
		}
		location := resolveLink(base, record.location)
		if location == "" {
			continue
		}
		if sitemap.Kind == SitemapKindIndex {
			children = append(children, ChildSitemap{URL: location, LastModifiedAt: optionalTimestamp(record.lastModified)})
			continue
		}
		sitemap.TotalEntryCount++
		entry := SitemapEntry{
			URL: location, LastModifiedAt: optionalTimestamp(record.lastModified),
			NewsTitle: boundedText(record.newsTitle, maxTitleCharacters), NewsPublishedAt: optionalTimestamp(record.newsPublication),
		}
		if filter.matches(entry) {
			entries = append(entries, entry)
		}
	}
	if sitemap.Kind == SitemapKindIndex {
		sitemap.TotalEntryCount, sitemap.MatchedEntryCount = len(children), len(children)
		sort.SliceStable(children, func(left, right int) bool {
			return isNewer(children[left].LastModifiedAt, children[right].LastModifiedAt)
		})
		sitemap.Sitemaps = append(sitemap.Sitemaps, children[:min(filter.limit, len(children))]...)
		sitemap.HasMore = len(children) > filter.limit
		return nil
	}
	sitemap.MatchedEntryCount = len(entries)
	sort.SliceStable(entries, func(left, right int) bool {
		return isNewer(entries[left].effectiveTime(), entries[right].effectiveTime())
	})
	sitemap.Entries = append(sitemap.Entries, entries[:min(filter.limit, len(entries))]...)
	sitemap.HasMore = len(entries) > filter.limit
	return nil
}

// readSitemapRecord reads the loc, lastmod, and Google News fields of the entry the decoder just opened.
func readSitemapRecord(decoder *xml.Decoder) (sitemapRecord, error) {
	var record sitemapRecord
	var path []xml.Name
	var text strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			return sitemapRecord{}, err
		}
		switch typed := token.(type) {
		case xml.StartElement:
			if len(path) == maxSitemapElementDepth {
				return sitemapRecord{}, errors.New("sitemap entry is nested too deeply")
			}
			path = append(path, typed.Name)
			text.Reset()
		case xml.CharData:
			if text.Len() < maxURLBytes*2 {
				text.Write(typed)
			}
		case xml.EndElement:
			if len(path) == 0 {
				return record, nil
			}
			record.assign(path, strings.TrimSpace(text.String()))
			path = path[:len(path)-1]
			text.Reset()
		}
	}
}

// assign stores the text of the element at path, relative to the entry element.
func (record *sitemapRecord) assign(path []xml.Name, value string) {
	leaf := path[len(path)-1]
	switch {
	case len(path) == 1 && leaf.Local == "loc" && record.location == "":
		record.location = value
	case len(path) == 1 && leaf.Local == "lastmod" && record.lastModified == "":
		record.lastModified = value
	case leaf.Space == namespaceGoogleNews && leaf.Local == "title" && record.newsTitle == "":
		record.newsTitle = value
	case leaf.Space == namespaceGoogleNews && leaf.Local == "publication_date" && record.newsPublication == "":
		record.newsPublication = value
	}
}

func (filter sitemapFilter) matches(entry SitemapEntry) bool {
	if filter.pathPrefix != "" {
		parsed, err := url.Parse(entry.URL)
		if err != nil || !strings.HasPrefix(parsed.Path, filter.pathPrefix) {
			return false
		}
	}
	if filter.lastModifiedAfter != nil {
		effective := entry.effectiveTime()
		return effective != nil && effective.After(*filter.lastModifiedAfter)
	}
	return true
}

// effectiveTime is lastmod, or the Google News publication date when lastmod is missing.
func (entry SitemapEntry) effectiveTime() *time.Time {
	if entry.LastModifiedAt != nil {
		return entry.LastModifiedAt
	}
	return entry.NewsPublishedAt
}

// isNewer orders dated values newest first and undated values last.
func isNewer(left, right *time.Time) bool {
	switch {
	case left == nil:
		return false
	case right == nil:
		return true
	}
	return left.After(*right)
}

// isSitemapMediaType accepts XML, gzip, generic binary, and text media types that sites serve sitemaps with.
func isSitemapMediaType(mediaType string) bool {
	switch {
	case mediaType == "", mediaType == "application/xml", mediaType == "application/gzip", mediaType == "application/x-gzip", mediaType == "application/octet-stream":
		return true
	case strings.HasPrefix(mediaType, "text/"):
		return mediaType != "text/css" && mediaType != "text/javascript"
	}
	return strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+xml")
}
