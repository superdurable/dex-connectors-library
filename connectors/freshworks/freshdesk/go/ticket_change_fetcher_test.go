// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go/internal/freshdeskticketfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const triggerTestAPIKey = "freshdeskTriggerKey0123456789"

var triggerTestConnection = sdkgo.ConnectionRef{Provider: "freshdesk", Name: "freshdesk-trigger"}

func newTriggerTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{Domain: "acme"}, sdkgo.StaticCredentialProvider[Credentials]{
		triggerTestConnection: {APIKey: sdkgo.NewSecretString(triggerTestAPIKey)},
	}, WithAPIBaseURL(baseURL))
	require.NoError(t, err)
	return client
}

func newTicketTestFetcher(t *testing.T, fake *freshdeskticketfake.Tickets, configuration TicketChangedTriggerConfiguration) *ticketChangeFetcher {
	t.Helper()
	return newTicketChangeFetcher(newTriggerTestClient(t, fake.BaseURL), triggerTestConnection, configuration)
}

func decodeTicketCursor(t *testing.T, cursor pollingtrigger.Cursor) ticketChangeCursor {
	t.Helper()
	var position ticketChangeCursor
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

func TestTicketChangeBaselineDeliversNoExistingTicket(t *testing.T) {
	fake := freshdeskticketfake.New(t, triggerTestAPIKey)
	now := time.Now().UTC().Truncate(time.Second)
	fake.CreateAt(1001, "Old", now.Add(-time.Hour), now.Add(-10*time.Minute))
	fake.CreateAt(1002, "Recent", now.Add(-time.Hour), now.Add(-20*time.Second))
	fetcher := newTicketTestFetcher(t, fake, TicketChangedTriggerConfiguration{})

	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	position := decodeTicketCursor(t, cursor)
	require.Contains(t, position.Delivered, deliveredTicketKey(Ticket{ID: 1002, UpdatedAt: now.Add(-20 * time.Second)}))
	require.NotContains(t, strings.Join(position.Delivered, ","), "1001@", "a ticket older than the overlap is outside every window")

	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "tickets changed before the baseline are never delivered")
	require.False(t, page.HasMorePages)

	query := fake.Requests()[0].Query
	require.Equal(t, "updated_at", query.Get("order_by"))
	require.Equal(t, "asc", query.Get("order_type"))
	require.Equal(t, "100", query.Get("per_page"))
	require.Equal(t, "1", query.Get("page"))
	require.Empty(t, query.Get("include"), "the list never pays extra API credits for descriptions")
}

func TestTicketChangesReportCreatedAndUpdatedTicketsWithStableIDs(t *testing.T) {
	fake := freshdeskticketfake.New(t, triggerTestAPIKey)
	now := time.Now().UTC().Truncate(time.Second)
	fake.CreateAt(1001, "Existing", now.Add(-time.Hour), now.Add(-10*time.Minute))
	fetcher := newTicketTestFetcher(t, fake, TicketChangedTriggerConfiguration{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Create(1002, "Double charge")
	fake.Update(1001, "Existing, escalated", 3)

	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, 2)
	kinds := map[int64]TicketChangeKind{}
	for _, change := range page.Changes {
		kinds[change.Payload.Ticket.ID] = change.Payload.ChangeKind
		require.Equal(t, change.Payload.Ticket.UpdatedAt, change.OccurredAt)
	}
	require.Equal(t, map[int64]TicketChangeKind{1001: TicketChangeUpdated, 1002: TicketChangeCreated}, kinds)

	refetched, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, changeIDs(page), changeIDs(refetched), "a refetch after a crash yields the same event IDs")

	again, err := fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Empty(t, again.Changes, "the overlap window never redelivers a version")

	fake.Update(1002, "Double charge, refunded", 4)
	later, err := fetcher.FetchChanges(context.Background(), again.Next)
	require.NoError(t, err)
	require.Len(t, later.Changes, 1)
	require.Equal(t, TicketChangeUpdated, later.Changes[0].Payload.ChangeKind)
	require.NotEqual(t, page.Changes[0].ID, later.Changes[0].ID)
}

func TestTicketChangesReadALateCommitInsideTheOverlap(t *testing.T) {
	fake := freshdeskticketfake.New(t, triggerTestAPIKey)
	fetcher := newTicketTestFetcher(t, fake, TicketChangedTriggerConfiguration{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	since, err := time.Parse(ticketListTimeLayout, decodeTicketCursor(t, cursor).UpdatedSince)
	require.NoError(t, err)
	fake.CreateAt(2001, "Committed late", since.Add(-30*time.Second), since.Add(-30*time.Second))

	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, []string{"freshdesk-ticket-2001-" + itoa64(since.Add(-30*time.Second).Unix())}, changeIDs(page))
	require.Equal(t, TicketChangeCreated, page.Changes[0].Payload.ChangeKind)
}

func TestTicketChangesPageThroughABulkUpdateSharingOneSecond(t *testing.T) {
	fake := freshdeskticketfake.New(t, triggerTestAPIKey)
	fetcher := newTicketTestFetcher(t, fake, TicketChangedTriggerConfiguration{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	bulkTime := time.Now().UTC().Truncate(time.Second).Add(time.Second)
	for ticketID := int64(3001); ticketID <= 3250; ticketID++ {
		fake.CreateAt(ticketID, "Bulk", bulkTime.Add(-time.Hour), bulkTime)
	}

	seen := map[string]bool{}
	pages := 0
	for {
		page, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		pages++
		for _, id := range changeIDs(page) {
			require.False(t, seen[id], "%s is delivered once", id)
			seen[id] = true
		}
		cursor = page.Next
		if !page.HasMorePages {
			break
		}
		require.Less(t, pages, 10, "paging makes progress")
	}
	require.Len(t, seen, 250)
	require.Equal(t, 4, pages, "one restart at the bulk second, then offsets through it")
	position := decodeTicketCursor(t, cursor)
	require.Zero(t, position.Page)
	require.Empty(t, position.WindowStart)
	require.LessOrEqual(t, len(position.Delivered), maximumDeliveredTicketKeys)
	require.Less(t, len(cursor.Token), 64<<10)
}

// drainTicketChanges fetches until no page remains and returns every delivered event ID once.
func drainTicketChanges(t *testing.T, fetcher *ticketChangeFetcher, cursor pollingtrigger.Cursor, beforeEachFetch func(fetch int)) (map[string]bool, pollingtrigger.Cursor) {
	t.Helper()
	seen := map[string]bool{}
	for fetch := 0; ; fetch++ {
		require.Less(t, fetch, 100, "draining makes progress")
		beforeEachFetch(fetch)
		page, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		for _, id := range changeIDs(page) {
			require.False(t, seen[id], "%s is delivered once", id)
			seen[id] = true
		}
		cursor = page.Next
		if !page.HasMorePages {
			return seen, cursor
		}
	}
}

func TestTicketChangesDrainABurstWithoutOffsetsSoAnUpdateMidDrainLosesNothing(t *testing.T) {
	fake := freshdeskticketfake.New(t, triggerTestAPIKey)
	fetcher := newTicketTestFetcher(t, fake, TicketChangedTriggerConfiguration{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	since, err := time.Parse(ticketListTimeLayout, decodeTicketCursor(t, cursor).UpdatedSince)
	require.NoError(t, err)
	// 250 tickets, five per second, all inside one overlap minute.
	for index := int64(0); index < 250; index++ {
		at := since.Add(time.Duration(1+index/5) * time.Second)
		fake.CreateAt(4001+index, "Burst", at, at)
	}
	movedAt := since.Add(10 * time.Minute)
	seen, cursor := drainTicketChanges(t, fetcher, cursor, func(fetch int) {
		if fetch == 1 {
			// A ticket on the first page changes, which would shift every later offset by one.
			fake.UpdateAt(4004, "Burst, escalated", movedAt)
		}
	})
	for index := int64(0); index < 250; index++ {
		at := since.Add(time.Duration(1+index/5) * time.Second)
		if index != 3 {
			require.True(t, seen["freshdesk-ticket-"+itoa64(4001+index)+"-"+itoa64(at.Unix())], "ticket %d", 4001+index)
		}
	}
	require.True(t, seen["freshdesk-ticket-4004-"+itoa64(movedAt.Unix())], "the moved ticket's new version arrives")
	for _, request := range fake.Requests() {
		require.Equal(t, "1", request.Query.Get("page"), "seconds that hold under a page never page by offset")
	}
	require.Empty(t, decodeTicketCursor(t, cursor).WindowStart)
}

func TestTicketChangesStopRereadingAfterABurstLargerThanTheDedupeSet(t *testing.T) {
	fake := freshdeskticketfake.New(t, triggerTestAPIKey)
	fetcher := newTicketTestFetcher(t, fake, TicketChangedTriggerConfiguration{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	since, err := time.Parse(ticketListTimeLayout, decodeTicketCursor(t, cursor).UpdatedSince)
	require.NoError(t, err)
	burst := maximumDeliveredTicketKeys + 300
	for index := range burst {
		at := since.Add(time.Duration(1+index/60) * time.Second)
		fake.CreateAt(int64(5001+index), "Bulk", at, at)
	}
	seen, cursor := drainTicketChanges(t, fetcher, cursor, func(int) {})
	require.Len(t, seen, burst)
	require.Less(t, len(cursor.Token), 64<<10)

	again, cursor := drainTicketChanges(t, fetcher, cursor, func(int) {})
	require.Empty(t, again, "versions the dedupe set dropped are not delivered again")

	// Once Freshdesk's clock passes the overlap, a complete read raises the floor past the burst.
	fake.AdvanceClock(3 * time.Minute)
	_, cursor = drainTicketChanges(t, fetcher, cursor, func(int) {})
	position := decodeTicketCursor(t, cursor)
	require.NotEmpty(t, position.Floor)
	require.Empty(t, position.Delivered)
	requestsBefore := fake.ListRequestCount()
	quiet, _ := drainTicketChanges(t, fetcher, cursor, func(int) {})
	require.Empty(t, quiet)
	require.Equal(t, 1, fake.ListRequestCount()-requestsBefore, "a quiet helpdesk costs one request per poll")
	require.Equal(t, position.Floor, fake.Requests()[len(fake.Requests())-1].Query.Get("updated_since"))
}

func TestTicketChangesStopAtFreshdesksLastPageOfOneSecond(t *testing.T) {
	position := ticketChangeCursor{UpdatedSince: "2026-03-02T09:00:00Z", WindowStart: "2026-03-02T09:00:00Z", Page: MaxTicketListPage}
	fake := freshdeskticketfake.New(t, triggerTestAPIKey)
	bulkTime := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	for ticketID := int64(1); ticketID <= 30001; ticketID++ {
		fake.CreateAt(ticketID, "Bulk", bulkTime.Add(-time.Hour), bulkTime)
	}
	fetcher := newTicketTestFetcher(t, fake, TicketChangedTriggerConfiguration{})
	cursor, err := encodeTicketChangeCursor(position)
	require.NoError(t, err)
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, TicketChangePageSize)
	require.True(t, page.HasMorePages, "the poll continues after the second Freshdesk cannot page past")
	next := decodeTicketCursor(t, page.Next)
	require.Zero(t, next.Page)
	floor, err := time.Parse(ticketListTimeLayout, next.Floor)
	require.NoError(t, err)
	require.True(t, floor.After(bulkTime), "the next window starts after the exhausted second")
}

func TestTicketChangeKindsFilterSkipsButStillPasses(t *testing.T) {
	fake := freshdeskticketfake.New(t, triggerTestAPIKey)
	now := time.Now().UTC().Truncate(time.Second)
	fake.CreateAt(1001, "Existing", now.Add(-time.Hour), now.Add(-10*time.Minute))
	fetcher := newTicketTestFetcher(t, fake, TicketChangedTriggerConfiguration{ChangeKinds: []TicketChangeKind{TicketChangeCreated}})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Update(1001, "Edited", 3)
	fake.Create(1002, "New")

	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, 1)
	require.Equal(t, int64(1002), page.Changes[0].Payload.Ticket.ID)
	require.Len(t, decodeTicketCursor(t, page.Next).Delivered, 2, "a skipped update still moves the cursor")
}

func TestTicketChangeFailuresMapToKitSignals(t *testing.T) {
	fake := freshdeskticketfake.New(t, triggerTestAPIKey)
	fetcher := newTicketTestFetcher(t, fake, TicketChangedTriggerConfiguration{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	fake.FailNextList(http.StatusTooManyRequests, "7", `{"code":"too_many_requests"}`)
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)

	fake.FailNextList(http.StatusBadRequest, "", `{"description":"Validation failed","errors":[{"field":"updated_since","code":"invalid_value","message":"x"}]}`)
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err))
	require.Contains(t, err.Error(), "updated_since=invalid_value")

	fake.FailNextList(http.StatusServiceUnavailable, "", `{}`)
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.Error(t, err)
	require.False(t, pollingtrigger.IsCursorInvalid(err))
	require.NotErrorAs(t, err, &rateLimited)

	for _, token := range []string{"not json", `{"updatedSince":"yesterday"}`, `{"updatedSince":"2026-03-02T09:00:00Z","page":1}`, `{"updatedSince":"2026-03-02T09:00:00Z","page":301,"windowStart":"2026-03-02T08:59:00Z"}`} {
		_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), token)
	}
	for _, request := range fake.Requests() {
		require.NotContains(t, request.Query.Encode(), triggerTestAPIKey)
	}
}

func TestTicketChangedConfigurationValidation(t *testing.T) {
	require.NoError(t, TicketChangedTriggerConfiguration{}.Validate())
	require.NoError(t, TicketChangedTriggerConfiguration{ChangeKinds: []TicketChangeKind{TicketChangeCreated, TicketChangeUpdated}}.Validate())
	require.ErrorContains(t, TicketChangedTriggerConfiguration{ChangeKinds: []TicketChangeKind{"deleted"}}.Validate(), "created or updated")
	require.ErrorContains(t, TicketChangedTriggerConfiguration{ChangeKinds: []TicketChangeKind{TicketChangeCreated, TicketChangeCreated}}.Validate(), "twice")
	require.True(t, TicketChangedTriggerConfiguration{}.DeliversChangeKind(TicketChangeUpdated))
	require.False(t, TicketChangedTriggerConfiguration{ChangeKinds: []TicketChangeKind{TicketChangeCreated}}.DeliversChangeKind(TicketChangeUpdated))
}

func TestPruneDeliveredTicketKeysKeepsTheNewestInsideTheWindow(t *testing.T) {
	windowStart := time.Unix(1000, 0)
	keys := []string{"1@999", "2@1000", "3@1001", "3@1001", "bad", "4@x"}
	pruned, floor := pruneDeliveredTicketKeys(keys, windowStart)
	require.Equal(t, []string{"2@1000", "3@1001"}, pruned)
	require.True(t, floor.IsZero(), "no key was dropped by the bound")
	many := make([]string, 0, maximumDeliveredTicketKeys+10)
	for index := range maximumDeliveredTicketKeys + 10 {
		many = append(many, itoa64(int64(index))+"@"+itoa64(2000+int64(index)))
	}
	pruned, floor = pruneDeliveredTicketKeys(many, windowStart)
	require.Len(t, pruned, maximumDeliveredTicketKeys)
	require.Equal(t, many[len(many)-1], pruned[len(pruned)-1])
	require.Equal(t, time.Unix(2010, 0).UTC(), floor, "the oldest kept second")

	sameSecond := []string{"1@3000", "2@3000"}
	for index := range maximumDeliveredTicketKeys {
		sameSecond = append(sameSecond, itoa64(int64(10+index))+"@"+itoa64(3000+int64(index%2)))
	}
	pruned, floor = pruneDeliveredTicketKeys(sameSecond, windowStart)
	require.Equal(t, time.Unix(3001, 0).UTC(), floor, "a partly dropped second is skipped whole")
	for _, key := range pruned {
		require.True(t, strings.HasSuffix(key, "@3001"), key)
	}
}

func itoa64(value int64) string { return strconv.FormatInt(value, 10) }
