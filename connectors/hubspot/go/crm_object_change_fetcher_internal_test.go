// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot/go/internal/hubspotsearchfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

var fakeStart = time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)

func newFakeChangeFetcher(t *testing.T, objectType ObjectType, properties ...string) (*crmObjectChangeFetcher, *hubspotsearchfake.Server) {
	t.Helper()
	fake := hubspotsearchfake.New(t, string(objectType), fakeStart)
	reference := sdkgo.ConnectionRef{Provider: "hubspot", Name: "hubspot-crm-watcher"}
	client, err := New(Config{Endpoint: fake.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AuthMethodID: PrivateAppTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(hubspotsearchfake.AccessToken)},
	})
	require.NoError(t, err)
	configuration := CRMObjectChangedTriggerConfiguration{ObjectType: objectType, Properties: properties}
	require.NoError(t, configuration.Validate())
	return newCRMObjectChangeFetcher(client, reference, configuration), fake
}

// drainChanges fetches until the feed has no more pages, as one kit poll does.
func drainChanges(t *testing.T, fetcher *crmObjectChangeFetcher, cursor pollingtrigger.Cursor) ([]sdkgo.TriggerEvent[CRMObjectChangedEvent], pollingtrigger.Cursor) {
	t.Helper()
	var events []sdkgo.TriggerEvent[CRMObjectChangedEvent]
	for range 50 {
		page, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		events, cursor = append(events, page.Changes...), page.Next
		if !page.HasMorePages {
			return events, cursor
		}
	}
	t.Fatal("change feed did not finish within 50 pages")
	return nil, cursor
}

func TestSeedRecordsABaselineAndLaterChangesArriveOnceWithTheirKind(t *testing.T) {
	fetcher, fake := newFakeChangeFetcher(t, ObjectTypeDeals, "dealname", "dealstage")
	existing := fake.Create(map[string]string{"dealname": "Existing renewal", "dealstage": "appointmentscheduled"})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	events, cursor := drainChanges(t, fetcher, cursor)
	require.Empty(t, events, "records that exist at seed are the baseline")

	created := fake.Create(map[string]string{"dealname": "New expansion", "dealstage": "appointmentscheduled"})
	fake.Update(existing, map[string]string{"dealstage": "qualifiedtobuy"})
	before := cursor
	events, cursor = drainChanges(t, fetcher, cursor)
	require.Len(t, events, 2)
	require.Equal(t, created, events[0].Payload.Object.ID)
	require.Equal(t, CRMObjectChangeCreated, events[0].Payload.ChangeKind)
	require.Equal(t, existing, events[1].Payload.Object.ID)
	require.Equal(t, CRMObjectChangeUpdated, events[1].Payload.ChangeKind)
	require.Equal(t, "qualifiedtobuy", events[1].Payload.Object.Properties["dealstage"])
	require.Equal(t, events[1].OccurredAt, events[1].Payload.ModifiedAt)

	refetched, _ := drainChanges(t, fetcher, before)
	require.Equal(t, []string{events[0].ID, events[1].ID}, []string{refetched[0].ID, refetched[1].ID}, "event IDs are pure functions of the record version")
	again, _ := drainChanges(t, fetcher, cursor)
	require.Empty(t, again, "the lag-window re-read skips delivered versions")
}

func TestSearchUsesTheObjectTypesLastModifiedPropertyAndRequestedProperties(t *testing.T) {
	fetcher, fake := newFakeChangeFetcher(t, ObjectTypeContacts, "email")
	fake.Create(map[string]string{"email": "ada@example.com"})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	_, _ = drainChanges(t, fetcher, cursor)
	bodies := fake.SearchBodies()
	require.Equal(t, []any{map[string]any{"propertyName": "lastmodifieddate", "direction": "DESCENDING"}}, bodies[0]["sorts"])
	require.Equal(t, []any{map[string]any{"propertyName": "lastmodifieddate", "direction": "ASCENDING"}}, bodies[1]["sorts"])
	require.Equal(t, []any{"email", "createdate", "lastmodifieddate"}, bodies[1]["properties"])
	filter := bodies[1]["filterGroups"].([]any)[0].(map[string]any)["filters"].([]any)[0].(map[string]any)
	require.Equal(t, "GTE", filter["operator"])
	require.Equal(t, float64(100), bodies[1]["limit"])
}

func TestEmptyObjectTypeSeedsAtZeroAndDeliversTheFirstRecord(t *testing.T) {
	fetcher, fake := newFakeChangeFetcher(t, ObjectTypeCompanies)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	created := fake.Create(map[string]string{"name": "Analytical Engines"})
	events, _ := drainChanges(t, fetcher, cursor)
	require.Len(t, events, 1)
	require.Equal(t, created, events[0].Payload.Object.ID)
	require.Equal(t, CRMObjectChangeCreated, events[0].Payload.ChangeKind)
}

func TestRecordIndexedLateWithinTheLagWindowIsStillDelivered(t *testing.T) {
	fetcher, fake := newFakeChangeFetcher(t, ObjectTypeDeals)
	fake.Create(map[string]string{"dealname": "Baseline"})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	late := fake.CreateUnindexed(map[string]string{"dealname": "Indexed late"})
	prompt := fake.Create(map[string]string{"dealname": "Indexed at once"})
	events, cursor := drainChanges(t, fetcher, cursor)
	require.Len(t, events, 1)
	require.Equal(t, prompt, events[0].Payload.Object.ID)

	fake.Index(late)
	events, _ = drainChanges(t, fetcher, cursor)
	require.Len(t, events, 1)
	require.Equal(t, late, events[0].Payload.Object.ID)
	require.Equal(t, CRMObjectChangeUpdated, events[0].Payload.ChangeKind, "a record found after a newer change counts as updated")
}

func TestMoreRecordsThanAPageAtOneTimestampPageByOffsetAndArriveOnce(t *testing.T) {
	fetcher, fake := newFakeChangeFetcher(t, ObjectTypeContacts)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	imported := fake.CreateAtSameTime(250, map[string]string{"lifecyclestage": "lead"})
	after := fake.Create(map[string]string{"lifecyclestage": "customer"})
	events, cursor := drainChanges(t, fetcher, cursor)
	seen := map[string]bool{}
	for _, event := range events {
		require.False(t, seen[event.Payload.Object.ID], "record %s delivered twice", event.Payload.Object.ID)
		seen[event.Payload.Object.ID] = true
	}
	require.Len(t, seen, len(imported)+1)
	require.True(t, seen[after])
	again, _ := drainChanges(t, fetcher, cursor)
	require.Empty(t, again)
}

func TestFetchMapsRateLimitsQuotasAndRejectedOffsetsToKitSignals(t *testing.T) {
	fetcher, fake := newFakeChangeFetcher(t, ObjectTypeDeals)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	fake.FailNextSearches(hubspotsearchfake.FailedResponse{Status: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"7"}}, Body: `{"category":"RATE_LIMITS","policyName":"SECONDLY"}`})
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)

	fake.FailNextSearches(hubspotsearchfake.FailedResponse{Status: http.StatusTooManyRequests, Body: `{"category":"RATE_LIMITS","policyName":"DAILY"}`})
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.ErrorAs(t, err, &rateLimited)
	require.Zero(t, rateLimited.RetryAfter)

	fake.FailNextSearches(hubspotsearchfake.FailedResponse{Status: http.StatusBadGateway, Body: `{}`})
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.Error(t, err)
	require.False(t, pollingtrigger.IsCursorInvalid(err))
	require.NotErrorAs(t, err, &rateLimited)

	paging, err := decodeCRMObjectChangeCursor(cursor.Token)
	require.NoError(t, err)
	paging.After = "9900"
	offsetCursor, err := encodeCRMObjectChangeCursor(paging)
	require.NoError(t, err)
	fake.FailNextSearches(hubspotsearchfake.FailedResponse{Status: http.StatusBadRequest, Body: `{"category":"VALIDATION_ERROR"}`})
	_, err = fetcher.FetchChanges(context.Background(), offsetCursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a rejected paging offset reseeds")

	fake.FailNextSearches(hubspotsearchfake.FailedResponse{Status: http.StatusBadRequest, Body: `{"category":"VALIDATION_ERROR"}`})
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.Error(t, err)
	require.False(t, pollingtrigger.IsCursorInvalid(err), "a rejected search without an offset is a failed poll")
}

func TestUnusableCursorsAreMarkedInvalid(t *testing.T) {
	fetcher, _ := newFakeChangeFetcher(t, ObjectTypeDeals)
	for _, token := range []string{
		"not json",
		`{"objectType":"contacts","floorMs":1,"highWaterMs":1,"anchorMs":2}`,
		`{"objectType":"deals","floorMs":5,"highWaterMs":4,"anchorMs":6}`,
		`{"objectType":"deals","floorMs":1,"highWaterMs":1,"anchorMs":2,"after":"x"}`,
		`{"objectType":"deals","floorMs":1,"highWaterMs":1,"anchorMs":2,"seen":{"abc":3}}`,
		`{"objectType":"deals","floorMs":1,"highWaterMs":1,"anchorMs":2,"droppedThroughMs":5}`,
	} {
		_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), token)
	}
}

func TestPruneSeenKeepsTheNewestVersionsInsideTheLagWindow(t *testing.T) {
	position := crmObjectChangeCursor{ObjectType: ObjectTypeDeals, FloorMilliseconds: 0, HighWaterMilliseconds: 10_000_000, Seen: map[string]int64{"1": 1}}
	for index := range maximumSeenRecordVersions + 10 {
		position.Seen[strconv.Itoa(100_000+index)] = 10_000_000 - int64(index)
	}
	position.pruneSeen()
	require.Len(t, position.Seen, maximumSeenRecordVersions)
	require.NotContains(t, position.Seen, "1", "a version older than the lag window is dropped")
	require.Contains(t, position.Seen, "100000")
	require.NotContains(t, position.Seen, strconv.Itoa(100_000+maximumSeenRecordVersions), "the oldest versions go first")
	require.Equal(t, int64(10_000_000-maximumSeenRecordVersions), position.DroppedThroughMilliseconds, "the re-read starts after the dropped versions")
	require.Equal(t, position.DroppedThroughMilliseconds+1, position.lagWindowStart())
	cursor, err := encodeCRMObjectChangeCursor(position)
	require.NoError(t, err)
	require.Less(t, len(cursor.Token), 64<<10)
}

func TestTriggerConfigurationValidation(t *testing.T) {
	require.ErrorContains(t, CRMObjectChangedTriggerConfiguration{}.Validate(), "objectType is required")
	require.Error(t, CRMObjectChangedTriggerConfiguration{ObjectType: "tickets"}.Validate())
	require.Error(t, CRMObjectChangedTriggerConfiguration{ObjectType: ObjectTypeDeals, Properties: []string{"bad name"}}.Validate())
	tooMany := make([]string, MaximumTriggerProperties+1)
	for index := range tooMany {
		tooMany[index] = "property_" + strconv.Itoa(index)
	}
	require.Error(t, CRMObjectChangedTriggerConfiguration{ObjectType: ObjectTypeDeals, Properties: tooMany}.Validate())
	require.NoError(t, CRMObjectChangedTriggerConfiguration{ObjectType: ObjectTypeDeals, Properties: []string{"dealname", "createdate"}}.Validate())
}

func TestBurstLargerThanTheSeenCapacityIsNotRedeliveredOnLaterPolls(t *testing.T) {
	fetcher, fake := newFakeChangeFetcher(t, ObjectTypeContacts)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	imported := fake.CreateAtSameTime(maximumSeenRecordVersions+250, map[string]string{"lifecyclestage": "lead"})
	events, cursor := drainChanges(t, fetcher, cursor)
	require.Len(t, events, len(imported))
	for range 3 {
		again, next := drainChanges(t, fetcher, cursor)
		require.Empty(t, again, "an import larger than the seen set must not repeat on every poll")
		cursor = next
	}
	later := fake.Create(map[string]string{"lifecyclestage": "customer"})
	events, _ = drainChanges(t, fetcher, cursor)
	require.Len(t, events, 1)
	require.Equal(t, later, events[0].Payload.Object.ID)
}
