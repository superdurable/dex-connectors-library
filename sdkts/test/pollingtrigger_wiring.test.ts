// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { setTimeout as delay } from 'node:timers/promises';
import { describe, expect, it } from 'vitest';
import { TriggerDeliveryAbortedError, newTrigger, newTriggerContext, withTriggerLogAttributes, type TriggerEvent } from '../src/index.js';
import type { KeyedSnapshotEntry } from '../src/projectconfig/index.js';
import {
  RateLimitedError, applyKeyedSnapshotChanges, isCursorInvalid, markCursorInvalid, markRateLimited, newBindingPoller, newBindingTriggerSource, newFeedMember, newFeedPoller,
  newMemoryCursorStore, newRunner, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor, type KeyedItem,
} from '../src/pollingtrigger/index.js';
import {
  ChangeLogFetcher, DurableTarget, LoggedCursorStore, OperationLog, RecordingTarget, installFakeClock, newTestBindingPoller, newTestRunner, seedCursor, testBinding, testConnection, testDefinition, type Item,
} from './pollingtrigger_support.js';

/** A delta-feed change that the feed snapshot tracks by file ID. */
class FileChange implements KeyedItem {
  constructor(readonly fileId: string, readonly version: number) {}
  snapshotKey(): string { return this.fileId; }
  snapshotHash(): string { return this.version.toString(16).padStart(16, '0'); }
  snapshotValue(): undefined { return undefined; }
}

/** Returns scripted pages and keeps the snapshot with applyKeyedSnapshotChanges. */
class FileFeedFetcher implements ChangeFetcher<FileChange> {
  constructor(private readonly log: OperationLog, private readonly pages: FileChange[][]) {}
  async seedCursor(): Promise<Cursor> { return { token: '0', snapshot: { entries: [] } }; }
  async fetchChanges(_context: unknown, cursor: Cursor): Promise<ChangePage<FileChange>> {
    this.log.add(`fetch after ${cursor.token}`);
    const index = Number(cursor.token);
    const page = this.pages[index];
    if (!page) return { changes: [], next: cursor };
    return { changes: page, next: { token: String(index + 1), snapshot: applyKeyedSnapshotChanges(cursor.snapshot, page, []) } };
  }
}

const selectFileEvents = (kind: 'created' | 'updated') => (change: FileChange, previous: KeyedSnapshotEntry | undefined): Array<TriggerEvent<Item>> =>
  (kind === 'created') !== (previous === undefined) ? [] : [{ id: `${change.fileId}@${change.version}`, occurredAt: new Date(0), payload: { sequence: change.version, title: change.fileId } }];

describe('feed pollers, mirroring the Go kit', () => {
  it('share one fetch and skip recorded members on retry', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const fetcher = new FileFeedFetcher(log, [[new FileChange('f1', 1)], [new FileChange('f1', 2), new FileChange('f2', 1)]]);
    const created = new DurableTarget(log, 'created:');
    const updated = new DurableTarget(log, 'updated:');
    updated.prepareErrors.set('f1@2', [new Error('inbox write failed')]);
    const store = new LoggedCursorStore(newMemoryCursorStore(), log);
    const poller = newFeedPoller({
      name: 'fileChanges', cursor: store, fetcher, snapshot: {},
      members: [
        newFeedMember(testDefinition('fileCreated'), testBinding('fileCreated', 'created'), selectFileEvents('created'), created),
        newFeedMember(testDefinition('fileUpdated'), testBinding('fileUpdated', 'updated'), selectFileEvents('updated'), updated),
      ],
    });
    await clock.runUntil(newTestRunner({}, poller), () => log.count('fetch after 2') === 1);
    expect(log.operations).toEqual([
      'read', 'replay created:', 'replay updated:', 'read', 'replace 0',
      'read', 'fetch after 0', 'prepare created:f1@1', 'replace 1', 'handle created:f1@1',
      'read', 'fetch after 1', 'prepare created:f2@1', 'prepare updated:f1@2',
      'read', 'fetch after 1', 'prepare updated:f1@2', 'replace 2', 'handle created:f2@1', 'handle updated:f1@2',
      'read', 'fetch after 2',
    ]);
    expect((await store.state()).snapshot?.entries).toEqual([{ key: 'f1', hash: '0000000000000002', seen: 3 }, { key: 'f2', hash: '0000000000000001', seen: 3 }]);
  });

  it('treats a snapshot over its bound as a configuration fault', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const member = newFeedMember(testDefinition('fileCreated'), testBinding('fileCreated', 'created'), selectFileEvents('created'), new RecordingTarget(log));
    const poller = newFeedPoller({ name: 'fileChanges', cursor: newMemoryCursorStore(), fetcher: new FileFeedFetcher(log, [[new FileChange('f1', 1), new FileChange('f2', 1)]]), members: [member], snapshot: { maxEntries: 1 } });
    await clock.runUntil(newTestRunner({ intervalMs: 60_000, maxBackoffMs: 420_000 }, poller), () => log.count('fetch after 0') === 2);
    expect(clock.waits[2]).toBe(420_000);
    expect(log.count('handle f1@1')).toBe(0);
  });
});

describe('poll runner wiring, mirroring the Go kit', () => {
  const log = new OperationLog();
  const fetcher = new ChangeLogFetcher(log, 'a');
  const target = new RecordingTarget(log);
  const schedule = { intervalMs: 60_000 };

  it('rejects invalid wiring', () => {
    expect(() => newRunner({ connection: testConnection, schedule, pollers: [] })).toThrow('at least one poller');
    const other = { ...testBinding('itemChanged', 'other'), connection: { ...testConnection, name: 'secondary' } };
    const otherPoller = newBindingPoller({ definition: testDefinition('itemChanged'), binding: other, cursor: newMemoryCursorStore(), fetcher, target });
    expect(() => newRunner({ connection: testConnection, schedule, pollers: [otherPoller] })).toThrow('another connection');
    expect(() => newRunner({ connection: testConnection, schedule, pollers: [newTestBindingPoller('same', newMemoryCursorStore(), fetcher, target), newTestBindingPoller('same', newMemoryCursorStore(), fetcher, target)] })).toThrow('two pollers');
    const attached = newTestBindingPoller('attached', newMemoryCursorStore(), fetcher, target);
    newTestRunner({}, attached);
    expect(() => newRunner({ connection: testConnection, schedule, pollers: [attached] })).toThrow('already belongs');
    for (const invalid of [{ intervalMs: 0 }, { intervalMs: 60_000, maxBackoffMs: 1000 }, { intervalMs: 60_000, maxPagesPerPoll: -1 }, { intervalMs: 60_000, jitterFraction: 0.6 }, { intervalMs: 60_000, maxBackoffMs: Infinity }]) {
      expect(() => newRunner({ connection: testConnection, schedule: invalid, pollers: [newTestBindingPoller('s', newMemoryCursorStore(), fetcher, target)] })).toThrow();
    }
    const definition = testDefinition('itemChanged');
    const binding = testBinding('itemChanged', 'a');
    for (const config of [
      { definition, binding, fetcher, target }, { definition, binding, cursor: newMemoryCursorStore(), target }, { definition, binding, cursor: newMemoryCursorStore(), fetcher },
      { definition, binding: testBinding('otherTrigger', 'a'), cursor: newMemoryCursorStore(), fetcher, target },
      { definition, binding, cursor: newMemoryCursorStore(), fetcher, target, snapshot: { maxEntries: 200_000 } },
    ]) expect(() => newBindingPoller(config as Parameters<typeof newBindingPoller<Item>>[0])).toThrow();
    const member = newFeedMember(testDefinition('fileCreated'), testBinding('fileCreated', 'created'), selectFileEvents('created'), target);
    const files = new FileFeedFetcher(log, []);
    for (const config of [
      { name: 'Feed', cursor: newMemoryCursorStore(), fetcher: files, members: [member] }, { name: 'files', cursor: newMemoryCursorStore(), fetcher: files, members: [] },
      { name: 'files', fetcher: files, members: [member] },
    ]) expect(() => newFeedPoller(config as Parameters<typeof newFeedPoller<FileChange>>[0])).toThrow();
    expect(() => newFeedMember(testDefinition('fileCreated'), testBinding('fileCreated', 'created'), undefined as never, target)).toThrow();
  });

  it('gates every provider request on the request limiter', async () => {
    await waitForPollRequest(newTriggerContext());
    const clock = installFakeClock(0.5);
    const requests = new OperationLog();
    let waits = 0;
    let failure: Error | undefined;
    const limiter = { waitForPollRequest: async () => { waits++; if (failure) throw failure; } };
    const poller = newTestBindingPoller('first', newMemoryCursorStore(), new ChangeLogFetcher(requests, 'a'), new RecordingTarget(requests));
    const runner = newRunner({ connection: testConnection, schedule, pollers: [poller], requestLimiter: limiter, logger: { isEnabled: () => false, log: () => undefined } });
    await clock.runUntil(runner, () => {
      if (requests.count('fetch a after 0') === 1) failure = new Error('budget exhausted');
      return clock.waits.length === 4;
    });
    expect(waits).toBe(3);
    expect(requests.operations).toEqual(['seed a', 'fetch a after 0']);
    expect(clock.waits[3]).toBe(60_000);
  });

  it('finds the request limiter through a context the fetcher derives, as Go context values propagate', async () => {
    const clock = installFakeClock(0.5);
    const requests = new OperationLog();
    const inner = new ChangeLogFetcher(requests, 'a');
    const fetcher: ChangeFetcher<TriggerEvent<Item>> = {
      seedCursor: context => inner.seedCursor(withTriggerLogAttributes(context, { request: 'seed' })),
      fetchChanges: (context, cursor) => inner.fetchChanges(withTriggerLogAttributes(context, { request: 'page' }), cursor),
    };
    let waits = 0;
    const limiter = { waitForPollRequest: async () => { waits++; } };
    const poller = newTestBindingPoller('first', newMemoryCursorStore(), fetcher, new RecordingTarget(requests));
    const runner = newRunner({ connection: testConnection, schedule, pollers: [poller], requestLimiter: limiter, logger: { isEnabled: () => false, log: () => undefined } });
    await clock.runUntil(runner, () => requests.count('fetch a after 0') === 1);
    expect(waits).toBe(2);
  });

  it('starts a poll before its interval on wakePoller and runs once', async () => {
    const requests = new OperationLog();
    const runner = newRunner({ connection: testConnection, schedule: { intervalMs: 3_600_000, jitterFraction: 0 }, pollers: [newTestBindingPoller('first', newMemoryCursorStore(), new ChangeLogFetcher(requests, 'a'), new RecordingTarget(requests))], logger: { isEnabled: () => false, log: () => undefined } });
    const controller = new AbortController();
    const running = runner.run(newTriggerContext(controller.signal)).then(() => undefined, error => error);
    for (let attempt = 0; requests.count('seed a') === 0 && attempt < 500; attempt++) await delay(10);
    runner.wakePoller('unknown');
    runner.wakePoller('first');
    for (let attempt = 0; requests.count('fetch a after 0') === 0 && attempt < 500; attempt++) await delay(10);
    expect(requests.count('fetch a after 0')).toBe(1);
    controller.abort();
    expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError);
    await expect(runner.run(newTriggerContext())).rejects.toThrow('already running');
  });

  it('runs a binding Trigger source under newTrigger', async () => {
    const clock = installFakeClock(0.5);
    const requests = new OperationLog();
    const store = newMemoryCursorStore();
    await seedCursor(store, '0');
    const changes = new ChangeLogFetcher(requests, 'a');
    changes.append('a');
    const config = { definition: testDefinition('itemChanged'), binding: testBinding('itemChanged', 'main'), cursor: store, fetcher: changes };
    const source = newBindingTriggerSource(config, schedule, { isEnabled: () => false, log: () => undefined });
    const trigger = newTrigger({ definition: config.definition, binding: config.binding, source, target: new DurableTarget(requests), logger: { isEnabled: () => false, log: () => undefined } });
    clock.controller = new AbortController();
    clock.stopWhen = () => requests.count('fetch a after 1') === 1;
    await expect(trigger.run(newTriggerContext(clock.controller.signal))).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
    expect(requests.operations).toEqual(['replay ', 'fetch a after 0', 'prepare a', 'handle a', 'fetch a after 1']);
    expect(() => newBindingTriggerSource({ ...config, target: new RecordingTarget(requests) }, schedule)).toThrow();
  });

  it('derives the Gmail Trigger credential call identity', () => {
    const call = newTriggerCredentialCall({ provider: 'gmail', name: 'gmail-inbox' }, { connectorId: 'gmail', triggerName: 'messageReceived' }, new AbortController().signal);
    expect(call.id).toBe('a1955d2a-b59d-531a-bca7-c6d350e55ea5');
    expect(call.operation).toEqual({ connectorId: 'gmail', operationId: 'messageReceived' });
  });

  it('marks cursor and rate-limit errors', () => {
    expect(markCursorInvalid(undefined)).toBeUndefined();
    expect(markRateLimited(undefined, 1000)).toBeUndefined();
    const invalid = markCursorInvalid(new Error('gone'));
    expect(markCursorInvalid(invalid)).toBe(invalid);
    expect(isCursorInvalid(new Error('wrapped', { cause: invalid }))).toBe(true);
    const rateLimited = markRateLimited(new Error('429'), -1000);
    expect(rateLimited).toBeInstanceOf(RateLimitedError);
    expect(rateLimited.retryAfterMs).toBe(0);
    expect(rateLimited.message).toBe('provider rate limited the poll: 429');
  });
});
