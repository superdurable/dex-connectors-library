// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go/internal/desksearchfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

var fetcherStart = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

const fetcherDepartmentID = "1892000000006907"

func newFetcherUnderTest(t *testing.T, departmentID string) (*ticketChangeFetcher, *desksearchfake.Desk) {
	t.Helper()
	fake := desksearchfake.New(t, fetcherStart)
	reference := sdkgo.ConnectionRef{Provider: "zoho", Name: "zoho-desk-fetcher"}
	client, err := New(Config{OrgID: desksearchfake.OrganizationID, PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AuthMethodID: USDataCenterAuthMethodID, AccessToken: sdkgo.NewSecretString(desksearchfake.AccessToken)},
	}, WithAPIBaseURL(fake.URL+"/api/v1"))
	require.NoError(t, err)
	client.now = fake.ClientNow
	return newTicketChangeFetcher(client, reference, TicketChangedTriggerConfiguration{DepartmentID: departmentID}), fake
}

func fetchAll(t *testing.T, fetcher *ticketChangeFetcher, cursor pollingtrigger.Cursor) ([]sdkgo.TriggerEvent[TicketChangedEvent], pollingtrigger.Cursor) {
	t.Helper()
	var events []sdkgo.TriggerEvent[TicketChangedEvent]
	for page := 0; page < 50; page++ {
		changes, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		events, cursor = append(events, changes.Changes...), changes.Next
		if !changes.HasMorePages {
			return events, cursor
		}
	}
	t.Fatal("the sweep never ended")
	return nil, cursor
}

func eventTicketIDs(events []sdkgo.TriggerEvent[TicketChangedEvent]) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.Payload.Ticket.ID)
	}
	return ids
}

func TestSeedRecordsRecentTicketsAndDeliversOnlyLaterVersions(t *testing.T) {
	fetcher, fake := newFetcherUnderTest(t, "")
	existing := fake.Create("Existing", fetcherDepartmentID)
	fake.Advance(time.Minute)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Len(t, cursor.Snapshot.Entries, 1, "the lookback window is the baseline")
	require.Equal(t, "2026-03-02T08:56:00.000Z,2026-03-02T09:01:00.000Z", fake.Searches()[0].ModifiedTimeRange)

	fake.Advance(time.Minute)
	created := fake.Create("Created after the baseline", fetcherDepartmentID)
	events, cursor := fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{created}, eventTicketIDs(events), "the baseline ticket is re-read but not delivered")
	require.Equal(t, TicketChangeCreated, events[0].Payload.ChangeKind)
	require.Equal(t, desksearchfake.OrganizationID, events[0].Payload.OrgID)
	require.Empty(t, events[0].Payload.Ticket.DescriptionHTML)

	fake.Advance(time.Minute)
	fake.Update(existing, "On Hold")
	events, cursor = fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{existing}, eventTicketIDs(events))
	require.Equal(t, TicketChangeUpdated, events[0].Payload.ChangeKind)
	require.Equal(t, "On Hold", string(events[0].Payload.Ticket.Status))

	again, _ := fetchAll(t, fetcher, cursor)
	require.Empty(t, again, "an unchanged ticket is never delivered twice")
}

func TestEventIDIsAPureFunctionOfTheTicketVersion(t *testing.T) {
	fetcher, fake := newFetcherUnderTest(t, "")
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Advance(time.Second)
	fake.Create("Refetched", "")
	first, _ := fetchAll(t, fetcher, cursor)
	refetched, _ := fetchAll(t, fetcher, cursor)
	require.Len(t, first, 1)
	require.Equal(t, first[0].ID, refetched[0].ID, "a refetch after a crash yields the same Trigger event ID")
}

func TestLateIndexedTicketIsDeliveredWithinTheLookback(t *testing.T) {
	fetcher, fake := newFetcherUnderTest(t, "")
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Advance(time.Second)
	late := fake.Create("Indexed late", "")
	fake.HideFromSearch(late, true)
	fake.Advance(time.Second)
	visible := fake.Create("Indexed at once", "")
	events, cursor := fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{visible}, eventTicketIDs(events))

	fake.Advance(2 * time.Minute)
	fake.HideFromSearch(late, false)
	events, _ = fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{late}, eventTicketIDs(events), "the next sweep starts before the newest modified time it read")
}

func TestBaselineFollowsZohoClockWhenTheLocalClockRunsAhead(t *testing.T) {
	fetcher, fake := newFetcherUnderTest(t, "")
	fake.SkewClientClock(10 * time.Minute)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Advance(time.Minute)
	changed := fake.Create("Changed after the baseline by Zoho's clock", "")
	events, cursor := fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{changed}, eventTicketIDs(events), "the next sweep starts from Zoho Desk's Date, not the local clock")

	fake.SkewClientClock(-10 * time.Minute)
	fake.Advance(time.Minute)
	later := fake.Create("Changed while the local clock runs behind", "")
	events, cursor = fetchAll(t, fetcher, cursor)
	require.Empty(t, events, "a ticket modified after the local clock is outside the range until the clock reaches it")
	fake.SkewClientClock(0)
	events, _ = fetchAll(t, fetcher, cursor)
	require.Equal(t, []string{later}, eventTicketIDs(events), "the floor never passed the ticket, so it arrives late, not never")
}

func TestTicketListedTwiceOnAPageIsDeliveredOnce(t *testing.T) {
	fetcher, fake := newFetcherUnderTest(t, "")
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Advance(time.Second)
	instant := fake.Now()
	var expected []string
	for index := 0; index < ticketPollPageLimit; index++ {
		expected = append(expected, fake.CreateAt("Busy minute", "", instant.Add(time.Duration(index)*time.Millisecond)))
	}
	fake.ListTwice(expected[0])
	fake.Advance(time.Minute)
	events, _ := fetchAll(t, fetcher, cursor)
	require.ElementsMatch(t, expected, eventTicketIDs(events), "the duplicate fills a slot but the full page still pages on")
	require.Greater(t, len(fake.Searches()), 2)
}

func TestSweepPagesByModifiedTimeAndByOffsetWithinOneInstant(t *testing.T) {
	fetcher, fake := newFetcherUnderTest(t, fetcherDepartmentID)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	instant := fake.Now().Add(time.Second)
	var expected []string
	for index := 0; index < 150; index++ {
		expected = append(expected, fake.CreateAt("Same instant", fetcherDepartmentID, instant))
	}
	for index := 0; index < 120; index++ {
		expected = append(expected, fake.CreateAt("Later", fetcherDepartmentID, instant.Add(time.Duration(index+1)*time.Millisecond)))
	}
	fake.CreateAt("Other department", "1892000000082069", instant)
	fake.Advance(time.Minute)
	events, _ := fetchAll(t, fetcher, cursor)
	require.ElementsMatch(t, expected, eventTicketIDs(events))
	for _, search := range fake.Searches()[1:] {
		require.Equal(t, fetcherDepartmentID, search.DepartmentID)
		require.Equal(t, 100, search.Limit)
		require.LessOrEqual(t, search.From, 900)
	}
	require.Contains(t, searchFroms(fake.Searches()), 100, "a full page of one instant pages by offset")
}

func TestRateLimitAndRangeRejectionAreMarkedForTheKit(t *testing.T) {
	fetcher, fake := newFetcherUnderTest(t, "")
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	fake.RateLimitNext(1, "30")
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	var limited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &limited)
	require.Equal(t, 30*time.Second, limited.RetryAfter)

	fake.RejectNextRanges(1)
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a rejected time range records a new baseline")
	require.NotContains(t, err.Error(), desksearchfake.AccessToken)
}

func TestCursorForAnotherDepartmentOrUndecodableIsInvalid(t *testing.T) {
	fetcher, _ := newFetcherUnderTest(t, "")
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	other, _ := newFetcherUnderTest(t, fetcherDepartmentID)
	_, err = other.FetchChanges(context.Background(), cursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err))

	_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: "not json"})
	require.True(t, pollingtrigger.IsCursorInvalid(err))
	require.True(t, errors.Is(err, errTicketCursorUndecodable))

	var stored ticketChangeCursor
	require.NoError(t, json.Unmarshal([]byte(cursor.Token), &stored))
	require.Equal(t, desksearchfake.OrganizationID, stored.OrgID)
}

func TestNextPositionStepsPastAnInstantBeyondTheOffsetLimit(t *testing.T) {
	instant := fetcherStart
	tickets := make([]Ticket, ticketPollPageLimit)
	for index := range tickets {
		tickets[index] = Ticket{ID: "1", ModifiedAt: instant}
	}
	page := ticketPollPage{tickets: tickets, received: len(tickets), last: instant}
	next, hasMorePages := nextTicketSearchPosition(ticketSearchPosition{sweepStart: instant, floor: instant, from: ticketPollMaximumFrom, highWater: instant}, page)
	require.True(t, hasMorePages)
	require.Equal(t, instant.Add(time.Millisecond), next.floor)
	require.Zero(t, next.from)
	require.Equal(t, instant, next.sweepStart)
}

func TestTicketChangedConfigurationRejectsANonNumericDepartment(t *testing.T) {
	require.NoError(t, TicketChangedTriggerConfiguration{}.Validate())
	require.NoError(t, TicketChangedTriggerConfiguration{DepartmentID: fetcherDepartmentID}.Validate())
	require.Error(t, TicketChangedTriggerConfiguration{DepartmentID: "Billing"}.Validate())
}

func searchFroms(searches []desksearchfake.Search) []int {
	froms := make([]int, 0, len(searches))
	for _, search := range searches {
		froms = append(froms, search.From)
	}
	return froms
}
