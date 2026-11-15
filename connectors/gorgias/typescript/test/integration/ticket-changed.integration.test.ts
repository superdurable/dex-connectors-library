// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache, type BlobCache } from '@superdurable/dex';
import { SecretString, TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { InMemoryObjectStore, TriggerInbox, newDurableTriggerTarget, newPollCursorStore, type Scope, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { afterEach, expect, it } from 'vitest';
import { GorgiasClient, decodeTicketChangedEventPayload, newConnection, newTicketTriggerRunner, type Connection, type TicketChangedEvent } from '../../src/index.js';
import { ConnectionName, TicketChangedFlow, TicketChangedTriggerBinding, acceptEveryTicketChange, mapToFlowInput, resolveFlowId, type TicketChangeOutcome } from '../../examples/ticket-changed/flow/workflow.js';
import { GorgiasEventsFake, fakeAPIKey, fakeEmail } from '../gorgias_events_fake.js';

const scope: Scope = { projectId: 'gorgias-ticket-changed', kind: 'live' };
const inboxKey: TriggerInboxKey = { connectorId: 'gorgias', connectionName: ConnectionName, triggerName: 'ticketChanged', bindingName: TicketChangedTriggerBinding };
const cleanups: (() => Promise<void>)[] = [];

afterEach(async () => { for (const cleanup of cleanups.splice(0).reverse()) await cleanup(); });

async function eventually(condition: () => boolean | Promise<boolean>, timeoutMs = 45_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await condition())) {
    if (Date.now() > deadline) throw new Error('condition did not converge');
    await new Promise(resolve => setTimeout(resolve, 50));
  }
}

/** Starts the example's Flow, then remembers each delivered event once, in delivery order. */
class CountingTarget implements TriggerTarget<TicketChangedEvent> {
  readonly delivered = new Map<string, TriggerEvent<TicketChangedEvent>>();
  readonly #start: TriggerTarget<TicketChangedEvent>;
  constructor(client: Client, flow: TicketChangedFlow) { this.#start = newDexFlowTriggerTarget(client, flow, acceptEveryTicketChange, resolveFlowId, mapToFlowInput); }
  async handleTrigger(context: TriggerContext, event: TriggerEvent<TicketChangedEvent>): Promise<void> {
    await this.#start.handleTrigger(context, event);
    if (!this.delivered.has(event.id)) this.delivered.set(event.id, event);
  }
}

/** One run of the application's Trigger side; stopping it models a process exit. */
interface TriggerProcess { runner: Runner; stop(): Promise<void> }

/** A Dex Worker and Client for the example Flow, the helpdesk fake, and storage that outlives processes. */
class TicketChangedEnvironment {
  readonly objects = new InMemoryObjectStore();
  // Unique ticket and event IDs keep Flow IDs unique across runs against one Dex server.
  readonly idBase = (Date.now() % 1_000_000_000) * 10;
  readonly target: CountingTarget;
  constructor(readonly client: Client, readonly flow: TicketChangedFlow, readonly connection: Connection, readonly provider: GorgiasEventsFake) {
    this.target = new CountingTarget(client, flow);
    provider.nextEventId = this.idBase;
  }

  ticketId(offset: number): number { return this.idBase + offset; }
  cursorStore() { return newPollCursorStore(this.objects, scope, inboxKey); }
  async cursorState() { return (await this.cursorStore().readPollCursor()).state; }

  /** Wire the counting target behind the durable inbox and cursor, as the project runner does. */
  startTriggerProcess(): TriggerProcess {
    const target = newDurableTriggerTarget(new TriggerInbox(this.objects, scope, inboxKey), inboxKey, this.target, { decodePayload: decodeTicketChangedEventPayload });
    const runner = newTicketTriggerRunner({ connection: this.connection, ticketChangedRoutes: [{ bindingName: TicketChangedTriggerBinding, target, cursor: this.cursorStore() }] });
    const abort = new AbortController();
    const running = runner.run(newTriggerContext(abort.signal)).then(() => undefined, (error: unknown) => error);
    const stop = async () => { abort.abort(); expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError); };
    cleanups.push(async () => { abort.abort(); await running; });
    return { runner, stop };
  }

  /** Wait for count deliveries, then each event's Flow, keyed by Gorgias event ID. */
  async waitForOutcomes(count: number): Promise<Map<number, TicketChangeOutcome>> {
    await eventually(() => this.target.delivered.size >= count);
    const outcomes = new Map<number, TicketChangeOutcome>();
    for (const event of this.target.delivered.values()) {
      const result = await this.client.waitForFlow(resolveFlowId(event), 30_000);
      expect(result.status, result.errorMessage).toBe('completed');
      const outcome = result.singleOutput<TicketChangeOutcome>();
      expect(outcome.ticketId).toBe(event.payload.ticketId);
      outcomes.set(event.payload.eventId, outcome);
    }
    return outcomes;
  }
}

async function newEnvironment(): Promise<TicketChangedEnvironment> {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const directory = await mkdtemp(join(tmpdir(), 'dex-gorgias-ticket-changed-'));
  const cache: BlobCache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new GorgiasEventsFake();
  const gorgias = new GorgiasClient({ domain: 'acme', pollIntervalMs: 10_000 }, { resolve: async () => ({ email: fakeEmail, apiKey: new SecretString(fakeAPIKey) }) }, { send: provider.send });
  const connection = newConnection(gorgias, { provider: 'gorgias', name: ConnectionName });
  const flow = new TicketChangedFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  cleanups.push(async () => { await client.close(); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true }); });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000 });
  return new TicketChangedEnvironment(client, flow, connection, provider);
}

it('starts one Flow per ticket event across a restart, never delivering the baseline', async () => {
  const environment = await newEnvironment();
  const provider = environment.provider;
  const [existing, created, offline] = [1, 2, 3].map(offset => environment.ticketId(offset)) as [number, number, number];
  provider.setTicket(existing, 'Existing', 'open');
  provider.recordTicketEvent('ticket-created', existing);
  const first = environment.startTriggerProcess();
  await eventually(async () => (await environment.cursorState()) !== undefined);

  provider.setTicket(created, 'Double charge on order 88213', 'open', 'billing');
  const createdEvent = provider.recordTicketEvent('ticket-created', created);
  provider.setTicket(existing, 'Existing', 'closed');
  const closedEvent = provider.recordTicketEvent('ticket-closed', existing, 12);
  first.runner.wakePoller(TicketChangedTriggerBinding);
  let outcomes = await environment.waitForOutcomes(2);
  expect(outcomes.get(createdEvent)?.eventType).toBe('ticket-created');
  // The Flow reads the ticket back with the customer and messages the event omits.
  expect(outcomes.get(createdEvent)?.current.latestMessages?.[0]?.body).toBe('Customer message for Double charge on order 88213');
  expect(outcomes.get(createdEvent)?.current.ticket.tags).toEqual(['billing']);
  expect(outcomes.get(createdEvent)?.current.requester?.email).toBe('ada@example.com');
  expect(outcomes.get(closedEvent)?.eventType).toBe('ticket-closed');
  expect(outcomes.get(closedEvent)?.current.ticket.status).toBe('closed');
  await first.stop();

  provider.setTicket(offline, 'Opened while stopped', 'open');
  const offlineEvent = provider.recordTicketEvent('ticket-created', offline);
  const reopenedEvent = provider.recordTicketEvent('ticket-reopened', existing, 12);
  const second = environment.startTriggerProcess();
  second.runner.wakePoller(TicketChangedTriggerBinding);
  outcomes = await environment.waitForOutcomes(4);
  expect(outcomes.get(offlineEvent)?.current.ticket.subject).toBe('Opened while stopped');
  expect(outcomes.get(reopenedEvent)?.eventType).toBe('ticket-reopened');
  const requestCount = provider.eventRequestCount();
  second.runner.wakePoller(TicketChangedTriggerBinding);
  await eventually(() => provider.eventRequestCount() > requestCount);
  const inbox = new TriggerInbox(environment.objects, scope, inboxKey);
  await eventually(async () => (await inbox.pending()).length === 0);
  // No event is delivered twice and the baseline is never delivered.
  expect(environment.target.delivered.size).toBe(4);
  await second.stop();
}, 120_000);

it('waits out a 429 and retries a rejected poll from the same cursor without a new baseline', async () => {
  const environment = await newEnvironment();
  const provider = environment.provider;
  const [throttled, rejected] = [1, 2].map(offset => environment.ticketId(offset)) as [number, number];
  const process = environment.startTriggerProcess();
  await eventually(async () => (await environment.cursorState()) !== undefined);

  provider.failNextEvents(429, '1', '{"error":{"msg":"too_many_requests"}}');
  provider.setTicket(throttled, 'Created during a rate limit', 'open');
  const throttledEvent = provider.recordTicketEvent('ticket-created', throttled);
  process.runner.wakePoller(TicketChangedTriggerBinding);
  let outcomes = await environment.waitForOutcomes(1);
  expect(outcomes.get(throttledEvent)?.ticketId).toBe(throttled);
  await process.stop();

  provider.setTicket(rejected, 'Recorded while Gorgias rejected a poll', 'open');
  const rejectedEvent = provider.recordTicketEvent('ticket-created', rejected);
  provider.failNextEvents(400, '', '{"error":{"msg":"invalid_query","data":{"created_datetime":["bad"]}}}');
  const restarted = environment.startTriggerProcess();
  restarted.runner.wakePoller(TicketChangedTriggerBinding);
  outcomes = await environment.waitForOutcomes(2);
  expect(outcomes.get(rejectedEvent)?.current.ticket.subject).toBe('Recorded while Gorgias rejected a poll');
  // A 400 never records a new baseline.
  expect((await environment.cursorState())?.resetCount ?? 0).toBe(0);
  await restarted.stop();
}, 120_000);
