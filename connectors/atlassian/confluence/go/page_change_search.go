// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const pageChangedTriggerID = "pageChanged"

// errSearchCursorRejected reports HTTP 400 for a stored search cursor, such as an expired one.
var errSearchCursorRejected = errors.New("Confluence rejected the stored search cursor")

// startTriggerSession resolves the credential and site for one Trigger poll; the caller must call cancel.
// An Atlassian 429 during the site lookup pauses the connection like a search 429.
func (client *Client) startTriggerSession(ctx context.Context, call sdkgo.Call) (*operationSession, context.CancelFunc, error) {
	if client.needsSiteLookup() {
		if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
			return nil, nil, err
		}
	}
	credentials, err := sdkgo.ResolveCredential(ctx, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return nil, nil, errors.New("Confluence Trigger credentials are unavailable; reconnect the connection")
	}
	sessionContext, cancel := context.WithTimeout(ctx, operationDeadline)
	session := &operationSession{context: sessionContext, call: call, credentials: credentials}
	cloudID, failure := client.siteCloudID(session, pageChangedTriggerID)
	if failure != nil {
		cancel()
		if failure.failure.Kind == sdkgo.FailureRateLimit {
			return nil, nil, pollingtrigger.MarkRateLimited(errors.New(failure.failure.Message), failure.retryAfter)
		}
		return nil, nil, errors.New(failure.failure.Message)
	}
	session.cloudID = cloudID
	return session, cancel, nil
}

// searchPageChanges reads one CQL content search page. A 429, or a 503 with Retry-After, is marked rate
// limited so the kit pauses every poller of the connection for the requested wait.
func (client *Client) searchPageChanges(session *operationSession, cql string, searchCursor string) (searchPageResult, error) {
	if err := pollingtrigger.WaitForPollRequest(session.context); err != nil {
		return searchPageResult{}, err
	}
	query := url.Values{"cql": {cql}, "limit": {strconv.Itoa(pageChangeSearchPageSize)}, "expand": {"space,version"}}
	if searchCursor != "" {
		query.Set("cursor", searchCursor)
	}
	result := client.exchange(session, confluenceRequest{method: http.MethodGet, api: searchAPI, path: contentSearchPath, query: query})
	if result.err != nil {
		return searchPageResult{}, errors.New("Confluence page search is temporarily unavailable")
	}
	response := result.response
	retryAfter := client.retryAfter(response)
	switch {
	case response.statusCode == http.StatusTooManyRequests:
		return searchPageResult{}, pollingtrigger.MarkRateLimited(errors.New("Confluence rate limited the page search (HTTP 429)"), retryAfter)
	case response.statusCode == http.StatusServiceUnavailable && retryAfter > 0:
		return searchPageResult{}, pollingtrigger.MarkRateLimited(errors.New("Confluence page search is unavailable (HTTP 503)"), retryAfter)
	case response.statusCode == http.StatusBadRequest && searchCursor != "":
		return searchPageResult{}, errSearchCursorRejected
	case !isSuccessStatus(response.statusCode):
		return searchPageResult{}, fmt.Errorf("Confluence rejected the page search with HTTP %d", response.statusCode)
	case response.hasBodyReadFailed:
		return searchPageResult{}, errors.New("Confluence page search response was interrupted")
	case response.isBodyTooLarge:
		return searchPageResult{}, errors.New("Confluence page search response exceeds maxResponseBytes")
	case response.reflectsSecret:
		return searchPageResult{}, errors.New("Confluence page search response contains credential material")
	}
	return decodePageChangeSearch(response.body)
}

// decodePageChangeSearch keeps pages only and requires each page's version, which orders the scan.
func decodePageChangeSearch(body []byte) (searchPageResult, error) {
	var page contentSearchResource
	if err := json.Unmarshal(body, &page); err != nil {
		return searchPageResult{}, errors.New("Confluence returned an invalid search page")
	}
	if len(page.Results) > pollingtrigger.MaxChangesPerPage {
		return searchPageResult{}, fmt.Errorf("Confluence search page has %d results, more than %d", len(page.Results), pollingtrigger.MaxChangesPerPage)
	}
	nextCursor, err := readNextCursor(page.Links.Next)
	if err != nil {
		return searchPageResult{}, errors.New("Confluence returned an invalid search cursor")
	}
	result := searchPageResult{pages: []PageSummary{}, searchCursor: nextCursor}
	for _, resource := range page.Results {
		summary, isPage, err := decodePageSummary(resource, page.Links.Base)
		if err != nil {
			return searchPageResult{}, fmt.Errorf("Confluence returned an invalid page in the search results: %w", err)
		}
		if !isPage {
			continue
		}
		if summary.VersionNumber < 1 || summary.LastModifiedAt.IsZero() {
			return searchPageResult{}, fmt.Errorf("Confluence search result for page %s has no version number or time", summary.ID)
		}
		result.pages = append(result.pages, summary)
	}
	return result, nil
}

// needsSiteLookup reports that the next session sends the accessible-resources request.
func (client *Client) needsSiteLookup() bool {
	if client.configuredCloudID != "" {
		return false
	}
	client.siteMutex.Lock()
	defer client.siteMutex.Unlock()
	return client.resolvedCloudID == ""
}
