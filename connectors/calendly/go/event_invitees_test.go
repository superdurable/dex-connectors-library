// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListEventInviteesReturnsOneBoundedPageAndItsToken(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"GET /scheduled_events/EVENT0001/invitees": respondJSON(http.StatusOK, fmt.Sprintf(`{"collection":[%s,%s],"pagination":{"count":2,"next_page_token":"tok_2"}}`,
			inviteeJSON("EVENT0001", "INVITEE01", "active"), inviteeJSON("EVENT0001", "INVITEE02", "canceled"))),
	})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	result, err := sdkgo.RunQuery(newStepContext("invitees"), client.ListEventInvitees(), testConnection, calendly.ListEventInviteesInput{
		ScheduledEventURI: testEventURI, Status: calendly.ScheduledEventStatusActive, Email: "ada@example.com", PageSize: 2, PageToken: "tok_1",
	})
	require.NoError(t, err)
	require.Equal(t, calendly.ListEventInviteesBranchListed, result.Branch)
	require.Equal(t, "tok_2", result.Value.NextPageToken)
	require.Len(t, result.Value.Invitees, 2)
	require.Equal(t, calendly.Invitee{
		URI: testEventURI + "/invitees/INVITEE01", ScheduledEventURI: testEventURI, Email: "ada@example.com", Name: "Ada Lovelace",
		Status: "active", Timezone: "Europe/London",
		QuestionsAndAnswers: []calendly.InviteeAnswer{{Question: "Topic?", Answer: "Engines", Position: 0}},
		CancelURL:           "https://calendly.com/cancellations/INVITEE01", RescheduleURL: "https://calendly.com/reschedulings/INVITEE01",
		CreatedAt: time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC),
	}, result.Value.Invitees[0])
	requireSecretFree(t, result)
	query := fake.requestsTo(http.MethodGet, "/scheduled_events/EVENT0001/invitees")[0].query
	require.Equal(t, map[string][]string{
		"status": {"active"}, "email": {"ada@example.com"}, "count": {"2"}, "page_token": {"tok_1"}, "sort": {"created_at:asc"},
	}, map[string][]string(query))
}

func TestListEventInviteesSelectsNotFoundDefectAndInvalidResponse(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"GET /scheduled_events/EVENT0001/invitees": respondJSON(http.StatusOK, fmt.Sprintf(`{"collection":[%s],"pagination":{"next_page_token":null}}`,
			inviteeJSON("EVENT0009", "INVITEE01", "active"))),
	})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	for _, test := range []struct {
		name           string
		input          calendly.ListEventInviteesInput
		expectedBranch sdkgo.BranchID
	}{
		{"unknown event", calendly.ListEventInviteesInput{ScheduledEventURI: "https://api.calendly.com/scheduled_events/MISSING"}, calendly.ListEventInviteesBranchNotFound},
		{"invitee of another event", calendly.ListEventInviteesInput{ScheduledEventURI: testEventURI}, calendly.ListEventInviteesBranchInvalidResponse},
		{"page too large", calendly.ListEventInviteesInput{ScheduledEventURI: testEventURI, PageSize: 500}, calendly.ListEventInviteesBranchDefect},
		{"bare identifier", calendly.ListEventInviteesInput{ScheduledEventURI: "EVENT0001"}, calendly.ListEventInviteesBranchDefect},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newStepContext("invitees"), client.ListEventInvitees(), testConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.NotNil(t, result.Failure)
		})
	}
}
