// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

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
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// eventSyncPageSize stays below pollingtrigger.MaxChangesPerPage and keeps a page of full events well
	// under the default response limit; Google's own default is also 250.
	eventSyncPageSize = 250
	// maximumBaselinePages bounds a first full sync at one million events.
	maximumBaselinePages = 4000
)

// eventSyncFetcher reads one calendar's events.list incremental sync feed for the poll Trigger kit.
type eventSyncFetcher struct {
	client     *Client
	call       sdkgo.Call
	calendarID string
}

// eventSyncCursor is the committed Trigger cursor. PageToken is set while an incremental sync spans pages:
// Google requires the next page to repeat the same syncToken query with the page token added.
type eventSyncCursor struct {
	CalendarID string `json:"calendarId"`
	SyncToken  string `json:"syncToken"`
	PageToken  string `json:"pageToken,omitempty"`
}

type googleEventSyncPage struct {
	NextPageToken string        `json:"nextPageToken"`
	NextSyncToken string        `json:"nextSyncToken"`
	Items         []googleEvent `json:"items"`
}

func newEventSyncFetcher(client *Client, connection sdkgo.ConnectionRef, configuration EventChangedTriggerConfiguration) *eventSyncFetcher {
	return &eventSyncFetcher{
		client:     client,
		call:       pollingtrigger.NewTriggerCredentialCall(connection, EventChangedTriggerDefinition.Trigger),
		calendarID: configuration.WatchedCalendarID(),
	}
}

// SeedCursor runs a full sync without filters and keeps only the final nextSyncToken, so existing events
// become the baseline and are never delivered.
func (fetcher *eventSyncFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	pageToken := ""
	requestedPageTokens := map[string]bool{}
	for page := 0; page < maximumBaselinePages; page++ {
		listed, err := fetcher.listSyncPage(ctx, "", pageToken)
		if err != nil {
			return pollingtrigger.Cursor{}, err
		}
		if listed.NextSyncToken != "" {
			return fetcher.encodeCursor(eventSyncCursor{CalendarID: fetcher.calendarID, SyncToken: listed.NextSyncToken})
		}
		if listed.NextPageToken == "" || requestedPageTokens[listed.NextPageToken] {
			return pollingtrigger.Cursor{}, errors.New("Google Calendar full sync page has neither a new page token nor a sync token")
		}
		requestedPageTokens[listed.NextPageToken] = true
		pageToken = listed.NextPageToken
	}
	return pollingtrigger.Cursor{}, fmt.Errorf("Google Calendar full sync exceeded %d pages", maximumBaselinePages)
}

// FetchChanges reads one incremental sync page after cursor. An expired sync token (HTTP 410), an
// undecodable cursor, or a cursor for another calendar is marked invalid, so the kit records a new baseline.
func (fetcher *eventSyncFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[EventChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[EventChangedEvent]]
	var position eventSyncCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil || position.SyncToken == "" {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Google Calendar sync cursor is not decodable"))
	}
	if position.CalendarID != fetcher.calendarID {
		return page, pollingtrigger.MarkCursorInvalid(errors.New("Google Calendar sync cursor belongs to another calendar"))
	}
	listed, err := fetcher.listSyncPage(ctx, position.SyncToken, position.PageToken)
	if err != nil {
		return page, err
	}
	if len(listed.Items) > pollingtrigger.MaxChangesPerPage {
		return page, fmt.Errorf("Google Calendar sync page has %d events, more than %d", len(listed.Items), pollingtrigger.MaxChangesPerPage)
	}
	now := fetcher.client.now()
	for _, item := range listed.Items {
		event, err := convertEvent(item, fetcher.calendarID)
		if err != nil {
			return page, fmt.Errorf("Google Calendar sync page has an invalid event: %w", err)
		}
		page.Changes = append(page.Changes, newEventChangedTriggerEvent(fetcher.calendarID, event, now))
	}
	next := eventSyncCursor{CalendarID: fetcher.calendarID}
	switch {
	case listed.NextPageToken != "":
		next.SyncToken, next.PageToken, page.HasMorePages = position.SyncToken, listed.NextPageToken, true
	case listed.NextSyncToken != "":
		next.SyncToken = listed.NextSyncToken
	default:
		return page, errors.New("Google Calendar sync page has neither a page token nor a sync token")
	}
	page.Next, err = fetcher.encodeCursor(next)
	return page, err
}

// listSyncPage sends one events.list request. Every request of a sync sequence uses the same parameters:
// maxResults, plus syncToken after the baseline and pageToken within a sequence. Recurring events stay
// unexpanded, and the filters Google forbids with syncToken are never sent.
func (fetcher *eventSyncFetcher) listSyncPage(ctx context.Context, syncToken string, pageToken string) (googleEventSyncPage, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return googleEventSyncPage{}, err
	}
	credentials, err := sdkgo.ResolveCredential(ctx, fetcher.client.credentials, fetcher.call, fetcher.client.refreshDriver)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return googleEventSyncPage{}, errors.New("Google Calendar Trigger credentials are unavailable")
	}
	query := url.Values{"maxResults": {strconv.Itoa(eventSyncPageSize)}}
	if syncToken != "" {
		query.Set("syncToken", syncToken)
	}
	if pageToken != "" {
		query.Set("pageToken", pageToken)
	}
	response, err := fetcher.client.sendWithContext(ctx, fetcher.call, &credentials, providerRequest{
		method: http.MethodGet, pathSuffix: calendarPath(fetcher.calendarID) + "/events", query: query,
	})
	if err != nil {
		return googleEventSyncPage{}, fmt.Errorf("Google Calendar sync request failed: %s", asProviderRequestError(err).message)
	}
	if response.statusCode == http.StatusGone && syncToken != "" {
		return googleEventSyncPage{}, pollingtrigger.MarkCursorInvalid(errors.New("Google Calendar sync token expired (HTTP 410)"))
	}
	if !isSuccessStatus(response.statusCode) {
		if reason, isThrottled := pollThrottleReason(response); isThrottled {
			// Quotas are per user and project; an HTTP-date Retry-After still pauses the connection.
			failure := fmt.Errorf("Google Calendar sync request: %s", describeStatus("provider rate limited the request", reason))
			return googleEventSyncPage{}, pollingtrigger.MarkRateLimited(failure,
				providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), fetcher.client.now()))
		}
		classification := classifyFailureStatus(response)
		return googleEventSyncPage{}, fmt.Errorf("Google Calendar sync request: %s", classification.message)
	}
	var listed googleEventSyncPage
	if err := json.Unmarshal(response.body, &listed); err != nil {
		return googleEventSyncPage{}, errors.New("Google Calendar sync page is not valid JSON")
	}
	return listed, nil
}

// pollThrottleReason reports a 429 or a usage-limit reason Google says to retry with backoff.
func pollThrottleReason(response providerResponse) (string, bool) {
	reason := googleErrorReason(response.body)
	switch reason {
	case "rateLimitExceeded", "userRateLimitExceeded", "quotaExceeded", "dailyLimitExceeded":
		return reason, true
	}
	return reason, response.statusCode == http.StatusTooManyRequests
}

func (fetcher *eventSyncFetcher) encodeCursor(position eventSyncCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Google Calendar sync cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}
