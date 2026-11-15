// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pollingtrigger

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestRateLimitPausesEveryPollerOfTheConnection(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	storeA := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: &operationLog{}}
	storeB := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: &operationLog{}}
	seedCursor(t, storeA, "0")
	seedCursor(t, storeB, "0")
	fetcherA := &changeLogFetcher{log: log, name: "a", failures: []error{MarkRateLimited(errors.New("429"), 120*time.Second)}}
	fetcherB := &changeLogFetcher{log: log, name: "b"}
	runner := newTestRunner(t, PollSchedule{},
		newTestBindingPoller(t, "first", storeA, fetcherA, &recordingTarget{log: log}),
		newTestBindingPoller(t, "second", storeB, fetcherB, &recordingTarget{log: log}))
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("fetch b after 0") == 1 }), context.Canceled)
	require.Equal(t, []string{"fetch a after 0", "fetch a after 0", "fetch b after 0"}, log.snapshot())
	require.Equal(t, []time.Duration{3 * time.Second, 120 * time.Second}, clock.recordedWaits()[:2])
}

func TestRateLimitWithoutRetryAfterAndFailuresBackOffExponentially(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: &operationLog{}}
	seedCursor(t, store, "0")
	failure := errors.New("503")
	fetcher := &changeLogFetcher{log: log, name: "a", failures: []error{
		failure, failure, failure, failure, failure, nil, MarkRateLimited(failure, 0), MarkRateLimited(failure, 0),
	}}
	runner := newTestRunner(t, PollSchedule{Interval: time.Minute, MaxBackoff: 5 * time.Minute},
		newTestBindingPoller(t, "first", store, fetcher, &recordingTarget{log: log}))
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("fetch a after 0") == 9 }), context.Canceled)
	require.Equal(t, []time.Duration{
		time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute,
		time.Minute, time.Minute, 2 * time.Minute,
	}, clock.recordedWaits()[1:9])
}

func TestJitterStaysWithinItsBounds(t *testing.T) {
	for _, jitter := range []float64{0.000001, 0.999999} {
		clock := installFakeClock(t, jitter)
		log := &operationLog{}
		store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: &operationLog{}}
		seedCursor(t, store, "0")
		fetcher := &changeLogFetcher{log: log, name: "a"}
		runner := newTestRunner(t, PollSchedule{Interval: 100 * time.Second},
			newTestBindingPoller(t, "first", store, fetcher, &recordingTarget{log: log}))
		require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("fetch a after 0") == 1 }), context.Canceled)
		waits := clock.recordedWaits()
		require.InDelta(t, float64(10*time.Second)*jitter, float64(waits[0]), float64(time.Millisecond))
		require.InDelta(t, float64(100*time.Second)*(0.9+0.2*jitter), float64(waits[1]), float64(time.Millisecond))
	}
}

func TestMaxPagesPerPollYieldsToTheNextPoller(t *testing.T) {
	clock := installFakeClock(t, 0)
	log := &operationLog{}
	storeA := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: &operationLog{}}
	storeB := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: &operationLog{}}
	seedCursor(t, storeA, "0")
	seedCursor(t, storeB, "0")
	fetcherA := &changeLogFetcher{log: log, name: "a", pageSize: 1}
	fetcherA.append("a1", "a2", "a3")
	fetcherB := &changeLogFetcher{log: log, name: "b", pageSize: 1}
	runner := newTestRunner(t, PollSchedule{MaxPagesPerPoll: 2},
		newTestBindingPoller(t, "first", storeA, fetcherA, &recordingTarget{log: log}),
		newTestBindingPoller(t, "second", storeB, fetcherB, &recordingTarget{log: log}))
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool { return log.count("handle a3") == 1 }), context.Canceled)
	require.Equal(t, []string{
		"fetch a after 0", "handle a1", "fetch a after 1", "handle a2", "fetch b after 0", "fetch a after 2", "handle a3",
	}, log.snapshot())
}

func TestCancellationBeforeCommitLeavesTheCursorUnchanged(t *testing.T) {
	for _, scenario := range []string{"during fetch", "between record and commit"} {
		t.Run(scenario, func(t *testing.T) {
			clock := installFakeClock(t, 0)
			log := &operationLog{}
			store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: log}
			seedCursor(t, store, "0")
			fetcher := &changeLogFetcher{log: log, name: "a"}
			fetcher.append("a")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			target := &durableTarget{recordingTarget: recordingTarget{log: log}}
			if scenario == "during fetch" {
				fetcher.onFetch = func(context.Context) { cancel() }
			}
			runner := newTestRunner(t, PollSchedule{}, newTestBindingPoller(t, "first", store, fetcher, cancelAfterPrepare{durableTarget: target, cancel: cancel}))
			clock.cancel = cancel
			require.ErrorIs(t, runner.Run(ctx), context.Canceled)
			require.Zero(t, log.count("replace 1"))
			require.Equal(t, "0", store.state(t).Token)
		})
	}
}

// cancelAfterPrepare cancels the run once an event is recorded, as a crash between record and commit.
type cancelAfterPrepare struct {
	*durableTarget
	cancel context.CancelFunc
}

func (target cancelAfterPrepare) PrepareTrigger(ctx context.Context, event sdkgo.TriggerEvent[item]) error {
	err := target.durableTarget.PrepareTrigger(ctx, event)
	target.cancel()
	return err
}

func TestRunRejectsAnUndecodableCursorBeforeFetching(t *testing.T) {
	installFakeClock(t, 0)
	objects := &memoryObjectStore{objects: map[string]projectconfig.Object{}}
	scope := projectconfig.Scope{ProjectID: "p", Kind: "live"}
	key := projectconfig.PollCursorKey{ConnectorID: "changelog", ConnectionName: "main", TriggerName: "itemChanged", BindingName: "first"}
	store, err := projectconfig.NewPollCursorStore(objects, scope, key)
	require.NoError(t, err)
	_, err = store.ReadPollCursor(context.Background())
	require.NoError(t, err)
	for path := range objects.objects {
		objects.objects[path] = projectconfig.Object{Key: path, ETag: "x", Contents: []byte(`{"schemaVersion":"other","state":null}`)}
	}
	log := &operationLog{}
	fetcher := &changeLogFetcher{log: log, name: "a"}
	var records bytes.Buffer
	poller := newTestBindingPoller(t, "first", store, fetcher, &recordingTarget{log: log})
	runner, err := NewRunner(RunnerConfig{Connection: testConnection, Schedule: PollSchedule{Interval: time.Minute}, Pollers: []Poller{poller},
		Logger: slog.New(slog.NewTextHandler(&records, nil))})
	require.NoError(t, err)
	require.ErrorIs(t, runner.Run(context.Background()), projectconfig.ErrPollCursorDocumentInvalid)
	require.Empty(t, log.snapshot())
	require.Contains(t, records.String(), `level=ERROR msg="poll cursor document invalid"`)
	for _, object := range objects.objects {
		require.Equal(t, `{"schemaVersion":"other","state":null}`, string(object.Contents))
	}
}

func TestPollRecordsCarryBindingIdentityAndNoPayloads(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	store := &loggedCursorStore{CursorStore: NewMemoryCursorStore(), log: log}
	fetcher := &changeLogFetcher{log: log, name: "a", seedFailures: []error{errors.New("provider unavailable")}}
	fetcher.append("secret-title")
	failure := errors.New("Dex is unavailable")
	target := &durableTarget{recordingTarget: recordingTarget{log: log, handleErrors: map[string][]error{"new": {failure}}}}
	var records bytes.Buffer
	runner, err := NewRunner(RunnerConfig{Connection: testConnection, Schedule: PollSchedule{Interval: time.Minute},
		Pollers: []Poller{newTestBindingPoller(t, "first", store, fetcher, target)}, Logger: slog.New(slog.NewTextHandler(&records, nil))})
	require.NoError(t, err)
	err = runUntil(t, clock, runner, func() bool {
		if log.count("replace 1") == 1 && log.count("fetch a after 1") == 0 {
			fetcher.append("new")
		}
		return log.count("handle new") == 2
	})
	require.ErrorIs(t, err, context.Canceled)
	text := records.String()
	identity := "connector=changelog connection=main trigger=itemChanged binding=first"
	for _, expected := range []string{
		`level=WARN msg="poll failed; retrying" ` + identity + ` attempt=1 delay=1m0s error="provider unavailable"`,
		`level=INFO msg="poll cursor seeded" ` + identity + ` reset=false`,
		`level=WARN msg="trigger delivery failed; retrying" ` + identity + ` event_id=new attempt=1 delay=1m0s error="Dex is unavailable"`,
		`level=INFO msg="trigger delivered after retry" ` + identity + ` event_id=new attempts=2`,
	} {
		require.Contains(t, text, expected)
	}
	require.False(t, strings.Contains(text, "secret-title"))
}
