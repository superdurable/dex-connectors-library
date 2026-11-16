// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedin

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/go/internal/linkedinprofilefake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const triggerTestConnectionName = "linkedin-watcher"

func newTriggerTestConnection(t *testing.T, userInfoURL, accessToken string) Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "linkedin", Name: triggerTestConnectionName}
	client, err := New(Config{UserInfoURL: userInfoURL, PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString(accessToken)},
	})
	require.NoError(t, err)
	connection, err := NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func newTestProfileFetcher(t *testing.T, fake *linkedinprofilefake.UserInfo) *profileFetcher {
	t.Helper()
	connection := newTriggerTestConnection(t, fake.UserInfoURL(), linkedinprofilefake.AccessToken)
	return newProfileFetcher(connection.client, connection.reference)
}

func TestProfileFetcherSeedsWithoutBacklogThenDeliversEachChange(t *testing.T) {
	fake := linkedinprofilefake.New(t, linkedinprofilefake.AdaProfile())
	fetcher := newTestProfileFetcher(t, fake)
	ctx := context.Background()

	seed, err := fetcher.SeedCursor(ctx)
	require.NoError(t, err)
	require.Contains(t, seed.Token, `"subject":"782bbtaQ"`)
	unchanged, err := fetcher.FetchChanges(ctx, seed)
	require.NoError(t, err)
	require.Empty(t, unchanged.Changes, "the baseline is never delivered")
	require.Equal(t, seed.Token, unchanged.Next.Token)

	fake.Update(func(profile *linkedinprofilefake.Profile) {
		profile.Picture = "https://media.licdn.com/dms/image/v2/ada/profile-displayphoto-shrink_100_100/0/1?e=2&v=beta&t=second"
	})
	resigned, err := fetcher.FetchChanges(ctx, seed)
	require.NoError(t, err)
	require.Empty(t, resigned.Changes, "a re-signed URL of the same picture is not a change")
	require.Contains(t, resigned.Next.Token, "t=second", "the stored profile keeps the latest picture URL")

	fake.Update(func(profile *linkedinprofilefake.Profile) {
		profile.Name, profile.FamilyName, profile.EmailVerified = "Ada King", "King", false
	})
	page, err := fetcher.FetchChanges(ctx, resigned.Next)
	require.NoError(t, err)
	require.Len(t, page.Changes, 1)
	change := page.Changes[0]
	require.Equal(t, "782bbtaQ", change.Payload.Subject)
	require.Equal(t, []ProfileField{ProfileFieldName, ProfileFieldFamilyName, ProfileFieldEmailVerified}, change.Payload.ChangedFields)
	require.Equal(t, "Ada Lovelace", change.Payload.Previous.Name)
	require.True(t, change.Payload.Previous.EmailVerified)
	require.Equal(t, "Ada King", change.Payload.Current.Name)
	require.Equal(t, "ada@example.com", change.Payload.Current.Email, "an unverified email is still reported")
	require.False(t, change.Payload.Current.EmailVerified)
	require.Regexp(t, `^linkedin-profile-[0-9a-f]{32}$`, change.ID)

	refetched, err := fetcher.FetchChanges(ctx, resigned.Next)
	require.NoError(t, err)
	require.Equal(t, change.ID, refetched.Changes[0].ID, "a refetch from the same cursor keeps the event ID")
	require.Equal(t, page.Next.Token, refetched.Next.Token)
}

func TestProfileFetcherGivesABackAndForthEditANewEventIDEachTime(t *testing.T) {
	fake := linkedinprofilefake.New(t, linkedinprofilefake.AdaProfile())
	fetcher := newTestProfileFetcher(t, fake)
	ctx := context.Background()
	cursor, err := fetcher.SeedCursor(ctx)
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, locale := range []string{"fr-FR", "en-US", "fr-FR"} {
		fake.Update(func(profile *linkedinprofilefake.Profile) { profile.Locale = locale })
		page, err := fetcher.FetchChanges(ctx, cursor)
		require.NoError(t, err)
		require.Len(t, page.Changes, 1)
		require.Equal(t, []ProfileField{ProfileFieldLocale}, page.Changes[0].Payload.ChangedFields)
		require.False(t, seen[page.Changes[0].ID], "locale %s reuses an earlier event ID", locale)
		seen[page.Changes[0].ID] = true
		cursor = page.Next
	}
}

func TestProfileFetcherReseedsForAnotherMemberOrAnUndecodableCursor(t *testing.T) {
	fake := linkedinprofilefake.New(t, linkedinprofilefake.AdaProfile())
	fetcher := newTestProfileFetcher(t, fake)
	ctx := context.Background()
	seed, err := fetcher.SeedCursor(ctx)
	require.NoError(t, err)

	fake.Update(func(profile *linkedinprofilefake.Profile) { profile.Subject, profile.Name = "9aQzr21", "Grace Hopper" })
	_, err = fetcher.FetchChanges(ctx, seed)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a connection re-authorized as another member reseeds: %v", err)

	for _, token := range []string{"", "not json", `{"version":2,"subject":"782bbtaQ","hash":"x","profile":{"subject":"782bbtaQ"}}`, `{"version":1,"subject":"782bbtaQ","hash":"x","profile":{"subject":"other"}}`} {
		_, err := fetcher.FetchChanges(ctx, pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), token)
	}
}

func TestProfileFetcherMapsProviderFailuresToPollSignals(t *testing.T) {
	fake := linkedinprofilefake.New(t, linkedinprofilefake.AdaProfile())
	fetcher := newTestProfileFetcher(t, fake)
	ctx := context.Background()
	seed, err := fetcher.SeedCursor(ctx)
	require.NoError(t, err)

	fake.FailNext(http.StatusTooManyRequests, "120")
	_, err = fetcher.FetchChanges(ctx, seed)
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 2*time.Minute, rateLimited.RetryAfter)

	fake.FailNext(http.StatusTooManyRequests, "")
	_, err = fetcher.FetchChanges(ctx, seed)
	require.ErrorAs(t, err, &rateLimited)
	require.Zero(t, rateLimited.RetryAfter, "without Retry-After the runner backs off exponentially")

	for status, message := range map[int]string{
		http.StatusForbidden: "forbidden", http.StatusInternalServerError: "HTTP 500", http.StatusBadRequest: "HTTP 400",
	} {
		fake.FailNext(status, "")
		_, err = fetcher.FetchChanges(ctx, seed)
		require.ErrorContains(t, err, message)
		require.False(t, pollingtrigger.IsCursorInvalid(err))
		require.False(t, errors.As(err, &rateLimited))
	}

	revoked := newTriggerTestConnection(t, fake.UserInfoURL(), "revoked-"+"grant")
	_, err = newProfileFetcher(revoked.client, revoked.reference).FetchChanges(ctx, seed)
	require.ErrorContains(t, err, "invalid or revoked", "a 401 is a failed poll that backs off, not a reseed")
	require.False(t, pollingtrigger.IsCursorInvalid(err))
}

func TestNewRejectsAPollIntervalUnderOneMinute(t *testing.T) {
	_, err := New(Config{PollInterval: 59 * time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval must be at least 1m0s")
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	require.Equal(t, 15*time.Minute, client.pollInterval)
}
