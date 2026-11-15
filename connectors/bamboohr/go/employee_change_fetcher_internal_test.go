// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr/go/internal/bamboohrfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

var triggerTestConnection = sdkgo.ConnectionRef{Provider: "bamboohr", Name: "bamboohr-trigger-test"}

func newTriggerTestClient(t *testing.T, apiBaseURL string, maxResponseBytes int64) *Client {
	t.Helper()
	client, err := New(Config{CompanyDomain: "acme", MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{
		triggerTestConnection: {APIKey: sdkgo.NewSecretString(bamboohrfake.APIKey)},
	}, WithAPIBaseURL(apiBaseURL))
	require.NoError(t, err)
	return client
}

func newTestFetcher(t *testing.T, apiBaseURL string, changeType EmployeeChangeType) *employeeChangeFetcher {
	t.Helper()
	return newEmployeeChangeFetcher(newTriggerTestClient(t, apiBaseURL, 0), triggerTestConnection, EmployeeChangedTriggerConfiguration{ChangeType: changeType})
}

func TestSeedStartsAtBambooHRClockWithoutBacklog(t *testing.T) {
	fake := bamboohrfake.New(t)
	fake.Insert("140", map[string]string{"firstName": "Ava"})
	fake.Advance(time.Second)
	fetcher := newTestFetcher(t, fake.APIBaseURL(), "")
	// The local clock runs an hour ahead; the cursor must still start at BambooHR's time.
	fetcher.client.now = func() time.Time { return bamboohrfake.Start.Add(time.Hour) }

	cursor, err := fetcher.SeedCursor(t.Context())
	require.NoError(t, err)
	require.JSONEq(t, `{"v":1,"since":"2026-10-13T09:00:02Z"}`, cursor.Token)
	fake.Advance(time.Minute)
	page, err := fetcher.FetchChanges(t.Context(), cursor)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "the employee inserted before the seed is not delivered")
	require.Equal(t, cursor.Token, page.Next.Token)
	queries := fake.ChangeListQueries()
	require.Len(t, queries, 2)
	require.Equal(t, "2026-10-13T09:59:59Z", queries[0].Get("since"))
	require.Equal(t, "2026-10-13T09:00:01Z", queries[1].Get("since"), "since is one second before the cursor")
}

func TestFetchDeliversChangesOldestFirstWithStableIDs(t *testing.T) {
	fake := bamboohrfake.New(t)
	fetcher := newTestFetcher(t, fake.APIBaseURL(), "")
	cursor, err := fetcher.SeedCursor(t.Context())
	require.NoError(t, err)
	fake.Insert("141", map[string]string{"firstName": "Sam"})
	fake.Update("9", map[string]string{"department": "Sales"})
	fake.Delete("77")
	fake.Advance(employeeChangeSettleWindow)

	page, err := fetcher.FetchChanges(t.Context(), cursor)
	require.NoError(t, err)
	require.False(t, page.HasMorePages)
	require.Equal(t, []string{"employee-141-inserted-1791882001", "employee-9-updated-1791882002", "employee-77-deleted-1791882003"}, eventIDs(page.Changes))
	require.Equal(t, EmployeeChangedEvent{EmployeeID: "9", Action: EmployeeChangeActionUpdated, LastChanged: bamboohrfake.Start.Add(2 * time.Second)}, page.Changes[1].Payload)
	require.Equal(t, page.Changes[1].Payload.LastChanged, page.Changes[1].OccurredAt)
	require.JSONEq(t, `{"v":1,"since":"2026-10-13T09:00:03Z","afterEmployeeId":"77"}`, page.Next.Token)

	refetched, err := fetcher.FetchChanges(t.Context(), cursor)
	require.NoError(t, err)
	require.Equal(t, eventIDs(page.Changes), eventIDs(refetched.Changes), "a refetch from the same cursor yields the same event IDs")
	next, err := fetcher.FetchChanges(t.Context(), page.Next)
	require.NoError(t, err)
	require.Empty(t, next.Changes)
}

func TestFetchWaitsForChangesToSettle(t *testing.T) {
	fake := bamboohrfake.New(t)
	fetcher := newTestFetcher(t, fake.APIBaseURL(), "")
	cursor, err := fetcher.SeedCursor(t.Context())
	require.NoError(t, err)
	fake.Insert("200", nil)
	fake.Advance(employeeChangeSettleWindow - 2*time.Second)

	page, err := fetcher.FetchChanges(t.Context(), cursor)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "a change newer than the settle window waits")
	require.Equal(t, cursor.Token, page.Next.Token)

	// Employee 150 shares employee 200's second but becomes visible only now; a cursor already past
	// (09:00:01, 200) would skip it.
	fake.InsertLate("150", bamboohrfake.Start.Add(time.Second))
	fake.Advance(2 * time.Second) // 09:00:01 is now exactly the settle window old.
	page, err = fetcher.FetchChanges(t.Context(), page.Next)
	require.NoError(t, err)
	require.Equal(t, []string{"employee-150-inserted-1791882001", "employee-200-inserted-1791882001"}, eventIDs(page.Changes))
	require.JSONEq(t, `{"v":1,"since":"2026-10-13T09:00:01Z","afterEmployeeId":"200"}`, page.Next.Token)
}

func TestFetchSendsTheConfiguredChangeType(t *testing.T) {
	fake := bamboohrfake.New(t)
	fetcher := newTestFetcher(t, fake.APIBaseURL(), EmployeeChangeTypeInserted)
	cursor, err := fetcher.SeedCursor(t.Context())
	require.NoError(t, err)
	require.JSONEq(t, `{"v":1,"since":"2026-10-13T09:00:00Z","changeType":"inserted"}`, cursor.Token)
	fake.Insert("141", nil)
	fake.Update("9", nil)
	fake.Advance(employeeChangeSettleWindow)

	page, err := fetcher.FetchChanges(t.Context(), cursor)
	require.NoError(t, err)
	require.Equal(t, []string{"employee-141-inserted-1791882001"}, eventIDs(page.Changes))
	for _, query := range fake.ChangeListQueries() {
		require.Equal(t, "inserted", query.Get("type"))
	}
}

func TestFetchPagesAChangeListLargerThanOnePage(t *testing.T) {
	fake := bamboohrfake.New(t)
	fetcher := newTestFetcher(t, fake.APIBaseURL(), "")
	cursor, err := fetcher.SeedCursor(t.Context())
	require.NoError(t, err)
	for employee := 1; employee <= pollingtrigger.MaxChangesPerPage+2; employee++ {
		fake.Insert(strconv.Itoa(employee), nil)
	}
	fake.Advance(employeeChangeSettleWindow)

	page, err := fetcher.FetchChanges(t.Context(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, pollingtrigger.MaxChangesPerPage)
	require.True(t, page.HasMorePages)
	rest, err := fetcher.FetchChanges(t.Context(), page.Next)
	require.NoError(t, err)
	require.Equal(t, []string{"employee-501-inserted-1791882501", "employee-502-inserted-1791882502"}, eventIDs(rest.Changes))
	require.False(t, rest.HasMorePages)
}

func TestFetchMarksThrottlingAsRateLimited(t *testing.T) {
	fake := bamboohrfake.New(t)
	fetcher := newTestFetcher(t, fake.APIBaseURL(), "")
	cursor, err := fetcher.SeedCursor(t.Context())
	require.NoError(t, err)
	fake.RateLimitChangeLists(1)

	_, err = fetcher.FetchChanges(t.Context(), cursor)
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, time.Second, rateLimited.RetryAfter)
	require.False(t, pollingtrigger.IsCursorInvalid(err))
}

func TestFetchStatusClassification(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		status        int
		header        map[string]string
		isRateLimited bool
		isInvalid     bool
		message       string
	}{
		{name: "unavailable", status: http.StatusServiceUnavailable, header: map[string]string{"Retry-After": "7"}, isRateLimited: true, message: "HTTP 503"},
		{name: "invalid since", status: http.StatusBadRequest, isInvalid: true, message: "HTTP 400"},
		{name: "unknown key", status: http.StatusUnauthorized, message: "HTTP 401"},
		{name: "server error", status: http.StatusInternalServerError, message: "HTTP 500"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				for name, value := range scenario.header {
					response.Header().Set(name, value)
				}
				response.WriteHeader(scenario.status)
			}))
			t.Cleanup(server.Close)
			fetcher := newTestFetcher(t, server.URL+"/api/v1", "")
			_, err := fetcher.FetchChanges(t.Context(), mustEncodeCursor(t, fetcher, employeeChangedCursor{Since: bamboohrfake.Start}))
			require.Error(t, err)
			var rateLimited *pollingtrigger.RateLimitedError
			require.Equal(t, scenario.isRateLimited, errors.As(err, &rateLimited))
			require.Equal(t, scenario.isInvalid, pollingtrigger.IsCursorInvalid(err))
			require.Contains(t, err.Error(), scenario.message)
			require.NotContains(t, err.Error(), bamboohrfake.APIKey)
			if scenario.isRateLimited {
				require.Equal(t, 7*time.Second, rateLimited.RetryAfter)
			}
		})
	}
}

func TestFetchReportsAnOversizedChangeList(t *testing.T) {
	fake := bamboohrfake.New(t)
	fetcher := newEmployeeChangeFetcher(newTriggerTestClient(t, fake.APIBaseURL(), 256), triggerTestConnection, EmployeeChangedTriggerConfiguration{})
	cursor := mustEncodeCursor(t, fetcher, employeeChangedCursor{Since: bamboohrfake.Start})
	for employee := 1; employee <= 20; employee++ {
		fake.Insert(strconv.Itoa(employee), nil)
	}
	_, err := fetcher.FetchChanges(t.Context(), cursor)
	require.ErrorContains(t, err, "raise maxResponseBytes")
	require.False(t, pollingtrigger.IsCursorInvalid(err), "an oversized list is a configuration fault, never a silent gap")
}

func TestFetchRejectsForeignCursors(t *testing.T) {
	fake := bamboohrfake.New(t)
	fetcher := newTestFetcher(t, fake.APIBaseURL(), EmployeeChangeTypeDeleted)
	for _, token := range []string{
		"", "not json", `{"v":2,"since":"2026-10-13T09:00:00Z","changeType":"deleted"}`,
		`{"v":1,"since":"2026-10-13T09:00:00Z","changeType":"deleted","extra":1}`,
		`{"v":1,"since":"2026-10-13T09:00:00Z","afterEmployeeId":"x1","changeType":"deleted"}`,
		`{"v":1,"since":"2026-10-13T09:00:00Z","changeType":"updated"}`,
		`{"v":1,"since":"2026-10-13T09:00:00Z"}`,
	} {
		_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), "token %q", token)
	}
	require.Empty(t, fake.ChangeListQueries(), "an invalid cursor makes no request")
}

func TestEmployeeChangedConfigurationValidation(t *testing.T) {
	for _, changeType := range []EmployeeChangeType{"", EmployeeChangeTypeInserted, EmployeeChangeTypeUpdated, EmployeeChangeTypeDeleted} {
		require.NoError(t, EmployeeChangedTriggerConfiguration{ChangeType: changeType}.Validate())
	}
	for _, changeType := range []EmployeeChangeType{"all", "Inserted", " inserted"} {
		require.ErrorContains(t, EmployeeChangedTriggerConfiguration{ChangeType: changeType}.Validate(), "inserted, updated, deleted, or blank")
	}
}

func mustEncodeCursor(t *testing.T, fetcher *employeeChangeFetcher, position employeeChangedCursor) pollingtrigger.Cursor {
	t.Helper()
	position.ChangeType = fetcher.changeType
	cursor, err := fetcher.encodeCursor(position)
	require.NoError(t, err)
	return cursor
}

func eventIDs(events []sdkgo.TriggerEvent[EmployeeChangedEvent]) []string {
	identifiers := make([]string, 0, len(events))
	for _, event := range events {
		identifiers = append(identifiers, event.ID)
	}
	return identifiers
}
