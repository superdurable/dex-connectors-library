// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { loadFromEnvironment, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { describe, expect, it } from 'vitest';
import {
  GorgiasClient, decodeTicketChangedEventPayload, defineTicketChangedTriggerBinding, newConnection, newProjectConnection, newProjectTicketTriggerRunner,
  newTicketChangedTrigger, newTicketTriggerRunner, parseTicketChangedTriggerConfiguration, type TicketChangedEvent,
} from '../src/index.js';
import { GeneratedMock } from '../src/mock.js';
import { GorgiasEventsFake, fakeAPIKey, fakeEmail } from './gorgias_events_fake.js';

const reference = { provider: 'gorgias', name: 'gorgias-ticket-watcher' };
const modulePath = 'github.com/superdurable/dex-connectors-library/connectors/gorgias/go';

/** Consume each event into a list a test waits on. */
class ListTarget implements TriggerTarget<TicketChangedEvent> {
  readonly events: TriggerEvent<TicketChangedEvent>[] = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<TicketChangedEvent>): Promise<void> { this.events.push(event); }
}

function fakeClient(fake: GorgiasEventsFake, pollIntervalMs = 60_000): GorgiasClient {
  return new GorgiasClient({ domain: 'acme', pollIntervalMs }, { resolve: async () => ({ email: fakeEmail, apiKey: new SecretString(fakeAPIKey) }) }, { send: fake.send });
}

async function eventually(condition: () => boolean | Promise<boolean>, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await condition())) {
    if (Date.now() > deadline) throw new Error('condition did not converge');
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}

async function projectFile(configuration: Record<string, unknown>, triggerBindings: unknown[] = []) {
  const directory = await mkdtemp(join(tmpdir(), 'gorgias-trigger-test-'));
  const path = join(directory, 'connections.json');
  await writeFile(path, JSON.stringify({
    schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
    connections: [{ connectorId: 'gorgias', connectionName: reference.name, modulePath, provider: 'gorgias', configuration, credentials: { email: fakeEmail, api_key: fakeAPIKey } }],
    triggerBindings,
  }));
  await chmod(path, 0o600);
  return { project: await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path }), cleanup: () => rm(directory, { recursive: true, force: true }) };
}

describe('ticketChanged configuration and wiring', () => {
  it('validates event types and rejects unknown saved fields as Go does', () => {
    expect(parseTicketChangedTriggerConfiguration({})).toEqual({});
    expect(parseTicketChangedTriggerConfiguration({ eventTypes: ['ticket-created', 'ticket-closed'] }).eventTypes).toHaveLength(2);
    expect(() => parseTicketChangedTriggerConfiguration({ eventTypes: ['ticket-message-created'] })).toThrow(/"ticket-message-created" is not a ticket event type/);
    expect(() => parseTicketChangedTriggerConfiguration({ eventTypes: ['ticket-created', 'ticket-created'] })).toThrow(/lists "ticket-created" twice/);
    expect(() => parseTicketChangedTriggerConfiguration({ eventType: ['ticket-created'] })).toThrow();
    expect(defineTicketChangedTriggerBinding({ connectionName: reference.name, bindingName: 'helpdesk-ticket-events' }).definition.trigger.triggerName).toBe('ticketChanged');
  });

  it('rejects a poll interval under ten seconds, as Go New does', () => {
    expect(() => fakeClient(new GorgiasEventsFake(), 9_999)).toThrow(/pollInterval must be at least 10s/);
    expect(fakeClient(new GorgiasEventsFake(), 0).pollIntervalMs).toBe(60_000);
  });

  it('rejects incomplete routes and mock connections', () => {
    const connection = newConnection(fakeClient(new GorgiasEventsFake()), reference);
    const target = new ListTarget();
    for (const routes of [
      [], [{ bindingName: '', target }], [{ bindingName: 'ticketEvents' } as never],
      [{ bindingName: 'ticketEvents', target }, { bindingName: 'ticketEvents', target }],
      [{ bindingName: 'ticketEvents', target, configuration: { eventTypes: ['ticket-message-created'] } }],
    ]) expect(() => newTicketTriggerRunner({ connection, ticketChangedRoutes: routes })).toThrow();
    expect(() => newTicketTriggerRunner({ connection: new GeneratedMock(reference.name).connection(), ticketChangedRoutes: [{ bindingName: 'ticketEvents', target }] })).toThrow(/mock/);
  });

  it('decodes an inbox payload the Go connector recorded', () => {
    const payload = { eventId: 9003, eventType: 'ticket-created', ticketId: 7002, occurredAt: '2026-11-02T09:00:10.000001Z', userId: 12, context: 'abc' };
    expect(decodeTicketChangedEventPayload(JSON.stringify(payload))).toEqual(payload);
    expect(() => decodeTicketChangedEventPayload('[]')).toThrow();
  });
});

describe('ticketChanged runners', () => {
  it('loads the saved types, delivers only the closed event, and drains the inbox', async () => {
    const fake = new GorgiasEventsFake();
    fake.recordTicketEvent('ticket-created', 7001);
    const { project, cleanup } = await projectFile({ domain: 'acme' }, [
      { connectorId: 'gorgias', connectionName: reference.name, triggerName: 'ticketChanged', bindingName: 'closedTickets', configuration: { eventTypes: ['ticket-closed'] } },
    ]);
    const target = new ListTarget();
    const stop = new AbortController();
    try {
      const connection = newProjectConnection(project, reference.name, { send: fake.send });
      const runner = newProjectTicketTriggerRunner(project, connection, { ticketChangedRoutes: [{ bindingName: 'closedTickets', target }] });
      const key: TriggerInboxKey = { connectorId: 'gorgias', connectionName: reference.name, triggerName: 'ticketChanged', bindingName: 'closedTickets' };
      const cursor = project.pollCursorStore(key);
      const committedToken = async (): Promise<string> => (await cursor.readPollCursor()).state?.token ?? '';
      // The first poll is due after up to a tenth of the minute interval of jitter; wake it now.
      runner.wakePoller('closedTickets');
      const running = runner.run(newTriggerContext(stop.signal));
      await eventually(async () => (await committedToken()) !== '');
      const baseline = await committedToken();
      fake.recordTicketEvent('ticket-updated', 7001, 12);
      const closed = fake.recordTicketEvent('ticket-closed', 7001, 12);
      runner.wakePoller('closedTickets');
      await eventually(() => target.events.length > 0);
      expect(target.events[0]!.id).toBe(`gorgias-event-${closed}`);
      expect(target.events[0]!.payload).toMatchObject({ eventType: 'ticket-closed', ticketId: 7001, userId: 12 });
      await eventually(async () => (await committedToken()) !== baseline);
      await eventually(async () => (await project.triggerInbox(key).pending()).length === 0);
      expect(target.events).toHaveLength(1);
      stop.abort();
      await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
    } finally {
      stop.abort();
      await cleanup();
    }
  }, 30_000);

  it('the in-memory Trigger baselines and delivers the next event', async () => {
    const fake = new GorgiasEventsFake();
    fake.recordTicketEvent('ticket-created', 7001);
    const connection = newConnection(fakeClient(fake, 10_000), reference);
    const target = new ListTarget();
    expect(() => newTicketChangedTrigger({ connection, connectionName: 'other', bindingName: 'allTicketEvents', target })).toThrow(/connectionName/);
    const trigger = newTicketChangedTrigger({ connection, connectionName: reference.name, bindingName: 'allTicketEvents', target });
    const stop = new AbortController();
    const running = trigger.run(newTriggerContext(stop.signal)).catch((error: unknown) => error);
    try {
      await eventually(() => fake.eventRequestCount() > 0);
      const reopened = fake.recordTicketEvent('ticket-reopened', 7001, 12);
      // The trigger polls again after one 10-second interval with jitter.
      await eventually(() => target.events.length > 0, 30_000);
      expect(target.events[0]!.id).toBe(`gorgias-event-${reopened}`);
      expect(target.events[0]!.payload.eventType).toBe('ticket-reopened');
    } finally {
      stop.abort();
      expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError);
    }
  }, 45_000);
});

describe('project pollInterval', () => {
  it('decodes integer nanoseconds as Go does, keeps the default for zero, and rejects duration text', async () => {
    for (const [pollInterval, expected] of [[30_000_000_000, 30_000], [0, 60_000]] as const) {
      const { project, cleanup } = await projectFile({ domain: 'acme', pollInterval });
      try { expect((newProjectConnection(project, reference.name).operations as GorgiasClient).pollIntervalMs).toBe(expected); }
      finally { await cleanup(); }
    }
    for (const pollInterval of ['30s', 1.5]) {
      const { project, cleanup } = await projectFile({ domain: 'acme', pollInterval });
      try { expect(() => newProjectConnection(project, reference.name)).toThrow(/pollInterval/); }
      finally { await cleanup(); }
    }
  });
});
