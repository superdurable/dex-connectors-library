// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze/go/internal/reamazefake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const (
	pollTestEmail = "agent@acme.example.com"
	pollTestToken = "reamazePollToken0123456789"
)

var pollTestStart = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

func newPollTestFetcher(t *testing.T, provider *reamazefake.Provider, channel string) *conversationCreatedFetcher {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "reamaze", Name: "reamaze-poll"}
	client, err := New(Config{Brand: "acme"}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {Email: pollTestEmail, APIToken: sdkgo.NewSecretString(pollTestToken)},
	}, WithAPIBaseURL(provider.URL))
	require.NoError(t, err)
	return newConversationCreatedFetcher(client, reference, "new-conversations", ConversationCreatedTriggerConfiguration{Channel: channel})
}

func insertConversations(provider *reamazefake.Provider, prefix string, count int, start time.Time) {
	for index := range count {
		provider.Insert(reamazefake.Conversation{
			Slug: prefix + "-" + strconv.Itoa(index), Subject: prefix + " " + strconv.Itoa(index),
			CreatedAt: start.Add(time.Duration(index) * time.Second),
		})
	}
}

func decodePollCursor(t *testing.T, cursor pollingtrigger.Cursor) conversationCreatedCursor {
	t.Helper()
	var position conversationCreatedCursor
	require.NoError(t, json.Unmarshal([]byte(cursor.Token), &position))
	return position
}

func deliveredSlugs(page pollingtrigger.ChangePage[sdkgo.TriggerEvent[ConversationCreatedEvent]]) []string {
	slugs := []string{}
	for _, change := range page.Changes {
		slugs = append(slugs, change.Payload.Conversation.ID)
	}
	return slugs
}

func TestConversationCreatedSeedReadsOnlyTheNewestPageAndDeliversNothingExisting(t *testing.T) {
	provider := reamazefake.New(t, pollTestEmail, pollTestToken)
	insertConversations(provider, "existing", 40, pollTestStart)
	fetcher := newPollTestFetcher(t, provider, "")

	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	position := decodePollCursor(t, cursor)
	require.Equal(t, []int{1}, provider.ListedPages())
	require.Equal(t, "acme", position.Brand)
	require.Equal(t, pollTestStart.Add(39*time.Second).UnixMilli(), position.WatermarkMillis)
	require.Equal(t, pollTestStart.Add(10*time.Second).UnixMilli()+1, position.FloorMillis, "the floor sits just past the oldest conversation read")
	require.Len(t, position.Seen, 29)

	provider.Insert(reamazefake.Conversation{Slug: "new-billing", Subject: "Double charge", Status: 4, CreatedAt: pollTestStart.Add(time.Minute)})
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, []string{"new-billing"}, deliveredSlugs(page))
	change := page.Changes[0]
	require.Equal(t, conversationCreatedEventID("acme", "new-billing"), change.ID)
	require.Equal(t, pollTestStart.Add(time.Minute), change.OccurredAt)
	require.Equal(t, "Opening message of Double charge", change.Payload.Conversation.FirstMessage, "the event carries the first message")
	require.Equal(t, ConversationStatusArchived, change.Payload.Conversation.Status, "every view is polled, so archived conversations arrive")
	filters, channels := provider.ListedQueries()
	require.Equal(t, []string{"all", "all", "all"}, filters)
	require.Equal(t, []string{"", "", ""}, channels)

	again, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, page.Changes[0].ID, again.Changes[0].ID, "a refetch from the same cursor yields the same event ID")
	next, err := fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Empty(t, next.Changes)
}

func TestConversationCreatedDeliversTiesAndLateListedConversationsOnce(t *testing.T) {
	provider := reamazefake.New(t, pollTestEmail, pollTestToken)
	fetcher := newPollTestFetcher(t, provider, "support")
	provider.Insert(reamazefake.Conversation{Slug: "existing", Subject: "Existing", Channel: "support", CreatedAt: pollTestStart})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	tie := pollTestStart.Add(time.Hour)
	provider.Insert(reamazefake.Conversation{Slug: "tie-a", Subject: "Tie A", Channel: "support", CreatedAt: tie})
	provider.Insert(reamazefake.Conversation{Slug: "tie-b", Subject: "Tie B", Channel: "support", CreatedAt: tie})
	provider.Insert(reamazefake.Conversation{Slug: "late", Subject: "Late", Channel: "support", CreatedAt: tie.Add(-5 * time.Minute)})
	provider.Insert(reamazefake.Conversation{Slug: "too-late", Subject: "Too late", Channel: "support", CreatedAt: tie.Add(-20 * time.Minute)})
	provider.Insert(reamazefake.Conversation{Slug: "elsewhere", Subject: "Sales", Channel: "sales", CreatedAt: tie})
	provider.Hide("tie-b")
	provider.Hide("late")
	provider.Hide("too-late")
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Equal(t, []string{"tie-a"}, deliveredSlugs(page))
	require.Equal(t, "support", page.Changes[0].Payload.Channel)

	provider.Reveal("tie-b")
	provider.Reveal("late")
	provider.Reveal("too-late")
	page, err = fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Equal(t, []string{"late", "tie-b"}, deliveredSlugs(page), "oldest first; too-late is older than the late-index window")
	page, err = fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Empty(t, page.Changes)
	_, channels := provider.ListedQueries()
	require.Equal(t, "support", channels[0])
}

func TestConversationCreatedWalksFromTheLastPageWhenTheListAscends(t *testing.T) {
	provider := reamazefake.New(t, pollTestEmail, pollTestToken)
	provider.ListOldestFirst()
	insertConversations(provider, "existing", 40, pollTestStart)
	fetcher := newPollTestFetcher(t, provider, "")
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Equal(t, []int{1, 2}, provider.ListedPages())
	require.Equal(t, pollTestStart.Add(39*time.Second).UnixMilli(), decodePollCursor(t, cursor).WatermarkMillis)

	insertConversations(provider, "new", 25, pollTestStart.Add(time.Hour))
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, 25)
	require.Equal(t, "new-0", page.Changes[0].Payload.Conversation.ID)
	require.Equal(t, "new-24", page.Changes[24].Payload.Conversation.ID)
	require.Equal(t, []int{1, 2, 1, 3, 2}, provider.ListedPages(), "the seed reads the last page; the poll walks back to the floor")
}

func TestConversationCreatedBoundsABacklogAndRaisesTheFloorPastIt(t *testing.T) {
	provider := reamazefake.New(t, pollTestEmail, pollTestToken)
	fetcher := newPollTestFetcher(t, provider, "")
	provider.Insert(reamazefake.Conversation{Slug: "existing", Subject: "Existing", CreatedAt: pollTestStart})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	insertConversations(provider, "burst", 1000, pollTestStart.Add(time.Hour))
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, MaxConversationCreatedPagesPerPoll*reamazefake.PageSize)
	require.LessOrEqual(t, len(page.Changes), pollingtrigger.MaxChangesPerPage)
	require.Equal(t, "burst-520", page.Changes[0].Payload.Conversation.ID, "the newest 480 arrive")
	position := decodePollCursor(t, page.Next)
	require.Equal(t, pollTestStart.Add(time.Hour+520*time.Second).UnixMilli()+1, position.FloorMillis)
	require.LessOrEqual(t, len(position.Seen), maximumSeenConversations)
	require.Less(t, len(page.Next.Token), 64<<10)

	page, err = fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "the skipped older part of the backlog is never delivered")
}

func TestConversationCreatedSeenSetStaysBounded(t *testing.T) {
	position := conversationCreatedCursor{Brand: "acme"}
	var walk conversationWalk
	for index := range maximumSeenConversations + 50 {
		walk.conversations = append(walk.conversations, Conversation{ID: "c" + strconv.Itoa(index), CreatedAt: pollTestStart.Add(time.Duration(index) * time.Millisecond)})
	}
	walk.reachesFloor = true
	next := advanceConversationCreatedCursor(position, walk)
	require.Len(t, next.Seen, maximumSeenConversations)
	require.Equal(t, pollTestStart.Add(50*time.Millisecond).UnixMilli(), next.FloorMillis, "trimming raises the floor to the oldest kept entry")
}

func TestConversationCreatedSeenSetTrimExcludesTiesAtTheCut(t *testing.T) {
	position := conversationCreatedCursor{Brand: "acme"}
	var walk conversationWalk
	for index := range maximumSeenConversations + 10 {
		// The 20 oldest share one creation millisecond, so the trim cuts through a tie.
		createdAt := pollTestStart.Add(time.Duration(max(index, 19)) * time.Millisecond)
		walk.conversations = append(walk.conversations, Conversation{ID: "c" + strconv.Itoa(index), CreatedAt: createdAt})
	}
	walk.reachesFloor = true
	next := advanceConversationCreatedCursor(position, walk)
	require.Equal(t, pollTestStart.Add(19*time.Millisecond).UnixMilli()+1, next.FloorMillis)
	require.Len(t, next.Seen, maximumSeenConversations-10)
	for _, entry := range next.Seen {
		require.GreaterOrEqual(t, entry.CreatedAtMillis, next.FloorMillis, "no kept entry sits below the floor")
	}
}

func TestConversationCreatedSeedExcludesTiesOnTheUnreadPage(t *testing.T) {
	provider := reamazefake.New(t, pollTestEmail, pollTestToken)
	tie := pollTestStart.Add(time.Hour)
	for index := range 35 {
		provider.Insert(reamazefake.Conversation{Slug: "existing-" + strconv.Itoa(index), Subject: "Existing", CreatedAt: tie.Add(time.Duration(max(index, 29)-29) * time.Second)})
	}
	fetcher := newPollTestFetcher(t, provider, "")
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Equal(t, []int{1}, provider.ListedPages())
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "existing conversations that share the oldest read creation time stay in the baseline")
}

func TestConversationCreatedReadsTiesAtTheFloorAcrossAPageEdge(t *testing.T) {
	provider := reamazefake.New(t, pollTestEmail, pollTestToken)
	fetcher := newPollTestFetcher(t, provider, "")
	provider.Insert(reamazefake.Conversation{Slug: "existing", Subject: "Existing", CreatedAt: pollTestStart})
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	newest := pollTestStart.Add(time.Hour)
	floor := newest.Add(-ConversationCreatedLateIndexWindow)
	insertConversations(provider, "recent", 29, newest.Add(-28*time.Second))
	provider.Insert(reamazefake.Conversation{Slug: "at-floor-a", Subject: "At floor", CreatedAt: floor})
	provider.Insert(reamazefake.Conversation{Slug: "at-floor-b", Subject: "At floor", CreatedAt: floor})
	provider.Hide("at-floor-b")
	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.Len(t, page.Changes, 30)
	require.Equal(t, floor.UnixMilli(), decodePollCursor(t, page.Next).FloorMillis)

	// Listed late, at-floor-b lands on page 2 beside the existing conversation, still at the floor.
	provider.Reveal("at-floor-b")
	page, err = fetcher.FetchChanges(context.Background(), page.Next)
	require.NoError(t, err)
	require.Equal(t, []string{"at-floor-b"}, deliveredSlugs(page))
}

func TestConversationCreatedRateLimitPausesThePollWithRetryAfter(t *testing.T) {
	provider := reamazefake.New(t, pollTestEmail, pollTestToken)
	fetcher := newPollTestFetcher(t, provider, "")
	provider.RateLimitNextList("7")
	_, err := fetcher.SeedCursor(context.Background())
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)

	provider.RateLimitNextList("")
	_, err = fetcher.SeedCursor(context.Background())
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, time.Minute, rateLimited.RetryAfter, "without Retry-After the poll waits Re:amaze's per-minute window")
	_, err = fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, provider.ListRequests())
}

func TestConversationCreatedRejectedRequestsAreNotRateLimitsAndKeepCredentialsOut(t *testing.T) {
	provider := reamazefake.New(t, pollTestEmail, "otherToken0123456789")
	fetcher := newPollTestFetcher(t, provider, "")
	_, err := fetcher.SeedCursor(context.Background())
	require.ErrorContains(t, err, "HTTP 401")
	var rateLimited *pollingtrigger.RateLimitedError
	require.False(t, errors.As(err, &rateLimited))
	require.False(t, pollingtrigger.IsCursorInvalid(err))
	require.NotContains(t, err.Error(), pollTestToken)
}

func TestConversationCreatedMarksForeignOrUndecodableCursorsInvalid(t *testing.T) {
	provider := reamazefake.New(t, pollTestEmail, pollTestToken)
	fetcher := newPollTestFetcher(t, provider, "support")
	for name, token := range map[string]string{
		"undecodable":   "not json",
		"no brand":      `{"watermark":1}`,
		"other brand":   `{"brand":"other","channel":"support","watermark":1,"floor":0,"seen":[]}`,
		"other channel": `{"brand":"acme","channel":"sales","watermark":1,"floor":0,"seen":[]}`,
	} {
		_, err := fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), name)
	}
	require.Zero(t, provider.ListRequests(), "an invalid cursor is rejected before any request")
}

func TestConversationCreatedConfigurationAcceptsOnlyChannelSlugs(t *testing.T) {
	require.NoError(t, ConversationCreatedTriggerConfiguration{}.Validate())
	require.NoError(t, ConversationCreatedTriggerConfiguration{Channel: "support"}.Validate())
	for _, channel := range []string{" support", "support/inbox", "https://acme.reamaze.io/support"} {
		require.Error(t, ConversationCreatedTriggerConfiguration{Channel: channel}.Validate(), channel)
	}
}
