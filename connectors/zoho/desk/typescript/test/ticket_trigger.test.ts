// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { afterEach, expect, it } from 'vitest';
import { GeneratedMock } from '../src/mock.js';
import {
  TicketChangedTriggerConfigurationSchema, ZohoDeskClient, newConnection, newProjectTicketTriggerRunner, newTicketChangedTrigger, newTicketTriggerRunner,
  validateTicketChangedTriggerConfiguration, type TicketChangedEvent,
} from '../src/index.js';
import { DeskSearchFake, FakeAccessToken, FakeOrganizationID, fakeCredentialSource } from './support/desk_search_fake.js';

const reference = { provider: 'zoho', name: 'zoho-desk-trigger' };
const billing = '1892000000006907';
const support = '1892000000082069';
const fakes: DeskSearchFake[] = [];
afterEach(async () => { await Promise.all(fakes.splice(0).map(fake => fake.close())); });

async function startFake(): Promise<DeskSearchFake> { const fake = await DeskSearchFake.start(); fakes.push(fake); return fake; }

class QueueTarget {
  readonly events: TriggerEvent<TicketChangedEvent>[] = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<TicketChangedEvent>): Promise<void> { this.events.push(event); }
}

it('validates and strictly decodes the binding configuration as Go does', () => {
  expect(() => validateTicketChangedTriggerConfiguration({})).not.toThrow();
  expect(() => validateTicketChangedTriggerConfiguration({ departmentId: billing })).not.toThrow();
  expect(() => validateTicketChangedTriggerConfiguration({ departmentId: 'Billing' })).toThrow('numeric Zoho Desk ID');
  expect(TicketChangedTriggerConfigurationSchema.parse({ departmentId: billing, departmentName: 'Billing' })).toEqual({ departmentId: billing, departmentName: 'Billing' });
  expect(() => TicketChangedTriggerConfigurationSchema.parse({ department: billing })).toThrow();
});

it('refuses a poll interval under 15 seconds, as Go does', () => {
  expect(() => new ZohoDeskClient({ orgId: FakeOrganizationID, pollIntervalMs: 14_999 }, fakeCredentialSource(reference))).toThrow('at least 15s');
  expect(new ZohoDeskClient({ orgId: FakeOrganizationID }, fakeCredentialSource(reference)).pollIntervalMs).toBe(60_000);
});

it('refuses incomplete routes, invalid departments, and mock connections', async () => {
  const fake = await startFake();
  const connection = newConnection(new ZohoDeskClient({ orgId: FakeOrganizationID }, fakeCredentialSource(reference), { apiBaseURL: fake.baseURL }), reference);
  const target = new QueueTarget();
  for (const routes of [
    [], [{ bindingName: ' ', target }], [{ bindingName: 'tickets', target: undefined as never }],
    [{ bindingName: 'tickets', target }, { bindingName: 'tickets', target }],
    [{ bindingName: 'tickets', target, configuration: { departmentId: 'Billing' } }],
  ]) expect(() => newTicketTriggerRunner({ connection, ticketChangedRoutes: routes })).toThrow();
  expect(() => newTicketTriggerRunner({ connection: new GeneratedMock('zoho-desk-trigger').connection(), ticketChangedRoutes: [{ bindingName: 'tickets', target }] })).toThrow('not a mock');
});

it('loads the saved department, delivers only its new ticket, and commits after recording', async () => {
  const fake = await startFake();
  fake.create('Existing', billing);
  const objects = new InMemoryObjectStore();
  const scope = { projectId: 'zoho-desk-unit', kind: 'live' } as const;
  await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'zoho-desk', connectionName: reference.name }, 0, new CredentialMaterial({
    auth_method: 'zoho-us-oauth', access_token: FakeAccessToken, oauth_client_id: 'fake-client', oauth_client_secret: 'fake-secret', refresh_token: 'fake-refresh',
  }, new Date(Date.now() + 3_600_000), 'zoho-us-oauth'));
  const project = (bindings: Array<Record<string, unknown>>, pollInterval = 15_000_000_000) => new LoadedProject({
    schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
    connections: [{ connectorId: 'zoho-desk', connectionName: reference.name, modulePath: 'zoho-desk', provider: 'zoho', configuration: { orgId: FakeOrganizationID, pollInterval } }],
    triggerBindings: bindings.map(configuration => ({ connectorId: 'zoho-desk', connectionName: reference.name, triggerName: 'ticketChanged', bindingName: 'billingTickets', configuration })),
  }, undefined, objects, undefined, undefined);
  const target = new QueueTarget();
  const routes = { ticketChangedRoutes: [{ bindingName: 'billingTickets', target }] };
  const options = { apiBaseURL: fake.baseURL };
  expect(() => newProjectTicketTriggerRunner(project([], 14_999_999_999), reference.name, routes, options)).toThrow('at least 15s');
  expect(() => newProjectTicketTriggerRunner(project([{ departmentId: 'Billing' }]), reference.name, routes, options)).toThrow('numeric Zoho Desk ID');
  expect(() => newProjectTicketTriggerRunner(project([{ departmentId: billing, unknown: true }]), reference.name, routes, options)).toThrow('configuration');
  expect(newProjectTicketTriggerRunner(project([]), reference.name, routes, options), 'an unsaved binding watches every department').toBeDefined();

  const loaded = project([{ departmentId: billing, departmentName: 'Billing' }]);
  const runner = newProjectTicketTriggerRunner(loaded, reference.name, routes, options);
  const key = { connectorId: 'zoho-desk', connectionName: reference.name, triggerName: 'ticketChanged', bindingName: 'billingTickets' };
  const committedToken = async () => (await loaded.pollCursorStore(key).readPollCursor()).state?.token ?? '';
  const stop = new AbortController();
  const running = runner.run(newTriggerContext(stop.signal));
  try {
    await expect.poll(committedToken, { timeout: 10_000 }).toContain(`"departmentId":"${billing}"`);
    const baseline = await committedToken();
    fake.create('Other department', support);
    const created = fake.create('Refund request', billing);
    runner.wakePoller('billingTickets');
    await expect.poll(() => target.events.length, { timeout: 10_000 }).toBe(1);
    expect(target.events[0]!.payload).toMatchObject({ orgId: FakeOrganizationID, departmentId: billing, changeKind: 'created', ticket: { id: created, subject: 'Refund request' } });
    await expect.poll(committedToken, { timeout: 10_000 }).not.toBe(baseline);
    await expect.poll(async () => (await loaded.triggerInbox(key).pending()).length, { timeout: 10_000 }).toBe(0);
    expect(target.events, 'the baseline and the other department are never delivered').toHaveLength(1);
    expect(new Set(fake.searches.map(search => search.departmentId))).toEqual(new Set([billing]));
  } finally { stop.abort(); await running.catch(() => undefined); }
});

it('runs one in-memory Trigger that baselines and delivers the next change', async () => {
  const fake = await startFake();
  const existing = fake.create('Existing', billing);
  const connection = newConnection(new ZohoDeskClient({ orgId: FakeOrganizationID, pollIntervalMs: 15_000 }, fakeCredentialSource(reference), { apiBaseURL: fake.baseURL }), reference);
  const target = new QueueTarget();
  expect(() => newTicketChangedTrigger({ connection, connectionName: 'other', bindingName: 'allTickets', target })).toThrow('connectionName');
  const trigger = newTicketChangedTrigger({ connection, connectionName: reference.name, bindingName: 'allTickets', target });
  const stop = new AbortController();
  const running = trigger.run(newTriggerContext(stop.signal));
  try {
    await expect.poll(() => fake.searches.length, { timeout: 10_000 }).toBeGreaterThan(0);
    fake.update(existing, 'On Hold');
    // The Trigger polls again after one 15-second interval with jitter.
    await expect.poll(() => target.events.length, { timeout: 40_000 }).toBe(1);
    expect(target.events[0]!.payload).toMatchObject({ changeKind: 'updated', ticket: { id: existing, status: 'On Hold' } });
  } finally { stop.abort(); await running.catch(() => undefined); }
}, 60_000);
