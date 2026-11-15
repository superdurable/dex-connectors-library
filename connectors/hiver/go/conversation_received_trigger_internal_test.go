// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hiver/go/internal/hiverfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const testInboxID = "105902"

var testConnectionReference = sdkgo.ConnectionRef{Provider: "hiver", Name: "hiver-support"}

func newConversationReceivedTestClient(t *testing.T, baseURL string, config Config) *Client {
	t.Helper()
	config.RequestIntervalMilliseconds = 1
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{
		testConnectionReference: {APIKey: sdkgo.NewSecretString(hiverfake.APIKey)},
	}, WithAPIBaseURL(baseURL))
	require.NoError(t, err)
	return client
}

func newConversationReceivedTestFetcher(t *testing.T, server *hiverfake.Server) *conversationReceivedFetcher {
	t.Helper()
	client := newConversationReceivedTestClient(t, server.URL, Config{})
	binding := sdkgo.TriggerBindingRef{Connection: testConnectionReference, Trigger: ConversationReceivedTriggerDefinition.Trigger, Name: "new-conversations"}
	return client.newConversationReceivedFetcher(binding, ConversationReceivedTriggerConfiguration{InboxID: " " + testInboxID + " "})
}

// conversationIDs returns n numeric IDs from first, such as 573741000.
func conversationIDs(first int, n int) []string {
	ids := make([]string, n)
	for index := range ids {
		ids[index] = fmt.Sprint(first + index)
	}
	return ids
}

// cursorHarness stands in for the kit's commits: it stamps Seen zero with the next commit count.
type cursorHarness struct {
	t       *testing.T
	fetcher *conversationReceivedFetcher
	cursor  pollingtrigger.Cursor
	commits int64
}

func newCursorHarness(t *testing.T, fetcher *conversationReceivedFetcher) *cursorHarness {
	t.Helper()
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	harness := &cursorHarness{t: t, fetcher: fetcher}
	harness.commit(cursor)
	return harness
}

func (harness *cursorHarness) commit(cursor pollingtrigger.Cursor) {
	harness.commits++
	if cursor.Snapshot != nil {
		for index := range cursor.Snapshot.Entries {
			if cursor.Snapshot.Entries[index].Seen == 0 {
				cursor.Snapshot.Entries[index].Seen = harness.commits
			}
		}
	}
	harness.cursor = cursor
}

// fetch runs one FetchChanges and commits it, returning the delivered conversation IDs.
func (harness *cursorHarness) fetch() ([]string, pollingtrigger.ChangePage[sdkgo.TriggerEvent[ConversationReceivedEvent]]) {
	harness.t.Helper()
	page, err := harness.fetcher.FetchChanges(context.Background(), harness.cursor)
	require.NoError(harness.t, err)
	harness.commit(page.Next)
	delivered := []string{}
	for _, event := range page.Changes {
		require.Equal(harness.t, "conversation-"+testInboxID+"-"+event.Payload.Conversation.ID, event.ID)
		require.Equal(harness.t, testInboxID, event.Payload.InboxID)
		delivered = append(delivered, event.Payload.Conversation.ID)
	}
	return delivered, page
}

func (harness *cursorHarness) completeBaseline() {
	harness.t.Helper()
	for {
		delivered, page := harness.fetch()
		require.Empty(harness.t, delivered, "the baseline delivers no existing conversation")
		if !page.HasMorePages {
			return
		}
	}
}

func (harness *cursorHarness) token() conversationReceivedCursorToken {
	harness.t.Helper()
	token, err := harness.fetcher.decodeCursorToken(harness.cursor.Token)
	require.NoError(harness.t, err)
	return token
}

func trackedKeys(cursor pollingtrigger.Cursor) []string {
	keys := []string{}
	for _, entry := range cursor.Snapshot.Entries {
		keys = append(keys, entry.Key)
	}
	return keys
}

func TestConversationReceivedBaselineRecordsExistingConversationsWithoutEvents(t *testing.T) {
	server := hiverfake.New(testInboxID, "support@acme.example.com")
	defer server.Close()
	server.AddConversations(testInboxID, conversationIDs(573741000, 1250)...)
	harness := newCursorHarness(t, newConversationReceivedTestFetcher(t, server))
	require.Zero(t, server.ConversationListRequests(), "seeding sends no request")

	_, first := harness.fetch()
	require.True(t, first.HasMorePages, "a baseline of 13 pages continues after 10")
	require.Equal(t, 10, server.ConversationListRequests())
	harness.completeBaseline()
	require.Equal(t, 13, server.ConversationListRequests())
	require.Len(t, harness.cursor.Snapshot.Entries, 1250)
	require.Equal(t, conversationReceivedCursorToken{Version: 1, InboxID: testInboxID, IsBaselineComplete: true}, harness.token())
}

func TestConversationReceivedDeliversNewConversationsFromTheFirstPageInListOrder(t *testing.T) {
	server := hiverfake.New(testInboxID, "support@acme.example.com")
	defer server.Close()
	server.AddConversations(testInboxID, conversationIDs(573741000, 250)...)
	harness := newCursorHarness(t, newConversationReceivedTestFetcher(t, server))
	harness.completeBaseline()

	server.AddConversations(testInboxID, "573742001", "573742002")
	before := server.ConversationListRequests()
	delivered, page := harness.fetch()
	require.Equal(t, []string{"573742002", "573742001"}, delivered, "newest-first list order")
	require.False(t, page.HasMorePages, "a poll never continues at once after the baseline")
	require.Equal(t, 3, server.ConversationListRequests()-before, "the first page plus two sweep pages")
	require.Equal(t, ConversationStatusOpen, page.Changes[0].Payload.Conversation.Status)

	delivered, _ = harness.fetch()
	require.Empty(t, delivered, "a tracked conversation is never delivered again")
}

func TestConversationReceivedSweepFindsConversationsListedOnLaterPages(t *testing.T) {
	server := hiverfake.New(testInboxID, "support@acme.example.com")
	defer server.Close()
	server.AddConversations(testInboxID, conversationIDs(573741000, 450)...)
	server.ListOldestFirst()
	harness := newCursorHarness(t, newConversationReceivedTestFetcher(t, server))
	harness.completeBaseline()

	server.AddConversations(testInboxID, "573749999")
	delivered, _ := harness.fetch()
	require.Empty(t, delivered, "pages 1 to 3 of 5 do not list it")
	delivered, _ = harness.fetch()
	require.Equal(t, []string{"573749999"}, delivered, "the sweep reaches the last page")
	require.Empty(t, harness.token().SweepPageToken, "the sweep finished and restarts from the first page")
	for range 3 {
		delivered, _ = harness.fetch()
		require.Empty(t, delivered)
	}
}

func TestConversationReceivedStopsTrackingConversationsAWholeSweepNeverListed(t *testing.T) {
	server := hiverfake.New(testInboxID, "support@acme.example.com")
	defer server.Close()
	server.AddConversations(testInboxID, "573741001", "573741002", "573741003")
	harness := newCursorHarness(t, newConversationReceivedTestFetcher(t, server))
	harness.completeBaseline()

	server.DeleteConversation(testInboxID, "573741002")
	delivered, _ := harness.fetch()
	require.Empty(t, delivered)
	require.Len(t, harness.cursor.Snapshot.Entries, 3, "one complete sweep without the ID keeps tracking it")
	delivered, _ = harness.fetch()
	require.Empty(t, delivered)
	require.Equal(t, []string{"573741001", "573741003"}, trackedKeys(harness.cursor), "two complete sweeps never listed it")
}

func TestConversationReceivedNeverRedeliversAConversationShiftedPastASweepPage(t *testing.T) {
	server := hiverfake.New(testInboxID, "support@acme.example.com")
	defer server.Close()
	server.AddConversations(testInboxID, conversationIDs(573741000, 250)...)
	client := newConversationReceivedTestClient(t, server.URL, Config{PollPagesPerPoll: 2})
	binding := sdkgo.TriggerBindingRef{Connection: testConnectionReference, Trigger: ConversationReceivedTriggerDefinition.Trigger, Name: "new-conversations"}
	harness := newCursorHarness(t, client.newConversationReceivedFetcher(binding, ConversationReceivedTriggerConfiguration{InboxID: testInboxID}))
	harness.completeBaseline()

	delivered, _ := harness.fetch()
	require.Empty(t, delivered, "the sweep reads offsets 0 to 199")
	// Newest first, offset 200 lists 573741049 until a deletion above it shifts it to offset 199.
	server.DeleteConversation(testInboxID, "573741249")
	delivered, _ = harness.fetch()
	require.Empty(t, delivered, "the sweep's last page starts at offset 200 and skips 573741049")
	require.Empty(t, harness.token().SweepPageToken, "the sweep completed")
	require.Contains(t, trackedKeys(harness.cursor), "573741049", "one sweep that skipped it keeps tracking it")
	for range 4 {
		delivered, _ = harness.fetch()
		require.Empty(t, delivered, "a conversation that was there all along is never delivered")
	}
	require.NotContains(t, trackedKeys(harness.cursor), "573741249", "the deleted conversation stops being tracked")
	require.Len(t, harness.cursor.Snapshot.Entries, 249)
}

func TestConversationReceivedRestartsTheSweepWhenHiverRejectsItsPageToken(t *testing.T) {
	server := hiverfake.New(testInboxID, "support@acme.example.com")
	defer server.Close()
	server.AddConversations(testInboxID, conversationIDs(573741000, 450)...)
	server.ListOldestFirst()
	harness := newCursorHarness(t, newConversationReceivedTestFetcher(t, server))
	harness.completeBaseline()
	harness.fetch()
	require.NotEmpty(t, harness.token().SweepPageToken)

	server.ExpirePageTokens()
	server.AddConversations(testInboxID, "573749999")
	delivered, _ := harness.fetch()
	require.Empty(t, delivered)
	require.Empty(t, harness.token().SweepPageToken, "the rejected token restarts the sweep")
	require.Len(t, harness.cursor.Snapshot.Entries, 450, "a restart keeps every tracked conversation")
	delivered, _ = harness.fetch()
	require.Empty(t, delivered)
	delivered, _ = harness.fetch()
	require.Equal(t, []string{"573749999"}, delivered, "the restarted sweep finds the conversation without a gap")
}

func TestConversationReceivedRestartsTheBaselineWhenHiverRejectsItsPageToken(t *testing.T) {
	server := hiverfake.New(testInboxID, "support@acme.example.com")
	defer server.Close()
	server.AddConversations(testInboxID, conversationIDs(573741000, 1250)...)
	harness := newCursorHarness(t, newConversationReceivedTestFetcher(t, server))
	harness.fetch()
	server.ExpirePageTokens()
	_, page := harness.fetch()
	require.True(t, page.HasMorePages)
	require.Equal(t, conversationReceivedCursorToken{Version: 1, InboxID: testInboxID}, harness.token())
	harness.completeBaseline()
	require.Len(t, harness.cursor.Snapshot.Entries, 1250)
}

func TestConversationReceivedMapsRateLimitsAndInvalidCursors(t *testing.T) {
	server := hiverfake.New(testInboxID, "support@acme.example.com")
	defer server.Close()
	server.AddConversations(testInboxID, "573741001")
	fetcher := newConversationReceivedTestFetcher(t, server)
	harness := newCursorHarness(t, fetcher)
	harness.completeBaseline()
	ctx := context.Background()

	server.RateLimitNext(2 * time.Second)
	_, err := fetcher.FetchChanges(ctx, harness.cursor)
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 2*time.Second, rateLimited.RetryAfter)
	require.NotContains(t, err.Error(), hiverfake.APIKey)

	server.RateLimitNext(0)
	_, err = fetcher.FetchChanges(ctx, harness.cursor)
	require.ErrorAs(t, err, &rateLimited)
	require.Zero(t, rateLimited.RetryAfter, "without Retry-After the runner backs off exponentially")

	for name, token := range map[string]string{
		"not JSON":      "row-12",
		"other version": `{"v":2,"inboxId":"105902"}`,
		"other inbox":   `{"v":1,"inboxId":"202","baselineComplete":true}`,
		"unknown field": `{"v":1,"inboxId":"105902","cursor":"x"}`,
		"bad page":      `{"v":1,"inboxId":"105902","sweepPageToken":"a b"}`,
	} {
		_, err := fetcher.FetchChanges(ctx, pollingtrigger.Cursor{Token: token, Snapshot: &projectconfig.KeyedSnapshot{}})
		require.True(t, pollingtrigger.IsCursorInvalid(err), name)
	}
}

func TestConversationReceivedReportsAMissingInboxAsARetriedFailure(t *testing.T) {
	server := hiverfake.New("999", "other@acme.example.com")
	defer server.Close()
	fetcher := newConversationReceivedTestFetcher(t, server)
	harness := newCursorHarness(t, fetcher)
	_, err := fetcher.FetchChanges(context.Background(), harness.cursor)
	require.ErrorContains(t, err, "HTTP 404")
	require.False(t, pollingtrigger.IsCursorInvalid(err), "a missing inbox is retried, never rebaselined")
}

func TestConversationReceivedConfigurationAndClientLimits(t *testing.T) {
	require.NoError(t, ConversationReceivedTriggerConfiguration{InboxID: "105902"}.Validate())
	require.ErrorContains(t, ConversationReceivedTriggerConfiguration{}.Validate(), "inboxId")
	require.Error(t, ConversationReceivedTriggerConfiguration{InboxID: "../inboxes"}.Validate())
	credentials := sdkgo.StaticCredentialProvider[Credentials]{}
	for _, pages := range []int64{1, 6} {
		_, err := New(Config{PollPagesPerPoll: pages}, credentials)
		require.ErrorContains(t, err, "pollPagesPerPoll must be from 2 to 5")
	}
	_, err := New(Config{PollInterval: time.Millisecond}, credentials)
	require.ErrorContains(t, err, "pollInterval")
	client, err := New(Config{}, credentials)
	require.NoError(t, err)
	require.Equal(t, pollingtrigger.PollSchedule{Interval: 5 * time.Minute, MaxBackoff: 30 * time.Minute}, client.conversationReceivedPollSchedule())
}
