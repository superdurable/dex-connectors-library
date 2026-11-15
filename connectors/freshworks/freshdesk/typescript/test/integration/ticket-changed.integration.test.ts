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
import { FreshdeskClient, TicketChangeCreated, TicketChangeUpdated, decodeTicketChangedEventPayload, newConnection, newTicketTriggerRunner, type Connection, type TicketChangedEvent } from '../../src/index.js';
import { ConnectionName, TicketChangedFlow, TicketChangedTriggerBinding, acceptEveryTicketChange, mapToFlowInput, resolveFlowId, type TicketChangeOutcome } from '../../examples/ticket-changed/flow/workflow.js';
import { FreshdeskTicketFake } from '../ticket_list_fake.js';

const apiKey = 'freshdeskIntegrationKey0123456789';
const scope: Scope = { projectId: 'freshdesk-ticket-changed', kind: 'live' };
const inboxKey: TriggerInboxKey = { connectorId: 'freshdesk', connectionName: ConnectionName, triggerName: 'ticketChanged', bindingName: TicketChangedTriggerBinding };
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

/** A Dex Worker and Client for the example Flow, the ticket fake, and storage that outlives processes. */
class TicketChangedEnvironment {
  readonly objects = new InMemoryObjectStore();
  // Unique ticket IDs keep Flow IDs unique across runs against one Dex server.
  readonly ticketIdBase = (Date.now() % 1_000_000_000) * 10;
  readonly target: CountingTarget;
  constructor(readonly client: Client, readonly flow: TicketChangedFlow, readonly connection: Connection, readonly provider: FreshdeskTicketFake) {
    this.target = new CountingTarget(client, flow);
  }

  ticketId(offset: number): number { return this.ticketIdBase + offset; }
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

  /** Wait for count deliveries, then each change's Flow; a later change replaces an earlier outcome. */
  async waitForOutcomes(count: number): Promise<Map<number, TicketChangeOutcome>> {
    await eventually(() => this.target.delivered.size >= count);
    const outcomes = new Map<number, TicketChangeOutcome>();
    for (const event of this.target.delivered.values()) {
      const result = await this.client.waitForFlow(resolveFlowId(event), 30_000);
      expect(result.status, result.errorMessage).toBe('completed');
      const outcome = result.singleOutput<TicketChangeOutcome>();
      expect(outcome.ticketId).toBe(event.payload.ticket.id);
      outcomes.set(outcome.ticketId, outcome);
    }
    return outcomes;
  }
}

async function newEnvironment(): Promise<TicketChangedEnvironment> {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const directory = await mkdtemp(join(tmpdir(), 'dex-freshdesk-ticket-changed-'));
  const cache: BlobCache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new FreshdeskTicketFake(apiKey);
  const freshdesk = new FreshdeskClient({ domain: 'acme', pollIntervalMs: 10_000 }, { resolve: async () => ({ apiKey: new SecretString(apiKey) }) }, { send: provider.send });
  const connection = newConnection(freshdesk, { provider: 'freshdesk', name: ConnectionName });
  const flow = new TicketChangedFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  cleanups.push(async () => { await client.close(); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true }); });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000 });
  return new TicketChangedEnvironment(client, flow, connection, provider);
}

it('starts one Flow per ticket change across a restart, never delivering the baseline', async () => {
  const environment = await newEnvironment();
  const provider = environment.provider;
  const [existing, created, offline] = [1, 2, 3].map(offset => environment.ticketId(offset)) as [number, number, number];
  const nowMs = Math.floor(Date.now() / 1000) * 1000;
  provider.createAt(existing, 'Existing', nowMs - 3_600_000, nowMs - 600_000);
  const first = environment.startTriggerProcess();
  await eventually(async () => (await environment.cursorState()) !== undefined);

  provider.create(created, 'Double charge on order 88213');
  provider.update(existing, 'Existing, escalated', 3);
  first.runner.wakePoller(TicketChangedTriggerBinding);
  let outcomes = await environment.waitForOutcomes(2);
  expect(outcomes.get(created)?.changeKind).toBe(TicketChangeCreated);
  // The Flow reads the ticket back with the text and requester the list omits.
  expect(outcomes.get(created)?.current.ticket.description).toBe('Customer message for Double charge on order 88213');
  expect(outcomes.get(created)?.current.requester?.email).toBe('jane@acme.example.com');
  expect(outcomes.get(existing)?.changeKind).toBe(TicketChangeUpdated);
  expect(outcomes.get(existing)?.current.ticket.status).toBe(3);
  await first.stop();

  provider.create(offline, 'Opened while stopped');
  provider.update(created, 'Double charge, refunded', 4);
  const second = environment.startTriggerProcess();
  second.runner.wakePoller(TicketChangedTriggerBinding);
  outcomes = await environment.waitForOutcomes(4);
  expect(outcomes.get(offline)?.current.ticket.subject).toBe('Opened while stopped');
  expect(outcomes.get(offline)?.changeKind).toBe(TicketChangeCreated);
  expect(outcomes.get(created)?.changeKind).toBe(TicketChangeUpdated);
  expect(outcomes.get(created)?.current.ticket.status).toBe(4);
  const listCount = provider.listRequestCount();
  second.runner.wakePoller(TicketChangedTriggerBinding);
  await eventually(() => provider.listRequestCount() > listCount);
  const inbox = new TriggerInbox(environment.objects, scope, inboxKey);
  await eventually(async () => (await inbox.pending()).length === 0);
  // No change is delivered twice and the baseline is never delivered.
  expect(environment.target.delivered.size).toBe(4);
  await second.stop();
}, 120_000);

it('waits out a 429 and reseeds after a rejected cursor without delivering the gap', async () => {
  const environment = await newEnvironment();
  const provider = environment.provider;
  const [throttled, gap, afterReset] = [1, 2, 3].map(offset => environment.ticketId(offset)) as [number, number, number];
  const process = environment.startTriggerProcess();
  await eventually(async () => (await environment.cursorState()) !== undefined);

  provider.failNextList(429, '1', '{"code":"too_many_requests"}');
  provider.create(throttled, 'Created during a rate limit');
  process.runner.wakePoller(TicketChangedTriggerBinding);
  let outcomes = await environment.waitForOutcomes(1);
  expect(outcomes.get(throttled)?.changeKind).toBe(TicketChangeCreated);
  await process.stop();

  provider.create(gap, 'Lost in the gap');
  provider.failNextList(400, '', '{"description":"Validation failed","errors":[{"field":"updated_since","code":"invalid_value"}]}');
  const restarted = environment.startTriggerProcess();
  restarted.runner.wakePoller(TicketChangedTriggerBinding);
  await eventually(async () => (await environment.cursorState())?.resetCount === 1);
  provider.create(afterReset, 'After the reset');
  restarted.runner.wakePoller(TicketChangedTriggerBinding);
  outcomes = await environment.waitForOutcomes(2);
  expect(outcomes.get(afterReset)?.current.ticket.subject).toBe('After the reset');
  // Changes between the rejected cursor and the new baseline are not delivered.
  expect(outcomes.has(gap)).toBe(false);
  await restarted.stop();
}, 120_000);
