// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm, writeFile, chmod } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { loadFromEnvironment, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { describe, expect, it } from 'vitest';
import {
  FreshdeskClient, TicketChangeCreated, TicketChangeUpdated, defineTicketChangedTriggerBinding, newConnection, newProjectConnection,
  newProjectTicketTriggerRunner, newTicketChangedTrigger, newTicketTriggerRunner, parseTicketChangedTriggerConfiguration, type TicketChangedEvent,
} from '../src/index.js';
import { GeneratedMock } from '../src/mock.js';
import { FreshdeskTicketFake } from './ticket_list_fake.js';

const apiKey = 'freshdeskTriggerKey0123456789';
const reference = { provider: 'freshdesk', name: 'freshdesk-trigger' };

/** Consume each event into a list a test waits on. */
class ListTarget implements TriggerTarget<TicketChangedEvent> {
  readonly events: TriggerEvent<TicketChangedEvent>[] = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<TicketChangedEvent>): Promise<void> { this.events.push(event); }
}

function fakeClient(fake: FreshdeskTicketFake, pollIntervalMs = 60_000): FreshdeskClient {
  return new FreshdeskClient({ domain: 'acme', pollIntervalMs }, { resolve: async () => ({ apiKey: new SecretString(apiKey) }) }, { send: fake.send });
}

async function eventually(condition: () => boolean | Promise<boolean>, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await condition())) {
    if (Date.now() > deadline) throw new Error('condition did not converge');
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}

describe('ticketChanged configuration and wiring', () => {
  it('validates change kinds and rejects unknown saved fields as Go does', () => {
    expect(parseTicketChangedTriggerConfiguration({})).toEqual({});
    expect(parseTicketChangedTriggerConfiguration({ changeKinds: [TicketChangeCreated, TicketChangeUpdated] }).changeKinds).toHaveLength(2);
    expect(() => parseTicketChangedTriggerConfiguration({ changeKinds: ['deleted'] })).toThrow(/created or updated/);
    expect(() => parseTicketChangedTriggerConfiguration({ changeKinds: ['created', 'created'] })).toThrow(/twice/);
    expect(() => parseTicketChangedTriggerConfiguration({ changeKind: ['created'] })).toThrow();
    expect(defineTicketChangedTriggerBinding({ connectionName: 'freshdesk-ticket-watcher', bindingName: 'helpdesk-ticket-changes' }).definition.trigger.triggerName).toBe('ticketChanged');
  });

  it('rejects a poll interval under ten seconds, as Go New does', () => {
    expect(() => fakeClient(new FreshdeskTicketFake(apiKey), 9_999)).toThrow(/pollInterval/);
  });

  it('rejects incomplete routes and mock connections', () => {
    const connection = newConnection(fakeClient(new FreshdeskTicketFake(apiKey)), reference);
    const target = new ListTarget();
    for (const routes of [
      [], [{ bindingName: '', target }], [{ bindingName: 'ticketChanges' } as never],
      [{ bindingName: 'ticketChanges', target }, { bindingName: 'ticketChanges', target }],
      [{ bindingName: 'ticketChanges', target, configuration: { changeKinds: ['merged'] } }],
    ]) expect(() => newTicketTriggerRunner({ connection, ticketChangedRoutes: routes })).toThrow();
    expect(() => newTicketTriggerRunner({ connection: new GeneratedMock(reference.name).connection(), ticketChangedRoutes: [{ bindingName: 'ticketChanges', target }] })).toThrow(/mock/);
  });
});

describe('ticketChanged runners', () => {
  it('loads the saved kinds, delivers the new ticket, skips the update, and drains the inbox', async () => {
    const fake = new FreshdeskTicketFake(apiKey);
    const nowMs = Math.floor(Date.now() / 1000) * 1000;
    fake.createAt(1001, 'Existing', nowMs - 3_600_000, nowMs - 600_000);
    const directory = await mkdtemp(join(tmpdir(), 'freshdesk-trigger-test-'));
    const path = join(directory, 'connections.json');
    await writeFile(path, JSON.stringify({
      schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
      connections: [{ connectorId: 'freshdesk', connectionName: reference.name, modulePath: 'github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go', provider: 'freshdesk', configuration: { domain: 'acme', pollInterval: 10_000_000_000 }, credentials: { api_key: apiKey } }],
      triggerBindings: [{ connectorId: 'freshdesk', connectionName: reference.name, triggerName: 'ticketChanged', bindingName: 'newTickets', configuration: { changeKinds: ['created'] } }],
    }));
    await chmod(path, 0o600);
    const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
    const target = new ListTarget();
    const stop = new AbortController();
    try {
      // The fake answers at the helpdesk's own https://acme.freshdesk.com/api/v2 base URL.
      const connection = newProjectConnection(project, reference.name, { send: fake.send });
      const runner = newProjectTicketTriggerRunner(project, connection, { ticketChangedRoutes: [{ bindingName: 'newTickets', target }] });
      const key: TriggerInboxKey = { connectorId: 'freshdesk', connectionName: reference.name, triggerName: 'ticketChanged', bindingName: 'newTickets' };
      const cursor = project.pollCursorStore(key);
      const committedToken = async (): Promise<string> => (await cursor.readPollCursor()).state?.token ?? '';
      const running = runner.run(newTriggerContext(stop.signal));
      await eventually(async () => (await committedToken()) !== '');
      const baseline = await committedToken();
      fake.update(1001, 'Existing, edited', 3);
      fake.create(1002, 'Double charge');
      runner.wakePoller('newTickets');
      await eventually(() => target.events.length > 0);
      expect(target.events[0]!.payload.ticket.id).toBe(1002);
      expect(target.events[0]!.payload.changeKind).toBe(TicketChangeCreated);
      await eventually(async () => (await committedToken()) !== baseline);
      await eventually(async () => (await project.triggerInbox(key).pending()).length === 0);
      expect(target.events).toHaveLength(1);
      stop.abort();
      await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
    } finally {
      stop.abort();
      await rm(directory, { recursive: true, force: true });
    }
  });

  it('the in-memory Trigger baselines and delivers the next change', async () => {
    const fake = new FreshdeskTicketFake(apiKey);
    const nowMs = Math.floor(Date.now() / 1000) * 1000;
    fake.createAt(1001, 'Existing', nowMs - 3_600_000, nowMs - 600_000);
    const connection = newConnection(fakeClient(fake, 10_000), reference);
    const target = new ListTarget();
    expect(() => newTicketChangedTrigger({ connection, connectionName: 'other', bindingName: 'allTickets', target })).toThrow(/connectionName/);
    const trigger = newTicketChangedTrigger({ connection, connectionName: reference.name, bindingName: 'allTickets', target });
    const stop = new AbortController();
    const running = trigger.run(newTriggerContext(stop.signal)).catch((error: unknown) => error);
    try {
      await eventually(() => fake.listRequestCount() > 0);
      fake.update(1001, 'Existing, escalated', 3);
      // The trigger polls again after one 10-second interval with jitter.
      await eventually(() => target.events.length > 0, 30_000);
      expect(target.events[0]!.payload.changeKind).toBe(TicketChangeUpdated);
      expect(target.events[0]!.payload.ticket.subject).toBe('Existing, escalated');
    } finally {
      stop.abort();
      expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError);
    }
  }, 45_000);
});

describe('project pollInterval', () => {
  async function projectWith(pollInterval: unknown) {
    const directory = await mkdtemp(join(tmpdir(), 'freshdesk-poll-interval-test-'));
    const path = join(directory, 'connections.json');
    await writeFile(path, JSON.stringify({
      schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
      connections: [{ connectorId: 'freshdesk', connectionName: reference.name, modulePath: 'github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go', provider: 'freshdesk', configuration: { domain: 'acme', pollInterval }, credentials: { api_key: apiKey } }],
    }));
    await chmod(path, 0o600);
    return { project: await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path }), cleanup: () => rm(directory, { recursive: true, force: true }) };
  }

  it('decodes integer nanoseconds as Go does, keeps the default for zero, and rejects duration text', async () => {
    for (const [pollInterval, expected] of [[30_000_000_000, 30_000], [0, 60_000]] as const) {
      const { project, cleanup } = await projectWith(pollInterval);
      try { expect((newProjectConnection(project, reference.name).operations as FreshdeskClient).pollIntervalMs).toBe(expected); }
      finally { await cleanup(); }
    }
    for (const pollInterval of ['30s', 1.5]) {
      const { project, cleanup } = await projectWith(pollInterval);
      try { expect(() => newProjectConnection(project, reference.name)).toThrow(/pollInterval/); }
      finally { await cleanup(); }
    }
  });
});
