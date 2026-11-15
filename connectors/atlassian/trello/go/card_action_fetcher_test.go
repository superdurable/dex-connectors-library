// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/go/internal/trelloboardfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func newTestCardActionFetcher(t *testing.T, board *trelloboardfake.Board) *cardActionFetcher {
	t.Helper()
	connection := newTriggerTestConnection(t, board.URL)
	return newCardActionFetcher(connection.client, connection.reference, CardChangedTriggerConfiguration{BoardID: board.BoardID})
}

func fetchAll(t *testing.T, fetcher *cardActionFetcher, cursor pollingtrigger.Cursor) ([]sdkgo.TriggerEvent[CardChangedEvent], pollingtrigger.Cursor) {
	t.Helper()
	var events []sdkgo.TriggerEvent[CardChangedEvent]
	for range 10 {
		page, err := fetcher.FetchChanges(context.Background(), cursor)
		require.NoError(t, err)
		events = append(events, page.Changes...)
		cursor = page.Next
		if !page.HasMorePages {
			return events, cursor
		}
	}
	t.Fatal("the fetcher kept reporting more pages")
	return nil, cursor
}

func TestCardActionFetcherBaselinesThenDeliversEachCardActionOnceInOrder(t *testing.T) {
	board := trelloboardfake.New(t)
	existing := board.CreateCard("Existing card")
	fetcher := newTestCardActionFetcher(t, board)
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	created := board.CreateCard("Replace badge reader")
	board.MoveCard(created)
	board.CommentOnCard(created, "Ordered the part.")
	board.RecordUnwatchedAction()
	board.DeleteCard(existing)
	events, next := fetchAll(t, fetcher, seed)
	require.Len(t, events, 4, "the baseline card and the board update are not delivered")
	kinds := []CardActionType{events[0].Payload.ActionType, events[1].Payload.ActionType, events[2].Payload.ActionType, events[3].Payload.ActionType}
	require.Equal(t, []CardActionType{CardActionCreated, CardActionUpdated, CardActionCommented, CardActionDeleted}, kinds)

	createdEvent := events[0].Payload
	require.Equal(t, board.BoardID, createdEvent.BoardID)
	require.Equal(t, "Support queue", createdEvent.BoardName)
	require.Equal(t, created, createdEvent.Card.ID)
	require.Equal(t, "Replace badge reader", createdEvent.Card.Name)
	require.Equal(t, &ActionList{ID: trelloboardfake.TodoListID, Name: "To do"}, createdEvent.List)
	require.Equal(t, &MemberReference{ID: trelloboardfake.MemberID, FullName: "Grace Hopper", Username: "gracehopper"}, createdEvent.MemberCreator)
	require.Equal(t, "trello-action-"+createdEvent.ActionID, events[0].ID)
	require.Equal(t, createdEvent.OccurredAt, events[0].OccurredAt)

	moved := events[1].Payload
	require.Equal(t, []string{"idList"}, moved.ChangedFields)
	require.Equal(t, trelloboardfake.TodoListID, moved.ListBefore.ID)
	require.Equal(t, &ActionList{ID: trelloboardfake.DoneListID, Name: "Done"}, moved.ListAfter)
	require.Equal(t, "Ordered the part.", events[2].Payload.CommentText)
	require.Equal(t, existing, events[3].Payload.Card.ID)
	require.Empty(t, events[3].Payload.Card.Name, "Trello keeps no name for a deleted card")

	again, _ := fetchAll(t, fetcher, seed)
	require.Equal(t, events, again, "a refetch from the same cursor yields the same event IDs")
	page, err := fetcher.FetchChanges(context.Background(), next)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "re-reading the lookback window skips delivered actions")
	require.Equal(t, next, page.Next, "an empty poll leaves the cursor where it was")
}

func TestCardActionFetcherDeliversALateActionInsideTheLookbackOnce(t *testing.T) {
	board := trelloboardfake.New(t)
	fetcher := newTestCardActionFetcher(t, board)
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	require.JSONEq(t, `{"boardId":"`+board.BoardID+`"}`, seed.Token, "a board without card actions has no watermark")

	cardID := board.CreateCard("Door sensor")
	events, next := fetchAll(t, fetcher, seed)
	require.Len(t, events, 1, "every card action after an empty baseline is new")
	board.RenameCardRecordedEarlier(cardID, "Door sensor (east)", 20*time.Second)
	events, next = fetchAll(t, fetcher, next)
	require.Len(t, events, 1)
	require.Equal(t, []string{"name"}, events[0].Payload.ChangedFields)
	events, _ = fetchAll(t, fetcher, next)
	require.Empty(t, events)
}

func TestCardActionFetcherPagesABacklogOldestFirst(t *testing.T) {
	board := trelloboardfake.New(t)
	fetcher := newTestCardActionFetcher(t, board)
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	for range actionPageSize + 120 {
		board.CreateCard("Bulk import")
	}
	first, err := fetcher.FetchChanges(context.Background(), seed)
	require.NoError(t, err)
	require.Len(t, first.Changes, actionPageSize)
	require.True(t, first.HasMorePages)
	second, err := fetcher.FetchChanges(context.Background(), first.Next)
	require.NoError(t, err)
	require.Len(t, second.Changes, 120)
	require.False(t, second.HasMorePages)
	seen := map[string]bool{}
	previous := ""
	for _, event := range append(first.Changes, second.Changes...) {
		require.False(t, seen[event.ID], "no action is delivered twice")
		seen[event.ID] = true
		require.Greater(t, event.Payload.ActionID, previous, "actions arrive oldest first")
		previous = event.Payload.ActionID
	}
	var position cardActionCursor
	require.NoError(t, json.Unmarshal([]byte(second.Next.Token), &position))
	require.LessOrEqual(t, len(position.SeenActionIDs), maximumSeenActionIDs)
}

func TestCardActionFetcherClassifiesProviderFailures(t *testing.T) {
	board := trelloboardfake.New(t)
	board.CreateCard("Existing card")
	fetcher := newTestCardActionFetcher(t, board)
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	board.RateLimitNextActionRequests(1)
	_, err = fetcher.FetchChanges(context.Background(), seed)
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, rateLimitWindow, rateLimited.RetryAfter, "Trello sends no Retry-After, so the pause is one window")
	require.Contains(t, err.Error(), "API_TOKEN_LIMIT_EXCEEDED")

	board.RejectNextCursor()
	_, err = fetcher.FetchChanges(context.Background(), seed)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a 400 for since reseeds")

	for name, token := range map[string]string{
		"undecodable":   "not json",
		"another board": `{"boardId":"5b6893f01cb3228998cf629e"}`,
		"bad watermark": `{"boardId":"` + board.BoardID + `","watermark":"yesterday"}`,
		"bad action":    `{"boardId":"` + board.BoardID + `","seenActionIds":["x"]}`,
		"bad floor":     `{"boardId":"` + board.BoardID + `","floorActionId":"x"}`,
	} {
		_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: token})
		require.True(t, pollingtrigger.IsCursorInvalid(err), name)
	}

	missing := newCardActionFetcher(fetcher.client, sdkgo.ConnectionRef{Provider: "trello", Name: "trello-cards"}, CardChangedTriggerConfiguration{BoardID: "5b6893f01cb3228998cf629e"})
	_, err = missing.SeedCursor(context.Background())
	require.Error(t, err)
	require.False(t, pollingtrigger.IsCursorInvalid(err), "a missing board backs off instead of reseeding")
	var limited *pollingtrigger.RateLimitedError
	require.False(t, errors.As(err, &limited))
}

func TestCardChangedTriggerConfigurationAcceptsOnlyABoardIDOrShortLink(t *testing.T) {
	for _, valid := range []string{"5b6893f01cb3228998cf629e", "AbCd1234"} {
		require.NoError(t, CardChangedTriggerConfiguration{BoardID: valid}.Validate(), valid)
	}
	for _, invalid := range []string{"", " AbCd1234", "https://trello.com/b/AbCd1234/support", "AbCd123"} {
		require.Error(t, CardChangedTriggerConfiguration{BoardID: invalid}.Validate(), invalid)
	}
}

func TestCardActionFetcherDeliversABurstOverTheSeenIDBoundExactlyOnce(t *testing.T) {
	board := trelloboardfake.New(t)
	fetcher := newTestCardActionFetcher(t, board)
	seed, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	burst := maximumSeenActionIDs + 3*actionPageSize/2
	for range burst {
		board.CreateCard("Bulk import")
	}
	events, next := fetchAll(t, fetcher, seed)
	delivered := map[string]bool{}
	for _, event := range events {
		require.False(t, delivered[event.ID], "no action inside one lookback window is delivered twice")
		delivered[event.ID] = true
	}
	require.Len(t, delivered, burst, "every action of the burst is delivered")
	var position cardActionCursor
	require.NoError(t, json.Unmarshal([]byte(next.Token), &position))
	require.Len(t, position.SeenActionIDs, maximumSeenActionIDs)
	require.LessOrEqual(t, len(next.Token), projectconfig.MaxPollCursorTokenBytes)
	page, err := fetcher.FetchChanges(context.Background(), next)
	require.NoError(t, err)
	require.Empty(t, page.Changes, "IDs dropped from the bounded seen set stay delivered")
}
