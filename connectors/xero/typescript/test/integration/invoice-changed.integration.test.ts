// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache, type BlobCache } from '@superdurable/dex';
import { SecretString, TriggerDeliveryAbortedError, newDexFlowTriggerTarget, newTriggerContext, type TriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { InMemoryObjectStore, TriggerInbox, newDurableTriggerTarget, newPollCursorStore, type Scope, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { afterEach, expect, it } from 'vitest';
import { XeroClient, decodeInvoiceChangedEventPayload, newConnection, newInvoiceTriggerRunner, type Connection, type InvoiceChangedEvent, type InvoiceChangedTriggerConfiguration } from '../../src/index.js';
import { ConnectionName, InvoiceChangedFlow, InvoiceChangedTriggerBinding, acceptEveryInvoiceChange, mapToFlowInput, resolveFlowId, type InvoiceChangeOutcome } from '../../examples/invoice-changed/flow/workflow.js';
import { XeroInvoiceFake } from '../invoice_fake.js';

const scope: Scope = { projectId: 'xero-invoice-changed', kind: 'live' };
const inboxKey: TriggerInboxKey = { connectorId: 'xero', connectionName: ConnectionName, triggerName: 'invoiceChanged', bindingName: InvoiceChangedTriggerBinding };
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
  configuration: InvoiceChangedTriggerConfiguration = {};
  constructor(readonly client: Client, readonly flow: InvoiceChangedFlow, readonly connection: Connection, readonly provider: XeroInvoiceFake) {
    this.target = new CountingTarget(client, flow);
  }

  cursorStore() { return newPollCursorStore(this.objects, scope, inboxKey); }
  async cursorState() { return (await this.cursorStore().readPollCursor()).state; }

  /** Wire the counting target behind the durable inbox and cursor, as the project runner does. */
  startTriggerProcess(): TriggerProcess {
    const target = newDurableTriggerTarget(new TriggerInbox(this.objects, scope, inboxKey), inboxKey, this.target, { decodePayload: decodeInvoiceChangedEventPayload });
    const runner = newInvoiceTriggerRunner({ connection: this.connection, invoiceChangedRoutes: [{ bindingName: InvoiceChangedTriggerBinding, configuration: this.configuration, target, cursor: this.cursorStore() }] });
    const abort = new AbortController();
    const running = runner.run(newTriggerContext(abort.signal)).then(() => undefined, (error: unknown) => error);
    const stop = async () => { abort.abort(); expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError); };
    cleanups.push(async () => { abort.abort(); await running; });
    return { runner, stop };
  }

  /** Wait until the committed cursor's high-water mark reaches sinceMs. */
  async waitForCommittedSince(sinceMs: number): Promise<void> {
    await eventually(async () => { const state = await this.cursorState(); return state !== undefined && (JSON.parse(state.token) as { since: number }).since === sinceMs; });
  }

  /** Wait for count deliveries, then each change's Flow; a later change replaces an earlier outcome. */
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
  const directory = await mkdtemp(join(tmpdir(), 'dex-xero-invoice-changed-'));
  const cache: BlobCache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new XeroInvoiceFake();
  const credentials = { authMethodId: 'custom-connection', clientId: '0F1E2D3C4B5A69788796A5B4C3D2E1F0', clientSecret: new SecretString('inert'), accessToken: new SecretString('inert-xero-access-token') };
  const xero = new XeroClient({ pollIntervalMs: 30_000 }, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }, { send: provider.send, localProviderURL: 'http://127.0.0.1:1' });
  const connection = newConnection(xero, { provider: 'xero', name: ConnectionName });
  const flow = new InvoiceChangedFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  cleanups.push(async () => { await client.close(); await worker.close(); cache.close(); await rm(directory, { recursive: true, force: true }); });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000 });
  return new InvoiceChangedEnvironment(client, flow, connection, provider);
}

// Random invoice IDs keep Trigger event IDs, and so Flow IDs, unique across runs against one Dex server.
const newInvoiceID = (): string => randomUUID();

it('starts one Flow per change across a restart, never delivering the baseline', async () => {
  const environment = await newEnvironment();
  const provider = environment.provider;
  const existing = newInvoiceID(), draft = newInvoiceID();
  provider.upsert(existing, 'INV-0001', 'ACCREC', 'PAID', '49.95');
  provider.upsert(draft, 'INV-0002', 'ACCREC', 'DRAFT', '20.00');
  provider.advanceClock(60_000);
  const first = environment.startTriggerProcess();
  await eventually(async () => (await environment.cursorState()) !== undefined);

  const bill = newInvoiceID();
  provider.upsert(bill, 'BILL-77', 'ACCPAY', 'AUTHORISED', '120.00');
  provider.setStatus(draft, 'AUTHORISED');
  first.runner.wakePoller(InvoiceChangedTriggerBinding);
  let outcomes = await environment.waitForOutcomes(2);
  expect(outcomes.get(bill)?.current.type).toBe('ACCPAY');
  // The Flow reads the invoice back from Xero.
  expect(outcomes.get(draft)).toMatchObject({ deliveredStatus: 'AUTHORISED', current: { status: 'AUTHORISED' } });
  await first.stop();

  const offline = newInvoiceID();
  provider.upsert(offline, 'INV-0003', 'ACCREC', 'DRAFT', '5.00');
  const lastUpdate = provider.setStatus(draft, 'PAID');
  const second = environment.startTriggerProcess();
  second.runner.wakePoller(InvoiceChangedTriggerBinding);
  outcomes = await environment.waitForOutcomes(4);
  expect(outcomes.get(offline)?.current.invoiceNumber).toBe('INV-0003');
  expect(outcomes.get(draft)?.current.status).toBe('PAID');
  // The baseline is never delivered, and no change is delivered twice.
  expect(outcomes.has(existing)).toBe(false);
  await environment.waitForCommittedSince(lastUpdate);
  expect(environment.target.delivered.size).toBe(4);
  await second.stop();
}, 120_000);

it('records a new baseline for a changed filter without delivering the gap', async () => {
  const environment = await newEnvironment();
  const provider = environment.provider;
  const first = environment.startTriggerProcess();
  await eventually(async () => (await environment.cursorState()) !== undefined);
  await first.stop();

  const gap = newInvoiceID();
  provider.upsert(gap, 'INV-GAP', 'ACCREC', 'PAID', '9.00');
  provider.advanceClock(60_000);
  environment.configuration = { statuses: ['PAID'] };
  const second = environment.startTriggerProcess();
  await eventually(async () => (await environment.cursorState())?.resetCount === 1);
  const paid = newInvoiceID();
  provider.upsert(paid, 'INV-PAID', 'ACCREC', 'PAID', '12.00');
  provider.upsert(newInvoiceID(), 'INV-OPEN', 'ACCREC', 'AUTHORISED', '3.00');
  second.runner.wakePoller(InvoiceChangedTriggerBinding);
  const outcomes = await environment.waitForOutcomes(1);
  expect(outcomes.get(paid)?.current.status).toBe('PAID');
  // Changes between the old cursor and the new baseline are not delivered.
  expect([outcomes.has(gap), outcomes.size]).toEqual([false, 1]);
  await second.stop();
}, 120_000);

it('pauses for a minute-limit 429, then delivers the change with one Flow', async () => {
  const environment = await newEnvironment();
  const provider = environment.provider;
  const process = environment.startTriggerProcess();
  await eventually(async () => (await environment.cursorState()) !== undefined);
  const invoiceID = newInvoiceID();
  provider.upsert(invoiceID, 'INV-0429', 'ACCREC', 'AUTHORISED', '7.00');
  provider.rateLimitNext(1, 'minute', '1');
  process.runner.wakePoller(InvoiceChangedTriggerBinding);
  const outcomes = await environment.waitForOutcomes(1);
  expect(outcomes.get(invoiceID)?.current.invoiceNumber).toBe('INV-0429');
  expect(provider.requests().filter(request => request.status === 429)).toHaveLength(1);
  await process.stop();
}, 120_000);
