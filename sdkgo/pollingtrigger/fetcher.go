// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pollingtrigger

import (
	"context"
	"errors"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// MaxChangesPerPage bounds ChangePage.Changes. A durable Trigger inbox holds at most 1000 pending events,
// so a larger page could never be recorded; the poller treats it as a fetcher fault.
const MaxChangesPerPage = 500

// Cursor is a committed provider position: an opaque connector token and an optional keyed snapshot.
type Cursor struct {
	// Token is the connector's encoded position, at most projectconfig.MaxPollCursorTokenBytes. Empty is a
	// valid position. Connectors encode structured positions, such as JSON, themselves.
	Token string
	// Snapshot is the keyed snapshot when the poller enables one with KeyedSnapshotOptions; nil otherwise.
	// It commits atomically with Token, so it never advances past unrecorded changes.
	Snapshot *projectconfig.KeyedSnapshot
}

// ChangePage is one page of provider changes after a cursor.
type ChangePage[CHANGE any] struct {
	// Changes are in provider order, at most MaxChangesPerPage. The kit records and delivers them in order.
	Changes []CHANGE
	// Next is the cursor that follows Changes. The kit commits it only after every target recorded or
	// consumed the changes.
	Next Cursor
	// HasMorePages reports that FetchChanges(Next) can return more changes immediately.
	HasMorePages bool
}

// ChangeFetcher reads one provider change feed. It never touches storage or Trigger targets. A poller
// owns its fetcher and calls it from one goroutine; implementations must not retain the context.
//
// Every event identity derived from a change must be a pure function of that change, so a refetch from
// the same cursor after a crash yields the same Trigger event IDs and the Dex targets deduplicate them.
// Call WaitForPollRequest before every provider request.
type ChangeFetcher[CHANGE any] interface {
	// SeedCursor returns the provider's current position without reporting existing items. The kit
	// calls it once when a cursor is first created and again after FetchChanges returns an error marked
	// with MarkCursorInvalid.
	SeedCursor(ctx context.Context) (Cursor, error)
	// FetchChanges returns one page of changes after cursor. Return an error wrapped by MarkCursorInvalid
	// when the provider rejects the cursor permanently, one wrapped by MarkRateLimited for a quota or 429
	// response, and any other error for a failure the next poll may fix.
	FetchChanges(ctx context.Context, cursor Cursor) (ChangePage[CHANGE], error)
}

// cursorInvalidError marks a cursor the provider can never accept again.
type cursorInvalidError struct{ err error }

// Error describes the failure.
func (e *cursorInvalidError) Error() string { return "poll cursor is invalid: " + e.err.Error() }

// Unwrap returns the underlying failure.
func (e *cursorInvalidError) Unwrap() error { return e.err }

// MarkCursorInvalid reports that the provider rejected a cursor permanently, such as Google Calendar 410
// Gone for a sync token. The poller then reseeds with SeedCursor, increments the stored reset count, and
// logs a WARN record; changes between the last commit and the reseed are not delivered. It returns nil
// for nil and returns an already marked error unchanged.
func MarkCursorInvalid(err error) error {
	if err == nil || IsCursorInvalid(err) {
		return err
	}
	return &cursorInvalidError{err: err}
}

// IsCursorInvalid reports whether err or an error it wraps was marked with MarkCursorInvalid.
func IsCursorInvalid(err error) bool {
	var invalid *cursorInvalidError
	return errors.As(err, &invalid)
}

// RateLimitedError reports a provider quota or 429 response. The runner pauses every poller of its
// connection, because provider quotas are per user or per OAuth client.
type RateLimitedError struct {
	// Err is the provider failure.
	Err error
	// RetryAfter is the provider's requested wait, or zero when it gave none. With zero the runner backs
	// off exponentially; otherwise it waits max(RetryAfter, PollSchedule.Interval).
	RetryAfter time.Duration
}

// Error describes the rate-limited request; it returns "<nil>" for a nil receiver.
func (e *RateLimitedError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err == nil {
		return "provider rate limited the poll"
	}
	return "provider rate limited the poll: " + e.Err.Error()
}

// Unwrap returns the provider failure; it returns nil for a nil receiver.
func (e *RateLimitedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// MarkRateLimited wraps err as a RateLimitedError with the provider's requested wait, such as the value
// providerhttp.ParseRetryAfter returns for a Retry-After header. A negative wait counts as zero. It
// returns nil for nil.
func MarkRateLimited(err error, retryAfter time.Duration) error {
	if err == nil {
		return nil
	}
	return &RateLimitedError{Err: err, RetryAfter: max(retryAfter, 0)}
}

// RequestLimiter waits before each provider request a poller makes, for example a
// golang.org/x/time/rate limiter shared with other work of the same application process.
type RequestLimiter interface {
	// WaitForPollRequest blocks until one more provider request may start, or returns an error. An error
	// other than ctx.Err() counts as a failed poll.
	WaitForPollRequest(ctx context.Context) error
}

type requestLimiterKey struct{}

// WaitForPollRequest waits on the RequestLimiter of the runner that called the fetcher with ctx.
// Fetchers call it before every provider request, including each page of a seed. It returns nil at once
// when the runner has no limiter, and ctx.Err() when ctx has ended.
func WaitForPollRequest(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	limiter, ok := ctx.Value(requestLimiterKey{}).(RequestLimiter)
	if !ok {
		return nil
	}
	return limiter.WaitForPollRequest(ctx)
}

func contextWithRequestLimiter(ctx context.Context, limiter RequestLimiter) context.Context {
	if limiter == nil {
		return ctx
	}
	return context.WithValue(ctx, requestLimiterKey{}, limiter)
}
