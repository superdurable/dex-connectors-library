// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { TriggerDeliveryAbortedError, type TriggerContext } from '../trigger_delivery.js';
import type { KeyedSnapshot } from '../projectconfig/poll_cursor.js';

/**
 * Bound of ChangePage.changes. A durable Trigger inbox holds at most 1000 pending events, so a larger page
 * could never be recorded; the poller treats it as a fetcher fault.
 */
export const MaxChangesPerPage = 500;

/** A committed provider position: an opaque connector token and an optional keyed snapshot. */
export interface Cursor {
  /**
   * The connector's encoded position, at most MaxPollCursorTokenBytes UTF-8 bytes. Empty is a valid position.
   * Connectors encode structured positions, such as JSON, themselves.
   */
  token: string;
  /**
   * The keyed snapshot when the poller enables one with KeyedSnapshotOptions; undefined otherwise. It commits
   * atomically with token, so it never advances past unrecorded changes.
   */
  snapshot?: KeyedSnapshot | undefined;
}

/** One page of provider changes after a cursor. */
export interface ChangePage<Change> {
  /** Changes in provider order, at most MaxChangesPerPage. The kit records and delivers them in order. */ changes: Change[];
  /** The cursor that follows changes, committed only after every target recorded or consumed them. */ next: Cursor;
  /** Whether fetchChanges(next) can return more changes immediately; omitted means false. */ hasMorePages?: boolean | undefined;
}

/**
 * Reads one provider change feed. It never touches storage or Trigger targets. A poller owns its fetcher and
 * calls it one request at a time; implementations must not retain the context.
 *
 * Every event identity derived from a change must be a pure function of that change, so a refetch from the
 * same cursor after a crash yields the same Trigger event IDs and the Dex targets deduplicate them. Call
 * waitForPollRequest before every provider request and stop when context.signal aborts.
 */
export interface ChangeFetcher<Change> {
  /**
   * Return the provider's current position without reporting existing items. The kit calls it once when a
   * cursor is first created and again after fetchChanges rejects with an error marked by markCursorInvalid.
   */
  seedCursor(context: TriggerContext): Promise<Cursor>;
  /**
   * Return one page of changes after cursor. Reject with an error marked by markCursorInvalid when the
   * provider rejects the cursor permanently, one marked by markRateLimited for a quota or 429 response, and
   * any other error for a failure the next poll may fix.
   */
  fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<Change>>;
}

/** Marks a cursor the provider can never accept again. */
class CursorInvalidError extends Error {
  constructor(cause: unknown) {
    super(`poll cursor is invalid: ${cause instanceof Error ? cause.message : String(cause)}`, { cause });
    this.name = 'CursorInvalidError';
  }
}

/**
 * Report that the provider rejected a cursor permanently, such as Google Calendar 410 Gone for a sync token.
 * The poller then reseeds with seedCursor, increments the stored reset count, and logs a WARN record;
 * changes between the last commit and the reseed are not delivered. Returns undefined for undefined and an
 * already marked error unchanged.
 */
export function markCursorInvalid(error: unknown): Error;
export function markCursorInvalid(error: undefined): undefined;
export function markCursorInvalid(error: unknown): Error | undefined {
  if (error === undefined) return undefined;
  if (isCursorInvalid(error)) return error as Error;
  return new CursorInvalidError(error);
}

/** Report whether the error, or an error in its cause chain, was marked with markCursorInvalid. */
export function isCursorInvalid(error: unknown): boolean {
  return findInErrorChain(error, candidate => candidate instanceof CursorInvalidError) !== undefined;
}

/**
 * Reports a provider quota or 429 response. The runner pauses every poller of its connection, because
 * provider quotas are per user or per OAuth client.
 */
export class RateLimitedError extends Error {
  /**
   * Wrap the provider failure. retryAfterMs is the provider's requested wait in milliseconds, or zero when it
   * gave none: with zero the runner backs off exponentially, otherwise it waits max(retryAfterMs, intervalMs).
   */
  constructor(cause: unknown, readonly retryAfterMs: number) {
    super(cause === undefined ? 'provider rate limited the poll' : `provider rate limited the poll: ${cause instanceof Error ? cause.message : String(cause)}`, { cause });
    this.name = 'RateLimitedError';
  }
}

/**
 * Wrap an error as a RateLimitedError with the provider's requested wait in milliseconds, such as the value
 * parseRetryAfter returns for a Retry-After header. A negative or non-finite wait counts as zero. Returns
 * undefined for undefined.
 */
export function markRateLimited(error: unknown, retryAfterMs: number): RateLimitedError;
export function markRateLimited(error: undefined, retryAfterMs: number): undefined;
export function markRateLimited(error: unknown, retryAfterMs: number): RateLimitedError | undefined {
  if (error === undefined) return undefined;
  return new RateLimitedError(error, Number.isFinite(retryAfterMs) ? Math.max(retryAfterMs, 0) : 0);
}

/** Waits before each provider request a poller makes, for example a token bucket shared with other work of the process. */
export interface RequestLimiter {
  /**
   * Resolve once one more provider request may start, or reject. A rejection other than the context's abort
   * counts as a failed poll.
   */
  waitForPollRequest(context: TriggerContext): Promise<void>;
}

/** Keyed by the signal a runner gives its poll context, which every derived TriggerContext keeps. */
const requestLimiters = new WeakMap<AbortSignal, RequestLimiter>();

/**
 * Wait on the RequestLimiter of the runner that passed context, or a context derived from it, to the fetcher. Fetchers call it before every
 * provider request, including each page of a seed. It resolves at once when the runner has no limiter and
 * rejects with TriggerDeliveryAbortedError when the context's signal has ended.
 */
export async function waitForPollRequest(context: TriggerContext): Promise<void> {
  if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
  await requestLimiters.get(context.signal)?.waitForPollRequest(context);
}

/** Register the limiter that waitForPollRequest uses for every context with signal. */
export function attachRequestLimiter(signal: AbortSignal, limiter: RequestLimiter | undefined): void {
  if (limiter) requestLimiters.set(signal, limiter);
}

/** Return the first error in the cause chain that matches, as Go's errors.As walks wrapped errors. */
export function findInErrorChain(error: unknown, matches: (candidate: Error) => boolean): Error | undefined {
  for (let current = error, depth = 0; current instanceof Error && depth < 32; current = current.cause, depth++) {
    if (matches(current)) return current;
  }
  return undefined;
}
