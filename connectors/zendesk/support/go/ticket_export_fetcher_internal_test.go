// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go/internal/zendeskexportfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

var exportTestConnection = sdkgo.ConnectionRef{Provider: providerName, Name: "zendesk-ticket-watcher"}

func TestSeedDrainsExistingChangesAndFetchDeliversLaterOnes(t *testing.T) {
	fake := zendeskexportfake.New(t)
	fake.Create(101, "Existing ticket")
	fetcher := newExportTestFetcher(t, fake, TicketChangedTriggerConfiguration{}, 0)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Contains(t, cursor.Token, fake.Cursor(), "the baseline ends at the latest change")

	fake.Create(102, "Double charge")
	fake.Update(101, "pending")
	fake.Create(103, "Spam")
	fake.Delete(103)
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.False(t, page.HasMorePages)
	require.Len(t, page.Changes, 2, "a deleted ticket is excluded by default")
	require.Equal(t, int64(102), page.Changes[0].Payload.Ticket.ID)
	require.Equal(t, TicketChangeCreated, page.Changes[0].Payload.ChangeKind)
	require.Equal(t, "https://acme.zendesk.com/agent/tickets/102", page.Changes[0].Payload.Ticket.AgentURL)
	require.Equal(t, TicketChangeUpdated, page.Changes[1].Payload.ChangeKind)
	require.Equal(t, TicketStatusPending, page.Changes[1].Payload.Ticket.Status)
	require.False(t, page.Changes[1].Payload.GeneratedAt.IsZero())

	requests := fake.Requests()
	seed, poll := requests[0], requests[len(requests)-1]
	require.NotEmpty(t, seed.Query.Get("start_time"))
	require.Empty(t, seed.Query.Get("cursor"))
	require.Equal(t, "100", poll.Query.Get("per_page"))
	require.Equal(t, "true", poll.Query.Get("exclude_deleted"))
	require.NotEmpty(t, poll.Query.Get("cursor"))

	again, err := fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Empty(t, again.Changes)
	require.Equal(t, page.Next, again.Next, "a caught-up poll leaves the cursor where it was")
}

func TestIncludeDeletedDeliversDeletions(t *testing.T) {
	fake := zendeskexportfake.New(t)
	fetcher := newExportTestFetcher(t, fake, TicketChangedTriggerConfiguration{IncludeDeleted: true}, 0)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Create(201, "Spam")
	fake.Delete(201)
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, 1)
	require.Equal(t, TicketChangeDeleted, page.Changes[0].Payload.ChangeKind)
	require.Equal(t, TicketStatusDeleted, page.Changes[0].Payload.Ticket.Status)
	require.Empty(t, fake.Requests()[1].Query.Get("exclude_deleted"))
}

func TestRejectedCursorRestartsAfterItsLastChangeTime(t *testing.T) {
	fake := zendeskexportfake.New(t)
	fetcher := newExportTestFetcher(t, fake, TicketChangedTriggerConfiguration{}, 0)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Create(301, "First")
	first, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, first.Changes, 1)

	fake.InvalidateCursors()
	fake.Create(302, "In the gap")
	restarted, err := fetcher.FetchChanges(context.Background(), first.Next)
	require.NoError(t, err)
	last := restarted.Changes[len(restarted.Changes)-1]
	require.Equal(t, int64(302), last.Payload.Ticket.ID, "the restart loses nothing")
	if len(restarted.Changes) == 2 {
		// A restart within the last minute repeats it; the repeated ticket version keeps its event ID.
		require.Equal(t, first.Changes[0].ID, restarted.Changes[0].ID)
	}
	requests := fake.Requests()
	require.Equal(t, http.StatusUnprocessableEntity, requests[len(requests)-2].Status)
	require.NotEmpty(t, requests[len(requests)-1].Query.Get("start_time"))
}

func TestTicketChangeIDIsAPureFunctionOfTheTicketVersion(t *testing.T) {
	fake := zendeskexportfake.New(t)
	fake.Create(351, "Repeated read")
	first, err := newExportTestFetcher(t, fake, TicketChangedTriggerConfiguration{}, 0).FetchChanges(context.Background(), pollingtrigger.Cursor{Token: `{"resumeAt":1}`})
	require.NoError(t, err)
	second, err := newExportTestFetcher(t, fake, TicketChangedTriggerConfiguration{}, 0).FetchChanges(context.Background(), pollingtrigger.Cursor{Token: `{"resumeAt":1}`})
	require.NoError(t, err)
	require.Len(t, first.Changes, 1)
	require.Equal(t, first.Changes[0].ID, second.Changes[0].ID, "a refetch yields the same event ID")
	fake.Update(351, "open")
	third, err := newExportTestFetcher(t, fake, TicketChangedTriggerConfiguration{}, 0).FetchChanges(context.Background(), pollingtrigger.Cursor{Token: `{"resumeAt":1}`})
	require.NoError(t, err)
	require.NotEqual(t, first.Changes[0].ID, third.Changes[0].ID, "a new ticket version is a new event")
}

func TestUndecodableCursorIsInvalid(t *testing.T) {
	fetcher := newExportTestFetcher(t, zendeskexportfake.New(t), TicketChangedTriggerConfiguration{}, 0)
	_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: "not-json"})
	require.True(t, pollingtrigger.IsCursorInvalid(err))
}

func TestRateLimitAndAgentRejection(t *testing.T) {
	fake := zendeskexportfake.New(t)
	fetcher := newExportTestFetcher(t, fake, TicketChangedTriggerConfiguration{}, 0)
	fake.RateLimitNextRequests(1, "17")
	_, err := fetcher.SeedCursor(context.Background())
	var rateLimited *pollingtrigger.RateLimitedError
	require.True(t, errors.As(err, &rateLimited))
	require.Equal(t, 17*time.Second, rateLimited.RetryAfter)

	fake.RejectAsAgent(true)
	_, err = fetcher.SeedCursor(context.Background())
	require.ErrorContains(t, err, "only for administrators")
	require.NotContains(t, err.Error(), zendeskexportfake.APIToken)
	require.False(t, pollingtrigger.IsCursorInvalid(err))
}

func TestPagesFollowAfterCursorUntilEndOfStream(t *testing.T) {
	fake := zendeskexportfake.New(t)
	fetcher := newExportTestFetcher(t, fake, TicketChangedTriggerConfiguration{}, 0)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.LimitPerPage(2)
	for ticketID := int64(401); ticketID <= 403; ticketID++ {
		fake.Create(ticketID, "Burst")
	}
	first, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, first.Changes, 2)
	require.True(t, first.HasMorePages)
	second, err := fetcher.FetchChanges(context.Background(), first.Next)
	require.NoError(t, err)
	require.Len(t, second.Changes, 1)
	require.Equal(t, int64(403), second.Changes[0].Payload.Ticket.ID)
	require.False(t, second.HasMorePages)
}

func TestOversizedPagesHalvePerPageBeforeFailing(t *testing.T) {
	fake := zendeskexportfake.New(t)
	for ticketID := int64(501); ticketID <= 520; ticketID++ {
		fake.Create(ticketID, "Large backlog")
	}
	fetcher := newExportTestFetcher(t, fake, TicketChangedTriggerConfiguration{}, 512)
	_, err := fetcher.SeedCursor(context.Background())
	require.ErrorContains(t, err, "raise maxResponseBytes")
	var pageSizes []string
	for _, request := range fake.Requests() {
		pageSizes = append(pageSizes, request.Query.Get("per_page"))
	}
	require.Equal(t, []string{"100", "50", "25", "12", "10"}, pageSizes)
}

func TestChangesTiedInOneSecondSurviveACursorResetAcrossAPageBoundary(t *testing.T) {
	fake := zendeskexportfake.New(t)
	fetcher := newExportTestFetcher(t, fake, TicketChangedTriggerConfiguration{}, 0)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.LimitPerPage(2)
	fake.TieChanges(true)
	for ticketID := int64(601); ticketID <= 603; ticketID++ {
		fake.Create(ticketID, "Same second")
	}
	first, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.True(t, first.HasMorePages)
	require.Len(t, first.Changes, 2)

	fake.InvalidateCursors()
	fake.LimitPerPage(1000)
	restarted, err := fetcher.FetchChanges(context.Background(), first.Next)
	require.NoError(t, err)
	var ticketIDs []int64
	for _, change := range restarted.Changes {
		ticketIDs = append(ticketIDs, change.Payload.Ticket.ID)
	}
	require.Equal(t, []int64{601, 602, 603}, ticketIDs, "the tied third ticket is not skipped")
	require.Equal(t, first.Changes[0].ID, restarted.Changes[0].ID, "a repeated ticket version keeps its event ID")
}

func TestSeedToleratesAWorkerClockAheadOfZendesk(t *testing.T) {
	fake := zendeskexportfake.New(t)
	fetcher := newExportTestFetcher(t, fake, TicketChangedTriggerConfiguration{}, 0)
	fetcher.client.now = func() time.Time { return time.Now().Add(3 * time.Minute) }
	_, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err, "a start_time five minutes before a clock three minutes ahead is still accepted")
}

func TestRejectedStartTimeRestartKeepsTheCursor(t *testing.T) {
	fake := zendeskexportfake.New(t)
	fetcher := newExportTestFetcher(t, fake, TicketChangedTriggerConfiguration{}, 0)
	fetcher.client.now = func() time.Time { return time.Now().Add(3 * time.Minute) }
	token := `{"resumeAt":` + strconv.FormatInt(time.Now().Unix(), 10) + `}`
	_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
	require.Error(t, err)
	require.False(t, pollingtrigger.IsCursorInvalid(err), "a new baseline would lose the changes after resumeAt")
	require.Equal(t, http.StatusBadRequest, fake.Requests()[0].Status)
}

func newExportTestFetcher(t *testing.T, fake *zendeskexportfake.Account, configuration TicketChangedTriggerConfiguration, maxResponseBytes int64) *ticketExportFetcher {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{exportTestConnection: {
		Email: zendeskexportfake.Email, APIToken: sdkgo.NewSecretString(zendeskexportfake.APIToken),
	}}
	client, err := New(Config{Subdomain: "acme", MaxResponseBytes: maxResponseBytes}, credentials,
		WithAPIBaseURL(fake.APIBaseURL()), WithIncrementalExportRequestsPerMinute(600))
	require.NoError(t, err)
	return newTicketExportFetcher(client, exportTestConnection, configuration)
}
