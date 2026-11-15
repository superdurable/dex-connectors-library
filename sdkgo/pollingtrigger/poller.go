// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pollingtrigger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/triggerlog"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// Poller is one durable cursor, its change fetcher, and the Trigger bindings it serves. It is sealed:
// build it with NewBindingPoller or NewFeedPoller and pass it to exactly one NewRunner.
type Poller interface {
	pollerName() string
	pollerBindings() []sdkgo.TriggerBindingRef
	attachRunner(logger *slog.Logger, pagesPerPoll int) error
	logRecord(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr)
	checkStoredCursor(ctx context.Context) error
	replay(ctx context.Context) error
	poll(ctx context.Context, turn *pollTurn) pollOutcome
}

// pollOutcomeKind is how one poll ended, which decides the next wait.
type pollOutcomeKind int

const (
	pollSucceeded pollOutcomeKind = iota
	pollHasMorePages
	pollFailed
	pollRateLimited
	pollConfigurationFault
	pollCanceled
)

type pollOutcome struct {
	kind pollOutcomeKind
	err  error
	// message is the WARN or ERROR record the runner writes; empty when the poll already logged it.
	message    string
	retryAfter time.Duration
}

// cursorPoller implements Poller for one cursor and its members; a binding poller has one member.
type cursorPoller[CHANGE any] struct {
	name         string
	identity     []slog.Attr
	cursor       CursorStore
	fetcher      ChangeFetcher[CHANGE]
	members      []pollMember[CHANGE]
	snapshot     *KeyedSnapshotOptions
	log          triggerlog.Logger
	isAttached   bool
	pagesPerPoll int
}

func (poller *cursorPoller[CHANGE]) pollerName() string { return poller.name }

func (poller *cursorPoller[CHANGE]) pollerBindings() []sdkgo.TriggerBindingRef {
	bindings := make([]sdkgo.TriggerBindingRef, 0, len(poller.members))
	for _, member := range poller.members {
		bindings = append(bindings, member.memberBinding())
	}
	return bindings
}

func (poller *cursorPoller[CHANGE]) attachRunner(logger *slog.Logger, pagesPerPoll int) error {
	if poller.isAttached {
		return fmt.Errorf("poller %q already belongs to a runner", poller.name)
	}
	poller.isAttached, poller.pagesPerPoll = true, pagesPerPoll
	poller.log = triggerlog.New(logger, poller.identity...)
	for _, member := range poller.members {
		member.attachLogger(logger)
	}
	return nil
}

func (poller *cursorPoller[CHANGE]) logRecord(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) {
	switch level {
	case slog.LevelError:
		poller.log.Error(ctx, message, attrs...)
	case slog.LevelWarn:
		poller.log.Warn(ctx, message, attrs...)
	default:
		poller.log.Info(ctx, message, attrs...)
	}
}

// checkStoredCursor reports a stored document that can never be decoded; other errors wait for a poll.
func (poller *cursorPoller[CHANGE]) checkStoredCursor(ctx context.Context) error {
	_, err := poller.cursor.ReadPollCursor(ctx)
	if errors.Is(err, projectconfig.ErrPollCursorDocumentInvalid) {
		poller.log.Error(ctx, "poll cursor document invalid", triggerlog.Err(err))
		return fmt.Errorf("poller %q: %w", poller.name, err)
	}
	return nil
}

func (poller *cursorPoller[CHANGE]) replay(ctx context.Context) error {
	for _, member := range poller.members {
		if err := member.replay(ctx); err != nil {
			return err
		}
	}
	return nil
}

// poll delivers earlier recorded events, then reads pages until the feed is current, the page budget
// is spent, or a step fails. A commit moves past a page only after every member recorded or consumed it.
func (poller *cursorPoller[CHANGE]) poll(ctx context.Context, turn *pollTurn) pollOutcome {
	if outcome, ok := poller.drainMembers(ctx, turn); !ok {
		return outcome
	}
	record, err := poller.cursor.ReadPollCursor(ctx)
	if err != nil {
		return poller.storeFailure(ctx, err)
	}
	if record.State == nil {
		return poller.seed(ctx, record, nil)
	}
	for pages := 1; ; pages++ {
		page, err := poller.fetcher.FetchChanges(ctx, Cursor{Token: record.State.Token, Snapshot: record.State.Snapshot})
		if err != nil {
			if IsCursorInvalid(err) && ctx.Err() == nil {
				return poller.seed(ctx, record, err)
			}
			return poller.providerFailure(ctx, err)
		}
		if fault := poller.validatePage(page); fault != nil {
			return pollOutcome{kind: pollConfigurationFault, err: fault, message: "poll paused: cursor state exceeds bound"}
		}
		next, outcome, ok := poller.recordAndCommit(ctx, turn, record, page)
		if !ok {
			return outcome
		}
		if outcome, ok := poller.drainMembers(ctx, turn); !ok {
			return outcome
		}
		if next == nil || !page.HasMorePages {
			return pollOutcome{kind: pollSucceeded}
		}
		if pages >= poller.pagesPerPoll {
			return pollOutcome{kind: pollHasMorePages}
		}
		record = *next
	}
}

// recordAndCommit records one page in every member and commits page.Next. It returns the new record, or
// nil after a commit conflict, when another replica owns the cursor position and the poll ends.
func (poller *cursorPoller[CHANGE]) recordAndCommit(
	ctx context.Context,
	turn *pollTurn,
	record projectconfig.PollCursorRecord,
	page ChangePage[CHANGE],
) (*projectconfig.PollCursorRecord, pollOutcome, bool) {
	previous := func(change CHANGE) *projectconfig.KeyedSnapshotEntry {
		item, isKeyed := any(change).(KeyedItem)
		if !isKeyed || record.State.Snapshot == nil {
			return nil
		}
		return lookupKeyedSnapshotEntry(record.State.Snapshot, item.SnapshotKey())
	}
	events := 0
	for _, member := range poller.members {
		member.stage(ctx, page.Changes, previous, record.Revision)
		if err := member.recordStaged(ctx, turn); err != nil {
			return nil, poller.recordFailure(ctx, err), false
		}
		events += member.stagedCount()
	}
	if err := ctx.Err(); err != nil {
		return nil, pollOutcome{kind: pollCanceled, err: err}, false
	}
	if events == 0 && len(page.Changes) == 0 && isSameCursor(*record.State, page.Next) {
		// An empty page that does not move the cursor needs no write. Events an earlier attempt recorded
		// at this position are owed a delivery.
		poller.acceptStaged()
		return &record, pollOutcome{}, true
	}
	state := *record.State
	state.Token, state.Snapshot = page.Next.Token, stampSnapshotSeen(page.Next.Snapshot, state.CommitCount+1)
	state.CommitCount++
	state.CommittedAtUnixMs = now().UnixMilli()
	written, err := poller.cursor.ReplacePollCursor(ctx, record.Revision, state)
	if errors.Is(err, projectconfig.ErrConflict) {
		poller.log.Debug(ctx, "poll cursor commit conflict")
		poller.acceptStaged()
		return nil, pollOutcome{}, true
	}
	if err != nil {
		return nil, poller.storeFailure(ctx, err), false
	}
	poller.log.Debug(ctx, "poll cursor committed", slog.Int64("commit_count", state.CommitCount), slog.Int("events", events))
	poller.acceptStaged()
	return &written, pollOutcome{}, true
}

func (poller *cursorPoller[CHANGE]) acceptStaged() {
	for _, member := range poller.members {
		member.acceptStaged()
	}
}

func (poller *cursorPoller[CHANGE]) drainMembers(ctx context.Context, turn *pollTurn) (pollOutcome, bool) {
	for _, member := range poller.members {
		if err := member.drain(ctx, turn); err != nil {
			if ctx.Err() != nil {
				return pollOutcome{kind: pollCanceled, err: ctx.Err()}, false
			}
			return pollOutcome{kind: pollFailed, err: err}, false
		}
	}
	return pollOutcome{}, true
}

// seed commits the provider's current position: the first time when invalidCursor is nil, otherwise as a
// reset after the provider rejected the committed cursor. Changes in a reset's gap are not delivered.
func (poller *cursorPoller[CHANGE]) seed(ctx context.Context, record projectconfig.PollCursorRecord, invalidCursor error) pollOutcome {
	state := projectconfig.PollCursorState{}
	if record.State != nil {
		state = *record.State
	}
	isReset := invalidCursor != nil
	if isReset {
		poller.log.Warn(ctx, "poll cursor invalid; reseeding", slog.Int64("reset_count", state.ResetCount+1), triggerlog.Err(invalidCursor))
	}
	cursor, err := poller.fetcher.SeedCursor(ctx)
	if err != nil {
		return poller.providerFailure(ctx, err)
	}
	if fault := poller.validateCursor(cursor); fault != nil {
		return pollOutcome{kind: pollConfigurationFault, err: fault, message: "poll paused: cursor state exceeds bound"}
	}
	if err := ctx.Err(); err != nil {
		return pollOutcome{kind: pollCanceled, err: err}
	}
	committedAt := now().UnixMilli()
	state.Token, state.Snapshot = cursor.Token, stampSnapshotSeen(cursor.Snapshot, state.CommitCount+1)
	state.SeededAtUnixMs, state.CommittedAtUnixMs = committedAt, committedAt
	state.CommitCount++
	if isReset {
		state.ResetCount++
		state.LastResetAtUnixMs = &committedAt
	}
	_, err = poller.cursor.ReplacePollCursor(ctx, record.Revision, state)
	if errors.Is(err, projectconfig.ErrConflict) {
		poller.log.Debug(ctx, "poll cursor commit conflict")
		return pollOutcome{kind: pollSucceeded}
	}
	if err != nil {
		return poller.storeFailure(ctx, err)
	}
	if !isReset {
		poller.log.Info(ctx, "poll cursor seeded", slog.Bool("reset", false))
	}
	return pollOutcome{kind: pollSucceeded}
}

func (poller *cursorPoller[CHANGE]) providerFailure(ctx context.Context, err error) pollOutcome {
	if ctx.Err() != nil {
		return pollOutcome{kind: pollCanceled, err: ctx.Err()}
	}
	var rateLimited *RateLimitedError
	if errors.As(err, &rateLimited) {
		return pollOutcome{kind: pollRateLimited, err: err, retryAfter: rateLimited.RetryAfter}
	}
	return pollOutcome{kind: pollFailed, err: err, message: "poll failed; retrying"}
}

func (poller *cursorPoller[CHANGE]) storeFailure(ctx context.Context, err error) pollOutcome {
	switch {
	case ctx.Err() != nil:
		return pollOutcome{kind: pollCanceled, err: ctx.Err()}
	case errors.Is(err, projectconfig.ErrPollCursorTooLarge):
		return pollOutcome{kind: pollConfigurationFault, err: err, message: "poll paused: cursor state exceeds bound"}
	case errors.Is(err, projectconfig.ErrPollCursorDocumentInvalid):
		return pollOutcome{kind: pollConfigurationFault, err: err, message: "poll cursor document invalid"}
	default:
		return pollOutcome{kind: pollFailed, err: err, message: "poll failed; retrying"}
	}
}

func (poller *cursorPoller[CHANGE]) recordFailure(ctx context.Context, err error) pollOutcome {
	var delivery *deliveryFailedError
	switch {
	case ctx.Err() != nil:
		return pollOutcome{kind: pollCanceled, err: ctx.Err()}
	case errors.As(err, &delivery):
		return pollOutcome{kind: pollFailed, err: err}
	case errors.Is(err, projectconfig.ErrTriggerInboxFull):
		return pollOutcome{kind: pollFailed, err: err, message: "poll paused: trigger inbox full"}
	default:
		return pollOutcome{kind: pollFailed, err: err, message: "poll failed; retrying"}
	}
}

// validatePage rejects fetcher output that no retry can store.
func (poller *cursorPoller[CHANGE]) validatePage(page ChangePage[CHANGE]) error {
	if len(page.Changes) > MaxChangesPerPage {
		return fmt.Errorf("%w: page has %d changes, more than %d", projectconfig.ErrPollCursorTooLarge, len(page.Changes), MaxChangesPerPage)
	}
	return poller.validateCursor(page.Next)
}

func (poller *cursorPoller[CHANGE]) validateCursor(cursor Cursor) error {
	if cursor.Snapshot != nil {
		if poller.snapshot == nil {
			return errors.New("fetcher returned a keyed snapshot but the poller has no KeyedSnapshotOptions")
		}
		if len(cursor.Snapshot.Entries) > poller.snapshot.MaxEntries {
			return fmt.Errorf("%w: snapshot has %d entries, more than %d", projectconfig.ErrPollCursorTooLarge,
				len(cursor.Snapshot.Entries), poller.snapshot.MaxEntries)
		}
	}
	return projectconfig.ValidatePollCursorState(projectconfig.PollCursorState{Token: cursor.Token, Snapshot: cursor.Snapshot})
}

// stampSnapshotSeen replaces Seen zero, meaning "this commit", with the commit's CommitCount.
func stampSnapshotSeen(snapshot *projectconfig.KeyedSnapshot, commitCount int64) *projectconfig.KeyedSnapshot {
	if snapshot == nil {
		return nil
	}
	stamped := &projectconfig.KeyedSnapshot{Entries: make([]projectconfig.KeyedSnapshotEntry, len(snapshot.Entries))}
	for index, entry := range snapshot.Entries {
		if entry.Seen == 0 {
			entry.Seen = commitCount
		}
		stamped.Entries[index] = entry
	}
	return stamped
}

// isSameCursor reports whether cursor equals the committed position, entry by entry.
func isSameCursor(state projectconfig.PollCursorState, cursor Cursor) bool {
	if state.Token != cursor.Token || (state.Snapshot == nil) != (cursor.Snapshot == nil) {
		return false
	}
	if state.Snapshot == nil {
		return true
	}
	if len(state.Snapshot.Entries) != len(cursor.Snapshot.Entries) {
		return false
	}
	for index, stored := range state.Snapshot.Entries {
		current := cursor.Snapshot.Entries[index]
		if stored.Key != current.Key || stored.Hash != current.Hash || (current.Seen != 0 && current.Seen != stored.Seen) ||
			!bytes.Equal(stored.Value, current.Value) {
			return false
		}
	}
	return true
}
