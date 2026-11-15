// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	issueChangedOperationID = "issueChanged"
	// issueChangePageSize is the enhanced search page size with standard fields; Jira caps such pages at 100.
	issueChangePageSize = 100
	// issueChangeLookback re-reads recent updates, because JQL dates have minute precision and Jira search
	// indexes an update a few seconds after it happens.
	issueChangeLookback = 2 * time.Minute
	// maximumSeenIssueVersions keeps the cursor far below the kit's 64 KiB token bound.
	maximumSeenIssueVersions = 1000
	// maximumSeedPages bounds the baseline read of issues updated within the lookback.
	maximumSeedPages = 10
	// pageChainProgress bounds how far pages follow one token; Jira does not promise tokens survive reordering.
	pageChainProgress = time.Minute
)

// issueChangeFetcher polls Jira's enhanced JQL search on the updated time for the poll Trigger kit.
type issueChangeFetcher struct {
	client          *Client
	call            sdkgo.Call
	projectClause   string
	projectIdentity string
	// jiraClockOffset is Jira's Date header minus the local clock, in nanoseconds, from the last search.
	jiraClockOffset atomic.Int64
}

// issueChangeCursor is the committed Trigger cursor. UpdatedThrough is the latest updated instant read, in
// Unix milliseconds; SeenVersions lists "issueID@updatedMillis" versions within the lookback before it.
// PageToken, SearchMinutes, SearchStartedAt, and SearchFloor are set only while one search spans pages,
// because Jira requires the next page to repeat the same JQL. SearchFloor is the restart floor, in Unix
// milliseconds, the search began from.
type issueChangeCursor struct {
	CloudID         string   `json:"cloudId"`
	Project         string   `json:"project,omitempty"`
	UpdatedThrough  int64    `json:"updatedThrough"`
	SeenVersions    []string `json:"seenVersions,omitempty"`
	PageToken       string   `json:"pageToken,omitempty"`
	SearchMinutes   int64    `json:"searchMinutes,omitempty"`
	SearchStartedAt int64    `json:"searchStartedAt,omitempty"`
	SearchFloor     int64    `json:"searchFloor,omitempty"`
}

// issueSearchPage is one enhanced search page; a rejected page token is reported separately.
type issueSearchPage struct {
	issues              []issueResource
	nextPageToken       string
	isPageTokenRejected bool
}

func newIssueChangeFetcher(client *Client, connection sdkgo.ConnectionRef, configuration IssueChangedTriggerConfiguration) *issueChangeFetcher {
	clauseValue, identity := configuration.watchedProject()
	projectClause := ""
	if clauseValue != "" {
		projectClause = "project = " + clauseValue + " AND "
	}
	return &issueChangeFetcher{
		client: client, call: pollingtrigger.NewTriggerCredentialCall(connection, IssueChangedTriggerDefinition.Trigger),
		projectClause: projectClause, projectIdentity: identity,
	}
}

// SeedCursor records Jira's current time, or a later updated time Jira reports, as the baseline. Issues
// updated within the lookback are marked seen, so neither they nor older issues are ever delivered.
func (fetcher *issueChangeFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	session, err := fetcher.startPollSession(ctx)
	if err != nil {
		return pollingtrigger.Cursor{}, err
	}
	position := issueChangeCursor{CloudID: session.cloudID, Project: fetcher.projectIdentity}
	minutes := int64(issueChangeLookback/time.Minute) + 2
	jql := fetcher.projectClause + "updated >= -" + strconv.FormatInt(minutes, 10) + "m ORDER BY updated DESC"
	var versions []issueVersion
	pageToken := ""
	for page := 0; page < maximumSeedPages; page++ {
		listed, err := fetcher.searchPage(session, jql, []string{"updated"}, pageToken)
		if err != nil {
			return pollingtrigger.Cursor{}, err
		}
		if listed.isPageTokenRejected {
			return pollingtrigger.Cursor{}, errors.New("Jira rejected the baseline search page token")
		}
		for _, resource := range listed.issues {
			version, err := decodeIssueVersion(resource)
			if err != nil {
				return pollingtrigger.Cursor{}, err
			}
			versions = append(versions, version)
		}
		if listed.nextPageToken == "" || listed.nextPageToken == pageToken {
			break
		}
		pageToken = listed.nextPageToken
	}
	// Read after the first response, so the baseline is on Jira's clock rather than the local one.
	position.UpdatedThrough = fetcher.jiraNow().UnixMilli()
	for _, version := range versions {
		position.UpdatedThrough = max(position.UpdatedThrough, version.updatedMillis)
	}
	position.SeenVersions = recentSeenVersions(nil, versions, position.UpdatedThrough)
	return encodeIssueChangeCursor(position)
}

// FetchChanges reads one search page of issues updated since the cursor's lookback, in ascending updated
// order, and returns the versions it has not seen. An undecodable cursor, or one for another site or
// project, is marked invalid, so the kit records a new baseline. A rejected page token restarts the search
// from the committed position, which loses nothing.
func (fetcher *issueChangeFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[IssueChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[IssueChangedEvent]]
	position, err := decodeIssueChangeCursor(cursor)
	if err != nil {
		return page, pollingtrigger.MarkCursorInvalid(err)
	}
	if position.Project != fetcher.projectIdentity {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Jira issue cursor belongs to another project"))
	}
	session, err := fetcher.startPollSession(ctx)
	if err != nil {
		return page, err
	}
	if position.CloudID != session.cloudID {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Jira issue cursor belongs to another site"))
	}
	now := fetcher.jiraNow()
	if position.PageToken != "" && now.Sub(time.UnixMilli(position.SearchStartedAt)) > issueChangeLookback {
		// A relative JQL bound moves with Jira's clock; restart before it passes the committed position.
		position.PageToken = ""
	}
	listed, err := fetcher.searchChangedIssues(session, &position, now)
	if err != nil {
		return page, err
	}
	if listed.isPageTokenRejected {
		position.PageToken = ""
		if listed, err = fetcher.searchChangedIssues(session, &position, now); err != nil {
			return page, err
		}
		if listed.isPageTokenRejected {
			return page, errors.New("Jira rejected the issue search without a page token")
		}
	}
	if len(listed.issues) > pollingtrigger.MaxChangesPerPage {
		return page, fmt.Errorf("Jira issue search page has %d issues, more than %d", len(listed.issues), pollingtrigger.MaxChangesPerPage)
	}
	seen := make(map[string]bool, len(position.SeenVersions))
	for _, version := range position.SeenVersions {
		seen[version] = true
	}
	threshold := position.UpdatedThrough - issueChangeLookback.Milliseconds()
	var delivered []issueVersion
	updatedThrough := position.UpdatedThrough
	for _, resource := range listed.issues {
		issue, err := decodeIssue(resource, nil, false)
		if err != nil {
			return page, fmt.Errorf("Jira issue search page has an invalid issue: %w", err)
		}
		version := issueVersion{issueID: issue.ID, updatedMillis: issue.UpdatedAt.UnixMilli()}
		if version.updatedMillis < threshold || seen[version.key()] {
			continue
		}
		seen[version.key()] = true
		delivered = append(delivered, version)
		updatedThrough = max(updatedThrough, version.updatedMillis)
		page.Changes = append(page.Changes, newIssueChangedTriggerEvent(session.cloudID, issue))
	}
	next := issueChangeCursor{CloudID: position.CloudID, Project: position.Project, UpdatedThrough: updatedThrough}
	next.SeenVersions = recentSeenVersions(position.SeenVersions, delivered, updatedThrough)
	if listed.nextPageToken != "" && listed.nextPageToken != position.PageToken {
		page.HasMorePages = true
		// A fresh search from the new floor re-reads any issue an update moved across the page boundary.
		if updatedThrough-issueChangeLookback.Milliseconds() < position.SearchFloor+pageChainProgress.Milliseconds() {
			next.PageToken, next.SearchMinutes, next.SearchStartedAt, next.SearchFloor =
				listed.nextPageToken, position.SearchMinutes, position.SearchStartedAt, position.SearchFloor
		}
	}
	page.Next, err = encodeIssueChangeCursor(next)
	return page, err
}

// searchChangedIssues reads the cursor's page, or starts a search whose relative bound covers the
// lookback before UpdatedThrough plus one minute for JQL's minute precision. A relative bound keeps the
// query independent of the account's time zone, which Jira applies to absolute JQL dates.
func (fetcher *issueChangeFetcher) searchChangedIssues(session *operationSession, position *issueChangeCursor, now time.Time) (issueSearchPage, error) {
	if position.PageToken == "" {
		since := time.UnixMilli(position.UpdatedThrough).Add(-issueChangeLookback)
		position.SearchMinutes = int64(max(now.Sub(since), 0)/time.Minute) + 2
		position.SearchStartedAt = now.UnixMilli()
		position.SearchFloor = since.UnixMilli()
	}
	jql := fetcher.projectClause + "updated >= -" + strconv.FormatInt(position.SearchMinutes, 10) + "m ORDER BY updated ASC"
	return fetcher.searchPage(session, jql, standardIssueFieldIDs, position.PageToken)
}

func (fetcher *issueChangeFetcher) startPollSession(ctx context.Context) (*operationSession, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return nil, err
	}
	credentials, err := sdkgo.ResolveCredential(ctx, fetcher.client.credentials, fetcher.call, fetcher.client.refreshDriver)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return nil, errors.New("Jira Trigger credentials are unavailable; reconnect the connection if this persists")
	}
	session := &operationSession{context: ctx, call: fetcher.call, credentials: credentials}
	cloudID, failure := fetcher.client.siteCloudID(session, issueChangedOperationID)
	if failure != nil {
		siteErr := errors.New(failure.failure.Message)
		if failure.failure.Kind == sdkgo.FailureRateLimit {
			return nil, pollingtrigger.MarkRateLimited(siteErr, failure.retryAfter)
		}
		return nil, siteErr
	}
	session.cloudID = cloudID
	return session, nil
}

// searchPage sends one enhanced search request. A 429 pauses the connection for Retry-After; a 400 for a
// request that carried a page token reports the token as rejected.
func (fetcher *issueChangeFetcher) searchPage(session *operationSession, jql string, fields []string, pageToken string) (issueSearchPage, error) {
	if err := pollingtrigger.WaitForPollRequest(session.context); err != nil {
		return issueSearchPage{}, err
	}
	body := searchRequestBody{JQL: jql, Fields: fields, MaxResults: issueChangePageSize, NextPageToken: pageToken}
	result := fetcher.client.exchange(session, jiraRequest{method: http.MethodPost, path: enhancedSearchPath, payload: body})
	if result.err != nil {
		return issueSearchPage{}, errors.New("Jira issue search is temporarily unavailable")
	}
	response := result.response
	if date, err := http.ParseTime(response.header.Get("Date")); err == nil {
		fetcher.jiraClockOffset.Store(int64(date.Sub(fetcher.client.now())))
	}
	switch {
	case response.statusCode == http.StatusTooManyRequests:
		// The hourly points quota answers 429 with Retry-After until the window resets.
		return issueSearchPage{}, pollingtrigger.MarkRateLimited(errors.New("Jira rate limited the issue search"), fetcher.client.retryAfter(response))
	case response.statusCode == http.StatusServiceUnavailable && response.header.Get("Retry-After") != "":
		// Jira documents Retry-After on some transient 503 responses; honor it like a rate limit.
		return issueSearchPage{}, pollingtrigger.MarkRateLimited(errors.New("Jira asked the issue search to wait"), fetcher.client.retryAfter(response))
	case response.statusCode == http.StatusBadRequest && pageToken != "":
		return issueSearchPage{isPageTokenRejected: true}, nil
	case !isSuccessStatus(response.statusCode):
		return issueSearchPage{}, errors.New(rejectionFailure(issueChangedOperationID, searchIssuesFailureSubject, response.statusCode, readJiraRejectedFieldIDs(response.body)).Message)
	case response.hasBodyReadFailed:
		return issueSearchPage{}, errors.New("Jira issue search response was interrupted")
	case response.isBodyTooLarge:
		return issueSearchPage{}, errors.New("Jira issue search response exceeds maxResponseBytes")
	case response.reflectsSecret:
		return issueSearchPage{}, errors.New("Jira issue search response contains credential material")
	}
	var page searchResponseBody
	if err := json.Unmarshal(response.body, &page); err != nil || len(page.NextPageToken) > maximumNextPageTokenBytes || !isPrintableASCII(page.NextPageToken) {
		return issueSearchPage{}, errors.New("Jira returned an invalid issue search page")
	}
	return issueSearchPage{issues: page.Issues, nextPageToken: page.NextPageToken}, nil
}

// jiraNow estimates Jira's clock, which evaluates relative JQL and stamps updated times.
func (fetcher *issueChangeFetcher) jiraNow() time.Time {
	return fetcher.client.now().Add(time.Duration(fetcher.jiraClockOffset.Load()))
}

// issueVersion identifies one issue state by its ID and updated instant.
type issueVersion struct {
	issueID       string
	updatedMillis int64
}

func (version issueVersion) key() string {
	return version.issueID + "@" + strconv.FormatInt(version.updatedMillis, 10)
}

func decodeIssueVersion(resource issueResource) (issueVersion, error) {
	var fields struct {
		Updated string `json:"updated"`
	}
	encoded, err := json.Marshal(resource.Fields)
	if err == nil {
		err = json.Unmarshal(encoded, &fields)
	}
	updated, timeErr := parseJiraTime(fields.Updated)
	if err != nil || timeErr != nil || updated.IsZero() || !numericIDPattern.MatchString(resource.ID) {
		return issueVersion{}, errors.New("Jira baseline search returned an issue without a valid ID and updated time")
	}
	return issueVersion{issueID: resource.ID, updatedMillis: updated.UnixMilli()}, nil
}

// recentSeenVersions merges versions into previous, keeps those within the lookback before
// updatedThrough, and keeps only the newest when they exceed the bound; a dropped entry can only cause
// a redelivery with the same event ID.
func recentSeenVersions(previous []string, versions []issueVersion, updatedThrough int64) []string {
	threshold := updatedThrough - issueChangeLookback.Milliseconds()
	byKey := map[string]int64{}
	for _, entry := range previous {
		separator := strings.LastIndexByte(entry, '@')
		if separator < 1 {
			continue
		}
		if millis, err := strconv.ParseInt(entry[separator+1:], 10, 64); err == nil {
			byKey[entry] = millis
		}
	}
	for _, version := range versions {
		byKey[version.key()] = version.updatedMillis
	}
	kept := make([]string, 0, len(byKey))
	for key, millis := range byKey {
		if millis >= threshold {
			kept = append(kept, key)
		}
	}
	sort.Slice(kept, func(i, j int) bool {
		if byKey[kept[i]] != byKey[kept[j]] {
			return byKey[kept[i]] > byKey[kept[j]]
		}
		return kept[i] < kept[j]
	})
	if len(kept) > maximumSeenIssueVersions {
		kept = kept[:maximumSeenIssueVersions]
	}
	sort.Strings(kept)
	return kept
}

func decodeIssueChangeCursor(cursor pollingtrigger.Cursor) (issueChangeCursor, error) {
	var position issueChangeCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil || position.CloudID == "" || position.UpdatedThrough <= 0 {
		return issueChangeCursor{}, errors.New("Jira issue cursor is not decodable")
	}
	if position.PageToken != "" && (position.SearchMinutes < 1 || position.SearchStartedAt <= 0 || position.SearchFloor <= 0) {
		return issueChangeCursor{}, errors.New("Jira issue cursor has a page token without its search")
	}
	return position, nil
}

func encodeIssueChangeCursor(position issueChangeCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Jira issue cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}
