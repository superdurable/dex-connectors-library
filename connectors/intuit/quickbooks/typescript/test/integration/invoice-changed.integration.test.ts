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
import { InvoiceChangeCreated, InvoiceChangeUpdated, QuickBooksClient, decodeInvoiceChangedEventPayload, newConnection, newInvoiceTriggerRunner, type Connection, type InvoiceChangedEvent } from '../../src/index.js';
import { ConnectionName, InvoiceChangedFlow, InvoiceChangedTriggerBinding, acceptEveryInvoiceChange, mapToFlowInput, resolveFlowId, type InvoiceChangeOutcome } from '../../examples/invoice-changed/flow/workflow.js';
import { QuickBooksInvoiceFake } from '../invoice_fake.js';

const scope: Scope = { projectId: 'quickbooks-invoice-changed', kind: 'live' };
const inboxKey: TriggerInboxKey = { connectorId: 'quickbooks', connectionName: ConnectionName, triggerName: 'invoiceChanged', bindingName: InvoiceChangedTriggerBinding };
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
class CountingTarget implements TriggerTarget<InvoiceChangedEvent> {
  readonly delivered = new Map<string, TriggerEvent<InvoiceChangedEvent>>();
  readonly #start: TriggerTarget<InvoiceChangedEvent>;
  constructor(client: Client, flow: InvoiceChangedFlow) { this.#start = newDexFlowTriggerTarget(client, flow, acceptEveryInvoiceChange, resolveFlowId, mapToFlowInput); }
  async handleTrigger(context: TriggerContext, event: TriggerEvent<InvoiceChangedEvent>): Promise<void> {
    await this.#start.handleTrigger(context, event);
    if (!this.delivered.has(event.id)) this.delivered.set(event.id, event);
  }
}

/** One run of the application's Trigger side; stopping it models a process exit. */
interface TriggerProcess { runner: Runner; stop(): Promise<void> }

/** A Dex Worker and Client for the example Flow, the invoice fake, and storage that outlives processes. */
class InvoiceChangedEnvironment {
  readonly objects = new InMemoryObjectStore();
  readonly target: CountingTarget;
  constructor(readonly client: Client, readonly flow: InvoiceChangedFlow, readonly connection: Connection, readonly provider: QuickBooksInvoiceFake) {
    this.target = new CountingTarget(client, flow);
  }

  cursorStore() { return newPollCursorStore(this.objects, scope, inboxKey); }
  async cursorState() { return (await this.cursorStore().readPollCursor()).state; }

  /** Wire the counting target behind the durable inbox and cursor, as the project runner does. */
  startTriggerProcess(): TriggerProcess {
    const target = newDurableTriggerTarget(new TriggerInbox(this.objects, scope, inboxKey), inboxKey, this.target, { decodePayload: decodeInvoiceChangedEventPayload });
    const runner = newInvoiceTriggerRunner({ connection: this.connection, invoiceChangedRoutes: [{ bindingName: InvoiceChangedTriggerBinding, target, cursor: this.cursorStore() }] });
    const abort = new AbortController();
    const running = runner.run(newTriggerContext(abort.signal)).then(() => undefined, (error: unknown) => error);
    const stop = async () => { abort.abort(); expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError); };
    cleanups.push(async () => { abort.abort(); await running; });
    return { runner, stop };
  }

  /** Wait for count deliveries, then each change's Flow; a later version replaces an earlier outcome. */
  async waitForOutcomes(count: number): Promise<Map<string, InvoiceChangeOutcome>> {
    await eventually(() => this.target.delivered.size >= count);
    const outcomes = new Map<string, InvoiceChangeOutcome>();
    for (const event of this.target.delivered.values()) {
      const result = await this.client.waitForFlow(resolveFlowId(event), 30_000);
      expect(result.status, result.errorMessage).toBe('completed');
      const outcome = result.singleOutput<InvoiceChangeOutcome>();
      expect(outcome.invoiceId).toBe(event.payload.invoice.invoiceId);
      outcomes.set(outcome.invoiceId, outcome);
    }
    return outcomes;
  }
}

async function newEnvironment(): Promise<InvoiceChangedEnvironment> {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const directory = await mkdtemp(join(tmpdir(), 'dex-quickbooks-invoice-changed-'));
  const cache: BlobCache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  // A fresh company keeps Trigger event IDs, and so Flow IDs, unique across runs against one Dex server.
  const realmId = String(1_000_000_000_000_000 + Math.floor(Math.random() * 8_000_000_000_000_000));
  const provider = new QuickBooksInvoiceFake(realmId);
  const credentials = { clientId: 'test-client', clientSecret: new SecretString('test-secret'), accessToken: new SecretString('integration-access-token'), refreshToken: new SecretString('integration-refresh-token'), idToken: new SecretString('') };
  const quickBooks = new QuickBooksClient({ realmId, pollIntervalMs: 5_000 }, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }, { send: provider.send, localProviderURL: 'http://127.0.0.1:1' });
  const connection = newConnection(quickBooks, { provider: 'quickbooks', name: ConnectionName });
  const flow = new InvoiceChangedFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  cleanups.push(async () => { await client.close(); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true }); });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000 });
  return new InvoiceChangedEnvironment(client, flow, connection, provider);
}

it('starts one Flow per invoice version across a throttled restart, never delivering the baseline', async () => {
  const environment = await newEnvironment();
  const provider = environment.provider;
  const existing = provider.create('ORDER-1', '58', '100.00');
  const first = environment.startTriggerProcess();
  await eventually(async () => (await environment.cursorState()) !== undefined);

  const created = provider.create('ORDER-2', '58', '25.50');
  provider.pay(existing);
  first.runner.wakePoller(InvoiceChangedTriggerBinding);
  let outcomes = await environment.waitForOutcomes(2);
  expect(outcomes.get(created)).toMatchObject({ changeKind: InvoiceChangeCreated, isPaid: false, current: { balance: '25.50' } });
  // The Flow reads the invoice back, so it records the payment QuickBooks applied.
  expect(outcomes.get(existing)).toMatchObject({ changeKind: InvoiceChangeUpdated, isPaid: true, realmId: provider.realmId });
  await first.stop();

  const offline = provider.create('ORDER-3', '59', '40.00');
  provider.pay(created);
  // The restarted process's first poll is throttled; it waits for Retry-After and then delivers.
  provider.throttle(1, '1');
  const second = environment.startTriggerProcess();
  second.runner.wakePoller(InvoiceChangedTriggerBinding);
  outcomes = await environment.waitForOutcomes(4);
  expect(outcomes.get(offline)).toMatchObject({ changeKind: InvoiceChangeCreated, current: { docNumber: 'ORDER-3' } });
  expect(outcomes.get(created)).toMatchObject({ changeKind: InvoiceChangeUpdated, isPaid: true });
  const queryCount = provider.queries().length;
  second.runner.wakePoller(InvoiceChangedTriggerBinding);
  await eventually(() => provider.queries().length > queryCount);
  const inbox = new TriggerInbox(environment.objects, scope, inboxKey);
  await eventually(async () => (await inbox.pending()).length === 0);
  // No version is delivered twice and the baseline is never delivered.
  expect(environment.target.delivered.size).toBe(4);
  await second.stop();
}, 120_000);

it('reseeds after a rejected watermark without delivering the gap', async () => {
  const environment = await newEnvironment();
  const provider = environment.provider;
  provider.create('ORDER-1', '58', '10.00');
  const process = environment.startTriggerProcess();
  await eventually(async () => (await environment.cursorState()) !== undefined);
  await process.stop();

  const gap = provider.create('LOST-IN-GAP', '58', '11.00');
  provider.rejectWatermarkQueries(true);
  const restarted = environment.startTriggerProcess();
  restarted.runner.wakePoller(InvoiceChangedTriggerBinding);
  await eventually(async () => (await environment.cursorState())?.resetCount === 1);
  provider.rejectWatermarkQueries(false);
  const afterReset = provider.create('AFTER-RESET', '58', '12.00');
  restarted.runner.wakePoller(InvoiceChangedTriggerBinding);
  const outcomes = await environment.waitForOutcomes(1);
  expect(outcomes.get(afterReset)?.current.docNumber).toBe('AFTER-RESET');
  // Changes between the rejected cursor and the new baseline are not delivered.
  expect(outcomes.has(gap)).toBe(false);
  await restarted.stop();
}, 120_000);
