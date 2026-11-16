// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { NotionClient, type NotionClientOptions } from '../src/client.js';
import { DatabaseRowFetcher } from '../src/database_row_fetcher.js';
import { GoTime, parseGoTime } from '../src/go_time.js';
import {
  DatabaseRowChangeKind, DatabaseRowChangedTriggerDefinition, DatabaseRowChangedTriggerConfigurationSchema, newConnection, newConnectionWithOperations,
  newDatabaseRowChangedTrigger, newDatabaseRowTriggerRunner, newProjectConnection, newProjectDatabaseRowTriggerRunner, validateDatabaseRowChangedTriggerConfiguration,
  type DatabaseRowChangedEvent,
} from '../src/index.js';
import { Endpoint, NotionRowsFake, Token } from './notion_rows_fake.js';

const dataSourceId = '2a4d6f80-1b3c-4d5e-8f70-112233445566';
const databaseId = '2a4d6f80-1b3c-4d5e-8f70-112233445500';
const rowA = '2a4d6f80-1b3c-4d5e-8f70-0000000000a1';
const rowB = '2a4d6f80-1b3c-4d5e-8f70-0000000000b2';
const rowC = '2a4d6f80-1b3c-4d5e-8f70-0000000000c3';
const reference = { provider: 'notion', name: 'rows' };
const baseTime = Date.UTC(2026, 10, 2, 9, 0);
const manifest = parse(readFileSync(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { languages: { typescript: { triggers: string[] } }; triggers: { name: string; description: string }[] } };
const descriptor = JSON.parse(readFileSync(new URL('../dex-connector.json', import.meta.url), 'utf8')) as { factories: { name: string; kind: string; triggerName?: string }[] };

function minute(offset: number): Date { return new Date(baseTime + offset * 60_000); }

function newClient(send: NotionClientOptions['send'], pollPageSize = 100): NotionClient {
  return new NotionClient({ endpoint: Endpoint, pollIntervalMs: 5000, pollPageSize }, { resolve: async () => ({ apiToken: new SecretString(Token) }) }, { send });
}

function newFetcher(fake: NotionRowsFake, pageSize = 100, watched = dataSourceId): DatabaseRowFetcher {
  return new DatabaseRowFetcher(newClient(fake.send, pageSize), reference, DatabaseRowChangedTriggerDefinition.trigger, watched);
}

/** Follow hasMorePages from cursor, as the kit does within one poll. */
async function fetchAll(fetcher: DatabaseRowFetcher, cursor: Cursor): Promise<[TriggerEvent<DatabaseRowChangedEvent>[], Cursor]> {
  const events: TriggerEvent<DatabaseRowChangedEvent>[] = [];
  for (let page = 0; page < 20; page++) {
    const changes = await fetcher.fetchChanges(newTriggerContext(), cursor);
    events.push(...changes.changes); cursor = changes.next;
    if (!changes.hasMorePages) return [events, cursor];
  }
  throw new Error('the fetcher never finished its query sequence');
}

function titles(events: TriggerEvent<DatabaseRowChangedEvent>[]): string[] { return events.map(event => `${event.payload.row.title}:${event.payload.changeKind}`); }

describe('databaseRowChanged fetcher', () => {
  it('records a baseline and then delivers each new row version once', async () => {
    const fake = new NotionRowsFake(dataSourceId, databaseId);
    fake.create(rowA, 'Existing old', minute(0));
    fake.create(rowB, 'Existing newest', minute(10));
    const fetcher = newFetcher(fake);
    const seed = await fetcher.seedCursor(newTriggerContext());
    expect(fake.requests.map(request => request.body?.sorts?.[0]?.direction)).toEqual(['descending', 'ascending']);
    expect(fake.requests[1]!.body?.filter?.last_edited_time?.on_or_after).toBe('2026-11-02T09:08:00Z');
    const [baseline, cursor] = await fetchAll(fetcher, seed);
    expect(baseline).toEqual([]);

    fake.create(rowC, 'New', minute(11));
    fake.edit(rowA, 'Existing edited', minute(12));
    const [events, next] = await fetchAll(fetcher, cursor);
    expect(titles(events)).toEqual(['New:created', 'Existing edited:updated']);
    expect(events[0]!.payload.dataSourceId).toBe(dataSourceId);
    expect(events[0]!.occurredAt).toEqual(minute(11));
    const [refetched] = await fetchAll(fetcher, cursor);
    expect(refetched.map(event => event.id)).toEqual(events.map(event => event.id));
    expect((await fetchAll(fetcher, next))[0]).toEqual([]);
  });

  it('delivers rows indexed late inside the lookback once', async () => {
    const fake = new NotionRowsFake(dataSourceId, databaseId);
    const fetcher = newFetcher(fake);
    const seed = await fetcher.seedCursor(newTriggerContext());
    fake.create(rowA, 'A', minute(31));
    fake.create(rowB, 'B late', minute(31));
    fake.hideFromQueries(rowB);
    let [events, cursor] = await fetchAll(fetcher, seed);
    expect(titles(events)).toEqual(['A:created']);
    fake.create(rowC, 'C', minute(32));
    [events, cursor] = await fetchAll(fetcher, cursor);
    expect(titles(events)).toEqual(['C:created']);
    fake.revealToQueries(rowB);
    [events] = await fetchAll(fetcher, cursor);
    expect(titles(events)).toEqual(['B late:created']);
  });

  it('pages through a burst and restarts the query after a stale start cursor', async () => {
    const fake = new NotionRowsFake(dataSourceId, databaseId);
    const fetcher = newFetcher(fake, 1);
    const seed = await fetcher.seedCursor(newTriggerContext());
    for (const [id, title] of [[rowA, 'A'], [rowB, 'B'], [rowC, 'C']] as const) fake.create(id, title, minute(31));
    const first = await fetcher.fetchChanges(newTriggerContext(), seed);
    expect(first.hasMorePages).toBe(true);
    expect(first.next.token).toContain('"startCursor":"rows-1"');
    fake.expireStartCursors();
    const restarted = await fetcher.fetchChanges(newTriggerContext(), first.next);
    expect([restarted.changes, restarted.hasMorePages]).toEqual([[], true]);
    expect(restarted.next.token).not.toContain('startCursor');
    const [remaining] = await fetchAll(fetcher, restarted.next);
    expect(titles(first.changes)).toEqual(['A:created']);
    expect(titles(remaining).sort()).toEqual(['B:created', 'C:created']);
  });

  it('does not deliver trashed rows and classifies failures for the kit', async () => {
    const fake = new NotionRowsFake(dataSourceId, databaseId);
    fake.create(rowA, 'A', minute(0));
    const fetcher = newFetcher(fake);
    const seed = await fetcher.seedCursor(newTriggerContext());
    fake.trash(rowA, minute(31));
    expect((await fetchAll(fetcher, seed))[0]).toEqual([]);

    fake.rateLimitNextRequests(1, '7');
    const limited = await fetcher.fetchChanges(newTriggerContext(), seed).catch((error: unknown) => error);
    expect(limited).toBeInstanceOf(RateLimitedError);
    expect((limited as RateLimitedError).retryAfterMs).toBe(7000);

    for (const token of [
      'not json',
      '{"v":1,"dataSourceId":"2a4d6f80-1b3c-4d5e-8f70-999999999999","highWater":"2026-11-02T09:00:00Z"}',
      `{"v":9,"dataSourceId":"${dataSourceId}","highWater":"2026-11-02T09:00:00Z"}`,
      `{"v":1,"dataSourceId":"${dataSourceId}"}`,
    ]) {
      const invalid = await fetcher.fetchChanges(newTriggerContext(), { token }).catch((error: unknown) => error);
      expect(isCursorInvalid(invalid), token).toBe(true);
    }

    const unshared = newFetcher(fake, 100, '2a4d6f80-1b3c-4d5e-8f70-999999999999');
    const failed = await unshared.seedCursor(newTriggerContext()).catch((error: unknown) => error);
    expect((failed as Error).message).toContain('share its database with the connection');
    expect(isCursorInvalid(failed) || failed instanceof RateLimitedError).toBe(false);
    expect(fake.requests.at(-1)!.status).toBe(404);
  });
});

describe('databaseRowChanged runner', () => {
  it('seeds, then delivers each row version to its target', async () => {
    const fake = new NotionRowsFake(dataSourceId, databaseId);
    fake.create(rowA, 'Existing', minute(0));
    const connection = newConnection(newClient(fake.send), reference);
    const events: TriggerEvent<DatabaseRowChangedEvent>[] = [];
    const target: TriggerTarget<DatabaseRowChangedEvent> = { handleTrigger: async (_context, event) => { events.push(event); } };
    const runner = newDatabaseRowTriggerRunner({ connection, databaseRowChangedRoutes: [{ bindingName: 'rows', configuration: { dataSourceId: dataSourceId.replaceAll('-', '') }, target }] });
    const abort = new AbortController();
    const running = runner.run(newTriggerContext(abort.signal));
    try {
      await eventually(() => fake.requests.length >= 2);
      fake.create(rowB, 'New', minute(1));
      runner.wakePoller('rows');
      await eventually(() => events.length === 1);
      expect([events[0]!.payload.changeKind, events[0]!.payload.dataSourceId]).toEqual([DatabaseRowChangeKind.Created, dataSourceId]);
    } finally {
      abort.abort();
      await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
    }
  });

  it('refuses incomplete routes, mock connections, and unsaved bindings', () => {
    const connection = newConnection(newClient(async () => ({ status: 500, headers: {}, body: new Uint8Array() })), reference);
    const target: TriggerTarget<DatabaseRowChangedEvent> = { handleTrigger: async () => {} };
    const configuration = { dataSourceId };
    for (const routes of [[], [{ bindingName: '', configuration, target }], [{ bindingName: 'rows', configuration: {}, target }]]) {
      expect(() => newDatabaseRowTriggerRunner({ connection, databaseRowChangedRoutes: routes })).toThrow(TypeError);
    }
    expect(() => newDatabaseRowTriggerRunner({ connection: newConnectionWithOperations({ ...connection.operations } as never, reference), databaseRowChangedRoutes: [{ bindingName: 'rows', configuration, target }] })).toThrow(/newConnection with a NotionClient/);
    expect(() => newDatabaseRowChangedTrigger({ connection, connectionName: 'rows', bindingName: 'rows', configuration: {}, target })).toThrow(/dataSourceId/);
    expect(newDatabaseRowChangedTrigger({ connection, connectionName: 'rows', bindingName: 'rows', configuration, target })).toBeDefined();
    const project = { configuration: { connections: [{ connectorId: 'notion', connectionName: 'rows', provider: 'notion', configuration: {} }], triggerBindings: [] } };
    expect(() => newProjectDatabaseRowTriggerRunner(project as never, 'rows', { databaseRowChangedRoutes: [{ bindingName: 'rows', target }] })).toThrow(/has no saved data source/);
  });

  it('validates the configuration and poll settings', () => {
    validateDatabaseRowChangedTriggerConfiguration({ dataSourceId: 'https://www.notion.so/2a4d6f801b3c4d5e8f70112233445566' });
    expect(() => validateDatabaseRowChangedTriggerConfiguration({})).toThrow(/dataSourceId is required/);
    expect(() => validateDatabaseRowChangedTriggerConfiguration({ dataSourceId: 'not-an-id' })).toThrow(TypeError);
    expect(() => DatabaseRowChangedTriggerConfigurationSchema.parse({ dataSourceId, databaseId: 'x' })).toThrow();
    const credentials = { resolve: async () => ({ apiToken: new SecretString(Token) }) };
    expect(() => new NotionClient({ pollIntervalMs: 1000 }, credentials)).toThrow(/pollInterval must be at least 5s/);
    expect(() => new NotionClient({ pollPageSize: 101 }, credentials)).toThrow(/pollPageSize must be between 1 and 100/);
    // A project pollInterval is a Go time.Duration in nanoseconds; Go refuses anything under 5s.
    const projectWithPollInterval = (pollInterval: number) => ({ configuration: { connections: [{ connectorId: 'notion', connectionName: 'rows', provider: 'notion', configuration: { pollInterval } }], triggerBindings: [] } }) as never;
    for (const pollInterval of [999_999, 4_999_999_999]) expect(() => newProjectConnection(projectWithPollInterval(pollInterval), 'rows')).toThrow(/pollInterval must be at least 5s/);
    expect((newProjectConnection(projectWithPollInterval(5_000_000_000), 'rows').operations as NotionClient).pollIntervalMs).toBe(5000);
  });

  it('matches the manifest and descriptor', () => {
    expect(manifest.spec.languages.typescript.triggers).toEqual(['databaseRowChanged']);
    const [trigger] = manifest.spec.triggers;
    expect(DatabaseRowChangedTriggerDefinition).toEqual({ trigger: { connectorId: 'notion', triggerName: trigger!.name }, description: trigger!.description });
    expect(descriptor.factories.filter(factory => factory.kind === 'trigger')).toEqual([expect.objectContaining({ name: 'defineDatabaseRowChangedTriggerBinding', triggerName: 'databaseRowChanged' })]);
  });
});

describe('Go time', () => {
  it.each([
    ['2026-11-02T09:00:00.000Z', 1793610000n * 1_000_000_000n, '2026-11-02T09:00:00Z'],
    ['2026-11-02T10:00:00.5+01:00', 1793610000n * 1_000_000_000n + 500_000_000n, '2026-11-02T10:00:00.5+01:00'],
    ['2026-11-02T9:00:00,1234567891Z', 1793610000n * 1_000_000_000n + 123_456_789n, '2026-11-02T09:00:00.123456789Z'],
    ['0000-02-29T00:00:00-00:00', -62162121600n * 1_000_000_000n, '0000-02-29T00:00:00Z'],
    ['1970-01-01T00:00:00Z', 0n, '1970-01-01T00:00:00Z'],
  ])('parses and formats %s as Go does', (text, unixNanoseconds, rendered) => {
    const parsed = parseGoTime(text);
    expect(parsed.unixNanoseconds).toBe(unixNanoseconds);
    expect(parsed.formatRFC3339Nano()).toBe(rendered);
  });

  it.each(['2026-02-29T00:00:00Z', '2026-11-02T24:00:00Z', '2026-11-02T09:00:00', '2026-11-02 09:00:00Z', 'x'])('refuses %s', text => {
    expect(() => parseGoTime(text)).toThrow(RangeError);
  });

  it('refuses to format a year Go cannot encode', () => {
    expect(() => new GoTime(parseGoTime('9999-12-31T23:59:59Z').unixNanoseconds + 1_000_000_000n).formatRFC3339Nano()).toThrow(RangeError);
  });
});

async function eventually(isMet: () => boolean, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!isMet()) {
    if (Date.now() > deadline) throw new Error('condition not met in time');
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}
