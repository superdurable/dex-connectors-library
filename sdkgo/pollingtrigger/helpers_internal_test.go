// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pollingtrigger

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var testConnection = sdkgo.ConnectionRef{Provider: "changelog", Name: "main"}

func testDefinition(triggerName string) sdkgo.TriggerDefinition {
	return sdkgo.TriggerDefinition{Trigger: sdkgo.TriggerRef{ConnectorID: "changelog", TriggerName: triggerName}, Description: "Receive a change."}
}

func testBinding(triggerName, bindingName string) sdkgo.TriggerBindingRef {
	return sdkgo.TriggerBindingRef{Connection: testConnection, Trigger: testDefinition(triggerName).Trigger, Name: bindingName}
}

type item struct {
	Sequence int    `json:"sequence"`
	Title    string `json:"title"`
}

// operationLog records the order of storage, provider, and target operations.
type operationLog struct {
	mutex      sync.Mutex
	operations []string
}

func (log *operationLog) add(format string, args ...any) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	log.operations = append(log.operations, fmt.Sprintf(format, args...))
}

func (log *operationLog) snapshot() []string {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	return append([]string(nil), log.operations...)
}

func (log *operationLog) count(operation string) int {
	count := 0
	for _, recorded := range log.snapshot() {
		if recorded == operation {
			count++
		}
	}
	return count
}

// fakeClock replaces the package clock, jitter, and wait, so schedules are observed without sleeping.
type fakeClock struct {
	mutex   sync.Mutex
	current time.Time
	waits   []time.Duration
	// stopWhen ends Run, by cancelling it, at the first wait where it returns true.
	stopWhen func() bool
	cancel   context.CancelFunc
}

func installFakeClock(t *testing.T, jitter float64) *fakeClock {
	t.Helper()
	clock := &fakeClock{current: time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)}
	previousNow, previousJitter, previousWait := now, jitterSource, waitForPoll
	now = clock.now
	jitterSource = func() float64 { return jitter }
	waitForPoll = clock.wait
	t.Cleanup(func() { now, jitterSource, waitForPoll = previousNow, previousJitter, previousWait })
	return clock
}

func (clock *fakeClock) now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.current
}

func (clock *fakeClock) advance(delay time.Duration) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.current = clock.current.Add(delay)
}

func (clock *fakeClock) wait(ctx context.Context, delay time.Duration, _ <-chan struct{}) error {
	clock.mutex.Lock()
	clock.waits = append(clock.waits, delay)
	isTooLong := len(clock.waits) > 500
	stopWhen := clock.stopWhen
	clock.mutex.Unlock()
	if isTooLong || (stopWhen != nil && stopWhen()) {
		clock.cancel()
		return ctx.Err()
	}
	clock.advance(delay)
	return ctx.Err()
}

func (clock *fakeClock) recordedWaits() []time.Duration {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return append([]time.Duration(nil), clock.waits...)
}

// runUntil runs runner until stop holds at a wait and returns Run's error.
func runUntil(t *testing.T, clock *fakeClock, runner *Runner, stop func() bool) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clock.cancel, clock.stopWhen = cancel, stop
	return runner.Run(ctx)
}

// changeLogFetcher is a scripted sequence-numbered provider feed.
type changeLogFetcher struct {
	mutex    sync.Mutex
	log      *operationLog
	name     string
	changes  []item
	pageSize int
	// failures are returned, in order, by the next fetches; nil entries succeed.
	failures     []error
	seedFailures []error
	onFetch      func(ctx context.Context)
}

func (fetcher *changeLogFetcher) append(titles ...string) {
	fetcher.mutex.Lock()
	defer fetcher.mutex.Unlock()
	for _, title := range titles {
		fetcher.changes = append(fetcher.changes, item{Sequence: len(fetcher.changes) + 1, Title: title})
	}
}

func (fetcher *changeLogFetcher) SeedCursor(ctx context.Context) (Cursor, error) {
	if err := WaitForPollRequest(ctx); err != nil {
		return Cursor{}, err
	}
	fetcher.mutex.Lock()
	defer fetcher.mutex.Unlock()
	fetcher.log.add("seed %s", fetcher.name)
	if len(fetcher.seedFailures) > 0 {
		failure := fetcher.seedFailures[0]
		fetcher.seedFailures = fetcher.seedFailures[1:]
		if failure != nil {
			return Cursor{}, failure
		}
	}
	return Cursor{Token: strconv.Itoa(len(fetcher.changes))}, nil
}

func (fetcher *changeLogFetcher) FetchChanges(ctx context.Context, cursor Cursor) (ChangePage[sdkgo.TriggerEvent[item]], error) {
	if err := WaitForPollRequest(ctx); err != nil {
		return ChangePage[sdkgo.TriggerEvent[item]]{}, err
	}
	if fetcher.onFetch != nil {
		fetcher.onFetch(ctx)
		if err := ctx.Err(); err != nil {
			return ChangePage[sdkgo.TriggerEvent[item]]{}, err
		}
	}
	fetcher.mutex.Lock()
	defer fetcher.mutex.Unlock()
	fetcher.log.add("fetch %s after %s", fetcher.name, cursor.Token)
	if len(fetcher.failures) > 0 {
		failure := fetcher.failures[0]
		fetcher.failures = fetcher.failures[1:]
		if failure != nil {
			return ChangePage[sdkgo.TriggerEvent[item]]{}, failure
		}
	}
	after, err := strconv.Atoi(cursor.Token)
	if err != nil {
		return ChangePage[sdkgo.TriggerEvent[item]]{}, MarkCursorInvalid(errors.New("cursor is not a sequence"))
	}
	pageSize := max(fetcher.pageSize, 1)
	end := min(after+pageSize, len(fetcher.changes))
	page := ChangePage[sdkgo.TriggerEvent[item]]{Next: Cursor{Token: strconv.Itoa(end)}, HasMorePages: end < len(fetcher.changes)}
	for _, change := range fetcher.changes[after:end] {
		page.Changes = append(page.Changes, sdkgo.TriggerEvent[item]{ID: change.Title, Payload: change})
	}
	return page, nil
}

// loggedCursorStore records reads and replaces and can fail or race the next replace.
type loggedCursorStore struct {
	CursorStore
	log          *operationLog
	replaceError error
	beforeCommit func(ctx context.Context)
}

func (store *loggedCursorStore) ReadPollCursor(ctx context.Context) (projectconfig.PollCursorRecord, error) {
	store.log.add("read")
	return store.CursorStore.ReadPollCursor(ctx)
}

func (store *loggedCursorStore) ReplacePollCursor(ctx context.Context, revision string, state projectconfig.PollCursorState) (projectconfig.PollCursorRecord, error) {
	if store.beforeCommit != nil {
		hook := store.beforeCommit
		store.beforeCommit = nil
		hook(ctx)
	}
	store.log.add("replace %s", state.Token)
	if err := store.replaceError; err != nil {
		store.replaceError = nil
		return projectconfig.PollCursorRecord{}, err
	}
	return store.CursorStore.ReplacePollCursor(ctx, revision, state)
}

func (store *loggedCursorStore) state(t *testing.T) projectconfig.PollCursorState {
	t.Helper()
	record, err := store.CursorStore.ReadPollCursor(context.Background())
	if err != nil || record.State == nil {
		t.Fatalf("cursor state is unavailable: %v", err)
	}
	return *record.State
}

// recordingTarget is a non-durable target; durableTarget adds PrepareTrigger and replay.
type recordingTarget struct {
	log          *operationLog
	name         string
	handleErrors map[string][]error
}

func (target *recordingTarget) HandleTrigger(_ context.Context, event sdkgo.TriggerEvent[item]) error {
	target.log.add("handle %s%s", target.name, event.ID)
	if failures := target.handleErrors[event.ID]; len(failures) > 0 {
		target.handleErrors[event.ID] = failures[1:]
		return failures[0]
	}
	return nil
}

type durableTarget struct {
	recordingTarget
	prepareErrors map[string][]error
	replayed      int
}

func (target *durableTarget) PrepareTrigger(_ context.Context, event sdkgo.TriggerEvent[item]) error {
	target.log.add("prepare %s%s", target.name, event.ID)
	if failures := target.prepareErrors[event.ID]; len(failures) > 0 {
		target.prepareErrors[event.ID] = failures[1:]
		return failures[0]
	}
	return nil
}

func (target *durableTarget) ReplayTriggerDeliveries(context.Context) error {
	target.replayed++
	target.log.add("replay %s", target.name)
	return nil
}

func newTestRunner(t *testing.T, schedule PollSchedule, pollers ...Poller) *Runner {
	t.Helper()
	if schedule.Interval == 0 {
		schedule.Interval = time.Minute
	}
	runner, err := NewRunner(RunnerConfig{Connection: testConnection, Schedule: schedule, Pollers: pollers})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func newTestBindingPoller(t *testing.T, bindingName string, store CursorStore, fetcher ChangeFetcher[sdkgo.TriggerEvent[item]], target sdkgo.TriggerTarget[item]) Poller {
	t.Helper()
	poller, err := NewBindingPoller(BindingPollerConfig[item]{
		Definition: testDefinition("itemChanged"), Binding: testBinding("itemChanged", bindingName),
		Cursor: store, Fetcher: fetcher, Target: target,
	})
	if err != nil {
		t.Fatal(err)
	}
	return poller
}
