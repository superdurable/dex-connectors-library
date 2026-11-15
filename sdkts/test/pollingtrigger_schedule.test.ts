// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { TriggerDeliveryAbortedError, newTriggerContext, type TriggerContext, type TriggerEvent } from '../src/index.js';
import { InMemoryObjectStore, PollCursorDocumentInvalidError, newPollCursorStore } from '../src/projectconfig/index.js';
import { PollWakeSignal, systemPollClock } from '../src/pollingtrigger/clock.js';
import { markRateLimited, newMemoryCursorStore, newRunner } from '../src/pollingtrigger/index.js';
import {
  ChangeLogFetcher, DurableTarget, LoggedCursorStore, OperationLog, RecordingTarget, installFakeClock, newTestBindingPoller, newTestRunner, seedCursor, testConnection, type Item,
} from './pollingtrigger_support.js';
import { newRecordingLogger } from './trigger_test_support.js';

const quietStore = () => new LoggedCursorStore(newMemoryCursorStore(), new OperationLog());

describe('poll runner schedule, mirroring the Go kit', () => {
  it('pauses every poller of the connection for a rate limit', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const [storeA, storeB] = [quietStore(), quietStore()];
    await seedCursor(storeA, '0');
    await seedCursor(storeB, '0');
    const fetcherA = new ChangeLogFetcher(log, 'a');
    fetcherA.failures.push(markRateLimited(new Error('429'), 120_000));
    const runner = newTestRunner({}, newTestBindingPoller('first', storeA, fetcherA, new RecordingTarget(log)), newTestBindingPoller('second', storeB, new ChangeLogFetcher(log, 'b'), new RecordingTarget(log)));
    expect(await clock.runUntil(runner, () => log.count('fetch b after 0') === 1)).toBeInstanceOf(TriggerDeliveryAbortedError);
    expect(log.operations).toEqual(['fetch a after 0', 'fetch a after 0', 'fetch b after 0']);
    expect(clock.waits.slice(0, 2)).toEqual([3000, 120_000]);
  });

  it('backs off exponentially for failures and a rate limit without Retry-After', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const store = quietStore();
    await seedCursor(store, '0');
    const failure = new Error('503');
    const fetcher = new ChangeLogFetcher(log, 'a');
    fetcher.failures.push(failure, failure, failure, failure, failure, undefined, markRateLimited(failure, 0), markRateLimited(failure, 0));
    const runner = newTestRunner({ intervalMs: 60_000, maxBackoffMs: 300_000 }, newTestBindingPoller('first', store, fetcher, new RecordingTarget(log)));
    await clock.runUntil(runner, () => log.count('fetch a after 0') === 9);
    expect(clock.waits.slice(1, 9)).toEqual([60_000, 120_000, 240_000, 300_000, 300_000, 60_000, 60_000, 120_000]);
  });

  it('keeps jitter within its bounds', async () => {
    for (const jitter of [0.000001, 0.999999]) {
      const clock = installFakeClock(jitter);
      const log = new OperationLog();
      const store = quietStore();
      await seedCursor(store, '0');
      const runner = newTestRunner({ intervalMs: 100_000 }, newTestBindingPoller('first', store, new ChangeLogFetcher(log, 'a'), new RecordingTarget(log)));
      await clock.runUntil(runner, () => log.count('fetch a after 0') === 1);
      expect(Math.abs(clock.waits[0]! - 10_000 * jitter)).toBeLessThanOrEqual(1);
      expect(Math.abs(clock.waits[1]! - 100_000 * (0.9 + 0.2 * jitter))).toBeLessThanOrEqual(1);
    }
  });

  it('yields to the next poller after maxPagesPerPoll', async () => {
    const clock = installFakeClock(0);
    const log = new OperationLog();
    const [storeA, storeB] = [quietStore(), quietStore()];
    await seedCursor(storeA, '0');
    await seedCursor(storeB, '0');
    const fetcherA = new ChangeLogFetcher(log, 'a', 1);
    fetcherA.append('a1', 'a2', 'a3');
    const runner = newTestRunner({ maxPagesPerPoll: 2 },
      newTestBindingPoller('first', storeA, fetcherA, new RecordingTarget(log)), newTestBindingPoller('second', storeB, new ChangeLogFetcher(log, 'b', 1), new RecordingTarget(log)));
    await clock.runUntil(runner, () => log.count('handle a3') === 1);
    expect(log.operations).toEqual(['fetch a after 0', 'handle a1', 'fetch a after 1', 'handle a2', 'fetch b after 0', 'fetch a after 2', 'handle a3']);
  });

  for (const scenario of ['during fetch', 'between record and commit']) {
    it(`leaves the cursor unchanged when cancelled ${scenario}`, async () => {
      const clock = installFakeClock(0);
      const log = new OperationLog();
      const store = new LoggedCursorStore(newMemoryCursorStore(), log);
      await seedCursor(store, '0');
      const fetcher = new ChangeLogFetcher(log, 'a');
      fetcher.append('a');
      const controller = new AbortController();
      if (scenario === 'during fetch') fetcher.onFetch = () => controller.abort();
      const target = new DurableTarget(log);
      const cancelAfterPrepare = Object.assign(Object.create(target) as DurableTarget, {
        prepareTrigger: async (context: TriggerContext, event: TriggerEvent<Item>) => { await target.prepareTrigger(context, event); controller.abort(); },
      });
      const runner = newTestRunner({}, newTestBindingPoller('first', store, fetcher, cancelAfterPrepare));
      clock.controller = controller;
      expect(await runner.run(newTriggerContext(controller.signal)).then(() => undefined, error => error)).toBeInstanceOf(TriggerDeliveryAbortedError);
      expect(log.count('replace 1')).toBe(0);
      expect((await store.state()).token).toBe('0');
    });
  }

  it('rejects an undecodable cursor before fetching and never overwrites it', async () => {
    installFakeClock(0);
    const objects = new InMemoryObjectStore();
    const store = newPollCursorStore(objects, { projectId: 'p', kind: 'live' }, { connectorId: 'changelog', connectionName: 'main', triggerName: 'itemChanged', bindingName: 'first' });
    const { revision } = await store.readPollCursor();
    await objects.compareAndSwapObject(store.key, revision, Buffer.from('{"schemaVersion":"other","state":null}'));
    const log = new OperationLog();
    const logger = newRecordingLogger();
    const runner = newRunner({ connection: testConnection, schedule: { intervalMs: 60_000 }, pollers: [newTestBindingPoller('first', store, new ChangeLogFetcher(log, 'a'), new RecordingTarget(log))], logger });
    const failure = await runner.run(newTriggerContext()).then(() => undefined, error => error);
    expect((failure as Error).cause).toBeInstanceOf(PollCursorDocumentInvalidError);
    expect(log.operations).toEqual([]);
    expect(logger.records.some(record => record.level === 'error' && record.message === 'poll cursor document invalid')).toBe(true);
    expect(Buffer.from((await objects.readObject(store.key)).contents).toString()).toBe('{"schemaVersion":"other","state":null}');
  });

  it('logs records with binding identity and no payloads', async () => {
    const clock = installFakeClock(0.5);
    const log = new OperationLog();
    const store = new LoggedCursorStore(newMemoryCursorStore(), log);
    const fetcher = new ChangeLogFetcher(log, 'a');
    fetcher.seedFailures.push(new Error('provider unavailable'));
    fetcher.append('secret-title');
    const target = new DurableTarget(log);
    target.handleErrors.set('new', [new Error('Dex is unavailable')]);
    const logger = newRecordingLogger();
    const runner = newRunner({ connection: testConnection, schedule: { intervalMs: 60_000 }, pollers: [newTestBindingPoller('first', store, fetcher, target)], logger });
    await clock.runUntil(runner, () => {
      if (log.count('replace 1') === 1 && log.count('fetch a after 1') === 0) fetcher.append('new');
      return log.count('handle new') === 2;
    });
    const identity = { connector: 'changelog', connection: 'main', trigger: 'itemChanged', binding: 'first' };
    const find = (message: string) => logger.records.find(record => record.message === message);
    expect(find('poll failed; retrying')).toEqual({ level: 'warn', message: 'poll failed; retrying', attributes: { ...identity, attempt: 1, delay_ms: 60_000, error_type: 'Error' } });
    expect(find('poll cursor seeded')).toEqual({ level: 'info', message: 'poll cursor seeded', attributes: { ...identity, reset: false } });
    expect(find('trigger delivery failed; retrying')).toEqual({ level: 'warn', message: 'trigger delivery failed; retrying', attributes: { ...identity, event_id: 'new', attempt: 1, delay_ms: 60_000, error_type: 'Error' } });
    expect(find('trigger delivered after retry')).toEqual({ level: 'info', message: 'trigger delivered after retry', attributes: { ...identity, event_id: 'new', attempts: 2 } });
    expect(JSON.stringify(logger.records)).not.toContain('secret-title');
  });

  it('waits out a delay longer than setTimeout holds instead of waking at once', async () => {
    const controller = new AbortController();
    let isSettled = false;
    const waiting = systemPollClock.waitForPoll(controller.signal, 2 ** 40, new PollWakeSignal()).then(() => { isSettled = true; }, () => { isSettled = true; throw new Error('aborted'); });
    await new Promise(resolve => setTimeout(resolve, 50));
    expect(isSettled).toBe(false);
    controller.abort();
    await expect(waiting).rejects.toThrow('aborted');
  });
});
