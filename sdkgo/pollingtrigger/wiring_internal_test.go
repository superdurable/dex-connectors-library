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
)

func TestNewRunnerAndPollersRejectInvalidWiring(t *testing.T) {
	log := &operationLog{}
	fetcher := &changeLogFetcher{log: log}
	target := &recordingTarget{log: log}
	_, err := NewRunner(RunnerConfig{Connection: testConnection, Schedule: PollSchedule{Interval: time.Minute}})
	require.ErrorContains(t, err, "at least one poller")
	other := testBinding("itemChanged", "other")
	other.Connection.Name = "secondary"
	otherPoller, err := NewBindingPoller(BindingPollerConfig[item]{Definition: testDefinition("itemChanged"), Binding: other,
		Cursor: NewMemoryCursorStore(), Fetcher: fetcher, Target: target})
	require.NoError(t, err)
	_, err = NewRunner(RunnerConfig{Connection: testConnection, Schedule: PollSchedule{Interval: time.Minute}, Pollers: []Poller{otherPoller}})
	require.ErrorContains(t, err, "another connection")
	_, err = NewRunner(RunnerConfig{Connection: testConnection, Schedule: PollSchedule{Interval: time.Minute}, Pollers: []Poller{
		newTestBindingPoller(t, "same", NewMemoryCursorStore(), fetcher, target), newTestBindingPoller(t, "same", NewMemoryCursorStore(), fetcher, target),
	}})
	require.ErrorContains(t, err, "two pollers")
	attached := newTestBindingPoller(t, "attached", NewMemoryCursorStore(), fetcher, target)
	newTestRunner(t, PollSchedule{}, attached)
	_, err = NewRunner(RunnerConfig{Connection: testConnection, Schedule: PollSchedule{Interval: time.Minute}, Pollers: []Poller{attached}})
	require.ErrorContains(t, err, "already belongs")
	for _, schedule := range []PollSchedule{{}, {Interval: time.Minute, MaxBackoff: time.Second}, {Interval: time.Minute, MaxPagesPerPoll: -1}} {
		_, err = NewRunner(RunnerConfig{Connection: testConnection, Schedule: schedule, Pollers: []Poller{newTestBindingPoller(t, "s", NewMemoryCursorStore(), fetcher, target)}})
		require.Error(t, err)
	}
	tooMuchJitter := 0.6
	_, err = NewRunner(RunnerConfig{Connection: testConnection, Schedule: PollSchedule{Interval: time.Minute, JitterFraction: &tooMuchJitter},
		Pollers: []Poller{newTestBindingPoller(t, "j", NewMemoryCursorStore(), fetcher, target)}})
	require.Error(t, err)

	for _, config := range []BindingPollerConfig[item]{
		{Definition: testDefinition("itemChanged"), Binding: testBinding("itemChanged", "a"), Fetcher: fetcher, Target: target},
		{Definition: testDefinition("itemChanged"), Binding: testBinding("itemChanged", "a"), Cursor: NewMemoryCursorStore(), Target: target},
		{Definition: testDefinition("itemChanged"), Binding: testBinding("itemChanged", "a"), Cursor: NewMemoryCursorStore(), Fetcher: fetcher},
		{Definition: testDefinition("itemChanged"), Binding: testBinding("otherTrigger", "a"), Cursor: NewMemoryCursorStore(), Fetcher: fetcher, Target: target},
		{Definition: testDefinition("itemChanged"), Binding: testBinding("itemChanged", "a"), Cursor: NewMemoryCursorStore(), Fetcher: fetcher, Target: target,
			Snapshot: &KeyedSnapshotOptions{MaxEntries: 200_000}},
	} {
		_, err := NewBindingPoller(config)
		require.Error(t, err)
	}
	member, err := NewFeedMember(testDefinition("fileCreated"), testBinding("fileCreated", "created"), selectFileEvents("created"), sdkgo.TriggerTarget[item](target))
	require.NoError(t, err)
	for _, config := range []FeedPollerConfig[fileChange]{
		{Name: "Feed", Cursor: NewMemoryCursorStore(), Fetcher: &fileFeedFetcher{}, Members: []FeedMember[fileChange]{member}},
		{Name: "files", Cursor: NewMemoryCursorStore(), Fetcher: &fileFeedFetcher{}},
		{Name: "files", Fetcher: &fileFeedFetcher{}, Members: []FeedMember[fileChange]{member}},
	} {
		_, err := NewFeedPoller(config)
		require.Error(t, err)
	}
	_, err = NewFeedMember[fileChange, item](testDefinition("fileCreated"), testBinding("fileCreated", "created"), nil, target)
	require.Error(t, err)
}

type countingLimiter struct {
	waits   int
	failure error
}

func (limiter *countingLimiter) WaitForPollRequest(context.Context) error {
	limiter.waits++
	return limiter.failure
}

func TestRequestLimiterGatesEveryProviderRequest(t *testing.T) {
	require.NoError(t, WaitForPollRequest(context.Background()))
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	limiter := &countingLimiter{}
	fetcher := &changeLogFetcher{log: log, name: "a"}
	poller := newTestBindingPoller(t, "first", NewMemoryCursorStore(), fetcher, &recordingTarget{log: log})
	runner, err := NewRunner(RunnerConfig{Connection: testConnection, Schedule: PollSchedule{Interval: time.Minute}, Pollers: []Poller{poller}, RequestLimiter: limiter})
	require.NoError(t, err)
	require.ErrorIs(t, runUntil(t, clock, runner, func() bool {
		if log.count("fetch a after 0") == 1 {
			limiter.failure = errors.New("budget exhausted")
		}
		return len(clock.recordedWaits()) == 4
	}), context.Canceled)
	require.Equal(t, 3, limiter.waits)
	require.Equal(t, []string{"seed a", "fetch a after 0"}, log.snapshot())
	require.Equal(t, time.Minute, clock.recordedWaits()[3])
}

func TestWakePollerStartsAPollBeforeItsInterval(t *testing.T) {
	log := &operationLog{}
	fetcher := &changeLogFetcher{log: log, name: "a"}
	noJitter := 0.0
	runner, err := NewRunner(RunnerConfig{Connection: testConnection, Schedule: PollSchedule{Interval: time.Hour, JitterFraction: &noJitter},
		Pollers: []Poller{newTestBindingPoller(t, "first", NewMemoryCursorStore(), fetcher, &recordingTarget{log: log})}})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	require.Eventually(t, func() bool { return log.count("seed a") == 1 }, 10*time.Second, 10*time.Millisecond)
	runner.WakePoller("unknown")
	runner.WakePoller("first")
	require.Eventually(t, func() bool { return log.count("fetch a after 0") == 1 }, 10*time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	require.ErrorContains(t, runner.Run(context.Background()), "already running")
}

func TestBindingTriggerSourceRunsUnderNewTrigger(t *testing.T) {
	clock := installFakeClock(t, 0.5)
	log := &operationLog{}
	store := NewMemoryCursorStore()
	seedCursor(t, store, "0")
	fetcher := &changeLogFetcher{log: log, name: "a", pageSize: 10}
	fetcher.append("a")
	config := BindingPollerConfig[item]{Definition: testDefinition("itemChanged"), Binding: testBinding("itemChanged", "main"), Cursor: store, Fetcher: fetcher}
	source, err := NewBindingTriggerSource(config, PollSchedule{Interval: time.Minute})
	require.NoError(t, err)
	target := &durableTarget{recordingTarget: recordingTarget{log: log}}
	trigger, err := sdkgo.NewTrigger(sdkgo.TriggerConfig[item]{Definition: config.Definition, Binding: config.Binding, Source: source, Target: target})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock.cancel, clock.stopWhen = cancel, func() bool { return log.count("fetch a after 1") == 1 }
	require.ErrorIs(t, trigger.Run(ctx), context.Canceled)
	require.Equal(t, []string{"replay ", "fetch a after 0", "prepare a", "handle a", "fetch a after 1"}, log.snapshot())

	config.Target = target
	_, err = NewBindingTriggerSource(config, PollSchedule{Interval: time.Minute})
	require.Error(t, err)
}

func TestNewTriggerCredentialCallMatchesTheGmailIdentity(t *testing.T) {
	call := NewTriggerCredentialCall(sdkgo.ConnectionRef{Provider: "gmail", Name: "gmail-inbox"}, sdkgo.TriggerRef{ConnectorID: "gmail", TriggerName: "messageReceived"})
	require.Equal(t, sdkgo.CallID("a1955d2a-b59d-531a-bca7-c6d350e55ea5"), call.ID)
	require.Equal(t, sdkgo.OperationRef{ConnectorID: "gmail", OperationID: "messageReceived"}, call.Operation)
	require.Equal(t, call, NewTriggerCredentialCall(call.Connection, sdkgo.TriggerRef{ConnectorID: "gmail", TriggerName: "messageReceived"}))
}

func TestCursorErrorMarkers(t *testing.T) {
	require.NoError(t, MarkCursorInvalid(nil))
	require.NoError(t, MarkRateLimited(nil, time.Second))
	invalid := MarkCursorInvalid(errors.New("gone"))
	require.Same(t, invalid, MarkCursorInvalid(invalid))
	require.True(t, IsCursorInvalid(errors.Join(errors.New("wrapped"), invalid)))
	var rateLimited *RateLimitedError
	require.ErrorAs(t, MarkRateLimited(errors.New("429"), -time.Second), &rateLimited)
	require.Zero(t, rateLimited.RetryAfter)
	require.Equal(t, "provider rate limited the poll: 429", rateLimited.Error())
}
