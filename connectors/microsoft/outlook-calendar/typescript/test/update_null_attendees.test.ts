// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { Client } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';
import { HeartbeatContext, scenarioCall, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

it('treats attendees:null with no other patch field as a defect before provider access', async () => {
  const connection = { provider: 'microsoft', name: 'outlook-calendar-scheduler' };
  const credentials: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'),
    accessToken: new SecretString('token'), refreshToken: new SecretString('refresh') };
  let requests = 0;
  const client = new Client({}, new StaticCredentialProvider([[connection, credentials]]), {
    transport: async () => { requests++; throw new Error('provider call was not expected'); },
    baseURL: 'http://127.0.0.1:12345/v1.0',
  });
  const call = scenarioCall({ callId: 'calendar-null-attendees', operation: 'updateEvent' } as Scenario,
    connection, 'outlook-calendar', new HeartbeatContext(undefined)) as Call;
  const attempt = await client.updateEvent().invoke(call, { eventId: 'event1', attendees: null } as never);
  expect(attempt).toMatchObject({ kind: 'branch', branch: 'defect' });
  expect(requests).toBe(0);
});

it('omits attendees from PATCH when null accompanies a changed subject', async () => {
  const connection = { provider: 'microsoft', name: 'outlook-calendar-scheduler' };
  const credentials: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'),
    accessToken: new SecretString('token'), refreshToken: new SecretString('refresh') };
  const requests: Array<{ method: string; body?: unknown }> = [];
  const client = new Client({}, new StaticCredentialProvider([[connection, credentials]]), {
    baseURL: 'http://127.0.0.1:12345/v1.0', transport: async request => {
      requests.push({ method: request.method, ...(request.body ? { body: JSON.parse(Buffer.from(request.body).toString()) as unknown } : {}) });
      return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ id: 'event1', subject: request.method === 'GET' ? 'Old subject' : 'New subject',
        start: { dateTime: '2026-02-24T17:00:00', timeZone: 'UTC' },
        end: { dateTime: '2026-02-24T18:00:00', timeZone: 'UTC' } })) };
    },
  });
  const call = scenarioCall({ callId: 'calendar-null-attendees-subject', operation: 'updateEvent' } as Scenario,
    connection, 'outlook-calendar', new HeartbeatContext(undefined)) as Call;
  const attempt = await client.updateEvent().invoke(call, { eventId: 'event1', subject: 'New subject', attendees: null });
  expect(attempt).toMatchObject({ kind: 'branch', branch: 'updated', value: { wasAlreadyApplied: false } });
  expect(requests).toEqual([{ method: 'GET' }, { method: 'PATCH', body: { subject: 'New subject' } }]);
});

it('does not PATCH when an empty location is already absent', async () => {
  const connection = { provider: 'microsoft', name: 'outlook-calendar-scheduler' };
  const credentials: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'),
    accessToken: new SecretString('token'), refreshToken: new SecretString('refresh') };
  const methods: string[] = [];
  const client = new Client({}, new StaticCredentialProvider([[connection, credentials]]), {
    baseURL: 'http://127.0.0.1:12345/v1.0', transport: async request => {
      methods.push(request.method);
      return { status: 200, headers: {}, body: Buffer.from(JSON.stringify({ id: 'event1',
        start: { dateTime: '2026-02-24T17:00:00', timeZone: 'UTC' },
        end: { dateTime: '2026-02-24T18:00:00', timeZone: 'UTC' } })) };
    },
  });
  const call = scenarioCall({ callId: 'calendar-empty-location', operation: 'updateEvent' } as Scenario,
    connection, 'outlook-calendar', new HeartbeatContext(undefined)) as Call;
  const attempt = await client.updateEvent().invoke(call, { eventId: 'event1', location: '' });
  expect(attempt).toMatchObject({ kind: 'branch', branch: 'updated', value: { wasAlreadyApplied: true } });
  expect(methods).toEqual(['GET']);
});

it('does not resend a calendar read with an invalid 401 replacement credential', async () => {
  const connection = { provider: 'microsoft', name: 'outlook-calendar-scheduler' };
  const credentials: Credentials = { authMethodId: 'microsoft-oauth', clientId: 'client', clientSecret: new SecretString('secret'),
    accessToken: new SecretString('token'), refreshToken: new SecretString('refresh') };
  let requests = 0;
  const source = { resolve: async () => credentials, resolveWithRefresh: async () => credentials,
    resolveAfterRejection: async () => ({ ...credentials, authMethodId: 'wrong-method' }) };
  const client = new Client({}, source, { baseURL: 'http://127.0.0.1:12345/v1.0',
    transport: async () => { requests++; return { status: 401, headers: {}, body: Buffer.from('{}') }; } });
  const call = scenarioCall({ callId: 'calendar-invalid-refresh', operation: 'getEvent' } as Scenario,
    connection, 'outlook-calendar', new HeartbeatContext(undefined)) as Call;
  const result = await client.getEvent().invoke(call, { eventId: 'event1' });
  expect(result).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
  expect(requests).toBe(1);
});
