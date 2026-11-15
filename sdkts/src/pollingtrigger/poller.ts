// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  PollCursorDocumentInvalidError, PollCursorTooLargeError, validatePollCursorState, type KeyedSnapshot, type KeyedSnapshotEntry, type PollCursorRecord, type PollCursorState,
} from '../projectconfig/poll_cursor.js';
import { ConflictError } from '../projectconfig/storage.js';
import { TriggerInboxFullError } from '../projectconfig/trigger_inbox.js';
import type { TriggerBindingRef } from '../trigger.js';
import { logTriggerRecord, triggerErrorAttributes, withTriggerLoggerAttributes, type TriggerContext, type TriggerLogAttributes, type TriggerLogLevel, type TriggerLogger } from '../trigger_delivery.js';
import { currentPollClock } from './clock.js';
import type { CursorStore } from './cursor_store.js';
import { MaxChangesPerPage, RateLimitedError, findInErrorChain, isCursorInvalid, type ChangeFetcher, type ChangePage, type Cursor } from './fetcher.js';
import { DeliveryFailedError, type PollMember, type PollTurn } from './member.js';
import { isKeyedItem, lookupKeyedSnapshotEntry, type ResolvedKeyedSnapshotOptions } from './snapshot.js';

/**
 * One durable cursor, its change fetcher, and the Trigger bindings it serves. It is sealed: build it with
 * newBindingPoller or newFeedPoller and pass it to exactly one newRunner.
 */
export interface Poller {
  /** The binding name or feed name; wakePoller takes it. */ readonly pollerName: string;
}

/** How one poll ended, which decides the next wait. */
export type PollOutcomeKind = 'succeeded' | 'hasMorePages' | 'failed' | 'rateLimited' | 'configurationFault' | 'canceled';

/** One poll's result; message is the WARN or ERROR record the runner writes, empty when the poll logged it. */
export interface PollOutcome { kind: PollOutcomeKind; error?: unknown; message?: string; retryAfterMs?: number }

const tooLargeMessage = 'poll paused: cursor state exceeds bound';

/** Implements Poller for one cursor and its members; a binding poller has one member. */
export class CursorPoller<Change> implements Poller {
  #logger: TriggerLogger;
  #isAttached = false;
  #pagesPerPoll = 1;

  constructor(
    readonly pollerName: string,
    private readonly identity: TriggerLogAttributes,
    private readonly cursor: CursorStore,
    private readonly fetcher: ChangeFetcher<Change>,
    private readonly members: PollMember<Change, unknown>[],
    private readonly snapshot: ResolvedKeyedSnapshotOptions | undefined,
  ) {
    this.#logger = withTriggerLoggerAttributes(undefined, identity);
  }

  bindings(): TriggerBindingRef[] { return this.members.map(member => member.binding); }

  attachRunner(logger: TriggerLogger | undefined, pagesPerPoll: number): void {
    if (this.#isAttached) throw new TypeError(`poller "${this.pollerName}" already belongs to a runner`);
    this.#isAttached = true;
    this.#pagesPerPoll = pagesPerPoll;
    this.#logger = withTriggerLoggerAttributes(logger, this.identity);
    for (const member of this.members) member.attachLogger(logger);
  }

  logRecord(context: TriggerContext, level: TriggerLogLevel, message: string, attributes: TriggerLogAttributes = {}): void {
    logTriggerRecord(this.#logger, context, level, message, attributes);
  }

  /** Reject for a stored document that can never be decoded; other errors wait for a poll. */
  async checkStoredCursor(context: TriggerContext): Promise<void> {
    try { await this.cursor.readPollCursor(); }
    catch (error: unknown) {
      if (!findInErrorChain(error, candidate => candidate instanceof PollCursorDocumentInvalidError)) return;
      this.logRecord(context, 'error', 'poll cursor document invalid', triggerErrorAttributes(error));
      throw new Error(`poller "${this.pollerName}": ${(error as Error).message}`, { cause: error });
    }
  }

  async replay(context: TriggerContext): Promise<void> {
    for (const member of this.members) await member.replay(context);
  }

  /**
   * Deliver earlier recorded events, then read pages until the feed is current, the page budget is spent, or
   * a step fails. A commit moves past a page only after every member recorded or consumed it.
   */
  async poll(context: TriggerContext, turn: PollTurn): Promise<PollOutcome> {
    const drained = await this.#drainMembers(context, turn);
    if (drained) return drained;
    let record: PollCursorRecord;
    try { record = await this.cursor.readPollCursor(); }
    catch (error: unknown) { return this.#storeFailure(context, error); }
    if (record.state === undefined) return this.#seed(context, record, undefined);
    for (let pages = 1; ; pages++) {
      const state: PollCursorState = record.state!;
      let page: ChangePage<Change>;
      try { page = await this.fetcher.fetchChanges(context, cursorOf(state)); }
      catch (error: unknown) {
        if (isCursorInvalid(error) && !context.signal.aborted) return this.#seed(context, record, error);
        return this.#providerFailure(context, error);
      }
      const fault = this.#validatePage(page);
      if (fault) return { kind: 'configurationFault', error: fault, message: tooLargeMessage };
      const committed = await this.#recordAndCommit(context, turn, record, page);
      if ('kind' in committed) return committed;
      const drainedAfterCommit = await this.#drainMembers(context, turn);
      if (drainedAfterCommit) return drainedAfterCommit;
      if (committed.next === undefined || !page.hasMorePages) return { kind: 'succeeded' };
      if (pages >= this.#pagesPerPoll) return { kind: 'hasMorePages' };
      record = committed.next;
    }
  }
  /**
   * Record one page in every member and commit page.next. Returns the new record, an undefined next after a
   * commit conflict, when another replica owns the cursor position and the poll ends, or the failed outcome.
   */
  async #recordAndCommit(context: TriggerContext, turn: PollTurn, record: PollCursorRecord, page: ChangePage<Change>): Promise<PollOutcome | { next: PollCursorRecord | undefined }> {
    const committedState = record.state!;
    const previous = (change: Change): KeyedSnapshotEntry | undefined =>
      isKeyedItem(change) && committedState.snapshot !== undefined ? lookupKeyedSnapshotEntry(committedState.snapshot, change.snapshotKey()) : undefined;
    let events = 0;
    for (const member of this.members) {
      member.stage(context, page.changes, previous, record.revision);
      try { await member.recordStaged(context, turn); }
      catch (error: unknown) { return this.#recordFailure(context, error); }
      events += member.stagedCount();
    }
    if (context.signal.aborted) return { kind: 'canceled' };
    if (events === 0 && page.changes.length === 0 && isSameCursor(committedState, page.next)) {
      // An empty page that does not move the cursor needs no write. Events an earlier attempt recorded
      // at this position are owed a delivery.
      this.#acceptStaged();
      return { next: record };
    }
    const commitCount = committedState.commitCount + 1;
    const state: PollCursorState = { ...committedState, token: page.next.token, snapshot: stampSnapshotSeen(page.next.snapshot, commitCount), commitCount, committedAtUnixMs: currentPollClock().now() };
    let written: PollCursorRecord;
    try { written = await this.cursor.replacePollCursor(record.revision, state); }
    catch (error: unknown) {
      if (isConflict(error)) {
        this.logRecord(context, 'debug', 'poll cursor commit conflict');
        this.#acceptStaged();
        return { next: undefined };
      }
      return this.#storeFailure(context, error);
    }
    this.logRecord(context, 'debug', 'poll cursor committed', { commit_count: commitCount, events });
    this.#acceptStaged();
    return { next: written };
  }

  #acceptStaged(): void { for (const member of this.members) member.acceptStaged(); }

  async #drainMembers(context: TriggerContext, turn: PollTurn): Promise<PollOutcome | undefined> {
    for (const member of this.members) {
      try { await member.drain(context, turn); }
      catch (error: unknown) { return context.signal.aborted ? { kind: 'canceled' } : { kind: 'failed', error }; }
    }
    return undefined;
  }

  /**
   * Commit the provider's current position: the first time when invalidCursor is undefined, otherwise as a
   * reset after the provider rejected the committed cursor. Changes in a reset's gap are not delivered.
   */
  async #seed(context: TriggerContext, record: PollCursorRecord, invalidCursor: unknown): Promise<PollOutcome> {
    const previous: PollCursorState = record.state ?? { token: '', seededAtUnixMs: 0, committedAtUnixMs: 0, commitCount: 0, resetCount: 0 };
    const isReset = invalidCursor !== undefined;
    if (isReset) this.logRecord(context, 'warn', 'poll cursor invalid; reseeding', { reset_count: previous.resetCount + 1, ...triggerErrorAttributes(invalidCursor) });
    let cursor: Cursor;
    try { cursor = await this.fetcher.seedCursor(context); }
    catch (error: unknown) { return this.#providerFailure(context, error); }
    const fault = this.#validateCursor(cursor);
    if (fault) return { kind: 'configurationFault', error: fault, message: tooLargeMessage };
    if (context.signal.aborted) return { kind: 'canceled' };
    const committedAt = currentPollClock().now();
    const commitCount = previous.commitCount + 1;
    const state: PollCursorState = {
      ...previous, token: cursor.token, snapshot: stampSnapshotSeen(cursor.snapshot, commitCount), seededAtUnixMs: committedAt, committedAtUnixMs: committedAt, commitCount,
    };
    if (isReset) { state.resetCount = previous.resetCount + 1; state.lastResetAtUnixMs = committedAt; }
    try { await this.cursor.replacePollCursor(record.revision, state); }
    catch (error: unknown) {
      if (!isConflict(error)) return this.#storeFailure(context, error);
      this.logRecord(context, 'debug', 'poll cursor commit conflict');
      return { kind: 'succeeded' };
    }
    if (!isReset) this.logRecord(context, 'info', 'poll cursor seeded', { reset: false });
    return { kind: 'succeeded' };
  }

  #providerFailure(context: TriggerContext, error: unknown): PollOutcome {
    if (context.signal.aborted) return { kind: 'canceled' };
    const rateLimited = findInErrorChain(error, candidate => candidate instanceof RateLimitedError) as RateLimitedError | undefined;
    if (rateLimited) return { kind: 'rateLimited', error, retryAfterMs: rateLimited.retryAfterMs };
    return { kind: 'failed', error, message: 'poll failed; retrying' };
  }

  #storeFailure(context: TriggerContext, error: unknown): PollOutcome {
    if (context.signal.aborted) return { kind: 'canceled' };
    if (findInErrorChain(error, candidate => candidate instanceof PollCursorTooLargeError)) return { kind: 'configurationFault', error, message: tooLargeMessage };
    if (findInErrorChain(error, candidate => candidate instanceof PollCursorDocumentInvalidError)) return { kind: 'configurationFault', error, message: 'poll cursor document invalid' };
    return { kind: 'failed', error, message: 'poll failed; retrying' };
  }

  #recordFailure(context: TriggerContext, error: unknown): PollOutcome {
    if (context.signal.aborted) return { kind: 'canceled' };
    if (error instanceof DeliveryFailedError) return { kind: 'failed', error };
    if (findInErrorChain(error, candidate => candidate instanceof TriggerInboxFullError)) return { kind: 'failed', error, message: 'poll paused: trigger inbox full' };
    return { kind: 'failed', error, message: 'poll failed; retrying' };
  }

  /** Reject fetcher output that no retry can store. */
  #validatePage(page: ChangePage<Change>): Error | undefined {
    if (!page || !Array.isArray(page.changes) || !page.next) return new TypeError('fetcher returned a malformed change page');
    if (page.changes.length > MaxChangesPerPage) return new PollCursorTooLargeError(`page has ${page.changes.length} changes, more than ${MaxChangesPerPage}`);
    return this.#validateCursor(page.next);
  }

  #validateCursor(cursor: Cursor): Error | undefined {
    if (!cursor || typeof cursor.token !== 'string') return new TypeError('fetcher returned a cursor without a token');
    if (cursor.snapshot !== undefined) {
      if (this.snapshot === undefined) return new TypeError('fetcher returned a keyed snapshot but the poller has no KeyedSnapshotOptions');
      if (cursor.snapshot.entries.length > this.snapshot.maxEntries) return new PollCursorTooLargeError(`snapshot has ${cursor.snapshot.entries.length} entries, more than ${this.snapshot.maxEntries}`);
    }
    try { validatePollCursorState({ token: cursor.token, snapshot: cursor.snapshot, seededAtUnixMs: 0, committedAtUnixMs: 0, commitCount: 0, resetCount: 0 }); }
    catch (error: unknown) { return error instanceof Error ? error : new Error(String(error)); }
    return undefined;
  }

}

/** Report a compare-and-swap conflict anywhere in the cause chain, as Go's errors.Is(err, ErrConflict) does. */
function isConflict(error: unknown): boolean {
  return findInErrorChain(error, candidate => candidate instanceof ConflictError) !== undefined;
}

function cursorOf(state: PollCursorState): Cursor {
  return state.snapshot === undefined ? { token: state.token } : { token: state.token, snapshot: state.snapshot };
}

/** Replace seen zero, meaning "this commit", with the commit's commitCount. */
function stampSnapshotSeen(snapshot: KeyedSnapshot | undefined, commitCount: number): KeyedSnapshot | undefined {
  if (snapshot === undefined) return undefined;
  return { entries: snapshot.entries.map(entry => entry.seen === 0 ? { ...entry, seen: commitCount } : entry) };
}

/** Report whether cursor equals the committed position, entry by entry. */
function isSameCursor(state: PollCursorState, cursor: Cursor): boolean {
  if (state.token !== cursor.token || (state.snapshot === undefined) !== (cursor.snapshot === undefined)) return false;
  if (state.snapshot === undefined || cursor.snapshot === undefined) return true;
  const current = cursor.snapshot.entries;
  if (state.snapshot.entries.length !== current.length) return false;
  return state.snapshot.entries.every((stored, index) => {
    const entry = current[index]!;
    return stored.key === entry.key && stored.hash === entry.hash && (entry.seen === 0 || entry.seen === stored.seen) && isSameRawValue(stored.value, entry.value);
  });
}

function isSameRawValue(left: Uint8Array | undefined, right: Uint8Array | undefined): boolean {
  if (left === undefined || right === undefined) return left === right;
  return Buffer.from(left.buffer, left.byteOffset, left.byteLength).equals(right);
}
