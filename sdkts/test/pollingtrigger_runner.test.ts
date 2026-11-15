// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { TriggerDeliveryAbortedError, type TriggerTarget } from '../src/index.js';
import { TriggerInboxFullError } from '../src/projectconfig/index.js';
import { markCursorInvalid, newMemoryCursorStore } from '../src/pollingtrigger/index.js';
import {
  ChangeLogFetcher, DurableTarget, LoggedCursorStore, OperationLog, RecordingTarget, installFakeClock, newLoggedStore, newTestBindingPoller, newTestRunner, seedCursor, type Item,
} from './pollingtrigger_support.js';

describe('binding poller ordering, mirroring the Go kit', () => {
  it('seeds once without backlog', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const store = newLoggedStore(log);
    const fetcher = new ChangeLogFetcher(log, 'feed');
    fetcher.append('old-1', 'old-2', 'old-3');
    const target = new DurableTarget(log);
    const runner = newTestRunner({}, newTestBindingPoller('main', store, fetcher, target));
    expect(await clock.runUntil(runner, () => log.count('fetch feed after 3') === 1)).toBeInstanceOf(TriggerDeliveryAbortedError);
    expect(log.operations).toEqual(['read', 'replay ', 'read', 'seed feed', 'replace 3', 'read', 'fetch feed after 3']);
    expect((await store.state()).commitCount).toBe(1);
    // The first poll waits a uniform share of interval*jitterFraction; jitter 0.5 is the midpoint.
    expect(clock.waits[0]).toBe(3000);

    const second = newTestRunner({}, newTestBindingPoller('main', store, fetcher, target));
    fetcher.append('new-1');
    expect(await clock.runUntil(second, () => log.count('handle new-1') === 1)).toBeInstanceOf(TriggerDeliveryAbortedError);
    expect(log.count('seed feed')).toBe(1);
  });

  for (const [name, makeTarget, expected] of [
    ['durable target', (log: OperationLog) => new DurableTarget(log), ['read', 'fetch feed after 0', 'prepare a', 'prepare b', 'replace 2', 'handle a', 'handle b']],
    ['non-durable target', (log: OperationLog) => new RecordingTarget(log), ['read', 'fetch feed after 0', 'handle a', 'handle b', 'replace 2']],
  ] as Array<[string, (log: OperationLog) => TriggerTarget<Item>, string[]]>) {
    it(`records before commit and delivers after with a ${name}`, async () => {
      const clock = installFakeClock(0.5);
      const log = new OperationLog();
      const store = newLoggedStore(log);
      await seedCursor(store, '0');
      const fetcher = new ChangeLogFetcher(log, 'feed');
      fetcher.append('a', 'b');
      const runner = newTestRunner({}, newTestBindingPoller('main', store, fetcher, makeTarget(log)));
      await clock.runUntil(runner, () => log.count('replace 2') === 1);
      expect(log.operations.slice(-expected.length)).toEqual(expected);
    });
  }

  it('retries a record failure from the old cursor', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const store = newLoggedStore(log);
    await seedCursor(store, '0');
    const fetcher = new ChangeLogFetcher(log, 'feed');
    fetcher.append('a', 'b');
    const target = new DurableTarget(log);
    target.prepareErrors.set('b', [new TriggerInboxFullError()]);
    await clock.runUntil(newTestRunner({}, newTestBindingPoller('main', store, fetcher, target)), () => log.count('handle b') === 1);
    expect(log.operations.slice(4)).toEqual(['read', 'fetch feed after 0', 'prepare a', 'prepare b', 'read', 'fetch feed after 0', 'prepare b', 'replace 2', 'handle a', 'handle b']);
    // The failed poll waits one interval of backoff before the retry.
    expect(clock.waits[1]).toBe(60_000);
  });

  it('retries a failed event before fetching again', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const store = newLoggedStore(log);
    await seedCursor(store, '0');
    const fetcher = new ChangeLogFetcher(log, 'feed');
    fetcher.append('a', 'b');
    const failure = new Error('Dex is unavailable');
    const target = new DurableTarget(log);
    target.handleErrors.set('a', [failure, failure]);
    await clock.runUntil(newTestRunner({}, newTestBindingPoller('main', store, fetcher, target)), () => log.count('fetch feed after 2') === 1);
    expect(log.operations.slice(4)).toEqual(['read', 'fetch feed after 0', 'prepare a', 'prepare b', 'replace 2', 'handle a', 'handle a', 'handle a', 'handle b', 'read', 'fetch feed after 2']);
    expect(clock.waits.slice(1, 4)).toEqual([60_000, 120_000, 60_000]);
  });

  it('delivers recorded events after a commit conflict', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const memory = newMemoryCursorStore();
    const store = new LoggedCursorStore(memory, log);
    await seedCursor(store, '0');
    store.beforeCommit = async () => {
      const record = await memory.readPollCursor();
      await memory.replacePollCursor(record.revision, { ...record.state!, token: '1', commitCount: record.state!.commitCount + 1 });
    };
    const fetcher = new ChangeLogFetcher(log, 'feed');
    fetcher.append('a', 'b');
    await clock.runUntil(newTestRunner({}, newTestBindingPoller('main', store, fetcher, new DurableTarget(log))), () => log.count('fetch feed after 1') === 1);
    expect(log.operations.slice(4)).toEqual([
      'read', 'fetch feed after 0', 'prepare a', 'prepare b', 'replace 2', 'handle a', 'handle b',
      'read', 'fetch feed after 1', 'prepare b', 'replace 2', 'handle b',
    ]);
  });

  it('reseeds an invalid cursor without delivering the gap', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const store = newLoggedStore(log);
    await seedCursor(store, '0');
    const fetcher = new ChangeLogFetcher(log, 'feed');
    fetcher.failures.push(markCursorInvalid(new Error('410 Gone')));
    fetcher.append('gap-1', 'gap-2');
    await clock.runUntil(newTestRunner({}, newTestBindingPoller('main', store, fetcher, new DurableTarget(log))), () => log.count('fetch feed after 2') === 1);
    expect(log.operations.slice(4)).toEqual(['read', 'fetch feed after 0', 'seed feed', 'replace 2', 'read', 'fetch feed after 2']);
    const state = await store.state();
    expect(state.resetCount).toBe(1);
    expect(state.lastResetAtUnixMs).toBeDefined();
    expect(log.count('prepare gap-1')).toBe(0);
  });

  it('skips consumed events by ID when the retry page changes', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const store = newLoggedStore(log);
    await seedCursor(store, '0');
    const fetcher = new ChangeLogFetcher(log, 'feed');
    fetcher.append('a', 'b', 'c');
    // The retry from cursor 0 no longer lists a, so b and c move up one position.
    fetcher.replaceChangesOnFetch(2, 'b', 'c', 'd');
    const target = new RecordingTarget(log);
    target.handleErrors.set('c', [new Error('Dex is unavailable')]);
    await clock.runUntil(newTestRunner({}, newTestBindingPoller('main', store, fetcher, target)), () => log.count('replace 3') === 1);
    expect(log.operations.slice(3)).toEqual(['read', 'fetch feed after 0', 'handle a', 'handle b', 'handle c', 'read', 'fetch feed after 0', 'handle c', 'handle d', 'replace 3']);
  });

  it('delivers a recorded event the retry no longer lists', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const store = newLoggedStore(log);
    await seedCursor(store, '0');
    const fetcher = new ChangeLogFetcher(log, 'feed');
    fetcher.append('a', 'b');
    fetcher.replaceChangesOnFetch(2, 'b');
    const target = new DurableTarget(log);
    target.prepareErrors.set('b', [new Error('inbox write failed')]);
    await clock.runUntil(newTestRunner({}, newTestBindingPoller('main', store, fetcher, target)), () => log.count('handle b') === 1);
    expect(log.operations.slice(4)).toEqual(['read', 'fetch feed after 0', 'prepare a', 'prepare b', 'read', 'fetch feed after 0', 'prepare b', 'replace 1', 'handle a', 'handle b']);
  });

  it('delivers recorded events when the retry page is empty', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const store = newLoggedStore(log);
    await seedCursor(store, '0');
    const fetcher = new ChangeLogFetcher(log, 'feed');
    fetcher.append('a', 'b');
    fetcher.replaceChangesOnFetch(2);
    const target = new DurableTarget(log);
    target.prepareErrors.set('b', [new Error('inbox write failed')]);
    await clock.runUntil(newTestRunner({}, newTestBindingPoller('main', store, fetcher, target)), () => log.count('handle a') === 1);
    expect(log.operations.slice(4)).toEqual(['read', 'fetch feed after 0', 'prepare a', 'prepare b', 'read', 'fetch feed after 0', 'handle a']);
  });
});
