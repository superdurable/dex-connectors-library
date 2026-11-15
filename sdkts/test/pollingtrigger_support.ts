// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, onTestFinished } from 'vitest';
import { TriggerDeliveryAbortedError, newTriggerContext, type TriggerContext, type TriggerEvent, type TriggerTarget } from '../src/index.js';
import type { PollCursorRecord, PollCursorState } from '../src/projectconfig/index.js';
import { replacePollClockForTests } from '../src/pollingtrigger/clock.js';
import {
  markCursorInvalid, newBindingPoller, newMemoryCursorStore, newRunner, waitForPollRequest,
  type ChangeFetcher, type ChangePage, type Cursor, type CursorStore, type PollSchedule, type Poller, type Runner,
} from '../src/pollingtrigger/index.js';

export const testConnection = { provider: 'changelog', name: 'main' };
export const testDefinition = (triggerName: string) => ({ trigger: { connectorId: 'changelog', triggerName }, description: 'Receive a change.' });
export const testBinding = (triggerName: string, name: string) => ({ connection: testConnection, trigger: testDefinition(triggerName).trigger, name });

export interface Item { sequence: number; title: string }

/** Records the order of storage, provider, and target operations. */
export class OperationLog {
  readonly operations: string[] = [];
  add(operation: string): void { this.operations.push(operation); }
  count(operation: string): number { return this.operations.filter(recorded => recorded === operation).length; }
}

/** Replaces the runner clock, jitter, and wait, so schedules are observed without sleeping. */
export class FakeClock {
  current = Date.UTC(2026, 10, 15, 12);
  readonly waits: number[] = [];
  stopWhen: (() => boolean) | undefined;
  controller = new AbortController();

  install(jitter: number): void {
    const restore = replacePollClockForTests({
      now: () => this.current,
      jitter: () => jitter,
      waitForPoll: async (signal, delayMs) => {
        this.waits.push(delayMs);
        if (this.waits.length > 500 || this.stopWhen?.()) this.controller.abort();
        if (signal.aborted) throw new TriggerDeliveryAbortedError();
        this.current += delayMs;
      },
    });
    onTestFinished(restore);
  }

  /** Run runner until stop holds at a wait; resolves with the rejection. */
  async runUntil(runner: Runner, stop: () => boolean, context?: TriggerContext): Promise<unknown> {
    this.controller = new AbortController();
    this.stopWhen = stop;
    return runner.run(context ?? newTriggerContext(this.controller.signal)).then(() => undefined, error => error);
  }
}

export function installFakeClock(jitter: number): FakeClock {
  const clock = new FakeClock();
  clock.install(jitter);
  return clock;
}

/** A scripted sequence-numbered provider feed. */
export class ChangeLogFetcher implements ChangeFetcher<TriggerEvent<Item>> {
  changes: Item[] = [];
  failures: Array<Error | undefined> = [];
  seedFailures: Array<Error | undefined> = [];
  onFetch: ((context: TriggerContext) => void) | undefined;

  constructor(private readonly log: OperationLog, private readonly name: string, private readonly pageSize = 10) {}

  append(...titles: string[]): void { for (const title of titles) this.changes.push({ sequence: this.changes.length + 1, title }); }

  async seedCursor(context: TriggerContext): Promise<Cursor> {
    await waitForPollRequest(context);
    this.log.add(`seed ${this.name}`);
    const failure = this.seedFailures.shift();
    if (failure) throw failure;
    return { token: String(this.changes.length) };
  }

  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<Item>>> {
    await waitForPollRequest(context);
    this.onFetch?.(context);
    if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
    this.log.add(`fetch ${this.name} after ${cursor.token}`);
    const failure = this.failures.shift();
    if (failure) throw failure;
    if (!/^\d+$/.test(cursor.token)) throw markCursorInvalid(new Error('cursor is not a sequence'));
    const after = Number(cursor.token);
    const end = Math.min(after + Math.max(this.pageSize, 1), this.changes.length);
    return {
      changes: this.changes.slice(after, end).map(change => ({ id: change.title, occurredAt: new Date(0), payload: change })),
      next: { token: String(end) }, hasMorePages: end < this.changes.length,
    };
  }

  /** Make the fetch with that number see titles as the whole feed, as a listing that changed before a retry. */
  replaceChangesOnFetch(fetchNumber: number, ...titles: string[]): void {
    let fetches = 0;
    this.onFetch = () => {
      if (++fetches !== fetchNumber) return;
      this.changes = [];
      this.append(...titles);
    };
  }
}

/** Records reads and replaces and can fail or race the next replace. */
export class LoggedCursorStore implements CursorStore {
  replaceError: Error | undefined;
  beforeCommit: (() => Promise<void>) | undefined;

  constructor(readonly inner: CursorStore, private readonly log: OperationLog) {}

  async readPollCursor(): Promise<PollCursorRecord> { this.log.add('read'); return this.inner.readPollCursor(); }

  async replacePollCursor(revision: string, state: PollCursorState): Promise<PollCursorRecord> {
    const hook = this.beforeCommit;
    this.beforeCommit = undefined;
    await hook?.();
    this.log.add(`replace ${state.token}`);
    const failure = this.replaceError;
    this.replaceError = undefined;
    if (failure) throw failure;
    return this.inner.replacePollCursor(revision, state);
  }

  async state(): Promise<PollCursorState> {
    const record = await this.inner.readPollCursor();
    expect(record.state).toBeDefined();
    return record.state!;
  }
}

/** A non-durable target. */
export class RecordingTarget implements TriggerTarget<Item> {
  handleErrors = new Map<string, Error[]>();
  constructor(protected readonly log: OperationLog, protected readonly name = '') {}
  async handleTrigger(_context: TriggerContext, event: TriggerEvent<Item>): Promise<void> {
    this.log.add(`handle ${this.name}${event.id}`);
    const failure = this.handleErrors.get(event.id)?.shift();
    if (failure) throw failure;
  }
}

/** A target that records before acknowledgement and replays. */
export class DurableTarget extends RecordingTarget {
  prepareErrors = new Map<string, Error[]>();
  replayed = 0;
  async prepareTrigger(_context: TriggerContext, event: TriggerEvent<Item>): Promise<void> {
    this.log.add(`prepare ${this.name}${event.id}`);
    const failure = this.prepareErrors.get(event.id)?.shift();
    if (failure) throw failure;
  }
  async replayTriggerDeliveries(): Promise<void> { this.replayed++; this.log.add(`replay ${this.name}`); }
}

export function newTestRunner(schedule: Partial<PollSchedule>, ...pollers: Poller[]): Runner {
  return newRunner({ connection: testConnection, schedule: { intervalMs: 60_000, ...schedule }, pollers, logger: { isEnabled: () => false, log: () => undefined } });
}

export function newTestBindingPoller(bindingName: string, cursor: CursorStore, fetcher: ChangeFetcher<TriggerEvent<Item>>, target: TriggerTarget<Item>): Poller {
  return newBindingPoller({ definition: testDefinition('itemChanged'), binding: testBinding('itemChanged', bindingName), cursor, fetcher, target });
}

export async function seedCursor(store: CursorStore, token: string): Promise<void> {
  const record = await store.readPollCursor();
  await store.replacePollCursor(record.revision, { token, seededAtUnixMs: 0, committedAtUnixMs: 0, commitCount: 1, resetCount: 0 });
}

export function newLoggedStore(log: OperationLog): LoggedCursorStore { return new LoggedCursorStore(newMemoryCursorStore(), log); }
