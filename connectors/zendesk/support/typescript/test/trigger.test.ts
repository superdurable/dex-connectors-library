// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid } from '@superdurable/connectors-sdk/pollingtrigger';
import { loadFromEnvironment, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { describe, expect, it } from 'vitest';
import {
  TicketChangeCreated, TicketChangeDeleted, TicketChangeUpdated, TicketExportFetcher, TicketStatusDeleted, ZendeskClient, defineTicketChangedTriggerBinding,
  newConnection, newProjectConnection, newProjectTicketTriggerRunner, newTicketChangedTrigger, newTicketTriggerRunner, parseTicketChangedTriggerConfiguration,
  type TicketChangedEvent, type ZendeskClientOptions,
} from '../src/index.js';
import { GeneratedMock } from '../src/mock.js';
import { ZendeskExportFake, fakeAPIBaseURL, fakeAPIToken, fakeEmail } from './zendesk_export_fake.js';

const reference = { provider: 'zendesk', name: 'zendesk-ticket-watcher' };
const credentials = { resolve: async () => ({ email: fakeEmail, apiToken: new SecretString(fakeAPIToken) }) };

/** Consume each event into a list a test waits on. */
class ListTarget implements TriggerTarget<TicketChangedEvent> {
  readonly events: TriggerEvent<TicketChangedEvent>[] = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<TicketChangedEvent>): Promise<void> { this.events.push(event); }
}

function fakeClient(fake: ZendeskExportFake, options: { maxResponseBytes?: number; pollIntervalMs?: number } & ZendeskClientOptions = {}): ZendeskClient {
  const { maxResponseBytes, pollIntervalMs, ...clientOptions } = options;
  return new ZendeskClient({ subdomain: 'acme', ...(maxResponseBytes ? { maxResponseBytes } : {}), ...(pollIntervalMs ? { pollIntervalMs } : {}) }, credentials,
    { apiBaseURL: fakeAPIBaseURL, send: fake.send, incrementalExportRequestsPerMinute: 600, ...clientOptions });
}

function fetcherOf(fake: ZendeskExportFake, includeDeleted = false, options: { maxResponseBytes?: number } & ZendeskClientOptions = {}): TicketExportFetcher {
  return new TicketExportFetcher(fakeClient(fake, options), reference, { includeDeleted });
}

async function eventually(condition: () => boolean | Promise<boolean>, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await condition())) {
    if (Date.now() > deadline) throw new Error('condition did not converge');
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}

const context = () => newTriggerContext();

describe('ticketChanged export fetches against the account fake', () => {
  it('seeds past existing changes and delivers later ones with derived kinds', async () => {
    const fake = new ZendeskExportFake();
    fake.create(101, 'Existing ticket');
    const fetcher = fetcherOf(fake);
    const cursor = await fetcher.seedCursor(context());
    expect(cursor.token).toContain(fake.cursor());
    fake.create(102, 'Double charge');
    fake.update(101, 'pending');
    fake.create(103, 'Spam');
    fake.delete(103);
    const page = await fetcher.fetchChanges(context(), cursor);
    expect(page.hasMorePages).toBe(false);
    expect(page.changes.map(change => change.payload.ticket.id)).toEqual([102, 101]);
    expect(page.changes[0]!.payload.changeKind).toBe(TicketChangeCreated);
    expect(page.changes[0]!.payload.ticket.agentUrl).toBe('https://acme.zendesk.com/agent/tickets/102');
    expect(page.changes[1]!.payload.changeKind).toBe(TicketChangeUpdated);
    expect(page.changes[1]!.payload.ticket.status).toBe('pending');
    expect(page.changes[1]!.payload.generatedAt).toBeDefined();
    const requests = fake.requests();
    expect(requests[0]!.query.has('start_time') && !requests[0]!.query.has('cursor')).toBe(true);
    expect(requests.at(-1)!.query.get('per_page')).toBe('100');
    expect(requests.at(-1)!.query.get('exclude_deleted')).toBe('true');
    const again = await fetcher.fetchChanges(context(), page.next);
    expect(again.changes).toEqual([]);
    expect(again.next).toEqual(page.next);
  });

  it('delivers deletions with includeDeleted', async () => {
    const fake = new ZendeskExportFake();
    const fetcher = fetcherOf(fake, true);
    const cursor = await fetcher.seedCursor(context());
    fake.create(201, 'Spam');
    fake.delete(201);
    const page = await fetcher.fetchChanges(context(), cursor);
    expect(page.changes.map(change => [change.payload.changeKind, change.payload.ticket.status])).toEqual([[TicketChangeDeleted, TicketStatusDeleted]]);
    expect(fake.requests()[1]!.query.has('exclude_deleted')).toBe(false);
  });

  it('restarts a rejected cursor after its last change time without a gap', async () => {
    const fake = new ZendeskExportFake();
    const fetcher = fetcherOf(fake);
    const cursor = await fetcher.seedCursor(context());
    fake.create(301, 'First');
    const first = await fetcher.fetchChanges(context(), cursor);
    expect(first.changes).toHaveLength(1);
    fake.invalidateCursors();
    fake.create(302, 'In the gap');
    const restarted = await fetcher.fetchChanges(context(), first.next);
    expect(restarted.changes.at(-1)!.payload.ticket.id).toBe(302);
    if (restarted.changes.length === 2) expect(restarted.changes[0]!.id).toBe(first.changes[0]!.id);
    const requests = fake.requests();
    expect(requests.at(-2)!.status).toBe(422);
    expect(requests.at(-1)!.query.has('start_time')).toBe(true);
  });

  it('keeps one event ID per ticket version', async () => {
    const fake = new ZendeskExportFake();
    fake.create(351, 'Repeated read');
    const old = { token: '{"resumeAt":1}' };
    const first = await fetcherOf(fake).fetchChanges(context(), old);
    const second = await fetcherOf(fake).fetchChanges(context(), old);
    expect(first.changes).toHaveLength(1);
    expect(second.changes[0]!.id).toBe(first.changes[0]!.id);
    fake.update(351, 'open');
    const third = await fetcherOf(fake).fetchChanges(context(), old);
    expect(third.changes[0]!.id).not.toBe(first.changes[0]!.id);
  });

  it('marks an undecodable cursor invalid without a request', async () => {
    const fake = new ZendeskExportFake();
    const error = await fetcherOf(fake).fetchChanges(context(), { token: 'not-json' }).catch((caught: unknown) => caught);
    expect(isCursorInvalid(error)).toBe(true);
    expect(fake.requests()).toEqual([]);
  });

  it('pauses for a 429 and names the administrator requirement for a 403', async () => {
    const fake = new ZendeskExportFake();
    const fetcher = fetcherOf(fake);
    fake.rateLimitNextRequests(1, '17');
    const rateLimited = await fetcher.seedCursor(context()).catch((caught: unknown) => caught);
    expect(rateLimited).toBeInstanceOf(RateLimitedError);
    expect((rateLimited as RateLimitedError).retryAfterMs).toBe(17_000);
    fake.rejectAsAgent(true);
    const rejected = await fetcher.seedCursor(context()).catch((caught: unknown) => caught);
    expect(String(rejected)).toContain('only for administrators');
    expect(String(rejected)).not.toContain(fakeAPIToken);
    expect(isCursorInvalid(rejected)).toBe(false);
  });

  it('follows after_cursor until the end of the stream', async () => {
    const fake = new ZendeskExportFake();
    const fetcher = fetcherOf(fake);
    const cursor = await fetcher.seedCursor(context());
    fake.limitPerPage(2);
    for (const ticketId of [401, 402, 403]) fake.create(ticketId, 'Burst');
    const first = await fetcher.fetchChanges(context(), cursor);
    expect([first.changes.length, first.hasMorePages]).toEqual([2, true]);
    const second = await fetcher.fetchChanges(context(), first.next);
    expect(second.changes.map(change => change.payload.ticket.id)).toEqual([403]);
    expect(second.hasMorePages).toBe(false);
  });

  it('halves per_page for oversized pages before failing', async () => {
    const fake = new ZendeskExportFake();
    for (let ticketId = 501; ticketId <= 520; ticketId++) fake.create(ticketId, 'Large backlog');
    const error = await fetcherOf(fake, false, { maxResponseBytes: 512 }).seedCursor(context()).catch((caught: unknown) => caught);
    expect(String(error)).toContain('raise maxResponseBytes');
    expect(fake.requests().map(request => request.query.get('per_page'))).toEqual(['100', '50', '25', '12', '10']);
  });

  it('refetches changes tied in one second across a page boundary after a cursor reset', async () => {
    const fake = new ZendeskExportFake();
    const fetcher = fetcherOf(fake);
    const cursor = await fetcher.seedCursor(context());
    fake.limitPerPage(2);
    fake.tieChanges(true);
    for (const ticketId of [601, 602, 603]) fake.create(ticketId, 'Same second');
    const first = await fetcher.fetchChanges(context(), cursor);
    expect([first.changes.length, first.hasMorePages]).toEqual([2, true]);
    fake.invalidateCursors();
    fake.limitPerPage(1000);
    const restarted = await fetcher.fetchChanges(context(), first.next);
    expect(restarted.changes.map(change => change.payload.ticket.id)).toEqual([601, 602, 603]);
    expect(restarted.changes[0]!.id).toBe(first.changes[0]!.id);
  });

  it('seeds with a Worker clock ahead of Zendesk and fails a rejected start_time restart', async () => {
    const fake = new ZendeskExportFake();
    const ahead = () => new Date(Date.now() + 180_000);
    await fetcherOf(fake, false, { now: ahead }).seedCursor(context());
    const error = await fetcherOf(fake, false, { now: ahead }).fetchChanges(context(), { token: `{"resumeAt":${Math.floor(Date.now() / 1000)}}` }).catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(Error);
    expect(isCursorInvalid(error)).toBe(false);
    expect(fake.requests().at(-1)!.status).toBe(400);
  });

  it('spaces export requests of every binding by the client rate', async () => {
    const fake = new ZendeskExportFake();
    const client = fakeClient(fake, { incrementalExportRequestsPerMinute: 120 });
    const started = Date.now();
    await Promise.all([0, 1, 2].map(() => new TicketExportFetcher(client, reference, {}).seedCursor(context())));
    // 120 per minute is one request each 500ms, so the third waits about a second.
    expect(Date.now() - started).toBeGreaterThanOrEqual(900);
  });
});

describe('ticketChanged configuration and wiring', () => {
  it('validates the saved configuration strictly, as Go does', () => {
    expect(parseTicketChangedTriggerConfiguration({})).toEqual({});
    expect(parseTicketChangedTriggerConfiguration({ includeDeleted: true }).includeDeleted).toBe(true);
    expect(() => parseTicketChangedTriggerConfiguration({ includeDeleted: 'true' })).toThrow();
    expect(() => parseTicketChangedTriggerConfiguration({ includeDeletions: true })).toThrow();
    expect(defineTicketChangedTriggerBinding({ connectionName: reference.name, bindingName: 'zendesk-ticket-changes' }).definition.trigger.triggerName).toBe('ticketChanged');
  });

  it('rejects a poll interval under five seconds and an export rate outside 1 to 600', () => {
    const fake = new ZendeskExportFake();
    expect(() => fakeClient(fake, { pollIntervalMs: 4_999 })).toThrow(/pollInterval/);
    for (const rate of [0, 601, 1.5]) expect(() => fakeClient(fake, { incrementalExportRequestsPerMinute: rate })).toThrow(/requests per minute/);
    expect(new ZendeskClient({ subdomain: 'acme' }, credentials).pollIntervalMs).toBe(60_000);
  });

  it('rejects a saved poll interval just under five seconds, as Go does', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'zendesk-poll-interval-test-'));
    try {
      for (const [index, pollInterval] of [4_999_999_999, '4.9999999s', 5_000_000_000, '5s'].entries()) {
        const path = join(directory, `connections-${index}.json`);
        await writeFile(path, JSON.stringify({
          schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
          connections: [{ connectorId: 'zendesk-support', connectionName: reference.name, modulePath: 'github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go', provider: 'zendesk', configuration: { subdomain: 'acme', pollInterval }, credentials: { email: fakeEmail, api_token: fakeAPIToken } }],
        }));
        await chmod(path, 0o600);
        const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
        const open = () => newProjectConnection(project, reference.name);
        if (index < 2) expect(open).toThrow(/pollInterval/);
        else expect((open().operations as ZendeskClient).pollIntervalMs).toBe(5_000);
      }
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  });

  it('rejects incomplete routes and mock connections', () => {
    const connection = newConnection(fakeClient(new ZendeskExportFake()), reference);
    const target = new ListTarget();
    for (const routes of [
      [], [{ bindingName: '', target }], [{ bindingName: 'ticketChanges' } as never],
      [{ bindingName: 'ticketChanges', target }, { bindingName: 'ticketChanges', target }],
      [{ bindingName: 'ticketChanges', target, configuration: { includeDeleted: 'yes' } as never }],
    ]) expect(() => newTicketTriggerRunner({ connection, ticketChangedRoutes: routes })).toThrow();
    expect(() => newTicketTriggerRunner({ connection: new GeneratedMock(reference.name).connection(), ticketChangedRoutes: [{ bindingName: 'ticketChanges', target }] })).toThrow(/mock/);
  });
});

describe('ticketChanged runners', () => {
  it('loads the saved includeDeleted, delivers a deletion through the inbox, and drains it', async () => {
    const fake = new ZendeskExportFake();
    fake.create(1001, 'Existing');
    const directory = await mkdtemp(join(tmpdir(), 'zendesk-trigger-test-'));
    const path = join(directory, 'connections.json');
    await writeFile(path, JSON.stringify({
      schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
      connections: [{ connectorId: 'zendesk-support', connectionName: reference.name, modulePath: 'github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go', provider: 'zendesk', configuration: { subdomain: 'acme', pollInterval: 5_000_000_000 }, credentials: { email: fakeEmail, api_token: fakeAPIToken } }],
      triggerBindings: [{ connectorId: 'zendesk-support', connectionName: reference.name, triggerName: 'ticketChanged', bindingName: 'allChanges', configuration: { includeDeleted: true } }],
    }));
    await chmod(path, 0o600);
    const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
    const target = new ListTarget();
    const stop = new AbortController();
    try {
      // The fake answers at the account's own https://acme.zendesk.com/api/v2 base URL.
      const connection = newProjectConnection(project, reference.name, { send: fake.send });
      const runner = newProjectTicketTriggerRunner(project, connection, { ticketChangedRoutes: [{ bindingName: 'allChanges', target }] });
      const key: TriggerInboxKey = { connectorId: 'zendesk-support', connectionName: reference.name, triggerName: 'ticketChanged', bindingName: 'allChanges' };
      const cursor = project.pollCursorStore(key);
      const committedToken = async (): Promise<string> => (await cursor.readPollCursor()).state?.token ?? '';
      const running = runner.run(newTriggerContext(stop.signal));
      await eventually(async () => (await committedToken()) !== '', 20_000);
      const baseline = await committedToken();
      expect(baseline).toContain('"includeDeleted":true');
      fake.delete(1001);
      runner.wakePoller('allChanges');
      await eventually(() => target.events.length > 0, 20_000);
      expect([target.events[0]!.payload.ticket.id, target.events[0]!.payload.changeKind]).toEqual([1001, TicketChangeDeleted]);
      await eventually(async () => (await committedToken()) !== baseline, 20_000);
      await eventually(async () => (await project.triggerInbox(key).pending()).length === 0);
      stop.abort();
      await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
    } finally {
      stop.abort();
      await rm(directory, { recursive: true, force: true });
    }
  }, 60_000);

  it('the in-memory Trigger baselines and delivers the next change', async () => {
    const fake = new ZendeskExportFake();
    fake.create(1001, 'Existing');
    const connection = newConnection(fakeClient(fake, { pollIntervalMs: 5_000 }), reference);
    const target = new ListTarget();
    expect(() => newTicketChangedTrigger({ connection, connectionName: 'other', bindingName: 'allTickets', target })).toThrow(/connectionName/);
    const trigger = newTicketChangedTrigger({ connection, connectionName: reference.name, bindingName: 'allTickets', target });
    const stop = new AbortController();
    const running = trigger.run(newTriggerContext(stop.signal)).catch((error: unknown) => error);
    try {
      await eventually(() => fake.requests().length > 0);
      fake.update(1001, 'solved');
      // The trigger polls again after one 5-second interval with jitter.
      await eventually(() => target.events.length > 0, 30_000);
      expect(target.events[0]!.payload.changeKind).toBe(TicketChangeUpdated);
      expect(target.events[0]!.payload.ticket.status).toBe('solved');
    } finally {
      stop.abort();
      expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError);
    }
  }, 45_000);
});
