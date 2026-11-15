// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pollingtrigger

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestBindingPollerSeedsOnceWithoutBacklog(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: log}
	fetcher := &changeLogFetcher{log: log, name: "feed", pageSize: 10}
	fetcher.append("old-1", "old-2", "old-3")
	target := &durableTarget{recordingTarget: recordingTarget{log: log}}
	runner := newTestRunner(t, PollSchedule{}, newTestBindingPoller(t, "main", store, fetcher, target))
	err := runUntil(t, clock, runner, func() bool { return log.count("fetch feed after 3") == 1 })
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []string{"read", "replay ", "read", "seed feed", "replace 3", "read", "fetch feed after 3"}, log.snapshot())
	require.Equal(t, int64(1), store.state(t).CommitCount, "an empty page writes nothing")
	// The first poll waits a uniform share of Interval*JitterFraction; jitter 0.5 is the midpoint.
	require.Equal(t, 3*time.Second, clock.recordedWaits()[0])

	second := newTestRunner(t, PollSchedule{}, newTestBindingPoller(t, "main", store, fetcher, target))
	fetcher.append("new-1")
	err = runUntil(t, clock, second, func() bool { return log.count("handle new-1") == 1 })
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, log.count("seed feed"))
}

func TestBindingPollerRecordsBeforeCommitAndDeliversAfter(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		target   sdkgo.TriggerTarget[item]
		expected []string
	}{
		{"durable target", &durableTarget{}, []string{"read", "fetch feed after 0", "prepare a", "prepare b", "replace 2", "handle a", "handle b"}},
		{"non-durable target", &recordingTarget{}, []string{"read", "fetch feed after 0", "handle a", "handle b", "replace 2"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			clock := installFakeClock(t, 0.5)
			log := &operationLog{}
			switch target := scenario.target.(type) {
			case *durableTarget:
				target.log = log
			case *recordingTarget:
				target.log = log
			}
			store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: log}
			seedCursor(t, store, "0")
			fetcher := &changeLogFetcher{log: log, name: "feed", pageSize: 10}
			fetcher.append("a", "b")
			runner := newTestRunner(t, PollSchedule{}, newTestBindingPoller(t, "main", store, fetcher, scenario.target))
			require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("replace 2") == 1 }), context.Canceled)
			operations := log.snapshot()
			require.Equal(t, scenario.expected, operations[len(operations)-len(scenario.expected):])
		})
	}
}

func TestBindingPollerRetriesARecordFailureFromTheOldCursor(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: log}
	seedCursor(t, store, "0")
	fetcher := &changeLogFetcher{log: log, name: "feed", pageSize: 10}
	fetcher.append("a", "b")
	target := &durableTarget{recordingTarget: recordingTarget{log: log}, prepareErrors: map[string][]error{"b": {projectconfig.ErrTriggerInboxFull}}}
	runner := newTestRunner(t, PollSchedule{}, newTestBindingPoller(t, "main", store, fetcher, target))
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("handle b") == 1 }), context.Canceled)
	require.Equal(t, []string{
		"read", "fetch feed after 0", "prepare a", "prepare b",
		"read", "fetch feed after 0", "prepare b", "replace 2", "handle a", "handle b",
	}, log.snapshot()[4:])
	// The failed poll waits one Interval of backoff before the retry.
	require.Equal(t, time.Minute, clock.recordedWaits()[1])
}

func TestBindingPollerRetriesAFailedEventBeforeFetchingAgain(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: log}
	seedCursor(t, store, "0")
	fetcher := &changeLogFetcher{log: log, name: "feed", pageSize: 10}
	fetcher.append("a", "b")
	failure := errors.New("Dex is unavailable")
	target := &durableTarget{recordingTarget: recordingTarget{log: log, handleErrors: map[string][]error{"a": {failure, failure}}}}
	runner := newTestRunner(t, PollSchedule{}, newTestBindingPoller(t, "main", store, fetcher, target))
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("fetch feed after 2") == 1 }), context.Canceled)
	require.Equal(t, []string{
		"read", "fetch feed after 0", "prepare a", "prepare b", "replace 2", "handle a",
		"handle a", "handle a", "handle b", "read", "fetch feed after 2",
	}, log.snapshot()[4:])
	waits := clock.recordedWaits()
	require.Equal(t, []time.Duration{time.Minute, 2 * time.Minute, time.Minute}, waits[1:4])
}

func TestBindingPollerDeliversRecordedEventsAfterACommitConflict(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	memory := NewMemoryCursorStore()
	store := &loggedCursorStore{CursorStore: memory, log: log}
	seedCursor(t, store, "0")
	store.beforeCommit = func(ctx context.Context) {
		record, err := memory.ReadPollCursor(ctx)
		require.NoError(t, err)
		winner := *record.State
		winner.Token, winner.CommitCount = "1", winner.CommitCount+1
		_, err = memory.ReplacePollCursor(ctx, record.Revision, winner)
		require.NoError(t, err)
	}
	fetcher := &changeLogFetcher{log: log, name: "feed", pageSize: 10}
	fetcher.append("a", "b")
	target := &durableTarget{recordingTarget: recordingTarget{log: log}}
	runner := newTestRunner(t, PollSchedule{}, newTestBindingPoller(t, "main", store, fetcher, target))
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("fetch feed after 1") == 1 }), context.Canceled)
	require.Equal(t, []string{
		"read", "fetch feed after 0", "prepare a", "prepare b", "replace 2", "handle a", "handle b",
		"read", "fetch feed after 1", "prepare b", "replace 2", "handle b",
	}, log.snapshot()[4:])
}

func TestBindingPollerReseedsAnInvalidCursor(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: log}
	seedCursor(t, store, "0")
	fetcher := &changeLogFetcher{log: log, name: "feed", pageSize: 10, failures: []error{MarkCursorInvalid(errors.New("410 Gone"))}}
	fetcher.append("gap-1", "gap-2")
	target := &durableTarget{recordingTarget: recordingTarget{log: log}}
	runner := newTestRunner(t, PollSchedule{}, newTestBindingPoller(t, "main", store, fetcher, target))
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("fetch feed after 2") == 1 }), context.Canceled)
	require.Equal(t, []string{"read", "fetch feed after 0", "seed feed", "replace 2", "read", "fetch feed after 2"}, log.snapshot()[4:])
	state := store.state(t)
	require.Equal(t, int64(1), state.ResetCount)
	require.NotNil(t, state.LastResetAtUnixMs)
	require.Zero(t, log.count("prepare gap-1"))
}

func seedCursor(t *testing.T, store CursorStore, token string) {
	t.Helper()
	record, err := store.ReadPollCursor(context.Background())
	require.NoError(t, err)
	_, err = store.ReplacePollCursor(context.Background(), record.Revision, projectconfig.PollCursorState{Token: token, CommitCount: 1})
	require.NoError(t, err)
}

// replaceChangesOnFetch makes the fetch with that number see titles as the whole feed, as a provider whose
// listing changed between a failed poll and its retry from the same cursor.
func replaceChangesOnFetch(fetcher *changeLogFetcher, fetchNumber int, titles ...string) {
	fetches := 0
	fetcher.onFetch = func(context.Context) {
		fetches++
		if fetches != fetchNumber {
			return
		}
		fetcher.mutex.Lock()
		fetcher.changes = nil
		fetcher.mutex.Unlock()
		fetcher.append(titles...)
	}
}

func TestBindingPollerRetrySkipsConsumedEventsByIDWhenThePageChanges(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: log}
	seedCursor(t, store, "0")
	fetcher := &changeLogFetcher{log: log, name: "feed", pageSize: 10}
	fetcher.append("a", "b", "c")
	// The retry from cursor 0 no longer lists a, so b and c move up one position.
	replaceChangesOnFetch(fetcher, 2, "b", "c", "d")
	target := &recordingTarget{log: log, handleErrors: map[string][]error{"c": {errors.New("Dex is unavailable")}}}
	runner := newTestRunner(t, PollSchedule{}, newTestBindingPoller(t, "main", store, fetcher, target))
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("replace 3") == 1 }), context.Canceled)
	require.Equal(t, []string{
		"read", "fetch feed after 0", "handle a", "handle b", "handle c",
		"read", "fetch feed after 0", "handle c", "handle d", "replace 3",
	}, log.snapshot()[3:])
}

func TestBindingPollerDeliversARecordedEventTheRetryNoLongerLists(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: log}
	seedCursor(t, store, "0")
	fetcher := &changeLogFetcher{log: log, name: "feed", pageSize: 10}
	fetcher.append("a", "b")
	replaceChangesOnFetch(fetcher, 2, "b")
	target := &durableTarget{recordingTarget: recordingTarget{log: log}, prepareErrors: map[string][]error{"b": {errors.New("inbox write failed")}}}
	runner := newTestRunner(t, PollSchedule{}, newTestBindingPoller(t, "main", store, fetcher, target))
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("handle b") == 1 }), context.Canceled)
	require.Equal(t, []string{
		"read", "fetch feed after 0", "prepare a", "prepare b",
		"read", "fetch feed after 0", "prepare b", "replace 1", "handle a", "handle b",
	}, log.snapshot()[4:])
}

func TestBindingPollerDeliversRecordedEventsWhenTheRetryPageIsEmpty(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: log}
	seedCursor(t, store, "0")
	fetcher := &changeLogFetcher{log: log, name: "feed", pageSize: 10}
	fetcher.append("a", "b")
	replaceChangesOnFetch(fetcher, 2)
	target := &durableTarget{recordingTarget: recordingTarget{log: log}, prepareErrors: map[string][]error{"b": {errors.New("inbox write failed")}}}
	runner := newTestRunner(t, PollSchedule{}, newTestBindingPoller(t, "main", store, fetcher, target))
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("handle a") == 1 }), context.Canceled)
	require.Equal(t, []string{
		"read", "fetch feed after 0", "prepare a", "prepare b", "read", "fetch feed after 0", "handle a",
	}, log.snapshot()[4:])
}
