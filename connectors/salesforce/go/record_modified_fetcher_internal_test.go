// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce/go/internal/salesforcepollfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const pollTestToken = "salesforce-poll-token"

// pollTestClock is a manual clock shared by the client and the fake org.
type pollTestClock struct {
	mutex sync.Mutex
	now   time.Time
}

func (clock *pollTestClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.now
}

func (clock *pollTestClock) Advance(duration time.Duration) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = clock.now.Add(duration)
}

func newPollTestFetcher(t *testing.T, pageSize int64, configuration RecordModifiedTriggerConfiguration) (*recordPollFetcher, *salesforcepollfake.Org, *pollTestClock) {
	t.Helper()
	clock := &pollTestClock{now: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)}
	org := salesforcepollfake.New(t, pollTestToken, clock.Now)
	reference := sdkgo.ConnectionRef{Provider: "salesforce", Name: "salesforce-crm"}
	client, err := New(Config{PollPageSize: pageSize}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AuthMethodID: ProductionOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(pollTestToken), InstanceURL: org.URL},
	})
	require.NoError(t, err)
	client.now = clock.Now
	require.NoError(t, configuration.Validate())
	return newRecordPollFetcher(client, reference, configuration), org, clock
}

func fetchAll(t *testing.T, fetcher *recordPollFetcher, cursor pollingtrigger.Cursor) ([]sdkgo.TriggerEvent[RecordModifiedEvent], pollingtrigger.Cursor) {
	t.Helper()
	var events []sdkgo.TriggerEvent[RecordModifiedEvent]
	for page := 0; page < 20; page++ {
		changes, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		events, cursor = append(events, changes.Changes...), changes.Next
		if !changes.HasMorePages {
			return events, cursor
		}
	}
	t.Fatal("poll never caught up")
	return nil, cursor
}

func TestSeedRecordsExistingRecordsAsABaselineAndDeliversLaterChanges(t *testing.T) {
	configuration := RecordModifiedTriggerConfiguration{SObjectType: "Opportunity", FieldNames: []string{"Name", "StageName", "Id"}}
	fetcher, org, clock := newPollTestFetcher(t, 200, configuration)
	existing := org.Insert("Opportunity", map[string]any{"Name": "Renewal", "StageName": "Prospecting"})
	org.Insert("Account", map[string]any{"Name": "Meridian"})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Contains(t, cursor.Token, existing, "the cursor sits on the newest existing record")

	events, cursor := fetchAll(t, fetcher, cursor)
	require.Empty(t, events, "existing records are never delivered")

	created := org.Insert("Opportunity", map[string]any{"Name": "Expansion", "StageName": "Qualification"})
	org.Update(existing, map[string]any{"StageName": "Closed Won"})
	clock.Advance(time.Second)
	refetched, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	events, next := fetchAll(t, fetcher, cursor)
	require.Len(t, events, 2)
	require.Equal(t, refetched.Changes, events, "a refetch from one cursor yields the same event IDs")
	require.Equal(t, created, events[0].Payload.RecordID)
	require.Equal(t, RecordChangeCreated, events[0].Payload.ChangeKind)
	require.Equal(t, existing, events[1].Payload.RecordID)
	require.Equal(t, RecordChangeUpdated, events[1].Payload.ChangeKind)
	stage, isPresent := events[1].Payload.Record.StringField("StageName")
	require.True(t, isPresent)
	require.Equal(t, "Closed Won", stage)
	require.True(t, strings.HasPrefix(events[0].ID, "sfdc-"))
	require.Equal(t, events[1].Payload.SystemModstamp, events[1].OccurredAt)
	require.Contains(t, org.Requests()[len(org.Requests())-1].SOQL, "SELECT Id, CreatedDate, LastModifiedDate, SystemModstamp, Name, StageName FROM Opportunity")

	again, _ := fetchAll(t, fetcher, next)
	require.Empty(t, again)
	org.Update(existing, map[string]any{"StageName": "Closed Lost"})
	later, _ := fetchAll(t, fetcher, next)
	require.Len(t, later, 1)
	require.NotEqual(t, events[1].ID, later[0].ID, "a later change to one record gets a new event ID")
}

func TestRecordsSharingOneSystemModstampPageByID(t *testing.T) {
	fetcher, org, _ := newPollTestFetcher(t, 2, RecordModifiedTriggerConfiguration{SObjectType: "Lead"})
	org.Insert("Lead", map[string]any{})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	ids := org.InsertBatch("Lead", 5, func(int) map[string]any { return map[string]any{} })
	events, _ := fetchAll(t, fetcher, cursor)
	require.Len(t, events, 5)
	for index, event := range events {
		require.Equal(t, ids[index], event.Payload.RecordID)
	}
}

func TestRecentChangesWaitForTheSettleDelay(t *testing.T) {
	fetcher, org, clock := newPollTestFetcher(t, 200, RecordModifiedTriggerConfiguration{SObjectType: "Case"})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Contains(t, cursor.Token, `"systemModstamp":"2026-03-02T08:59:45Z"`, "an empty object seeds at the settle horizon")
	clock.Advance(50 * time.Second)
	org.Insert("Case", map[string]any{}) // Stamped 08:59:50, after the 08:59:35 horizon of a clock 50s back.
	clock.Advance(-50 * time.Second)
	queries := org.QueryCount()
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Empty(t, page.Changes)
	require.Equal(t, cursor, page.Next)
	require.Equal(t, queries, org.QueryCount(), "a cursor at the horizon sends no request")
	clock.Advance(30 * time.Second)
	events, _ := fetchAll(t, fetcher, cursor)
	require.Len(t, events, 1)
}

func TestSalesforceClockBehindTheHostKeepsTheSettleDelay(t *testing.T) {
	fetcher, org, clock := newPollTestFetcher(t, 200, RecordModifiedTriggerConfiguration{SObjectType: "Case"})
	org.SetClockSkew(-time.Minute) // Salesforce reads 08:59:00 while this host reads 09:00:00.
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Contains(t, cursor.Token, `"systemModstamp":"2026-03-02T08:58:45Z"`, "an empty object seeds at Salesforce's settle horizon")
	recent := org.InsertRecent("Case", map[string]any{}) // Stamped 08:59:00, after Salesforce's horizon.
	events, cursor := fetchAll(t, fetcher, cursor)
	require.Empty(t, events, "a change Salesforce stamped within the settle delay waits")
	clock.Advance(20 * time.Second)
	events, _ = fetchAll(t, fetcher, cursor)
	require.Len(t, events, 1, "the change arrives once it settles on Salesforce's clock")
	require.Equal(t, recent, events[0].Payload.RecordID)
}

func TestSalesforceClockBehindTheHostStopsAPageAtItsHorizon(t *testing.T) {
	fetcher, org, clock := newPollTestFetcher(t, 200, RecordModifiedTriggerConfiguration{SObjectType: "Case"})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	clock.Advance(time.Minute)
	settled := org.Insert("Case", map[string]any{})
	org.SetClockSkew(-30 * time.Second) // The host clock jumps 30s ahead of Salesforce between polls.
	org.InsertRecent("Case", map[string]any{})
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, 1, "the first response after the jump still stops at Salesforce's horizon")
	require.Equal(t, settled, page.Changes[0].Payload.RecordID)
	require.False(t, page.HasMorePages)
	events, _ := fetchAll(t, fetcher, page.Next)
	require.Empty(t, events)
	clock.Advance(20 * time.Second)
	events, _ = fetchAll(t, fetcher, page.Next)
	require.Len(t, events, 1)
}

func TestInvalidCursorsAreMarkedForReseed(t *testing.T) {
	fetcher, _, _ := newPollTestFetcher(t, 200, RecordModifiedTriggerConfiguration{SObjectType: "Contact"})
	for _, token := range []string{
		"not json",
		`{"sObjectType":"Contact","systemModstamp":"yesterday"}`,
		`{"sObjectType":"Contact","systemModstamp":"2026-03-02T08:00:00Z","recordId":"bad id"}`,
		`{"sObjectType":"Lead","systemModstamp":"2026-03-02T08:00:00Z"}`,
	} {
		_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), token)
	}
}

func TestRequestLimitExceededPausesThePoller(t *testing.T) {
	fetcher, org, _ := newPollTestFetcher(t, 200, RecordModifiedTriggerConfiguration{SObjectType: "Contact"})
	org.RateLimitNext(2, "120")
	_, err := fetcher.SeedCursor(context.Background())
	var rateLimited *pollingtrigger.RateLimitedError
	require.True(t, errors.As(err, &rateLimited))
	require.Equal(t, 2*time.Minute, rateLimited.RetryAfter)
	require.Contains(t, err.Error(), "REQUEST_LIMIT_EXCEEDED")
	require.NotContains(t, err.Error(), pollTestToken)
	_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: `{"sObjectType":"Contact","systemModstamp":"2026-03-02T08:00:00Z"}`})
	require.True(t, errors.As(err, &rateLimited))
	require.False(t, pollingtrigger.IsCursorInvalid(err))
}

func TestUnreadableObjectIsAPollFailureNotAReset(t *testing.T) {
	fetcher, org, _ := newPollTestFetcher(t, 200, RecordModifiedTriggerConfiguration{SObjectType: "Contact"})
	org.RejectObject("Contact", "INVALID_TYPE")
	_, err := fetcher.SeedCursor(context.Background())
	require.ErrorContains(t, err, "INVALID_TYPE")
	require.False(t, pollingtrigger.IsCursorInvalid(err))
	_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: `{"sObjectType":"Contact","systemModstamp":"2026-03-02T08:00:00Z"}`})
	require.Error(t, err)
	require.False(t, pollingtrigger.IsCursorInvalid(err))
}

func TestRecordModifiedTriggerConfigurationValidation(t *testing.T) {
	require.NoError(t, RecordModifiedTriggerConfiguration{SObjectType: "Invoice__c", FieldNames: []string{"Amount__c"}}.Validate())
	tooMany := make([]string, MaxTriggerFieldNames+1)
	for index := range tooMany {
		tooMany[index] = "Field" + strings.Repeat("x", index+1)
	}
	for _, configuration := range []RecordModifiedTriggerConfiguration{
		{}, {SObjectType: "Bad Name"}, {SObjectType: "Contact", FieldNames: []string{"Account.Name"}},
		{SObjectType: "Contact", FieldNames: []string{"Email", "email"}}, {SObjectType: "Contact", FieldNames: tooMany},
	} {
		require.Error(t, configuration.Validate(), "%+v", configuration)
	}
}

func TestNewRejectsAnUnsupportedPollConfiguration(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{}
	_, err := New(Config{PollInterval: time.Second}, credentials)
	require.ErrorContains(t, err, "pollInterval")
	_, err = New(Config{PollPageSize: 501}, credentials)
	require.ErrorContains(t, err, "pollPageSize")
}
