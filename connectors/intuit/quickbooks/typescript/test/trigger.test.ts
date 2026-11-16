// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { chmod, mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { loadFromEnvironment, type TriggerInboxKey } from '@superdurable/connectors-sdk/projectconfig';
import { describe, expect, it } from 'vitest';
import {
  InvoiceChangeCreated, InvoiceChangeUpdated, QuickBooksClient, defineInvoiceChangedTriggerBinding, invoiceChangeEventId, newConnection, newInvoiceChangedTrigger,
  newInvoiceTriggerRunner, newProjectConnection, newProjectInvoiceTriggerRunner, parseInvoiceChangedTriggerConfiguration, type InvoiceChangedEvent,
} from '../src/index.js';
import { GeneratedMock } from '../src/mock.js';
import { QuickBooksInvoiceFake } from './invoice_fake.js';

const realmId = '9341453050298464';
const reference = { provider: 'quickbooks', name: 'quickbooks-invoice-watcher' };

/** Consume each event into a list a test waits on. */
class ListTarget implements TriggerTarget<InvoiceChangedEvent> {
  readonly events: TriggerEvent<InvoiceChangedEvent>[] = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<InvoiceChangedEvent>): Promise<void> { this.events.push(event); }
}

function fakeClient(fake: QuickBooksInvoiceFake, pollIntervalMs = 5_000): QuickBooksClient {
  const credentials = { clientId: 'client', clientSecret: new SecretString('secret'), accessToken: new SecretString('access'), refreshToken: new SecretString('refresh'), idToken: new SecretString('') };
  return new QuickBooksClient({ realmId, pollIntervalMs }, { resolve: async () => credentials, resolveWithRefresh: async () => credentials }, { send: fake.send, localProviderURL: 'http://127.0.0.1:1' });
}

async function eventually(condition: () => boolean | Promise<boolean>, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await condition())) {
    if (Date.now() > deadline) throw new Error('condition did not converge');
    await new Promise(resolve => setTimeout(resolve, 10));
  }
}

describe('invoiceChanged configuration and wiring', () => {
  it('accepts the empty configuration and rejects any field, as Go does', () => {
    expect(parseInvoiceChangedTriggerConfiguration({})).toEqual({});
    expect(() => parseInvoiceChangedTriggerConfiguration({ customerId: '58' })).toThrow();
    expect(defineInvoiceChangedTriggerBinding({ connectionName: reference.name, bindingName: 'quickbooks-invoice-changes' }).definition.trigger.triggerName).toBe('invoiceChanged');
  });

  it('rejects a poll interval under five seconds, as Go New does', () => {
    expect(() => fakeClient(new QuickBooksInvoiceFake(realmId), 4_999)).toThrow(/pollInterval/);
  });

  it('keeps the 30-second default for a zero poll interval, as Go does', () => {
    expect(fakeClient(new QuickBooksInvoiceFake(realmId), 0).pollIntervalMs).toBe(30_000);
  });

  it('rejects incomplete routes and mock connections', () => {
    const connection = newConnection(fakeClient(new QuickBooksInvoiceFake(realmId)), reference);
    const target = new ListTarget();
    for (const routes of [[], [{ bindingName: '', target }], [{ bindingName: 'invoices' } as never], [{ bindingName: 'invoices', target, configuration: { field: 1 } as never }]]) {
      expect(() => newInvoiceTriggerRunner({ connection, invoiceChangedRoutes: routes })).toThrow();
    }
    expect(() => newInvoiceTriggerRunner({ connection: new GeneratedMock(reference.name).connection(), invoiceChangedRoutes: [{ bindingName: 'invoices', target }] })).toThrow(/mock/);
  });

  it('hashes the invoice version into the event ID as Go does', () => {
    const invoice = { invoiceId: '132', syncToken: '0', customerId: '58', lines: [], totalAmount: '25.50', balance: '25.50', updatedAt: '2026-11-02T16:00:02.25Z' };
    expect(invoiceChangeEventId(realmId, 'production', invoice)).toBe('qbo-invoice-ae56cc0fa7319340be6402e6ffb5b852');
    expect(invoiceChangeEventId(realmId, 'sandbox', invoice)).not.toBe('qbo-invoice-ae56cc0fa7319340be6402e6ffb5b852');
  });
});

describe('invoiceChanged runners', () => {
  it('decodes pollInterval as Go decodes a time.Duration: integer nanoseconds, 0 keeping the default', async () => {
    const directory = await mkdtemp(join(tmpdir(), 'quickbooks-poll-interval-test-'));
    const open = async (pollInterval: unknown): Promise<number> => {
      const path = join(directory, `connections-${String(pollInterval)}.json`);
      await writeFile(path, JSON.stringify({
        schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
        connections: [{ connectorId: 'quickbooks', connectionName: reference.name, modulePath: 'github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/go', provider: 'quickbooks', configuration: { realmId, pollInterval }, credentials: { client_id: 'client', client_secret: 'secret', access_token: 'access', refresh_token: 'refresh', id_token: '' }, credentialExpiresAt: new Date(Date.now() + 3_600_000).toISOString() }],
      }));
      await chmod(path, 0o600);
      const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
      return (newProjectConnection(project, reference.name, { localProviderURL: 'http://127.0.0.1:1' }).operations as QuickBooksClient).pollIntervalMs;
    };
    try {
      expect(await open(0)).toBe(30_000);
      expect(await open(60_000_000_000)).toBe(60_000);
      await expect(open('30s')).rejects.toThrow(/integer nanoseconds/);
      await expect(open(-5_000_000_000)).rejects.toThrow(/negative/);
    } finally {
      await rm(directory, { recursive: true, force: true });
    }
  });

  it('reads pollInterval from the project, records the change in the inbox, and commits the cursor', async () => {
    const fake = new QuickBooksInvoiceFake(realmId);
    fake.create('EXISTING', '58', '10.00');
    const directory = await mkdtemp(join(tmpdir(), 'quickbooks-trigger-test-'));
    const path = join(directory, 'connections.json');
    await writeFile(path, JSON.stringify({
      schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
      connections: [{ connectorId: 'quickbooks', connectionName: reference.name, modulePath: 'github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/go', provider: 'quickbooks', configuration: { realmId, pollInterval: 5_000_000_000 }, credentials: { client_id: 'client', client_secret: 'secret', access_token: 'access', refresh_token: 'refresh', id_token: '' }, credentialExpiresAt: new Date(Date.now() + 3_600_000).toISOString() }],
    }));
    await chmod(path, 0o600);
    const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
    const target = new ListTarget();
    const stop = new AbortController();
    try {
      const connection = newProjectConnection(project, reference.name, { send: fake.send, localProviderURL: 'http://127.0.0.1:1' });
      expect((connection.operations as QuickBooksClient).pollIntervalMs).toBe(5_000);
      const runner = newProjectInvoiceTriggerRunner(project, connection, { invoiceChangedRoutes: [{ bindingName: 'invoices', target }] });
      const key: TriggerInboxKey = { connectorId: 'quickbooks', connectionName: reference.name, triggerName: 'invoiceChanged', bindingName: 'invoices' };
      const cursor = project.pollCursorStore(key);
      const committedToken = async (): Promise<string> => (await cursor.readPollCursor()).state?.token ?? '';
      const running = runner.run(newTriggerContext(stop.signal));
      await eventually(async () => (await committedToken()) !== '');
      const baseline = await committedToken();
      const created = fake.create('NEW', '58', '20.00');
      runner.wakePoller('invoices');
      await eventually(() => target.events.length > 0);
      expect(target.events[0]!.payload.invoice.invoiceId).toBe(created);
      expect(target.events[0]!.payload.changeKind).toBe(InvoiceChangeCreated);
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

  it('the in-memory Trigger baselines and delivers the next version', async () => {
    const fake = new QuickBooksInvoiceFake(realmId);
    const existing = fake.create('EXISTING', '58', '10.00');
    const connection = newConnection(fakeClient(fake), reference);
    const target = new ListTarget();
    expect(() => newInvoiceChangedTrigger({ connection, connectionName: 'other', bindingName: 'invoices', target })).toThrow(/connectionName/);
    const trigger = newInvoiceChangedTrigger({ connection, connectionName: reference.name, bindingName: 'invoices', target });
    const stop = new AbortController();
    const running = trigger.run(newTriggerContext(stop.signal)).catch((error: unknown) => error);
    try {
      await eventually(() => fake.queries().length > 0);
      fake.pay(existing);
      // The trigger polls again after one 5-second interval with jitter.
      await eventually(() => target.events.length > 0, 30_000);
      expect(target.events[0]!.payload.changeKind).toBe(InvoiceChangeUpdated);
      expect(target.events[0]!.payload.invoice.balance).toBe('0');
    } finally {
      stop.abort();
      expect(await running).toBeInstanceOf(TriggerDeliveryAbortedError);
    }
  }, 45_000);
});
