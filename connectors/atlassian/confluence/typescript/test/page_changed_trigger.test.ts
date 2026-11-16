// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid, newMemoryCursorStore } from '@superdurable/connectors-sdk/pollingtrigger';
import { describe, expect, it } from 'vitest';
import { ConfluenceClient } from '../src/client.js';
import { PageChangeFetcher } from '../src/page_change_fetcher.js';
import {
  PageChangeCreated, PageChangeUpdated, PageChangedTriggerConfigurationSchema, PageChangedTriggerDefinition, decodePageChangedEventPayload,
  definePageChangedTriggerBinding, newConnection, newPageChangedTrigger, newPageTriggerRunner, type PageChangedEvent,
} from '../src/index.js';
import { AccessToken, CloudID, ConfluencePageSite } from './confluence_page_fake.js';

const connection = { provider: 'atlassian', name: 'confluence-trigger-test' };
const credentials = { oauthClientId: 'client-id', oauthClientSecret: new SecretString('secret'), accessToken: new SecretString(AccessToken), refreshToken: new SecretString('r') };
const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials };

class Clock { now = Date.UTC(2026, 10, 2, 9, 0, 0); advance(milliseconds: number): void { this.now += milliseconds; } }
function newClient(site: ConfluencePageSite, clock?: Clock, cloudId = ''): ConfluenceClient {
  return new ConfluenceClient({ cloudId, endpoint: 'http://127.0.0.1:12345' }, source, { send: site.send, ...(clock ? { now: () => new Date(clock.now) } : {}) });
}

describe('PageChangeFetcher', () => {
  it('skips the baseline and delivers each new version once, oldest first', async () => {
    const clock = new Clock();
    const site = new ConfluencePageSite(CloudID, () => clock.now);
    const fetcher = new PageChangeFetcher(newClient(site, clock), connection, { spaceKey: 'OPS' });
    const existing = site.publish('OPS', 'Existing policy', 'Before the Trigger started');
    const context = newTriggerContext();
    const cursor = await fetcher.seedCursor(context);
    expect(cursor.snapshot?.entries.map(entry => entry.key)).toEqual([existing]);
    let page = await fetcher.fetchChanges(context, cursor);
    expect(page.changes).toEqual([]);
    clock.advance(30_000);
    const created = site.publish('OPS', 'Remote work policy', 'Two days a week');
    clock.advance(1_000);
    site.edit(existing, 'Existing policy', 'Revised');
    site.publish('HR', 'Other space', 'Ignored');
    page = await fetcher.fetchChanges(context, page.next);
    expect(page.changes.map(change => [change.payload.page.id, change.payload.changeKind])).toEqual([[created, PageChangeCreated], [existing, PageChangeUpdated]]);
    expect(page.changes[0]!.payload.cloudId, 'the site comes from accessible resources').toBe(CloudID);
    expect(page.changes[0]!.payload.page.webUrl).toBe(`https://ops.atlassian.net/wiki/spaces/OPS/pages/${created}`);
    expect((await fetcher.fetchChanges(context, page.next)).changes, 'a delivered version is never delivered again').toEqual([]);
    expect(site.cqlQueries.every(cql => cql.includes('space = "OPS"'))).toBe(true);
  });

  it('spans a long scan over polls and restarts it after an expired search cursor', async () => {
    const clock = new Clock();
    const site = new ConfluencePageSite(CloudID, () => clock.now);
    const fetcher = new PageChangeFetcher(newClient(site, clock, CloudID), connection, {});
    const context = newTriggerContext();
    const cursor = await fetcher.seedCursor(context);
    for (let index = 0; index < 150; index++) { clock.advance(10); site.publish('OPS', `Page ${index}`, 'body'); }
    const page = await fetcher.fetchChanges(context, cursor);
    expect([page.changes.length, page.hasMorePages]).toEqual([100, true]);
    expect(JSON.parse(page.next.token).scan.searchCursor).toBeTruthy();
    site.expireSearchCursors();
    const restarted = await fetcher.fetchChanges(context, page.next);
    expect([restarted.changes.length, restarted.hasMorePages, JSON.parse(restarted.next.token).scan.searchCursor]).toEqual([0, true, undefined]);
    const first = await fetcher.fetchChanges(context, restarted.next);
    expect([first.changes.length, first.hasMorePages], 'the first 100 were already delivered').toEqual([0, true]);
    const rest = await fetcher.fetchChanges(context, first.next);
    expect([rest.changes.length, rest.hasMorePages ?? false]).toEqual([50, false]);
    expect(new Set([...page.changes, ...rest.changes].map(change => change.id)).size).toBe(150);
  });

  it('pauses on a 429 and reseeds a cursor for another space or site', async () => {
    const clock = new Clock();
    const site = new ConfluencePageSite(CloudID, () => clock.now);
    const client = newClient(site, clock, CloudID);
    const context = newTriggerContext();
    const cursor = await new PageChangeFetcher(client, connection, { spaceKey: 'OPS' }).seedCursor(context);
    site.throttleSearches(1, 7);
    const limited = await new PageChangeFetcher(client, connection, { spaceKey: 'OPS' }).fetchChanges(context, cursor).catch((error: unknown) => error);
    expect(limited).toBeInstanceOf(RateLimitedError);
    expect((limited as RateLimitedError).retryAfterMs).toBe(7_000);
    expect(isCursorInvalid(await new PageChangeFetcher(client, connection, { spaceKey: 'HR' }).fetchChanges(context, cursor).catch((error: unknown) => error))).toBe(true);
    const otherSite = newClient(new ConfluencePageSite('aaaaaaaa-a1b2-4b33-8c44-def123456789'), clock);
    expect(isCursorInvalid(await new PageChangeFetcher(otherSite, connection, { spaceKey: 'OPS' }).fetchChanges(context, cursor).catch((error: unknown) => error))).toBe(true);
  });
});

describe('pageChanged configuration and wiring', () => {
  it('validates the configuration, the payload, and pollInterval', () => {
    expect(PageChangedTriggerConfigurationSchema.parse({ spaceId: '98306', spaceKey: 'OPS', spaceName: 'Ops' })).toEqual({ spaceId: '98306', spaceKey: 'OPS', spaceName: 'Ops' });
    expect(PageChangedTriggerConfigurationSchema.safeParse({ spaceKey: '~5b10ac8d82e05b22cc7d4ef5' }).success).toBe(true);
    expect(PageChangedTriggerConfigurationSchema.safeParse({ spaceKey: ' OPS' }).success).toBe(false);
    expect(PageChangedTriggerConfigurationSchema.safeParse({ spaceKey: 'OPS', cql: 'type = page' }).success).toBe(false);
    expect(() => decodePageChangedEventPayload('', new TextEncoder().encode('{}'))).toThrow();
    expect(() => new ConfluenceClient({ pollIntervalMs: 5_000 }, source)).toThrow('at least 10s');
    expect(new ConfluenceClient({ pollIntervalMs: 0 }, source).pollIntervalMs).toBe(60_000);
    const binding = definePageChangedTriggerBinding({ connectionName: 'confluence-page-watcher', bindingName: 'changes', configurationUI: { units: [] } });
    expect([binding.definition, binding.connectionName, binding.bindingName]).toEqual([PageChangedTriggerDefinition, 'confluence-page-watcher', 'changes']);
  });

  it('runs one binding through a runner and a single Trigger, and rejects bad wiring', async () => {
    const site = new ConfluencePageSite();
    const trigger = newConnection(newClient(site, undefined, CloudID), connection);
    const events: TriggerEvent<PageChangedEvent>[] = [];
    const target = { handleTrigger: async (_context: unknown, event: TriggerEvent<PageChangedEvent>) => { events.push(event); } };
    site.publish('OPS', 'Existing', 'body');
    const cursor = newMemoryCursorStore();
    const runner = newPageTriggerRunner({ connection: trigger, pageChangedRoutes: [{ bindingName: 'changes', configuration: { spaceKey: 'OPS' }, target, cursor }] });
    const stop = new AbortController();
    const running = runner.run(newTriggerContext(stop.signal));
    await expect.poll(async () => (await cursor.readPollCursor()).state !== undefined, { timeout: 10_000 }).toBe(true);
    await new Promise(resolve => setTimeout(resolve, 2));
    const created = site.publish('OPS', 'Created after the baseline', 'body');
    runner.wakePoller('changes');
    await expect.poll(() => events.map(event => event.payload.page.id), { timeout: 10_000 }).toEqual([created]);
    stop.abort();
    await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);

    const single = newPageChangedTrigger({ connection: trigger, bindingName: 'changes', configuration: {}, target });
    const stopSingle = new AbortController();
    const searchesBefore = site.cqlQueries.length;
    const singleRun = single.run(newTriggerContext(stopSingle.signal));
    await expect.poll(() => site.cqlQueries.length, { timeout: 10_000 }).toBeGreaterThan(searchesBefore);
    stopSingle.abort();
    await expect(singleRun).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
    expect(site.cqlQueries.at(-1)).toMatch(/^type = page AND lastmodified >= /);
    expect(() => newPageTriggerRunner({ connection: trigger, pageChangedRoutes: [] })).toThrow('routes are required');
    expect(() => newPageChangedTrigger({ connection: trigger, bindingName: 'changes', configuration: { spaceKey: 'bad key' }, target })).toThrow('space key');
  }, 60_000);
});
