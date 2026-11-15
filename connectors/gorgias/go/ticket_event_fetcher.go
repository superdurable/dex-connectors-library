// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	// TicketEventPageSize is the limit of each ticketChanged event request, Gorgias's maximum of 100.
	TicketEventPageSize = 100
	// TicketEventOverlap is how far before the committed high-water mark each poll starts reading, so an
	// event whose write commits a little after its created_datetime is still read. Events already seen in
	// that window are recognized by ID and not delivered again.
	TicketEventOverlap = 15 * time.Second
	// maximumSeenEventKeys bounds the cursor's dedupe set under the kit's 64 KiB cursor token limit even at
	// the longest key, a 19-digit event ID and a 16-digit timestamp (39 encoded bytes); past it the oldest
	// keys drop, and the Dex Flow target deduplicates any repeat by its stable event ID.
	maximumSeenEventKeys = 1500
	// maximumSeedPages bounds the seed listing of events recorded within the overlap before the baseline.
	maximumSeedPages = 10
	// maximumPagesPerPoll leaves most of the account's 40 requests per 20 seconds to operations.
	maximumPagesPerPoll = 5

	ticketChangedOperation = "ticketChanged"
	ticketEventObjectType  = "Ticket"
	// eventQueryTimeLayout writes created_datetime filters at Gorgias's microsecond precision, in UTC.
	eventQueryTimeLayout = "2006-01-02T15:04:05.000000Z07:00"
)

var errTicketEventCursorInvalid = errors.New("Gorgias ticketChanged cursor is not decodable")

// ticketEventFetcher reads Gorgias's event log in created_datetime order for the poll Trigger kit.
type ticketEventFetcher struct {
	client        *Client
	call          sdkgo.Call
	configuration TicketChangedTriggerConfiguration
}

// ticketEventCursor is the committed Trigger cursor. Since is the newest created_datetime read so far.
// ScanFrom is set only while the overlap window holds more than one full page of events. Seen holds
// "<event id>@<created_datetime Unix microseconds>" keys at or after Since minus TicketEventOverlap.
type ticketEventCursor struct {
	Since    string   `json:"since"`
	ScanFrom string   `json:"scanFrom,omitempty"`
	Seen     []string `json:"seen,omitempty"`
}

// ticketEventPage is one decoded GET /api/events response.
type ticketEventPage struct {
	events           []gorgiasEventWire
	createdAt        []time.Time
	serverTime       time.Time
	isRateBudgetLow  bool
	isPageSizeFilled bool
}

// gorgiasEventWire is one event of GET /api/events.
type gorgiasEventWire struct {
	ID              int64   `json:"id"`
	Type            string  `json:"type"`
	ObjectID        *int64  `json:"object_id"`
	ObjectType      *string `json:"object_type"`
	UserID          *int64  `json:"user_id"`
	Context         *string `json:"context"`
	CreatedDatetime string  `json:"created_datetime"`
}

func newTicketEventFetcher(client *Client, connection sdkgo.ConnectionRef, configuration TicketChangedTriggerConfiguration) *ticketEventFetcher {
	return &ticketEventFetcher{
		client:        client,
		call:          pollingtrigger.NewTriggerCredentialCall(connection, TicketChangedTriggerDefinition.Trigger),
		configuration: configuration,
	}
}

// SeedCursor records the baseline: the high-water mark is Gorgias's current time from the first response's
// Date header, or the local clock only without one, and events recorded within TicketEventOverlap before it
// are remembered as seen, so no earlier event is delivered. A local clock ahead of Gorgias's never moves the
// baseline past events Gorgias records afterwards.
func (fetcher *ticketEventFetcher) SeedCursor(ctx context.Context) (pollingtrigger.Cursor, error) {
	localNow := fetcher.client.now().UTC()
	start := localNow.Add(-TicketEventOverlap)
	seed := ticketEventCursor{}
	var baseline time.Time
	for pageNumber := 1; pageNumber <= maximumSeedPages; pageNumber++ {
		listed, err := fetcher.listEventPage(ctx, start)
		if err != nil {
			return pollingtrigger.Cursor{}, err
		}
		if pageNumber == 1 {
			baseline = listed.serverTime
			if baseline.IsZero() {
				baseline = localNow
			}
		}
		for index, event := range listed.events {
			seed.Seen = append(seed.Seen, seenEventKey(event.ID, listed.createdAt[index]))
			if listed.createdAt[index].After(baseline) {
				baseline = listed.createdAt[index]
			}
		}
		if !listed.isPageSizeFilled {
			break
		}
		start = nextScanStart(start, listed.createdAt[len(listed.createdAt)-1])
	}
	seed.Since = baseline.Format(time.RFC3339Nano)
	seed.Seen = pruneSeenEventKeys(seed.Seen, baseline.Add(-TicketEventOverlap))
	return encodeTicketEventCursor(seed)
}

// FetchChanges reads one page of ticket events recorded since the cursor's window start, oldest first. Only
// an undecodable cursor is marked invalid, so the kit records a new baseline; a created_datetime filter never
// expires, so a Gorgias 400 backs off and retries from the same position. A 429 pauses the connection for
// Retry-After.
func (fetcher *ticketEventFetcher) FetchChanges(ctx context.Context, cursor pollingtrigger.Cursor) (pollingtrigger.ChangePage[sdkgo.TriggerEvent[TicketChangedEvent]], error) {
	var page pollingtrigger.ChangePage[sdkgo.TriggerEvent[TicketChangedEvent]]
	position, since, start, err := decodeTicketEventCursor(cursor)
	if err != nil {
		return page, pollingtrigger.MarkCursorInvalid(err)
	}
	listed, err := fetcher.listEventPage(ctx, start)
	if err != nil {
		return page, err
	}
	seen := make(map[string]bool, len(position.Seen))
	for _, key := range position.Seen {
		seen[key] = true
	}
	next := ticketEventCursor{Seen: position.Seen}
	highWater := since
	for index, event := range listed.events {
		createdAt := listed.createdAt[index]
		key := seenEventKey(event.ID, createdAt)
		if seen[key] || createdAt.Before(start) {
			continue
		}
		seen[key] = true
		next.Seen = append(next.Seen, key)
		if createdAt.After(highWater) {
			highWater = createdAt
		}
		if change, isDelivered := fetcher.ticketChange(event, createdAt); isDelivered {
			page.Changes = append(page.Changes, newTicketChangedTriggerEvent(change))
		}
	}
	next.Since = highWater.Format(time.RFC3339Nano)
	if listed.isPageSizeFilled {
		// A full page may hide more events at or after its last one, so the next request starts there.
		next.ScanFrom = nextScanStart(start, listed.createdAt[len(listed.createdAt)-1]).Format(time.RFC3339Nano)
		// Near Gorgias's rate limit, the rest waits for the next scheduled poll.
		page.HasMorePages = !listed.isRateBudgetLow
	}
	next.Seen = pruneSeenEventKeys(next.Seen, highWater.Add(-TicketEventOverlap))
	page.Next, err = encodeTicketEventCursor(next)
	return page, err
}

// ticketChange converts an event the binding delivers; other types and non-ticket objects only move the cursor.
func (fetcher *ticketEventFetcher) ticketChange(event gorgiasEventWire, createdAt time.Time) (TicketChangedEvent, bool) {
	eventType := TicketEventType(event.Type)
	if !fetcher.configuration.DeliversEventType(eventType) || event.ObjectType == nil || *event.ObjectType != ticketEventObjectType ||
		event.ObjectID == nil || *event.ObjectID < 1 {
		return TicketChangedEvent{}, false
	}
	change := TicketChangedEvent{EventID: event.ID, EventType: eventType, TicketID: *event.ObjectID, OccurredAt: createdAt, Context: stringValue(event.Context)}
	if event.UserID != nil && *event.UserID > 0 {
		change.UserID = *event.UserID
	}
	return change, true
}

// nextScanStart is the start of the request after a full page ending at lastCreatedAt. When every event of
// the page shares start's microsecond, it steps one microsecond past it, the one case that can skip events.
func nextScanStart(start time.Time, lastCreatedAt time.Time) time.Time {
	if lastCreatedAt.After(start) {
		return lastCreatedAt
	}
	return start.Add(time.Microsecond)
}

// listEventPage sends one GET /api/events request for ticket events recorded at or after start.
func (fetcher *ticketEventFetcher) listEventPage(ctx context.Context, start time.Time) (ticketEventPage, error) {
	if err := pollingtrigger.WaitForPollRequest(ctx); err != nil {
		return ticketEventPage{}, err
	}
	credentials, err := fetcher.resolveCredentials(ctx)
	if err != nil {
		return ticketEventPage{}, err
	}
	query := url.Values{
		"order_by":              {"created_datetime:asc"},
		"created_datetime[gte]": {start.UTC().Format(eventQueryTimeLayout)},
		"limit":                 {strconv.Itoa(TicketEventPageSize)},
	}
	// Gorgias filters by every read type, so a binding's own selection never changes what the cursor passes.
	for _, eventType := range ticketEventTypes {
		query.Add("types", string(eventType))
	}
	result := fetcher.client.exchangeWithContext(ctx, credentials, ticketChangedOperation, gorgiasRequest{method: http.MethodGet, path: "/events", query: query})
	failure := errors.New("Gorgias event list request: " + result.failure.Message)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited:
		return ticketEventPage{}, pollingtrigger.MarkRateLimited(failure, result.retryAfter)
	default:
		return ticketEventPage{}, failure
	}
	listed, err := decodeEventPage(result.response.body)
	if err != nil {
		return ticketEventPage{}, fmt.Errorf("Gorgias event list page is invalid: %w", err)
	}
	if serverTime, err := http.ParseTime(result.response.header.Get("Date")); err == nil {
		listed.serverTime = serverTime.UTC()
	}
	listed.isRateBudgetLow = isRateBudgetLow(result.response.apiCallLimit)
	return listed, nil
}

// resolveCredentials reads the connection's API key with the poller's context when the provider supports it.
func (fetcher *ticketEventFetcher) resolveCredentials(ctx context.Context) (Credentials, error) {
	var credentials Credentials
	var err error
	if contextProvider, ok := fetcher.client.credentials.(sdkgo.ContextCredentialProvider[Credentials]); ok {
		credentials, err = contextProvider.ResolveContext(ctx, fetcher.call)
	} else {
		credentials, err = fetcher.client.credentials.Resolve(fetcher.call)
	}
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return Credentials{}, errors.New("Gorgias ticketChanged credentials are unavailable")
	}
	return credentials, nil
}

// decodeEventPage accepts the documented bare array and Gorgias's usual {"data": [...]} list envelope.
func decodeEventPage(body []byte) (ticketEventPage, error) {
	var events []gorgiasEventWire
	if err := json.Unmarshal(body, &events); err != nil {
		var listErr error
		if events, _, listErr = decodeListBody[gorgiasEventWire](body, "event"); listErr != nil {
			return ticketEventPage{}, errors.New("the response is neither an event array nor a Gorgias list")
		}
	}
	if len(events) > TicketEventPageSize {
		return ticketEventPage{}, fmt.Errorf("the page has %d events, more than %d", len(events), TicketEventPageSize)
	}
	listed := ticketEventPage{events: events, createdAt: make([]time.Time, len(events)), isPageSizeFilled: len(events) == TicketEventPageSize}
	for index, event := range events {
		if event.ID < 1 {
			return ticketEventPage{}, errors.New("an event has no ID")
		}
		createdAt, err := parseGorgiasTimestamp(event.CreatedDatetime)
		if err != nil {
			return ticketEventPage{}, errors.New("an event created_datetime is not an ISO 8601 timestamp")
		}
		// The cursor relies on ascending order; a page out of order would skip events.
		if index > 0 && createdAt.Before(listed.createdAt[index-1]) {
			return ticketEventPage{}, errors.New("the events are not in ascending created_datetime order")
		}
		listed.createdAt[index] = createdAt
	}
	return listed, nil
}

// isRateBudgetLow reports an X-Gorgias-Account-Api-Call-Limit value, such as 31/40, at three quarters or more.
func isRateBudgetLow(apiCallLimit string) bool {
	used, limit, found := strings.Cut(apiCallLimit, "/")
	if !found {
		return false
	}
	usedCount, usedErr := strconv.Atoi(used)
	limitCount, limitErr := strconv.Atoi(limit)
	return usedErr == nil && limitErr == nil && limitCount > 0 && usedCount*4 >= limitCount*3
}

func decodeTicketEventCursor(cursor pollingtrigger.Cursor) (ticketEventCursor, time.Time, time.Time, error) {
	var position ticketEventCursor
	if err := json.Unmarshal([]byte(cursor.Token), &position); err != nil {
		return position, time.Time{}, time.Time{}, errTicketEventCursorInvalid
	}
	since, err := time.Parse(time.RFC3339Nano, position.Since)
	if err != nil {
		return position, time.Time{}, time.Time{}, errTicketEventCursorInvalid
	}
	start := since.Add(-TicketEventOverlap)
	if position.ScanFrom != "" {
		scanFrom, err := time.Parse(time.RFC3339Nano, position.ScanFrom)
		if err != nil {
			return position, time.Time{}, time.Time{}, errTicketEventCursorInvalid
		}
		if scanFrom.After(start) {
			start = scanFrom
		}
	}
	return position, since.UTC(), start.UTC(), nil
}

func encodeTicketEventCursor(position ticketEventCursor) (pollingtrigger.Cursor, error) {
	encoded, err := json.Marshal(position)
	if err != nil {
		return pollingtrigger.Cursor{}, fmt.Errorf("encode Gorgias ticketChanged cursor: %w", err)
	}
	return pollingtrigger.Cursor{Token: string(encoded)}, nil
}

func seenEventKey(eventID int64, createdAt time.Time) string {
	return strconv.FormatInt(eventID, 10) + "@" + strconv.FormatInt(createdAt.UnixMicro(), 10)
}

// pruneSeenEventKeys keeps keys recorded at or after windowStart, the newest maximumSeenEventKeys.
func pruneSeenEventKeys(keys []string, windowStart time.Time) []string {
	type seenKey struct {
		key       string
		createdAt int64
	}
	kept := make([]seenKey, 0, len(keys))
	isKept := make(map[string]bool, len(keys))
	for _, key := range keys {
		_, createdText, found := strings.Cut(key, "@")
		if !found || isKept[key] {
			continue
		}
		createdAt, err := strconv.ParseInt(createdText, 10, 64)
		if err != nil || createdAt < windowStart.UnixMicro() {
			continue
		}
		isKept[key] = true
		kept = append(kept, seenKey{key: key, createdAt: createdAt})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].createdAt < kept[j].createdAt })
	kept = kept[max(len(kept)-maximumSeenEventKeys, 0):]
	if len(kept) == 0 {
		return nil
	}
	pruned := make([]string, len(kept))
	for index, entry := range kept {
		pruned[index] = entry.key
	}
	return pruned
}
