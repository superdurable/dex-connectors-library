// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias/go/internal/gorgiaseventfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const (
	triggerTestEmail  = "agent@example.com"
	triggerTestAPIKey = "gorgiasTriggerKey0123456789"
)

var triggerTestConnection = sdkgo.ConnectionRef{Provider: "gorgias", Name: "gorgias-trigger"}

func newTriggerTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{Domain: "acme"}, sdkgo.StaticCredentialProvider[Credentials]{
		triggerTestConnection: {Email: triggerTestEmail, APIKey: sdkgo.NewSecretString(triggerTestAPIKey)},
	}, WithAPIBaseURL(baseURL))
	require.NoError(t, err)
	return client
}

func newEventTestFetcher(t *testing.T, fake *gorgiaseventfake.Helpdesk, configuration TicketChangedTriggerConfiguration) *ticketEventFetcher {
	t.Helper()
	return newTicketEventFetcher(newTriggerTestClient(t, fake.BaseURL), triggerTestConnection, configuration)
}

func decodeEventCursor(t *testing.T, cursor pollingtrigger.Cursor) ticketEventCursor {
	t.Helper()
	var position ticketEventCursor
	require.NoError(t, json.Unmarshal([]byte(cursor.Token), &position))
	return position
}

func changeIDs(page pollingtrigger.ChangePage[sdkgo.TriggerEvent[TicketChangedEvent]]) []string {
	ids := []string{}
	for _, change := range page.Changes {
		ids = append(ids, change.ID)
	}
	return ids
}

func TestTicketEventBaselineDeliversNoEarlierEvent(t *testing.T) {
	fake := gorgiaseventfake.New(t, triggerTestEmail, triggerTestAPIKey)
	now := time.Now().UTC()
	fake.RecordEventAt("ticket-created", "Ticket", 7001, 0, now.Add(-time.Hour))
	recent := fake.RecordEventAt("ticket-updated", "Ticket", 7001, 12, now.Add(-5*time.Second))
	fetcher := newEventTestFetcher(t, fake, TicketChangedTriggerConfiguration{})

	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	position := decodeEventCursor(t, cursor)
	require.Len(t, position.Seen, 1, "only the event inside the overlap window is remembered")
	require.Contains(t, position.Seen[0], "9002@")
	require.Equal(t, int64(9002), recent)

	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "events recorded before the baseline are never delivered")
	require.False(t, page.HasMorePages)
	require.Equal(t, cursor.Token, page.Next.Token, "an empty poll leaves the cursor where it was")

	query := fake.Requests()[0].Query
	require.Equal(t, "created_datetime:asc", query.Get("order_by"))
	require.Equal(t, "100", query.Get("limit"))
	require.Len(t, query["types"], len(TicketEventTypes()))
	require.NotContains(t, query["types"], "ticket-message-created")
}

func TestTicketEventsArriveOnceInOrderWithStableIDsAndBindingFilter(t *testing.T) {
	fake := gorgiaseventfake.New(t, triggerTestEmail, triggerTestAPIKey)
	fetcher := newEventTestFetcher(t, fake, TicketChangedTriggerConfiguration{EventTypes: []TicketEventType{TicketEventCreated, TicketEventClosed}})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	created := fake.RecordTicketEvent("ticket-created", 7002, 0)
	fake.RecordTicketEvent("ticket-updated", 7002, 12)
	closed := fake.RecordTicketEvent("ticket-closed", 7002, 12)
	fake.RecordEventAt("ticket-created", "TicketMessage", 88, 0, time.Now())
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, []string{ticketEventKey(created), ticketEventKey(closed)}, changeIDs(page), "the update and the non-ticket object only move the cursor")
	first := page.Changes[0].Payload
	require.Equal(t, TicketChangedEvent{EventID: created, EventType: TicketEventCreated, TicketID: 7002, OccurredAt: first.OccurredAt, Context: "dd4ff312-69df-494a-be96-1a58b3d8b8e0"}, first)
	require.Equal(t, int64(12), page.Changes[1].Payload.UserID)

	refetched, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, changeIDs(page), changeIDs(refetched), "a refetch from the same cursor yields the same IDs")

	again, err := fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Empty(t, again.Changes, "events inside the overlap window are not delivered twice")
}

func TestFullPagesScanForwardInsideTheOverlapWindow(t *testing.T) {
	fake := gorgiaseventfake.New(t, triggerTestEmail, triggerTestAPIKey)
	fetcher := newEventTestFetcher(t, fake, TicketChangedTriggerConfiguration{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	base := time.Now().UTC()
	for index := range TicketEventPageSize + 30 {
		fake.RecordEventAt("ticket-updated", "Ticket", int64(7000+index), 0, base.Add(time.Duration(index)*time.Millisecond))
	}

	delivered := map[string]bool{}
	for poll := 0; poll < 4; poll++ {
		page, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		for _, id := range changeIDs(page) {
			require.False(t, delivered[id], "event %s delivered twice", id)
			delivered[id] = true
		}
		cursor = page.Next
		if !page.HasMorePages && decodeEventCursor(t, cursor).ScanFrom == "" {
			break
		}
	}
	require.Len(t, delivered, TicketEventPageSize+30)
	require.Empty(t, decodeEventCursor(t, cursor).ScanFrom, "a short page ends the scan")
}

func TestLowRateBudgetDefersTheNextPage(t *testing.T) {
	fake := gorgiaseventfake.New(t, triggerTestEmail, triggerTestAPIKey)
	fetcher := newEventTestFetcher(t, fake, TicketChangedTriggerConfiguration{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	for index := range TicketEventPageSize {
		fake.RecordTicketEvent("ticket-updated", int64(7000+index), 0)
	}
	fake.SetAPICallLimit("30/40")
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, TicketEventPageSize)
	require.False(t, page.HasMorePages, "three quarters of the window is spent, so the rest waits for the next poll")
	require.NotEmpty(t, decodeEventCursor(t, page.Next).ScanFrom)
	require.False(t, isRateBudgetLow("29/40"))
	require.False(t, isRateBudgetLow("bogus"))
}

func TestEventListFailuresMapToKitSignals(t *testing.T) {
	fake := gorgiaseventfake.New(t, triggerTestEmail, triggerTestAPIKey)
	fetcher := newEventTestFetcher(t, fake, TicketChangedTriggerConfiguration{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	fake.FailNextEvents(http.StatusTooManyRequests, "7", `{"error":{"msg":"rate_limited"}}`)
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	var rateLimited *pollingtrigger.RateLimitedError
	require.True(t, errors.As(err, &rateLimited))
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)

	fake.FailNextEvents(http.StatusBadRequest, "", `{"error":{"msg":"invalid_query","data":{"created_datetime":["bad"]}}}`)
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.Error(t, err)
	require.False(t, pollingtrigger.IsCursorInvalid(err), "a created_datetime filter never expires, so a 400 retries from the same position")

	fake.FailNextEvents(http.StatusForbidden, "", `{"error":{"msg":"forbidden"}}`)
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.Error(t, err)
	require.False(t, pollingtrigger.IsCursorInvalid(err), "a permission problem backs off without losing the position")
	require.NotContains(t, err.Error(), triggerTestAPIKey)

	fake.FailNextEvents(http.StatusOK, "", `[{"id":2,"type":"ticket-created","created_datetime":"2026-10-20T09:00:01"},{"id":1,"type":"ticket-created","created_datetime":"2026-10-20T09:00:00"}]`)
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.ErrorContains(t, err, "ascending")

	_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: "not json"})
	require.True(t, pollingtrigger.IsCursorInvalid(err))
}

func TestDecodeEventPageAcceptsTheListEnvelope(t *testing.T) {
	listed, err := decodeEventPage([]byte(`{"data":[{"id":5,"type":"ticket-created","object_type":"Ticket","object_id":7,"created_datetime":"2026-10-20T09:00:00.123456"}],"meta":{"next_cursor":null}}`))
	require.NoError(t, err)
	require.Len(t, listed.events, 1)
	require.Equal(t, time.Date(2026, 10, 20, 9, 0, 0, 123456000, time.UTC), listed.createdAt[0], "an offset-less timestamp is UTC")
	_, err = decodeEventPage([]byte(`{"error":"x"}`))
	require.Error(t, err)
	_, err = decodeEventPage([]byte(`[{"id":0,"created_datetime":"2026-10-20T09:00:00"}]`))
	require.ErrorContains(t, err, "no ID")
}

func TestTicketChangedConfigurationValidation(t *testing.T) {
	require.NoError(t, TicketChangedTriggerConfiguration{}.Validate())
	require.ErrorContains(t, TicketChangedTriggerConfiguration{EventTypes: []TicketEventType{"ticket-message-created"}}.Validate(), "not a ticket event type")
	require.ErrorContains(t, TicketChangedTriggerConfiguration{EventTypes: []TicketEventType{TicketEventClosed, TicketEventClosed}}.Validate(), "twice")
	require.True(t, TicketChangedTriggerConfiguration{}.DeliversEventType(TicketEventMerged))
	require.False(t, TicketChangedTriggerConfiguration{}.DeliversEventType("ticket-message-created"))
	require.False(t, TicketChangedTriggerConfiguration{EventTypes: []TicketEventType{TicketEventCreated}}.DeliversEventType(TicketEventUpdated))
}

func TestPruneSeenEventKeysKeepsTheWindowNewestFirstBound(t *testing.T) {
	windowStart := time.UnixMicro(1_000_000)
	keys := []string{"1@999999", "2@1000000", "2@1000000", "3@1000001", "bogus"}
	require.Equal(t, []string{"2@1000000", "3@1000001"}, pruneSeenEventKeys(keys, windowStart))
	many := make([]string, 0, maximumSeenEventKeys+5)
	for index := range maximumSeenEventKeys + 5 {
		many = append(many, seenEventKey(int64(index+1), windowStart.Add(time.Duration(index)*time.Microsecond)))
	}
	pruned := pruneSeenEventKeys(many, windowStart)
	require.Len(t, pruned, maximumSeenEventKeys)
	require.Equal(t, many[5], pruned[0], "the oldest keys drop first")
	require.Equal(t, windowStart.Add(time.Microsecond), nextScanStart(windowStart, windowStart))
}

func TestBaselineFollowsGorgiasClockWhenTheWorkerClockRunsAhead(t *testing.T) {
	fake := gorgiaseventfake.New(t, triggerTestEmail, triggerTestAPIKey)
	fetcher := newEventTestFetcher(t, fake, TicketChangedTriggerConfiguration{})
	fetcher.client.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	since, err := time.Parse(time.RFC3339Nano, decodeEventCursor(t, cursor).Since)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), since, 2*time.Second, "the baseline is Gorgias's Date header, not the Worker clock")

	recorded := fake.RecordTicketEvent("ticket-created", 7003, 0)
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, []string{ticketEventKey(recorded)}, changeIDs(page), "an event Gorgias records after the baseline is delivered")
}

func TestFullSeenSetFitsTheCursorTokenBound(t *testing.T) {
	createdAt := time.Date(2286, 11, 20, 17, 46, 39, 999999000, time.UTC)
	keys := make([]string, 0, maximumSeenEventKeys)
	for index := range maximumSeenEventKeys {
		keys = append(keys, seenEventKey(math.MaxInt64-int64(index), createdAt))
	}
	since := createdAt.Format(time.RFC3339Nano)
	cursor, err := encodeTicketEventCursor(ticketEventCursor{Since: since, ScanFrom: since, Seen: pruneSeenEventKeys(keys, createdAt.Add(-TicketEventOverlap))})
	require.NoError(t, err)
	require.LessOrEqual(t, len(cursor.Token), projectconfig.MaxPollCursorTokenBytes, "the longest keys still fit the kit's token bound")
}
