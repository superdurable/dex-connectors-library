// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { HeartbeatContext, scenarioCall } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { CalendlyClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';

const connection = { provider: 'calendly', name: 'scheduling' };
const eventURI = 'https://api.calendly.com/scheduled_events/EVT00001';
const userURI = 'https://api.calendly.com/users/USER0001';
const organizationURI = 'https://api.calendly.com/organizations/ORG0001';
const eventResource = { uri: eventURI, status: 'active', start_time: '2026-11-10T10:00:00Z', end_time: '2026-11-10T10:30:00Z' };
const credentials: Credentials = { authMethodId: 'personal-access-token', accessToken: new SecretString('token') };

function call(operation: string): Call {
  return scenarioCall({ name: operation, operation, callId: '472485d6-a578-50af-a4d2-124cf93b188f', input: {}, exchanges: [], expected: { branch: '', failureKind: '', retry: false, retryAfterMs: 0, uncertain: false, requestCount: 0 } }, connection, 'calendly', new HeartbeatContext(undefined)) as Call;
}

function response(status: number, document: unknown): ProviderResponse {
  return { status, headers: {}, body: Buffer.from(JSON.stringify(document)) };
}

describe('Calendly operation edge cases', () => {
  it('validates every manifest connection limit, including the Go-only webhook settings', () => {
    const provider = new StaticCredentialProvider([[connection, credentials]]);
    expect(() => new CalendlyClient({ maxResponseBytes: -1 }, provider)).toThrow();
    expect(() => new CalendlyClient({ webhookMaxBodyBytes: -1 }, provider)).toThrow();
    expect(() => new CalendlyClient({ webhookSignatureToleranceMs: -1 }, provider)).toThrow();
  });

  it.each([
    ['hana@example.com', 'listed'],
    ['é@例子.com', 'listed'],
    ['hana@[127.0.0.1]', 'listed'],
    ['Name <hana@example.com>', 'defect'],
    ['"hana"@example.com', 'defect'],
    ['hana..x@example.com', 'defect'],
    ['hana@example.com,other@example.com', 'defect'],
  ])('matches Go plain-email validation for %s', async (email, branch) => {
    const requests: ProviderRequest[] = [];
    const client = new CalendlyClient({}, new StaticCredentialProvider([[connection, credentials]]), { send: async request => { requests.push(request); return response(200, { collection: [], pagination: {} }); } });
    const attempt = await client.listEventInvitees().invoke(call('listEventInvitees'), { scheduledEventUri: eventURI, email });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') expect(attempt.branch).toBe(branch);
    expect(requests).toHaveLength(branch === 'defect' ? 0 : 1);
  });

  it.each(['2026-02-30T00:00:00Z', '2025-02-29T00:00:00Z', '2026-11-04T24:00:00Z'])('rejects the invalid Go timestamp %s', async timestamp => {
    const client = new CalendlyClient({}, new StaticCredentialProvider([[connection, credentials]]), { send: async () => { throw new Error('no request expected'); } });
    const attempt = await client.listScheduledEvents().invoke(call('listScheduledEvents'), { userUri: userURI, minStartTime: timestamp, maxStartTime: '2026-12-01T00:00:00Z' });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') expect(attempt.branch).toBe('defect');
  });

  it('accepts a Go timestamp with a comma fraction and a large offset', async () => {
    const requests: ProviderRequest[] = [];
    const client = new CalendlyClient({}, new StaticCredentialProvider([[connection, credentials]]), { send: async request => { requests.push(request); return response(200, { collection: [], pagination: {} }); } });
    const attempt = await client.listScheduledEvents().invoke(call('listScheduledEvents'), { userUri: userURI, minStartTime: '2026-11-04T10:00:00,5+99:59', maxStartTime: '2026-12-01T00:00:00Z' });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') expect(attempt.branch).toBe('listed');
    expect(requests).toHaveLength(1);
  });

  it('keeps a 429 classification when its body exceeds the successful-response limit', async () => {
    const client = new CalendlyClient({ maxResponseBytes: 1 }, new StaticCredentialProvider([[connection, credentials]]), { send: async () => { throw new BodyTooLargeError(429, { 'retry-after': '2' }); } });
    const attempt = await client.getScheduledEvent().invoke(call('getScheduledEvent'), { scheduledEventUri: eventURI });
    expect(attempt).toMatchObject({ kind: 'retry', failure: { kind: 'RATE_LIMIT' }, retryAfterMs: 2000 });
  });

  it('rejects a wrong-typed provider status as invalidResponse', async () => {
    const client = new CalendlyClient({}, new StaticCredentialProvider([[connection, credentials]]), { send: async () => response(200, { resource: { ...eventResource, status: 7 } }) });
    const attempt = await client.getScheduledEvent().invoke(call('getScheduledEvent'), { scheduledEventUri: eventURI });
    expect(attempt).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
  });

  it('rejects a fractional provider answer position as invalidResponse', async () => {
    const invitee = { uri: `${eventURI}/invitees/INV0001`, event: eventURI, email: 'hana@example.com', status: 'active', questions_and_answers: [{ question: 'Team?', answer: 'Platform', position: 1.5 }] };
    const client = new CalendlyClient({}, new StaticCredentialProvider([[connection, credentials]]), { send: async () => response(200, { collection: [invitee], pagination: {} }) });
    const attempt = await client.listEventInvitees().invoke(call('listEventInvitees'), { scheduledEventUri: eventURI });
    expect(attempt).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
  });

  it('classifies terminal credential errors and transient lookup errors separately', async () => {
    const terminal = new CalendlyClient({}, { resolve: async () => { throw new ReauthorizationRequiredError(); }, resolveWithRefresh: async () => { throw new ReauthorizationRequiredError(); } });
    const transient = new CalendlyClient({}, { resolve: async () => { throw new Error('temporary unavailable'); }, resolveWithRefresh: async () => { throw new Error('temporary unavailable'); } });
    expect(await terminal.getScheduledEvent().invoke(call('getScheduledEvent'), { scheduledEventUri: eventURI })).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
    expect(await transient.getScheduledEvent().invoke(call('getScheduledEvent'), { scheduledEventUri: eventURI })).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
  });

  it('uses a refreshed OAuth token on later requests in the same operation', async () => {
    const authorization: string[] = [];
    let refreshes = 0;
    const initial: Credentials = { authMethodId: 'calendly-oauth', accessToken: new SecretString('old'), oauthClientId: 'client', oauthClientSecret: new SecretString('secret'), refreshToken: new SecretString('refresh') };
    const source = {
      resolve: async () => initial,
      resolveWithRefresh: async () => initial,
      resolveAfterRejection: async () => { refreshes++; return { ...initial, accessToken: new SecretString('new') }; },
    };
    const client = new CalendlyClient({}, source, { send: async request => {
      authorization.push(request.headers?.Authorization ?? '');
      if (authorization.length === 1) return response(401, {});
      if (request.url.endsWith('/users/me')) return response(200, { resource: { uri: userURI, current_organization: organizationURI } });
      return response(200, { collection: [], pagination: {} });
    } });
    const attempt = await client.listScheduledEvents().invoke(call('listScheduledEvents'), { minStartTime: '2026-11-01T00:00:00Z', maxStartTime: '2026-12-01T00:00:00Z' });
    expect(attempt).toMatchObject({ kind: 'branch', branch: 'listed' });
    expect(refreshes).toBe(1);
    expect(authorization).toEqual(['Bearer old', 'Bearer new', 'Bearer new']);
  });
});
