// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go/internal/mailchimpaudiencefake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const fetcherTestAudienceID = "57afe96172"

var fetcherTestConnection = sdkgo.ConnectionRef{Provider: "mailchimp", Name: "mailchimp-fetcher-test"}

func newFetcherTestFetcher(t *testing.T, audience *mailchimpaudiencefake.Audience, statuses ...MemberStatus) *memberChangeFetcher {
	t.Helper()
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{
		fetcherTestConnection: {APIKey: sdkgo.NewSecretString(mailchimpaudiencefake.APIKey)},
	}, WithAPIBaseURL(audience.URL))
	require.NoError(t, err)
	return newMemberChangeFetcher(client, fetcherTestConnection, MemberChangedTriggerConfiguration{AudienceID: fetcherTestAudienceID, Statuses: statuses})
}

// drain fetches pages until the pass ends and returns the delivered emails and the final cursor.
func drain(t *testing.T, fetcher *memberChangeFetcher, cursor pollingtrigger.Cursor) ([]string, pollingtrigger.Cursor) {
	t.Helper()
	var emails []string
	for page := 0; page < 100; page++ {
		changes, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		for _, change := range changes.Changes {
			require.Equal(t, memberChangeID(fetcherTestAudienceID, change.Payload.Member), change.ID)
			emails = append(emails, change.Payload.Member.EmailAddress)
		}
		cursor = changes.Next
		if !changes.HasMorePages {
			return emails, cursor
		}
	}
	t.Fatal("the pass never ended")
	return nil, cursor
}

func TestSeedRecordsABaselineAndLaterChangesArriveOnce(t *testing.T) {
	audience := mailchimpaudiencefake.New(t, fetcherTestAudienceID)
	audience.Upsert("ben@example.com", "subscribed", "Ben")
	audience.Upsert("cara@example.com", "subscribed", "Cara")
	fetcher := newFetcherTestFetcher(t, audience)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Contains(t, cursor.Token, `"baseline":"2026-11-01T09:00:02Z"`)

	emails, cursor := drain(t, fetcher, cursor)
	require.Empty(t, emails, "the baseline is never delivered")

	audience.Upsert("dana@example.com", "pending", "Dana")
	audience.Upsert("ben@example.com", "unsubscribed", "Ben")
	emails, cursor = drain(t, fetcher, cursor)
	require.Equal(t, []string{"dana@example.com", "ben@example.com"}, emails)

	emails, _ = drain(t, fetcher, cursor)
	require.Empty(t, emails, "a pass over the lookback window delivers each version once")
}

func TestChangesInTheBaselineSecondAfterTheSeedAreDelivered(t *testing.T) {
	audience := mailchimpaudiencefake.New(t, fetcherTestAudienceID)
	seededAt := time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)
	audience.UpsertAt("ben@example.com", "subscribed", "Ben", seededAt)
	audience.UpsertAt("cara@example.com", "subscribed", "Cara", seededAt)
	audience.UpsertAt("old@example.com", "subscribed", "Old", seededAt.Add(-time.Minute))
	fetcher := newFetcherTestFetcher(t, audience)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Len(t, cursor.Snapshot.Entries, 2, "the seed snapshots only the baseline second")

	audience.UpsertAt("dana@example.com", "subscribed", "Dana", seededAt)
	audience.UpsertAt("ben@example.com", "subscribed", "Benjamin", seededAt)
	emails, cursor := drain(t, fetcher, cursor)
	require.Equal(t, []string{"ben@example.com", "dana@example.com"}, emails, "new versions in the baseline second arrive")
	emails, _ = drain(t, fetcher, cursor)
	require.Empty(t, emails)
}

func TestTwoChangesInOneSecondWithDifferentContentArriveTwice(t *testing.T) {
	audience := mailchimpaudiencefake.New(t, fetcherTestAudienceID)
	fetcher := newFetcherTestFetcher(t, audience)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	changedAt := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	audience.UpsertAt("ben@example.com", "subscribed", "Ben", changedAt)
	_, cursor = drain(t, fetcher, cursor)
	audience.UpsertAt("ben@example.com", "subscribed", "Benjamin", changedAt)
	changes, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, changes.Changes, 1)
	require.Equal(t, "Benjamin", changes.Changes[0].Payload.Member.MergeFields["FNAME"])
}

func TestSeedOfAFullPageInOneSecondSkipsThatSecond(t *testing.T) {
	audience := mailchimpaudiencefake.New(t, fetcherTestAudienceID)
	importedAt := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	for index := range memberPollPageSize + 3 {
		audience.UpsertAt(fmt.Sprintf("import%04d@example.com", index), "subscribed", "Import", importedAt)
	}
	fetcher := newFetcherTestFetcher(t, audience)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Contains(t, cursor.Token, `"baseline":"2026-11-01T10:00:01Z"`)
	emails, _ := drain(t, fetcher, cursor)
	require.Empty(t, emails, "no contact of the imported second is delivered as new")
}

func TestLateListedChangeInsideTheLookbackIsDelivered(t *testing.T) {
	audience := mailchimpaudiencefake.New(t, fetcherTestAudienceID)
	audience.Upsert("ben@example.com", "subscribed", "Ben")
	fetcher := newFetcherTestFetcher(t, audience)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	for range 10 {
		audience.Upsert("cara@example.com", "subscribed", "Cara")
	}
	_, cursor = drain(t, fetcher, cursor)
	audience.UpsertAt("late@example.com", "subscribed", "Late", audience.Clock().Add(-3*time.Second))
	emails, _ := drain(t, fetcher, cursor)
	require.Equal(t, []string{"late@example.com"}, emails)
}

func TestContactsSharingOneSecondPageByOffsetAndArriveOnce(t *testing.T) {
	audience := mailchimpaudiencefake.New(t, fetcherTestAudienceID)
	fetcher := newFetcherTestFetcher(t, audience)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	importedAt := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	for index := range 2*memberPollPageSize + 7 {
		audience.UpsertAt(fmt.Sprintf("import%04d@example.com", index), "subscribed", "Import", importedAt.Add(time.Duration(index/300)*time.Second))
	}
	emails, cursor := drain(t, fetcher, cursor)
	require.Len(t, emails, 2*memberPollPageSize+7)
	unique := map[string]bool{}
	for _, email := range emails {
		unique[email] = true
	}
	require.Len(t, unique, len(emails), "no contact is delivered twice")
	require.Contains(t, cursor.Token, `"watermark":"2026-11-01T10:00:01Z"`)
	require.NotContains(t, cursor.Token, "passOffset", "a finished pass keeps no offset")
}

func TestStatusFilterSkipsOtherStatusesButPassesThem(t *testing.T) {
	audience := mailchimpaudiencefake.New(t, fetcherTestAudienceID)
	fetcher := newFetcherTestFetcher(t, audience, MemberStatusUnsubscribed, MemberStatusCleaned)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	audience.Upsert("ben@example.com", "subscribed", "Ben")
	audience.Upsert("cara@example.com", "unsubscribed", "Cara")
	emails, cursor := drain(t, fetcher, cursor)
	require.Equal(t, []string{"cara@example.com"}, emails)
	audience.Upsert("ben@example.com", "cleaned", "Ben")
	emails, _ = drain(t, fetcher, cursor)
	require.Equal(t, []string{"ben@example.com"}, emails)
}

func TestProviderFailuresAreClassifiedForTheKit(t *testing.T) {
	audience := mailchimpaudiencefake.New(t, fetcherTestAudienceID)
	fetcher := newFetcherTestFetcher(t, audience)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	audience.FailNextList(mailchimpaudiencefake.Failure{Status: http.StatusTooManyRequests, RetryAfter: "7"})
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)

	audience.FailNextList(mailchimpaudiencefake.Failure{Status: http.StatusForbidden, WithoutDocument: true})
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.ErrorAs(t, err, &rateLimited, "a document-less 403 is Mailchimp throttling")
	require.Zero(t, rateLimited.RetryAfter)

	audience.FailNextList(mailchimpaudiencefake.Failure{Status: http.StatusBadRequest})
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a 400 for the cursor's query reseeds")

	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable} {
		audience.FailNextList(mailchimpaudiencefake.Failure{Status: status})
		_, err = fetcher.FetchChanges(context.Background(), cursor)
		require.Error(t, err)
		require.False(t, pollingtrigger.IsCursorInvalid(err), "status %d backs off without a reseed", status)
		require.False(t, errors.As(err, &rateLimited), "status %d is not a rate limit", status)
		require.NotContains(t, err.Error(), mailchimpaudiencefake.APIKey)
	}
}

func TestForeignOrCorruptCursorIsInvalid(t *testing.T) {
	audience := mailchimpaudiencefake.New(t, fetcherTestAudienceID)
	fetcher := newFetcherTestFetcher(t, audience)
	for _, token := range []string{
		"not json", `{"audienceId":"0000000000"}`, `{"audienceId":"57afe96172","watermark":"yesterday"}`,
		`{"audienceId":"57afe96172","passOffset":-1}`,
	} {
		_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), token)
	}
	require.Empty(t, audience.Requests(), "an invalid cursor sends no request")
}

func TestPollRequestsAskForAscendingChangesAfterTheLookback(t *testing.T) {
	audience := mailchimpaudiencefake.New(t, fetcherTestAudienceID)
	audience.Upsert("ben@example.com", "subscribed", "Ben")
	fetcher := newFetcherTestFetcher(t, audience)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	audience.UpsertAt("cara@example.com", "subscribed", "Cara", time.Date(2026, 11, 1, 9, 10, 0, 0, time.UTC))
	_, cursor = drain(t, fetcher, cursor)
	_, _ = drain(t, fetcher, cursor)
	requests := audience.Requests()
	require.Len(t, requests, 3)
	require.Contains(t, requests[0], "sort_dir=DESC")
	require.Contains(t, requests[1], "since_last_changed=2026-11-01T09%3A00%3A00%2B00%3A00", "the first pass lists the baseline second")
	require.Contains(t, requests[2], "since_last_changed=2026-11-01T09%3A05%3A00%2B00%3A00", "later passes reread five minutes, never before the baseline")
	for _, request := range requests[1:] {
		require.True(t, strings.Contains(request, "sort_dir=ASC") && strings.Contains(request, "count=250"), request)
	}
}

func TestConfigurationValidation(t *testing.T) {
	require.Error(t, MemberChangedTriggerConfiguration{}.Validate())
	require.Error(t, MemberChangedTriggerConfiguration{AudienceID: "57afe96172/x"}.Validate())
	require.Error(t, MemberChangedTriggerConfiguration{AudienceID: fetcherTestAudienceID, Statuses: []MemberStatus{"gone"}}.Validate())
	require.Error(t, MemberChangedTriggerConfiguration{AudienceID: fetcherTestAudienceID, Statuses: []MemberStatus{MemberStatusPending, MemberStatusPending}}.Validate())
	require.NoError(t, MemberChangedTriggerConfiguration{AudienceID: fetcherTestAudienceID, Statuses: MemberStatuses()}.Validate())
}
