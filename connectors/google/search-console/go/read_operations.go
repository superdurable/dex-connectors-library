// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package searchconsole

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var domainPropertyPattern = regexp.MustCompile(`^sc-domain:[A-Za-z0-9][A-Za-z0-9.-]{1,252}$`)
var languageCodePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// GetSiteInput selects one accessible Search Console property.
type GetSiteInput struct {
	// SiteURL is the exact URL-prefix or sc-domain property identifier.
	SiteURL string `json:"siteUrl"`
}

// QuerySearchAnalyticsInput selects search traffic rows for one date range.
type QuerySearchAnalyticsInput struct {
	// SiteURL is the exact property identifier from Search Console.
	SiteURL string `json:"siteUrl"`
	// StartDate is the inclusive YYYY-MM-DD Pacific date.
	StartDate string `json:"startDate"`
	// EndDate is the inclusive YYYY-MM-DD Pacific date.
	EndDate string `json:"endDate"`
	// Dimensions group rows, for example date, query, page, country, or device.
	Dimensions []string `json:"dimensions,omitempty"`
	// SearchType selects web, image, video, news, discover, or googleNews; blank uses web.
	SearchType string `json:"type,omitempty"`
	// RowLimit is 1 through 25000; zero uses Google's default.
	RowLimit int `json:"rowLimit,omitempty"`
	// StartRow is the non-negative zero-based pagination offset.
	StartRow int `json:"startRow,omitempty"`
}

// SearchAnalyticsRow is one group of search performance metrics.
type SearchAnalyticsRow struct {
	// Keys contain dimension values in request order.
	Keys []string `json:"keys,omitempty"`
	// Clicks counts search result clicks.
	Clicks float64 `json:"clicks"`
	// Impressions counts search result impressions.
	Impressions float64 `json:"impressions"`
	// CTR is clicks divided by impressions.
	CTR float64 `json:"ctr"`
	// Position is the average search result position.
	Position float64 `json:"position"`
}

// SearchAnalyticsResult contains one bounded set of performance rows.
type SearchAnalyticsResult struct {
	// Rows are grouped search performance metrics.
	Rows []SearchAnalyticsRow `json:"rows"`
	// ResponseAggregationType identifies Google's aggregation mode.
	ResponseAggregationType string `json:"responseAggregationType,omitempty"`
}

// Sitemap is a submitted sitemap's status metadata.
type Sitemap struct {
	// Path is the submitted sitemap URL.
	Path string `json:"path"`
	// Type is Google's sitemap type.
	Type string `json:"type,omitempty"`
	// LastSubmitted is the provider's submission timestamp.
	LastSubmitted string `json:"lastSubmitted,omitempty"`
	// LastDownloaded is the provider's last download timestamp.
	LastDownloaded string `json:"lastDownloaded,omitempty"`
	// IsPending indicates Google has not yet processed it.
	IsPending bool `json:"isPending,omitempty"`
	// IsSitemapsIndex indicates the sitemap is an index.
	IsSitemapsIndex bool `json:"isSitemapsIndex,omitempty"`
	// Errors is the number of processing errors.
	Errors json.Number `json:"errors,omitempty"`
	// Warnings is the number of processing warnings.
	Warnings json.Number `json:"warnings,omitempty"`
}

// SitemapsPage contains the returned sitemaps for one property.
type SitemapsPage struct {
	// Sitemaps are the submitted sitemaps.
	Sitemaps []Sitemap `json:"sitemaps"`
}

// ListSitemapsInput selects a property's sitemaps.
type ListSitemapsInput struct {
	// SiteURL is the exact Search Console property identifier.
	SiteURL string `json:"siteUrl"`
	// SitemapIndex optionally filters to children of this sitemap index URL.
	SitemapIndex string `json:"sitemapIndex,omitempty"`
}

// GetSitemapInput selects one submitted sitemap.
type GetSitemapInput struct {
	// SiteURL is the exact Search Console property identifier.
	SiteURL string `json:"siteUrl"`
	// FeedPath is the full submitted sitemap URL.
	FeedPath string `json:"feedPath"`
}

// InspectURLInput selects a page in one Search Console property.
type InspectURLInput struct {
	// SiteURL is the exact Search Console property identifier.
	SiteURL string `json:"siteUrl"`
	// InspectionURL is the full page URL under that property.
	InspectionURL string `json:"inspectionUrl"`
	// LanguageCode is an optional BCP-47 translation language; blank uses English.
	LanguageCode string `json:"languageCode,omitempty"`
}

// IndexStatusResult is Google's indexed status for one page URL.
type IndexStatusResult struct {
	// Verdict summarizes whether the URL is indexed.
	Verdict string `json:"verdict,omitempty"`
	// CoverageState describes Google's coverage result.
	CoverageState string `json:"coverageState,omitempty"`
	// IndexingState describes indexing allowance.
	IndexingState string `json:"indexingState,omitempty"`
	// PageFetchState describes Google's fetch result.
	PageFetchState string `json:"pageFetchState,omitempty"`
	// LastCrawlTime is the provider's last crawl timestamp.
	LastCrawlTime string `json:"lastCrawlTime,omitempty"`
	// GoogleCanonical is Google's selected canonical URL.
	GoogleCanonical string `json:"googleCanonical,omitempty"`
	// UserCanonical is the site's declared canonical URL.
	UserCanonical string `json:"userCanonical,omitempty"`
}

// URLInspectionResult contains Google's index status for a page.
type URLInspectionResult struct {
	// IndexStatusResult is the indexed status, when available.
	IndexStatusResult IndexStatusResult `json:"indexStatusResult"`
}

// GetSiteOperation reads one property.
type GetSiteOperation struct{ client *Client }

// Definition returns the generated property read contract.
func (GetSiteOperation) Definition() sdkgo.QueryDefinition { return GetSiteDefinition }

// GetSite binds the property Query to the client.
func (client *Client) GetSite() GetSiteOperation { return GetSiteOperation{client} }

// Invoke reads a property's permission level.
func (operation GetSiteOperation) Invoke(call sdkgo.Call, input GetSiteInput) sdkgo.QueryAttempt[Site] {
	if !validSiteURL(input.SiteURL) {
		return consoleQueryBranch(GetSiteBranchDefect, Site{}, sdkgo.FailureValidation, "Search Console property URL is invalid")
	}
	response := operation.client.get(call, "/webmasters/v3/sites/"+url.PathEscape(input.SiteURL))
	if outcome, hasFailure := consoleQueryFailure[Site](response, GetSiteBranchDefect, GetSiteBranchNotFound, GetSiteBranchProviderRejected, GetSiteBranchInvalidResponse, "getSite"); hasFailure {
		return outcome
	}
	var site Site
	if json.Unmarshal(response.body, &site) != nil || site.SiteURL != input.SiteURL || !isPermissionLevel(site.PermissionLevel) {
		return consoleQueryBranch(GetSiteBranchInvalidResponse, Site{}, sdkgo.FailureProtocol, "Google returned invalid property JSON")
	}
	return sdkgo.NewQueryBranch(GetSiteBranchFound, site, nil, sdkgo.Receipt{})
}

// QuerySearchAnalyticsOperation reads search traffic metrics.
type QuerySearchAnalyticsOperation struct{ client *Client }

// Definition returns the generated analytics contract.
func (QuerySearchAnalyticsOperation) Definition() sdkgo.QueryDefinition {
	return QuerySearchAnalyticsDefinition
}

// QuerySearchAnalytics binds the analytics Query to the client.
func (client *Client) QuerySearchAnalytics() QuerySearchAnalyticsOperation {
	return QuerySearchAnalyticsOperation{client}
}

// Invoke reads one bounded analytics result.
func (operation QuerySearchAnalyticsOperation) Invoke(call sdkgo.Call, input QuerySearchAnalyticsInput) sdkgo.QueryAttempt[SearchAnalyticsResult] {
	if !validAnalyticsInput(input) {
		return consoleQueryBranch(QuerySearchAnalyticsBranchDefect, SearchAnalyticsResult{}, sdkgo.FailureValidation, "Search Console analytics input is invalid")
	}
	body, _ := json.Marshal(struct {
		StartDate  string   `json:"startDate"`
		EndDate    string   `json:"endDate"`
		Dimensions []string `json:"dimensions,omitempty"`
		Type       string   `json:"type,omitempty"`
		RowLimit   int      `json:"rowLimit,omitempty"`
		StartRow   int      `json:"startRow,omitempty"`
	}{input.StartDate, input.EndDate, input.Dimensions, input.SearchType, input.RowLimit, input.StartRow})
	response := operation.client.request(call, http.MethodPost, operation.client.baseURL, "/webmasters/v3/sites/"+url.PathEscape(input.SiteURL)+"/searchAnalytics/query", body)
	if outcome, hasFailure := consoleQueryFailure[SearchAnalyticsResult](response, QuerySearchAnalyticsBranchDefect, QuerySearchAnalyticsBranchNotFound, QuerySearchAnalyticsBranchProviderRejected, QuerySearchAnalyticsBranchInvalidResponse, "querySearchAnalytics"); hasFailure {
		return outcome
	}
	var result SearchAnalyticsResult
	if json.Unmarshal(response.body, &result) != nil || len(result.Rows) > 25000 {
		return consoleQueryBranch(QuerySearchAnalyticsBranchInvalidResponse, SearchAnalyticsResult{}, sdkgo.FailureProtocol, "Google returned invalid analytics JSON")
	}
	for _, row := range result.Rows {
		if row.CTR < 0 || row.CTR > 1 || row.Impressions < 0 || row.Clicks < 0 || row.Position < 0 {
			return consoleQueryBranch(QuerySearchAnalyticsBranchInvalidResponse, SearchAnalyticsResult{}, sdkgo.FailureProtocol, "Google returned invalid analytics metrics")
		}
	}
	return sdkgo.NewQueryBranch(QuerySearchAnalyticsBranchQueried, result, nil, sdkgo.Receipt{})
}

// ListSitemapsOperation reads one property's submitted sitemaps.
type ListSitemapsOperation struct{ client *Client }

// Definition returns the generated sitemap list contract.
func (ListSitemapsOperation) Definition() sdkgo.QueryDefinition { return ListSitemapsDefinition }

// ListSitemaps binds the sitemap list Query to the client.
func (client *Client) ListSitemaps() ListSitemapsOperation { return ListSitemapsOperation{client} }

// Invoke lists the property sitemaps.
func (operation ListSitemapsOperation) Invoke(call sdkgo.Call, input ListSitemapsInput) sdkgo.QueryAttempt[SitemapsPage] {
	if !validSiteURL(input.SiteURL) || (input.SitemapIndex != "" && !validHTTPURL(input.SitemapIndex)) {
		return consoleQueryBranch(ListSitemapsBranchDefect, SitemapsPage{}, sdkgo.FailureValidation, "Search Console sitemap list input is invalid")
	}
	path := "/webmasters/v3/sites/" + url.PathEscape(input.SiteURL) + "/sitemaps"
	if input.SitemapIndex != "" {
		path += "?sitemapIndex=" + url.QueryEscape(input.SitemapIndex)
	}
	response := operation.client.get(call, path)
	if outcome, hasFailure := consoleQueryFailure[SitemapsPage](response, ListSitemapsBranchDefect, ListSitemapsBranchNotFound, ListSitemapsBranchProviderRejected, ListSitemapsBranchInvalidResponse, "listSitemaps"); hasFailure {
		return outcome
	}
	var wire struct {
		Sitemaps []Sitemap `json:"sitemap"`
	}
	if json.Unmarshal(response.body, &wire) != nil || len(wire.Sitemaps) > 10000 {
		return consoleQueryBranch(ListSitemapsBranchInvalidResponse, SitemapsPage{}, sdkgo.FailureProtocol, "Google returned invalid sitemap page JSON")
	}
	for _, sitemap := range wire.Sitemaps {
		if !validHTTPURL(sitemap.Path) || !validSitemapCount(sitemap.Errors) || !validSitemapCount(sitemap.Warnings) {
			return consoleQueryBranch(ListSitemapsBranchInvalidResponse, SitemapsPage{}, sdkgo.FailureProtocol, "Google returned invalid sitemap metadata")
		}
	}
	return sdkgo.NewQueryBranch(ListSitemapsBranchListed, SitemapsPage{Sitemaps: wire.Sitemaps}, nil, sdkgo.Receipt{})
}

// GetSitemapOperation reads one sitemap's processing status.
type GetSitemapOperation struct{ client *Client }

// Definition returns the generated sitemap read contract.
func (GetSitemapOperation) Definition() sdkgo.QueryDefinition { return GetSitemapDefinition }

// GetSitemap binds the sitemap read Query to the client.
func (client *Client) GetSitemap() GetSitemapOperation { return GetSitemapOperation{client} }

// Invoke reads the submitted sitemap.
func (operation GetSitemapOperation) Invoke(call sdkgo.Call, input GetSitemapInput) sdkgo.QueryAttempt[Sitemap] {
	if !validSiteURL(input.SiteURL) || !validHTTPURL(input.FeedPath) {
		return consoleQueryBranch(GetSitemapBranchDefect, Sitemap{}, sdkgo.FailureValidation, "Search Console sitemap input is invalid")
	}
	response := operation.client.get(call, "/webmasters/v3/sites/"+url.PathEscape(input.SiteURL)+"/sitemaps/"+url.PathEscape(input.FeedPath))
	if outcome, hasFailure := consoleQueryFailure[Sitemap](response, GetSitemapBranchDefect, GetSitemapBranchNotFound, GetSitemapBranchProviderRejected, GetSitemapBranchInvalidResponse, "getSitemap"); hasFailure {
		return outcome
	}
	var sitemap Sitemap
	if json.Unmarshal(response.body, &sitemap) != nil || sitemap.Path != input.FeedPath || !validSitemapCount(sitemap.Errors) || !validSitemapCount(sitemap.Warnings) {
		return consoleQueryBranch(GetSitemapBranchInvalidResponse, Sitemap{}, sdkgo.FailureProtocol, "Google returned invalid sitemap JSON")
	}
	return sdkgo.NewQueryBranch(GetSitemapBranchFound, sitemap, nil, sdkgo.Receipt{})
}

// InspectURLOperation reads Google's indexed status for one page.
type InspectURLOperation struct{ client *Client }

// Definition returns the generated URL inspection contract.
func (InspectURLOperation) Definition() sdkgo.QueryDefinition { return InspectURLDefinition }

// InspectURL binds the inspection Query to the client.
func (client *Client) InspectURL() InspectURLOperation { return InspectURLOperation{client} }

// Invoke inspects the indexed version of one URL.
func (operation InspectURLOperation) Invoke(call sdkgo.Call, input InspectURLInput) sdkgo.QueryAttempt[URLInspectionResult] {
	if !validSiteURL(input.SiteURL) || !validHTTPURL(input.InspectionURL) || (input.LanguageCode != "" && !languageCodePattern.MatchString(input.LanguageCode)) {
		return consoleQueryBranch(InspectURLBranchDefect, URLInspectionResult{}, sdkgo.FailureValidation, "Search Console inspection input is invalid")
	}
	body, _ := json.Marshal(struct {
		InspectionURL string `json:"inspectionUrl"`
		SiteURL       string `json:"siteUrl"`
		LanguageCode  string `json:"languageCode,omitempty"`
	}{input.InspectionURL, input.SiteURL, input.LanguageCode})
	response := operation.client.request(call, http.MethodPost, operation.client.inspectionBaseURL, "/v1/urlInspection/index:inspect", body)
	if outcome, hasFailure := consoleQueryFailure[URLInspectionResult](response, InspectURLBranchDefect, InspectURLBranchNotFound, InspectURLBranchProviderRejected, InspectURLBranchInvalidResponse, "inspectURL"); hasFailure {
		return outcome
	}
	var wire struct {
		InspectionResult URLInspectionResult `json:"inspectionResult"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.InspectionResult.IndexStatusResult.Verdict == "" {
		return consoleQueryBranch(InspectURLBranchInvalidResponse, URLInspectionResult{}, sdkgo.FailureProtocol, "Google returned invalid URL inspection JSON")
	}
	return sdkgo.NewQueryBranch(InspectURLBranchInspected, wire.InspectionResult, nil, sdkgo.Receipt{})
}

func consoleQueryFailure[T any](response providerResponse, defectBranch, notFoundBranch, rejectedBranch, invalidBranch sdkgo.BranchID, operationID string) (sdkgo.QueryAttempt[T], bool) {
	if errors.Is(response.err, errCredential) {
		return consoleQueryBranch(defectBranch, *new(T), sdkgo.FailureAuthentication, "Search Console credentials are unavailable"), true
	}
	if response.status == http.StatusTooManyRequests {
		return consoleQueryRetry[T](sdkgo.FailureRateLimit, operationID, "Google rate limited the request", response.retryAfter), true
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return consoleQueryRetry[T](sdkgo.FailureAvailability, operationID, "Search Console is temporarily unavailable", response.retryAfter), true
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return consoleQueryBranch(invalidBranch, *new(T), sdkgo.FailureResponseTooLarge, "Search Console response exceeded the limit"), true
	}
	if response.err != nil {
		return consoleQueryRetry[T](sdkgo.FailureTransport, operationID, "Search Console request failed", 0), true
	}
	if response.status == http.StatusNotFound {
		return consoleQueryBranch(notFoundBranch, *new(T), sdkgo.FailureNotFound, "Search Console resource was not found"), true
	}
	if response.status != http.StatusOK {
		return consoleQueryBranch(rejectedBranch, *new(T), sdkgo.FailureProviderRejection, "Google rejected HTTP "+strconv.Itoa(response.status)), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}
func consoleQueryBranch[T any](branch sdkgo.BranchID, output T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "google-search-console", Operation: "read", Message: message}
	return sdkgo.NewQueryBranch(branch, output, &failure, sdkgo.Receipt{})
}
func consoleQueryRetry[T any](kind sdkgo.FailureKind, operationID, message string, delay time.Duration) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryRetry[T](sdkgo.Failure{Kind: kind, Provider: "google-search-console", Operation: operationID, Message: message}, delay)
}
func validSiteURL(siteURL string) bool {
	return domainPropertyPattern.MatchString(siteURL) || (strings.HasSuffix(siteURL, "/") && validHTTPURL(siteURL))
}
func validHTTPURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}
func validSitemapCount(count json.Number) bool {
	if count == "" {
		return true
	}
	value, err := count.Int64()
	return err == nil && value >= 0
}
func validAnalyticsInput(input QuerySearchAnalyticsInput) bool {
	if !validSiteURL(input.SiteURL) || input.RowLimit < 0 || input.RowLimit > 25000 || input.StartRow < 0 {
		return false
	}
	startDate, startError := time.Parse("2006-01-02", input.StartDate)
	endDate, endError := time.Parse("2006-01-02", input.EndDate)
	if startError != nil || endError != nil || startDate.After(endDate) {
		return false
	}
	if input.SearchType != "" {
		switch input.SearchType {
		case "web", "image", "video", "news", "discover", "googleNews", "webMultimodal":
		default:
			return false
		}
	}
	seen := map[string]bool{}
	for _, dimension := range input.Dimensions {
		switch dimension {
		case "date", "hour", "query", "page", "country", "device", "searchAppearance", "brandedClassification":
		default:
			return false
		}
		if seen[dimension] {
			return false
		}
		seen[dimension] = true
	}
	return true
}
