//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
)

// TestPollingTriggerDeliversOnlyNewChangesAcrossRestartsWithRealDex covers R1 and R2: no backlog on the
// first start, and changes that arrive while the Worker and the process are down each start one Flow.
func TestPollingTriggerDeliversOnlyNewChangesAcrossRestartsWithRealDex(t *testing.T) {
	environment := newPollEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	backlog := environment.provider.appendItems("backlog-"+environment.testRunID, 5)
	first := environment.startProcess(t, pollProcessOptions{})
	waitForSeed(t, environment, "main")
	fresh := environment.provider.appendItems("fresh-"+environment.testRunID, 3)
	for index, itemID := range fresh {
		require.Equal(t, pollItem{ItemID: itemID, Sequence: 6 + index}, waitForPollItemFlow(ctx, t, environment.harness.client, environment.itemFlowID(itemID, 6+index)))
	}
	for index, itemID := range backlog {
		_, err := environment.harness.client.DescribeFlow(ctx, environment.itemFlowID(itemID, 1+index))
		require.Error(t, err, "backlog item %s must not start a Flow", itemID)
	}
	first.stop(t)

	environment.harness.stopWorker(t)
	offline := environment.provider.appendItems("offline-"+environment.testRunID, 4)
	for restart := 0; restart < 2; restart++ {
		process := environment.startProcess(t, pollProcessOptions{})
		require.Eventually(t, func() bool { return environment.cursorState(t, "main").Token == "12" }, 30*time.Second, 50*time.Millisecond)
		process.stop(t)
	}
	commitsWhileDown := environment.cursorState(t, "main").CommitCount
	environment.harness.startWorker(t)
	environment.startProcess(t, pollProcessOptions{})
	for index, itemID := range offline {
		require.Equal(t, pollItem{ItemID: itemID, Sequence: 9 + index}, waitForPollItemFlow(ctx, t, environment.harness.client, environment.itemFlowID(itemID, 9+index)))
	}
	require.Eventually(t, func() bool { return len(environment.pendingEvents(t, "main")) == 0 }, 30*time.Second, 50*time.Millisecond)
	require.Equal(t, commitsWhileDown, environment.cursorState(t, "main").CommitCount, "restarts and empty polls write no commits")
	require.Len(t, environment.logs.find("poll cursor seeded", map[string]string{"binding": "main"}), 1)
}

// TestPollingTriggerRecoversACrashAroundTheCommitWithRealDex covers R3 and R4.
func TestPollingTriggerRecoversACrashAroundTheCommitWithRealDex(t *testing.T) {
	for _, scenario := range []string{"crash between record and commit", "crash after commit before delivery"} {
		t.Run(scenario, func(t *testing.T) {
			environment := newPollEnvironment(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			environment.seedAndStop(t)
			itemIDs := environment.provider.appendItems("crash-"+environment.testRunID, 3)
			crashed := environment.startProcess(t, pollProcessOptions{wrapCursor: func(cursor pollingtrigger.CursorStore, crash context.CancelFunc) pollingtrigger.CursorStore {
				hooked := &commitHookCursor{CursorStore: cursor}
				if scenario == "crash between record and commit" {
					hooked.beforeCommit = func() error { crash(); return errSimulatedCrash }
				} else {
					hooked.afterCommit = crash
				}
				return hooked
			}})
			select {
			case err := <-crashed.result:
				require.ErrorIs(t, err, context.Canceled)
				close(crashed.result)
			case <-ctx.Done():
				t.Fatal("the simulated crash did not stop the process")
			}
			require.Len(t, environment.pendingEvents(t, "main"), 3, "every recorded event survives the crash")
			requestsBeforeRestart := len(environment.provider.changeRequests())

			environment.startProcess(t, pollProcessOptions{})
			for index, itemID := range itemIDs {
				require.Equal(t, pollItem{ItemID: itemID, Sequence: 1 + index}, waitForPollItemFlow(ctx, t, environment.harness.client, environment.itemFlowID(itemID, 1+index)))
				require.GreaterOrEqual(t, environment.deliveries(itemID+"@"+strconv.Itoa(1+index)), 1)
			}
			require.Eventually(t, func() bool { return len(environment.pendingEvents(t, "main")) == 0 }, 30*time.Second, 50*time.Millisecond)
			require.Eventually(t, func() bool { return len(environment.provider.changeRequests()) > requestsBeforeRestart }, 30*time.Second, 50*time.Millisecond)
			afterRestart := environment.provider.changeRequests()[requestsBeforeRestart:]
			if scenario == "crash after commit before delivery" {
				for _, request := range afterRestart {
					require.Equal(t, "3", request.after, "the committed page is never fetched again")
				}
			} else {
				require.Equal(t, "0", afterRestart[0].after, "an uncommitted page is fetched again from the old cursor")
			}
		})
	}
}

// TestPollingTriggerReseedsAnExpiredCursorWithRealDex covers R5: a 410 reseeds and the gap is logged.
func TestPollingTriggerReseedsAnExpiredCursorWithRealDex(t *testing.T) {
	environment := newPollEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	environment.provider.appendItems("before-"+environment.testRunID, 3)
	environment.seedAndStop(t)
	environment.provider.truncate()
	gap := environment.provider.appendItems("gap-"+environment.testRunID, 2)
	environment.startProcess(t, pollProcessOptions{})
	require.Eventually(t, func() bool { return environment.cursorState(t, "main").ResetCount == 1 }, 30*time.Second, 50*time.Millisecond)
	after := environment.provider.appendItems("after-"+environment.testRunID, 2)
	for index, itemID := range after {
		waitForPollItemFlow(ctx, t, environment.harness.client, environment.itemFlowID(itemID, 3+index))
	}
	for index, itemID := range gap {
		require.Zero(t, environment.deliveries(itemID+"@"+strconv.Itoa(1+index)))
	}
	reset := environment.logs.find("poll cursor invalid; reseeding", map[string]string{"binding": "main"})
	require.Len(t, reset, 1)
	require.Equal(t, slog.LevelWarn, reset[0].level)
	require.Equal(t, "1", reset[0].attrs["reset_count"])
	require.NotNil(t, environment.cursorState(t, "main").LastResetAtUnixMs)
}

// TestPollingTriggerHonorsRetryAfterWithRealDex covers R6; elapsed time is the behavior under test.
func TestPollingTriggerHonorsRetryAfterWithRealDex(t *testing.T) {
	environment := newPollEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	environment.seedAndStop(t)
	environment.provider.script(changeLogResponse{status: http.StatusTooManyRequests, retryAfter: "3"})
	itemIDs := environment.provider.appendItems("limited-"+environment.testRunID, 1)
	requestsBeforeStart := len(environment.provider.changeRequests())
	environment.startProcess(t, pollProcessOptions{})
	waitForPollItemFlow(ctx, t, environment.harness.client, environment.itemFlowID(itemIDs[0], 1))
	requests := environment.provider.changeRequests()[requestsBeforeStart:]
	require.GreaterOrEqual(t, len(requests), 2)
	require.GreaterOrEqual(t, requests[1].at.Sub(requests[0].at), 3*time.Second)
	paused := environment.logs.find("poll rate limited; pausing connection", map[string]string{"binding": "main"})
	require.Len(t, paused, 1)
	require.Equal(t, "3s", paused[0].attrs["delay"])
}

// TestPollingTriggerReplicasShareOneCursorWithRealDex covers R7: two replicas never lose or regress.
func TestPollingTriggerReplicasShareOneCursorWithRealDex(t *testing.T) {
	environment := newPollEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	environment.seedAndStop(t)
	commitCounts := make(chan int64, 1000)
	observe := func(cursor pollingtrigger.CursorStore, _ context.CancelFunc) pollingtrigger.CursorStore {
		return &commitObservingCursor{CursorStore: cursor, commitCounts: commitCounts}
	}
	environment.startProcess(t, pollProcessOptions{pageSize: 10, wrapCursor: observe})
	environment.startProcess(t, pollProcessOptions{pageSize: 10, wrapCursor: observe})
	itemIDs := environment.provider.appendItems("replica-"+environment.testRunID, 50)
	for index, itemID := range itemIDs {
		waitForPollItemFlow(ctx, t, environment.harness.client, environment.itemFlowID(itemID, 1+index))
	}
	require.Eventually(t, func() bool { return len(environment.pendingEvents(t, "main")) == 0 }, 30*time.Second, 50*time.Millisecond)
	require.Equal(t, "50", environment.cursorState(t, "main").Token)
	seen := map[int64]bool{}
	for len(commitCounts) > 0 {
		count := <-commitCounts
		require.False(t, seen[count], "commit count %d was written twice", count)
		seen[count] = true
	}
}

// commitObservingCursor reports every successful commit's CommitCount.
type commitObservingCursor struct {
	pollingtrigger.CursorStore
	commitCounts chan int64
}

func (cursor *commitObservingCursor) ReplacePollCursor(ctx context.Context, revision string, state projectconfig.PollCursorState) (projectconfig.PollCursorRecord, error) {
	record, err := cursor.CursorStore.ReplacePollCursor(ctx, revision, state)
	if err == nil {
		cursor.commitCounts <- state.CommitCount
	}
	return record, err
}

// TestPollingTriggerFeedServesTwoBindingsWithRealDex covers R8: one feed, a durable Flow member and a
// non-durable member, each receiving exactly its selected events.
func TestPollingTriggerFeedServesTwoBindingsWithRealDex(t *testing.T) {
	environment := newPollEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	key := environment.inboxKey("odd")
	inbox, err := projectconfig.NewTriggerInbox(environment.objects, pollTriggerScope, key)
	require.NoError(t, err)
	durable, err := provider.NewDurableTriggerTarget(inbox, key, environment.flowTarget(), provider.WithTriggerLogger(environment.logs.logger()))
	require.NoError(t, err)
	selectSequences := func(isOdd bool) pollingtrigger.FeedEventSelector[sdkgo.TriggerEvent[pollItem], pollItem] {
		return func(change sdkgo.TriggerEvent[pollItem], _ *projectconfig.KeyedSnapshotEntry) []sdkgo.TriggerEvent[pollItem] {
			if (change.Payload.Sequence%2 == 1) != isOdd {
				return nil
			}
			return []sdkgo.TriggerEvent[pollItem]{change}
		}
	}
	oddMember, err := pollingtrigger.NewFeedMember(pollDefinition, sdkgo.TriggerBindingRef{Connection: pollConnection, Trigger: pollDefinition.Trigger, Name: "odd"}, selectSequences(true), durable)
	require.NoError(t, err)
	evenEvents := make(chan string, 100)
	evenMember, err := pollingtrigger.NewFeedMember(pollDefinition, sdkgo.TriggerBindingRef{Connection: pollConnection, Trigger: pollDefinition.Trigger, Name: "even"}, selectSequences(false),
		sdkgo.TriggerTarget[pollItem](sdkgo.TriggerTargetFunc[pollItem](func(_ context.Context, event sdkgo.TriggerEvent[pollItem]) error {
			evenEvents <- event.ID
			return nil
		})))
	require.NoError(t, err)
	feedCursor, err := projectconfig.NewPollFeedCursorStore(environment.objects, pollTriggerScope, projectconfig.PollFeedKey{ConnectorID: "changelog", ConnectionName: pollConnection.Name, FeedName: "itemFeed"})
	require.NoError(t, err)
	poller, err := pollingtrigger.NewFeedPoller(pollingtrigger.FeedPollerConfig[sdkgo.TriggerEvent[pollItem]]{
		Name: "itemFeed", Cursor: feedCursor, Fetcher: changeLogFetcher{baseURL: environment.provider.server.URL, pageSize: 100},
		Members: []pollingtrigger.FeedMember[sdkgo.TriggerEvent[pollItem]]{oddMember, evenMember},
	})
	require.NoError(t, err)
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	result := make(chan error, 1)
	go func() { result <- newPollRunner(t, environment, poller).Run(runCtx) }()
	require.Eventually(t, func() bool {
		record, err := feedCursor.ReadPollCursor(ctx)
		return err == nil && record.State != nil
	}, 20*time.Second, 50*time.Millisecond)
	itemIDs := environment.provider.appendItems("feed-"+environment.testRunID, 4)
	for _, sequence := range []int{1, 3} {
		waitForPollItemFlow(ctx, t, environment.harness.client, environment.itemFlowID(itemIDs[sequence-1], sequence))
	}
	received := []string{<-evenEvents, <-evenEvents}
	require.Equal(t, []string{itemIDs[1] + "@2", itemIDs[3] + "@4"}, received)
	for _, sequence := range []int{2, 4} {
		require.Zero(t, environment.deliveries(itemIDs[sequence-1]+"@"+strconv.Itoa(sequence)))
	}
	stop()
	require.True(t, errors.Is(<-result, context.Canceled))
	require.Empty(t, evenEvents)
}
