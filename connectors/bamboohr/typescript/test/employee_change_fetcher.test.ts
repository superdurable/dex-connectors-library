// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// The employeeChanged fetcher, ported from the Go connector's employee_change_fetcher tests.
import { newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { MaxChangesPerPage, RateLimitedError, isCursorInvalid } from '@superdurable/connectors-sdk/pollingtrigger';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { describe, expect, it } from 'vitest';
import { EmployeeChangeSettleWindowMs, EmployeeChangedTriggerDefinition, validateEmployeeChangedTriggerConfiguration, type EmployeeChangedEvent } from '../src/index.js';
import { EmployeeChangeFetcher } from '../src/employee_change_fetcher.js';
import { APIKey, BambooHRFake, Start, TriggerTestConnection, newFakeClient } from './bamboohr_fake.js';

const context = newTriggerContext();
const startCursor = { token: '{"v":1,"since":"2026-10-13T09:00:00Z"}' };

function fetcherFor(fake: BambooHRFake, changeType = '', options: Parameters<typeof newFakeClient>[1] = {}): EmployeeChangeFetcher {
  return new EmployeeChangeFetcher(newFakeClient(fake, options), TriggerTestConnection, EmployeeChangedTriggerDefinition.trigger, changeType ? { changeType } : {});
}

const eventIds = (events: Array<TriggerEvent<EmployeeChangedEvent>>) => events.map(event => event.id);

async function rejection(promise: Promise<unknown>): Promise<Error> {
  try { await promise; } catch (error) { return error as Error; }
  throw new Error('expected a rejection');
}

describe('employeeChanged fetcher', () => {
  it('seeds at BambooHR clock without backlog', async () => {
    const fake = new BambooHRFake();
    fake.insert('140', { firstName: 'Ava' });
    fake.advance(1000);
    // The local clock runs an hour ahead; the cursor must still start at BambooHR's time.
    const fetcher = fetcherFor(fake, '', { now: () => new Date(Start.getTime() + 3_600_000) });
    const cursor = await fetcher.seedCursor(context);
    expect(cursor.token).toBe('{"v":1,"since":"2026-10-13T09:00:02Z"}');
    fake.advance(60_000);
    const page = await fetcher.fetchChanges(context, cursor);
    expect(page.changes).toEqual([]);
    expect(page.next.token).toBe(cursor.token);
    expect(fake.changeListQueries.map(query => query.get('since'))).toEqual(['2026-10-13T09:59:59Z', '2026-10-13T09:00:01Z']);
  });

  it('delivers changes oldest first with stable IDs', async () => {
    const fake = new BambooHRFake();
    const fetcher = fetcherFor(fake);
    const cursor = await fetcher.seedCursor(context);
    fake.insert('141', { firstName: 'Sam' });
    fake.update('9', { department: 'Sales' });
    fake.delete('77');
    fake.advance(EmployeeChangeSettleWindowMs);
    const page = await fetcher.fetchChanges(context, cursor);
    expect(page.hasMorePages).toBe(false);
    expect(eventIds(page.changes)).toEqual(['employee-141-inserted-1791882001', 'employee-9-updated-1791882002', 'employee-77-deleted-1791882003']);
    expect(page.changes[1]!.payload).toEqual({ employeeId: '9', action: 'Updated', lastChanged: '2026-10-13T09:00:02Z' });
    expect(page.changes[1]!.occurredAt).toEqual(new Date('2026-10-13T09:00:02Z'));
    expect(page.next.token).toBe('{"v":1,"since":"2026-10-13T09:00:03Z","afterEmployeeId":"77"}');
    expect(eventIds((await fetcher.fetchChanges(context, cursor)).changes)).toEqual(eventIds(page.changes));
    expect((await fetcher.fetchChanges(context, page.next)).changes).toEqual([]);
  });

  it('waits for changes to settle', async () => {
    const fake = new BambooHRFake();
    const fetcher = fetcherFor(fake);
    const cursor = await fetcher.seedCursor(context);
    fake.insert('200');
    fake.advance(EmployeeChangeSettleWindowMs - 2000);
    let page = await fetcher.fetchChanges(context, cursor);
    expect(page.changes).toEqual([]);
    expect(page.next.token).toBe(cursor.token);
    // Employee 150 shares employee 200's second but becomes visible only now.
    fake.insertLate('150', new Date(Start.getTime() + 1000));
    fake.advance(2000);
    page = await fetcher.fetchChanges(context, page.next);
    expect(eventIds(page.changes)).toEqual(['employee-150-inserted-1791882001', 'employee-200-inserted-1791882001']);
    expect(page.next.token).toBe('{"v":1,"since":"2026-10-13T09:00:01Z","afterEmployeeId":"200"}');
  });

  it('sends the configured change type', async () => {
    const fake = new BambooHRFake();
    const fetcher = fetcherFor(fake, 'inserted');
    const cursor = await fetcher.seedCursor(context);
    expect(cursor.token).toBe('{"v":1,"since":"2026-10-13T09:00:00Z","changeType":"inserted"}');
    fake.insert('141');
    fake.update('9');
    fake.advance(EmployeeChangeSettleWindowMs);
    expect(eventIds((await fetcher.fetchChanges(context, cursor)).changes)).toEqual(['employee-141-inserted-1791882001']);
    expect(fake.changeListQueries.every(query => query.get('type') === 'inserted')).toBe(true);
  });

  it('pages a change list larger than one page', async () => {
    const fake = new BambooHRFake();
    const fetcher = fetcherFor(fake);
    const cursor = await fetcher.seedCursor(context);
    for (let employee = 1; employee <= MaxChangesPerPage + 2; employee++) fake.insert(String(employee));
    fake.advance(EmployeeChangeSettleWindowMs);
    const page = await fetcher.fetchChanges(context, cursor);
    expect(page.changes).toHaveLength(MaxChangesPerPage);
    expect(page.hasMorePages).toBe(true);
    const rest = await fetcher.fetchChanges(context, page.next);
    expect(eventIds(rest.changes)).toEqual(['employee-501-inserted-1791882501', 'employee-502-inserted-1791882502']);
    expect(rest.hasMorePages).toBe(false);
  });

  it('marks throttling as rate limited', async () => {
    const fake = new BambooHRFake();
    const fetcher = fetcherFor(fake);
    const cursor = await fetcher.seedCursor(context);
    fake.rateLimitChangeLists(1);
    const error = await rejection(fetcher.fetchChanges(context, cursor));
    expect(error).toBeInstanceOf(RateLimitedError);
    expect((error as RateLimitedError).retryAfterMs).toBe(1000);
    expect(isCursorInvalid(error)).toBe(false);
  });

  it.each([
    [503, { 'retry-after': '7' }, true, false, 'HTTP 503'],
    [400, {}, false, true, 'HTTP 400'],
    [401, {}, false, false, 'HTTP 401'],
    [500, {}, false, false, 'HTTP 500'],
  ] as const)('classifies HTTP %i', async (status, headers, isRateLimited, isInvalid, message) => {
    const fake = new BambooHRFake();
    const fetcher = fetcherFor(fake, '', { send: async () => ({ status, headers: { ...headers }, body: new Uint8Array() }) });
    const error = await rejection(fetcher.fetchChanges(context, startCursor));
    expect(error instanceof RateLimitedError).toBe(isRateLimited);
    expect(isCursorInvalid(error)).toBe(isInvalid);
    expect(error.message).toContain(message);
    expect(error.message).not.toContain(APIKey);
    if (isRateLimited) expect((error as RateLimitedError).retryAfterMs).toBe(7000);
  });

  it('reports an oversized change list', async () => {
    const fetcher = fetcherFor(new BambooHRFake(), '', { send: async () => { throw new BodyTooLargeError(200, {}); } });
    const error = await rejection(fetcher.fetchChanges(context, startCursor));
    expect(error.message).toContain('raise maxResponseBytes');
    expect(isCursorInvalid(error)).toBe(false);
  });

  it('rejects a byte order mark and non-JSON trailing whitespace, as Go does', async () => {
    const encoder = new TextEncoder();
    for (const body of [new Uint8Array([0xef, 0xbb, 0xbf, ...encoder.encode('{"employees":[]}')]), encoder.encode('{"employees":[]} ')]) {
      const send = async () => ({ status: 200, headers: { date: 'Tue, 13 Oct 2026 10:00:00 GMT' }, body });
      const error = await rejection(fetcherFor(new BambooHRFake(), '', { send }).fetchChanges(context, startCursor));
      expect(error.message).toBe('BambooHR returned an invalid change list: the response is not a change list');
    }
  });

  it('rejects foreign cursors without a request', async () => {
    const fake = new BambooHRFake();
    const fetcher = fetcherFor(fake, 'deleted');
    for (const token of ['', 'not json', '{"v":2,"since":"2026-10-13T09:00:00Z","changeType":"deleted"}',
      '{"v":1,"since":"2026-10-13T09:00:00Z","changeType":"deleted","extra":1}',
      '{"v":1,"since":"2026-10-13T09:00:00Z","afterEmployeeId":"x1","changeType":"deleted"}',
      '{"v":1,"since":"2026-10-13T09:00:00Z","changeType":"updated"}', '{"v":1,"since":"2026-10-13T09:00:00Z"}']) {
      expect(isCursorInvalid(await rejection(fetcher.fetchChanges(context, { token }))), token).toBe(true);
    }
    expect(fake.changeListQueries).toEqual([]);
  });

  it('validates the configured change type', () => {
    for (const changeType of [undefined, '', 'inserted', 'updated', 'deleted']) expect(() => validateEmployeeChangedTriggerConfiguration({ changeType })).not.toThrow();
    for (const changeType of ['all', 'Inserted', ' inserted']) expect(() => validateEmployeeChangedTriggerConfiguration({ changeType })).toThrow('inserted, updated, deleted, or blank');
  });
});
