// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable/go/internal/airtablefake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

var (
	fakeConnection = sdkgo.ConnectionRef{Provider: providerName, Name: "airtable-intake"}
	fakeBinding    = sdkgo.TriggerBindingRef{Connection: fakeConnection, Trigger: RecordChangedTriggerDefinition.Trigger, Name: "lead-changed"}
	fakeTrigger    = RecordChangedTriggerConfiguration{BaseID: airtablefake.BaseID, TableID: airtablefake.TableID, LastModifiedTimeFieldID: airtablefake.FieldID}
	seedTime       = time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)
)

func newFakeFetcher(t *testing.T, table *airtablefake.Table, pageSize int64) *recordChangedFetcher {
	t.Helper()
	client, err := New(Config{Endpoint: table.URL, PollInterval: time.Second, PollPageSize: pageSize}, sdkgo.StaticCredentialProvider[Credentials]{
		fakeConnection: {PersonalAccessToken: sdkgo.NewSecretString(airtablefake.AccessToken)},
	})
	require.NoError(t, err)
	return client.newRecordChangedFetcher(fakeBinding, fakeTrigger)
}

func fakeRecord(id string, modifiedAt time.Time) airtablefake.Record {
	return airtablefake.Record{ID: id, CreatedAt: modifiedAt, ModifiedAt: modifiedAt, Fields: map[string]any{"Email": id + "@example.test"}}
}

// fetchIDs fetches one page from cursor and returns the delivered record IDs and the next cursor.
func fetchIDs(t *testing.T, fetcher *recordChangedFetcher, cursor pollingtrigger.Cursor) ([]string, pollingtrigger.Cursor, bool) {
	t.Helper()
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	var ids []string
	for _, event := range page.Changes {
		ids = append(ids, event.Payload.Record.ID)
	}
	return ids, page.Next, page.HasMorePages
}

func TestSeedSkipsExistingRecordsAndDeliversLaterChangesOldestFirst(t *testing.T) {
	table := airtablefake.New(t, fakeRecord("recExisting000001", seedTime), fakeRecord("recExisting000002", seedTime), fakeRecord("recOlder000000001", seedTime.Add(-time.Hour)))
	fetcher := newFakeFetcher(t, table, 100)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, `{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z","seen":["recExisting000002:0","recExisting000001:0"]}`, cursor.Token)
	ids, cursor, _ := fetchIDs(t, fetcher, cursor)
	require.Empty(t, ids, "records present at seed never start Flows")

	edited := fakeRecord("recExisting000001", seedTime.Add(2*time.Second))
	edited.CreatedAt = seedTime.Add(-time.Hour)
	table.Put(fakeRecord("recNewLead0000001", seedTime.Add(time.Second)), edited, fakeRecord("recSameMilli00001", seedTime))
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, 3)
	require.Equal(t, []string{"recSameMilli00001", "recNewLead0000001", "recExisting000001"}, []string{
		page.Changes[0].Payload.Record.ID, page.Changes[1].Payload.Record.ID, page.Changes[2].Payload.Record.ID,
	}, "a record written at the cursor's own millisecond is still delivered")
	created, update := page.Changes[1], page.Changes[2]
	require.True(t, created.Payload.IsCreatedRecord)
	require.False(t, update.Payload.IsCreatedRecord)
	require.Equal(t, "recExisting000001-20261115T090002000000000Z", update.ID)
	require.Equal(t, seedTime.Add(2*time.Second), update.OccurredAt)
	require.Equal(t, airtablefake.BaseID, update.Payload.BaseID)
	email, isText := update.Payload.Record.Fields["Email"].Text()
	require.True(t, isText)
	require.Equal(t, "recExisting000001@example.test", email)

	ids, cursor, _ = fetchIDs(t, fetcher, page.Next)
	require.Empty(t, ids, "a delivered change is not delivered again")
	table.Put(fakeRecord("recExisting000001", seedTime.Add(3*time.Second)))
	ids, _, _ = fetchIDs(t, fetcher, cursor)
	require.Equal(t, []string{"recExisting000001"}, ids, "a later edit of the same record is a new change")
	for _, request := range table.ListRequests()[1:] {
		require.Equal(t, "asc", request.Sort[0].Direction)
	}
	require.Equal(t, `IS_AFTER({fldLastModified01}, DATETIME_PARSE("2026-11-15T08:59:56.000Z"))`, table.ListRequests()[len(table.ListRequests())-1].FilterByFormula,
		"the filter reaches the overlap and one more second before the latest change")
	require.Equal(t, 1, table.SchemaRequests(), "the field name is read once")
}

func TestPagesAndTieGroupsLargerThanAPageAreDeliveredOnce(t *testing.T) {
	table := airtablefake.New(t)
	fetcher := newFakeFetcher(t, table, 2)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, `{"v":2,"fieldId":"fldLastModified01","after":""}`, cursor.Token, "an empty table seeds before every change")
	changedAt := seedTime.Add(time.Minute)
	table.Put(fakeRecord("recTie00000000001", changedAt), fakeRecord("recTie00000000002", changedAt), fakeRecord("recTie00000000003", changedAt), fakeRecord("recLater000000001", changedAt.Add(time.Second)))

	var delivered []string
	for range 4 {
		ids, next, hasMore := fetchIDs(t, fetcher, cursor)
		delivered = append(delivered, ids...)
		cursor = next
		if !hasMore && len(ids) == 0 {
			break
		}
	}
	require.ElementsMatch(t, []string{"recTie00000000001", "recTie00000000002", "recTie00000000003", "recLater000000001"}, delivered)
	require.Len(t, delivered, 4, "no record is delivered twice")
	offsetFollowed := false
	for _, request := range table.ListRequests() {
		offsetFollowed = offsetFollowed || request.Offset != ""
	}
	require.True(t, offsetFollowed, "a page of already delivered ties is skipped through Airtable's offset")
}

func TestARenamedFieldIsResolvedAgainAndAChangedFieldTypeFailsThePoll(t *testing.T) {
	table := airtablefake.New(t)
	fetcher := newFakeFetcher(t, table, 100)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	table.RenameField("Updated At")
	table.Put(fakeRecord("recRenamed0000001", seedTime))
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.ErrorContains(t, err, "field name is read again")
	ids, _, _ := fetchIDs(t, fetcher, cursor)
	require.Equal(t, []string{"recRenamed0000001"}, ids)
	require.Equal(t, 2, table.SchemaRequests())

	table.SetFieldType("dateTime")
	table.RenameField("Due")
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.Error(t, err)
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.ErrorContains(t, err, "not a Last modified time field")
	require.False(t, pollingtrigger.IsCursorInvalid(err), "a configuration fault backs off without losing the cursor")
}

func TestRateLimitsPauseTheRunnerForAirtablesThirtySeconds(t *testing.T) {
	table := airtablefake.New(t)
	fetcher := newFakeFetcher(t, table, 100)
	table.Script(airtablefake.ScriptedResponse{Status: http.StatusTooManyRequests, RetryAfter: "1", ErrorType: "RATE_LIMIT_REACHED"})
	_, err := fetcher.SeedCursor(context.Background())
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.GreaterOrEqual(t, rateLimited.RetryAfter, 29*time.Second)
	require.NotContains(t, err.Error(), "SENTINEL")

	// The per-base hold makes the next request wait too, without calling Airtable.
	before := table.CompletedRequests()
	_, err = fetcher.SeedCursor(context.Background())
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, before, table.CompletedRequests())
}

func TestUnusableCursorsAreReseededAndProviderFailuresAreNot(t *testing.T) {
	table := airtablefake.New(t)
	fetcher := newFakeFetcher(t, table, 100)
	for _, token := range []string{"", "not-json", `{"v":1,"fieldId":"fldLastModified01","after":""}`, `{"v":2,"fieldId":"fldSomethingElse1","after":""}`,
		`{"v":2,"fieldId":"fldLastModified01","after":"yesterday"}`, `{"v":2,"fieldId":"fldLastModified01","after":"","seen":["recExisting000001:0"]}`,
		`{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z","seen":["Lead:0"]}`,
		`{"v":2,"fieldId":"fldLastModified01","after":"2026-11-15T09:00:00Z","scan":{"since":"","offset":"itr1/recExisting000001"}}`} {
		_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), token)
	}
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	table.Script(airtablefake.ScriptedResponse{Status: http.StatusServiceUnavailable}, airtablefake.ScriptedResponse{Status: http.StatusForbidden, ErrorType: "INVALID_PERMISSIONS_OR_MODEL_NOT_FOUND"})
	for range 2 {
		_, err = fetcher.FetchChanges(context.Background(), cursor)
		require.Error(t, err)
		require.False(t, pollingtrigger.IsCursorInvalid(err))
		var rateLimited *pollingtrigger.RateLimitedError
		require.False(t, errors.As(err, &rateLimited))
	}
}

func TestRecordChangedConfigurationValidation(t *testing.T) {
	require.NoError(t, fakeTrigger.Validate())
	for _, invalid := range []RecordChangedTriggerConfiguration{
		{TableID: airtablefake.TableID, LastModifiedTimeFieldID: airtablefake.FieldID},
		{BaseID: airtablefake.BaseID, TableID: "Leads", LastModifiedTimeFieldID: airtablefake.FieldID},
		{BaseID: airtablefake.BaseID, TableID: airtablefake.TableID, LastModifiedTimeFieldID: "Last Modified"},
	} {
		require.Error(t, invalid.Validate())
	}
	_, err := New(Config{PollInterval: time.Millisecond}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval")
	_, err = New(Config{PollPageSize: 101}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollPageSize")
}
