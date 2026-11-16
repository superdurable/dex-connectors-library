// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { loadFromEnvironment, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { describe, expect, it } from 'vitest';
import {
  XeroClient, defineInvoiceChangedTriggerBinding, invoiceChangeEventId, invoiceChangeFilterKey, newConnection, newInvoiceChangedTrigger, newInvoiceTriggerRunner,
  newProjectConnection, newProjectInvoiceTriggerRunner, parseInvoiceChangedTriggerConfiguration, type InvoiceChangedEvent,
} from '../src/index.js';
import { GeneratedMock } from '../src/mock.js';
import { XeroInvoiceFake } from './invoice_fake.js';

const reference = { provider: 'xero', name: 'xero-invoice-watcher' };
const invoiceB = '8a1d0e2f-0000-4000-8000-00000000000b';
const credentials = { authMethodId: 'custom-connection', clientId: '0F1E2D3C4B5A69788796A5B4C3D2E1F0', clientSecret: new SecretString('inert'), accessToken: new SecretString('inert-access') };

/** Consume each event into a list a test waits on. */
class ListTarget implements TriggerTarget<InvoiceChangedEvent> {
  readonly events: TriggerEvent<InvoiceChangedEvent>[] = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<InvoiceChangedEvent>): Promise<void> { this.events.push(event); }
}

function fakeClient(fake: XeroInvoiceFake, pollIntervalMs = 30_000): XeroClient {
  return new XeroClient({ pollIntervalMs }, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }, { send: fake.send, localProviderURL: 'http://127.0.0.1:1' });
}

async function eventually(condition: () => boolean | Promise<boolean>, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await condition())) {
    if (Date.now() > deadline) throw new Error('condition did not converge');
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}

describe('invoiceChanged configuration and wiring', () => {
  it('validates the filter as Go does', () => {
    expect(parseInvoiceChangedTriggerConfiguration({})).toEqual({});
    expect(parseInvoiceChangedTriggerConfiguration({ type: null, statuses: null })).toEqual({});
    expect(parseInvoiceChangedTriggerConfiguration({ type: 'ACCPAY', statuses: ['PAID'] })).toEqual({ type: 'ACCPAY', statuses: ['PAID'] });
    expect(() => parseInvoiceChangedTriggerConfiguration({ contactId: 'x' })).toThrow();
    expect(() => parseInvoiceChangedTriggerConfiguration({ type: 'SALES' })).toThrow(/type must be ACCREC/);
    expect(() => parseInvoiceChangedTriggerConfiguration({ statuses: ['OPEN'] })).toThrow(/statuses must contain/);
    expect(() => parseInvoiceChangedTriggerConfiguration({ statuses: ['PAID', 'PAID'] })).toThrow(/more than once/);
    expect(invoiceChangeFilterKey({ type: 'ACCREC', statuses: ['PAID', 'AUTHORISED'] })).toBe('type=ACCREC;statuses=AUTHORISED,PAID');
    expect(defineInvoiceChangedTriggerBinding({ connectionName: reference.name, bindingName: 'invoice-changes' }).definition.trigger.triggerName).toBe('invoiceChanged');
  });

  it('rejects a poll interval under 30 seconds and keeps the five-minute default for zero, as Go New does', () => {
    expect(() => fakeClient(new XeroInvoiceFake(), 29_999)).toThrow(/at least 30s/);
    expect(fakeClient(new XeroInvoiceFake(), 0).pollIntervalMs).toBe(300_000);
  });

  it('rejects incomplete routes, invalid filters, and mock connections', () => {
    const connection = newConnection(fakeClient(new XeroInvoiceFake()), reference);
    const target = new ListTarget();
    for (const routes of [[], [{ bindingName: '', target }], [{ bindingName: 'invoices' } as never], [{ bindingName: 'invoices', target, configuration: { type: 'X' } }]]) {
      expect(() => newInvoiceTriggerRunner({ connection, invoiceChangedRoutes: routes })).toThrow();
    }
    expect(() => newInvoiceTriggerRunner({ connection: new GeneratedMock(reference.name).connection(), invoiceChangedRoutes: [{ bindingName: 'invoices', target }] })).toThrow(/mock/);
  });

  it('hashes the lower-case invoice ID and UpdatedDateUTC milliseconds into the event ID as Go does', () => {
    const updatedAt = Date.parse('2026-10-01T09:00:02.5Z');
    expect(invoiceChangeEventId(invoiceB, updatedAt)).toBe('xero-invoice-2320ceb3271e677af3a09fb91ca96e10');
    expect(invoiceChangeEventId(invoiceB.toUpperCase(), updatedAt)).toBe('xero-invoice-2320ceb3271e677af3a09fb91ca96e10');
    expect(invoiceChangeEventId(invoiceB, updatedAt + 1)).not.toBe('xero-invoice-2320ceb3271e677af3a09fb91ca96e10');
  });
});

/** Write a private Dex Web connections file with the xero-invoice-watcher connection and optional binding filter. */
async function writeConnectionsFile(directory: string, pollInterval: unknown, filter?: Record<string, unknown>): Promise<string> {
  const path = join(directory, `connections-${String(pollInterval)}.json`);
  await writeFile(path, JSON.stringify({
    schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
    connections: [{ connectorId: 'xero', connectionName: reference.name, modulePath: 'github.com/superdurable/dex-connectors-library/connectors/xero/go', provider: 'xero', configuration: { pollInterval }, credentials: { auth_method: 'custom-connection', client_id: '0F1E2D3C4B5A69788796A5B4C3D2E1F0', client_secret: 'inert', access_token: 'inert-access' }, credentialExpiresAt: new Date(Date.now() + 3_600_000).toISOString() }],
    ...(filter ? { triggerBindings: [{ connectorId: 'xero', connectionName: reference.name, triggerName: 'invoiceChanged', bindingName: 'invoices', configuration: filter }] } : {}),
  }));
  await chmod(path, 0o600);
  return path;
}

describe('invoiceChanged runners', () => {
  it('decodes pollInterval as Go decodes a time.Duration: integer nanoseconds, 0 keeping the default', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'xero-poll-interval-test-'));
    const open = async (pollInterval: unknown): Promise<number> => {
      const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: await writeConnectionsFile(directory, pollInterval) });
      return (newProjectConnection(project, reference.name, { localProviderURL: 'http://127.0.0.1:1' }).operations as XeroClient).pollIntervalMs;
    };
    try {
      expect(await open(0)).toBe(300_000);
      expect(await open(60_000_000_000)).toBe(60_000);
      await expect(open(10_000_000_000)).rejects.toThrow(/at least 30s/);
      await expect(open('5m')).rejects.toThrow(/integer nanoseconds/);
      await expect(open(-5_000_000_000)).rejects.toThrow(/negative/);
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  });

  it('applies the saved filter, records the change in the inbox, and commits the cursor', async () => {
    const fake = new XeroInvoiceFake();
    fake.upsert('8a1d0e2f-0000-4000-8000-00000000000a', 'BILL-OLD', 'ACCPAY', 'PAID', '1.00');
    fake.advanceClock(60_000);
    const directory = await mkdtemp(join(tmpdir(), 'xero-trigger-test-'));
    const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: await writeConnectionsFile(directory, 30_000_000_000, { type: 'ACCPAY' }) });
    const target = new ListTarget();
    const stop = new AbortController();
    try {
      const connection = newProjectConnection(project, reference.name, { send: fake.send, localProviderURL: 'http://127.0.0.1:1' });
      const runner = newProjectInvoiceTriggerRunner(project, connection, { invoiceChangedRoutes: [{ bindingName: 'invoices', target }] });
      const key: TriggerInboxKey = { connectorId: 'xero', connectionName: reference.name, triggerName: 'invoiceChanged', bindingName: 'invoices' };
      const cursor = project.pollCursorStore(key);
      const committedToken = async (): Promise<string> => (await cursor.readPollCursor()).state?.token ?? '';
      const running = runner.run(newTriggerContext(stop.signal));
      await eventually(async () => (await committedToken()) !== '');
      const baseline = await committedToken();
      expect(JSON.parse(baseline).filter).toBe('type=ACCPAY;statuses=');
      fake.upsert('8a1d0e2f-0000-4000-8000-00000000000c', 'INV-1', 'ACCREC', 'DRAFT', '2.00');
      fake.upsert(invoiceB, 'BILL-NEW', 'ACCPAY', 'AUTHORISED', '3.00');
      runner.wakePoller('invoices');
      await eventually(() => target.events.length > 0);
      expect(target.events.map(event => event.payload.invoice.invoiceNumber)).toEqual(['BILL-NEW']);
      expect(fake.listRequests().at(-1)?.query.get('where')).toBe('Type=="ACCPAY"');
      await eventually(async () => (await committedToken()) !== baseline);
      await eventually(async () => (await project.triggerInbox(key).pending()).length === 0);
      expect(target.events).toHaveLength(1);
      stop.abort();
      await expect(running).rejects.toBeInstanceOf(TriggerDeliveryAbortedError);
    } finally {
      stop.abort();
      await rm(directory, { recursive: true, force: true });
    }
  }, 30_000);

  it('the in-memory Trigger records a baseline with the binding filter on start', async () => {
    const fake = new XeroInvoiceFake();
    const connection = newConnection(fakeClient(fake), reference);
    const target = new ListTarget();
    expect(() => newInvoiceChangedTrigger({ connection, connectionName: 'other', bindingName: 'invoices', target })).toThrow(/connectionName/);
    const trigger = newInvoiceChangedTrigger({ connection, connectionName: reference.name, bindingName: 'invoices', target, configuration: { type: 'ACCREC' } });
    const stop = new AbortController();
    const running = trigger.run(newTriggerContext(stop.signal)).catch((error: unknown) => error);
    try {
      await eventually(() => fake.listRequests().length > 0);
      const [seed] = fake.listRequests();
      expect([seed?.query.get('order'), seed?.query.get('where'), seed?.modifiedSince]).toEqual(['UpdatedDateUTC DESC', 'Type=="ACCREC"', '']);
    } finally {
      stop.abort();
      expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError);
    }
  });
});
