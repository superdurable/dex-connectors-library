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
import {
  TicketChangeCreated, TicketChangeDeleted, TicketChangeUpdated, ZendeskClient, decodeTicketChangedEventPayload, newConnection, newTicketTriggerRunner,
  type Connection, type TicketChangedEvent, type TicketChangedTriggerConfiguration,
} from '../../src/index.js';
import { ConnectionName, TicketChangedFlow, TicketChangedTriggerBinding, acceptEveryTicketChange, mapToFlowInput, resolveFlowId, type TicketChangeOutcome } from '../../examples/ticket-changed/flow/workflow.js';
import { ZendeskExportFake, fakeAPIBaseURL, fakeAPIToken, fakeEmail } from '../zendesk_export_fake.js';

const scope: Scope = { projectId: 'zendesk-ticket-changed', kind: 'live' };
const inboxKey: TriggerInboxKey = { connectorId: 'zendesk-support', connectionName: ConnectionName, triggerName: 'ticketChanged', bindingName: TicketChangedTriggerBinding };
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

/** A Dex Worker and Client for the example Flow, the account fake, and storage that outlives processes. */
class TicketChangedEnvironment {
  readonly objects = new InMemoryObjectStore();
  // Unique ticket IDs keep Flow IDs unique across runs against one Dex server.
  readonly ticketIdBase = (Date.now() % 1_000_000_000) * 10;
  readonly target: CountingTarget;
  constructor(readonly client: Client, readonly flow: TicketChangedFlow, readonly connection: Connection, readonly provider: ZendeskExportFake, readonly configuration: TicketChangedTriggerConfiguration) {
    this.target = new CountingTarget(client, flow);
  }

  ticketId(offset: number): number { return this.ticketIdBase + offset; }
  cursorStore() { return newPollCursorStore(this.objects, scope, inboxKey); }
  async cursorState() { return (await this.cursorStore().readPollCursor()).state; }

  /** Wire the counting target behind the durable inbox and cursor, as the project runner does. */
  startTriggerProcess(): TriggerProcess {
    const target = newDurableTriggerTarget(new TriggerInbox(this.objects, scope, inboxKey), inboxKey, this.target, { decodePayload: decodeTicketChangedEventPayload });
    const runner = newTicketTriggerRunner({ connection: this.connection, ticketChangedRoutes: [{ bindingName: TicketChangedTriggerBinding, configuration: this.configuration, target, cursor: this.cursorStore() }] });
    const abort = new AbortController();
    const running = runner.run(newTriggerContext(abort.signal)).then(() => undefined, (error: unknown) => error);
    const stop = async () => { abort.abort(); expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError); };
    cleanups.push(async () => { abort.abort(); await running; });
    return { runner, stop };
  }

  /** Wait until the committed cursor ends at the fake's latest change. */
  async waitForCaughtUpCursor(): Promise<void> {
    await eventually(async () => ((await this.cursorState())?.token ?? '').includes(`"afterCursor":"${this.provider.cursor()}"`));
  }

  /** Report whether any change of ticketId was delivered. */
  isDelivered(ticketId: number): boolean { return [...this.target.delivered.values()].some(event => event.payload.ticket.id === ticketId); }

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

async function newEnvironment(configuration: TicketChangedTriggerConfiguration): Promise<TicketChangedEnvironment> {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const directory = await mkdtemp(join(tmpdir(), 'dex-zendesk-ticket-changed-'));
  const cache: BlobCache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ZendeskExportFake();
  const zendesk = new ZendeskClient({ subdomain: 'acme', pollIntervalMs: 5_000 }, { resolve: async () => ({ email: fakeEmail, apiToken: new SecretString(fakeAPIToken) }) },
    { apiBaseURL: fakeAPIBaseURL, send: provider.send, incrementalExportRequestsPerMinute: 600 });
  const connection = newConnection(zendesk, { provider: 'zendesk', name: ConnectionName });
  const flow = new TicketChangedFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  cleanups.push(async () => { await client.close(); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true }); });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000 });
  return new TicketChangedEnvironment(client, flow, connection, provider, configuration);
}

it('starts one Flow per ticket change across a restart after a 429, never delivering the baseline', async () => {
  const environment = await newEnvironment({ includeDeleted: true });
  const provider = environment.provider;
  const [existing, created, spam, offline] = [1, 2, 3, 4].map(offset => environment.ticketId(offset)) as [number, number, number, number];
  provider.create(existing, 'Existing ticket');
  provider.create(spam, 'Spam');
  const first = environment.startTriggerProcess();
  await environment.waitForCaughtUpCursor();

  provider.create(created, 'Double charge on order 88213');
  provider.update(existing, 'pending');
  provider.delete(spam);
  first.runner.wakePoller(TicketChangedTriggerBinding);
  let outcomes = await environment.waitForOutcomes(3);
  expect(outcomes.get(created)?.changeKind).toBe(TicketChangeCreated);
  expect(outcomes.get(created)?.current?.latestComments?.[0]?.body).toBe('Opened: Double charge on order 88213');
  expect(outcomes.get(existing)?.changeKind).toBe(TicketChangeUpdated);
  // The Flow reads the ticket back from Zendesk.
  expect(outcomes.get(existing)?.current?.ticket.status).toBe('pending');
  expect(outcomes.get(spam)?.changeKind).toBe(TicketChangeDeleted);
  expect(outcomes.get(spam)?.current, 'a deleted ticket is not read back').toBeUndefined();
  await first.stop();

  provider.create(offline, 'Opened while stopped');
  provider.update(created, 'solved');
  provider.rateLimitNextRequests(1, '1');
  const second = environment.startTriggerProcess();
  second.runner.wakePoller(TicketChangedTriggerBinding);
  outcomes = await environment.waitForOutcomes(5);
  expect(outcomes.get(offline)?.current?.ticket.subject).toBe('Opened while stopped');
  expect(outcomes.get(created)?.current?.ticket.status).toBe('solved');
  await environment.waitForCaughtUpCursor();
  // No change is delivered twice and the baseline is never delivered.
  expect(environment.target.delivered.size).toBe(5);
  expect(provider.requests().map(request => request.status)).toContain(429);
  await second.stop();
}, 120_000);

it('restarts a rejected cursor from its last change time, so a change in between still starts its Flow', async () => {
  const environment = await newEnvironment({});
  const provider = environment.provider;
  const [before, gap] = [1, 2].map(offset => environment.ticketId(offset)) as [number, number];
  provider.create(before, 'Before the restart');
  const first = environment.startTriggerProcess();
  await environment.waitForCaughtUpCursor();
  await first.stop();

  provider.invalidateCursors();
  provider.create(gap, 'Created after the cursor expired');
  const second = environment.startTriggerProcess();
  second.runner.wakePoller(TicketChangedTriggerBinding);
  await eventually(() => environment.isDelivered(gap));
  // The restart may repeat up to the last minute, so the baseline ticket is not asserted either way.
  const outcomes = await environment.waitForOutcomes(1);
  expect(outcomes.get(gap)?.current?.ticket.subject).toBe('Created after the cursor expired');
  await environment.waitForCaughtUpCursor();
  expect((await environment.cursorState())?.resetCount, 'a rejected cursor restarts by time instead of reseeding').toBe(0);
  expect(provider.requests().map(request => request.status)).toContain(422);
  await second.stop();
}, 120_000);
