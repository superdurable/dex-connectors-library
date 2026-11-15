// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

func TestChangeLogFetcherSeedsWithoutBacklogAndPagesChanges(t *testing.T) {
	ctx := context.Background()
	changeLog, server := startChangeLogProvider()
	defer server.Close()
	changeLog.addChange("item-1", "existing")
	fetcher := &changeLogFetcher{api: &changeLogClient{baseURL: server.URL, httpClient: http.DefaultClient}}
	seeded, err := fetcher.SeedCursor(ctx)
	require.NoError(t, err)
	require.Equal(t, pollingtrigger.Cursor{Token: "1"}, seeded)
	changeLog.addChange("item-2", "added")
	page, err := fetcher.FetchChanges(ctx, seeded)
	require.NoError(t, err)
	require.Len(t, page.Changes, 1)
	require.Equal(t, "item-2@2", page.Changes[0].ID)
	require.Equal(t, ItemChanged{ItemID: "item-2", Sequence: 2, Title: "added"}, page.Changes[0].Payload)
	require.Equal(t, pollingtrigger.Cursor{Token: "2"}, page.Next)
	require.False(t, page.HasMorePages)
}

func TestChangeLogFetcherClassifiesExpiredCursorsAndRateLimits(t *testing.T) {
	ctx := context.Background()
	changeLog, server := startChangeLogProvider()
	defer server.Close()
	fetcher := &changeLogFetcher{api: &changeLogClient{baseURL: server.URL, httpClient: http.DefaultClient}}
	_, err := fetcher.FetchChanges(ctx, pollingtrigger.Cursor{Token: "9"})
	require.True(t, pollingtrigger.IsCursorInvalid(err))
	_, err = fetcher.FetchChanges(ctx, pollingtrigger.Cursor{Token: "not-a-sequence"})
	require.True(t, pollingtrigger.IsCursorInvalid(err))
	changeLog.nextStatus, changeLog.nextRetryAfter = http.StatusTooManyRequests, "30"
	_, err = fetcher.FetchChanges(ctx, pollingtrigger.Cursor{Token: "0"})
	var rateLimited *pollingtrigger.RateLimitedError
	require.ErrorAs(t, err, &rateLimited)
	require.Equal(t, 30*time.Second, rateLimited.RetryAfter)
}

func TestItemTriggerDeliversOnlyChangesAfterTheSeed(t *testing.T) {
	changeLog, server := startChangeLogProvider()
	defer server.Close()
	changeLog.addChange("item-1", "existing")
	delivered := make(chan sdkgo.TriggerEvent[ItemChanged], 10)
	definition := itemChangedBindingDefinition.Definition
	binding := sdkgo.TriggerBindingRef{Connection: sdkgo.ConnectionRef{Provider: "changelog", Name: ConnectionName}, Trigger: definition.Trigger, Name: "syncItem"}
	cursor := pollingtrigger.NewMemoryCursorStore()
	poller, err := pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[ItemChanged]{
		Definition: definition, Binding: binding, Cursor: cursor,
		Fetcher: &changeLogFetcher{api: &changeLogClient{baseURL: server.URL, httpClient: http.DefaultClient}},
		Target: sdkgo.TriggerTargetFunc[ItemChanged](func(_ context.Context, event sdkgo.TriggerEvent[ItemChanged]) error {
			delivered <- event
			return nil
		}),
	})
	require.NoError(t, err)
	noJitter := 0.0
	runner, err := pollingtrigger.NewRunner(pollingtrigger.RunnerConfig{
		Connection: binding.Connection, Schedule: pollingtrigger.PollSchedule{Interval: time.Second, JitterFraction: &noJitter},
		Pollers: []pollingtrigger.Poller{poller},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	require.Eventually(t, func() bool {
		record, err := cursor.ReadPollCursor(ctx)
		return err == nil && record.State != nil
	}, 10*time.Second, 10*time.Millisecond)
	changeLog.addChange("item-2", "added")
	select {
	case event := <-delivered:
		require.Equal(t, "item-2@2", event.ID)
	case <-time.After(10 * time.Second):
		t.Fatal("the new change was not delivered")
	}
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	require.Empty(t, delivered)
}
