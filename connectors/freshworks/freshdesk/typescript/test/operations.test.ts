// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { once } from 'node:events';
import { createServer } from 'node:net';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { FreshdeskClient } from '../src/client.js';

function testCall() {
  const heartbeats: unknown[] = [];
  const call = {
    id: '703d7ec0-692e-50db-a914-86d5fecf7dcc',
    connection: { provider: 'freshdesk', name: 'freshdesk-test' }, operation: { connector: 'freshdesk', name: 'createTicket' },
    context: { attempt: 1, cancellationSignal: new AbortController().signal, hasLastHeartbeatValue: () => false,
      getLastHeartbeatValue: () => undefined, recordHeartbeat: async (value: unknown) => { heartbeats.push(value); } },
  } as unknown as Call;
  return { call, heartbeats };
}

const createInput = { subject: 'Billing issue', description: 'Charged twice', requester: { email: 'jane@example.com' } };

describe('Freshdesk operation boundaries', () => {
  it('keeps terminal credential errors on the defect branch without sending', async () => {
    const client = new FreshdeskClient({ domain: 'acme' }, { resolve: async () => { throw new ReauthorizationRequiredError(); } }, { send: async () => { throw new Error('unexpected request'); } });
    const { call } = testCall();
    const attempt = await client.getTicket().invoke(call, { ticketId: 1201 });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new Error('expected defect branch');
    expect(attempt.branch).toBe('defect');
    expect(attempt.failure?.kind).toBe('AUTHENTICATION');
  });

  it('retries transient credential errors without sending', async () => {
    const client = new FreshdeskClient({ domain: 'acme' }, { resolve: async () => { throw new Error('temporary store failure'); } }, { send: async () => { throw new Error('unexpected request'); } });
    const { call } = testCall();
    const attempt = await client.getTicket().invoke(call, { ticketId: 1201 });
    expect(attempt.kind).toBe('retry');
    expect(attempt.failure?.kind).toBe('AVAILABILITY');
  });

  it('records a next-attempt permit after a provably unsent create', async () => {
    let sent = 0;
    const client = new FreshdeskClient({ domain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('inertKey') }) }, {
      send: async () => { sent++; throw new ProviderTransportError(true); },
    });
    const { call, heartbeats } = testCall();
    const attempt = await client.createTicket().invoke(call, createInput);
    expect(attempt.kind).toBe('retry');
    expect(attempt.failure?.kind).toBe('TRANSPORT');
    expect(sent).toBe(1);
    expect(heartbeats).toEqual([
      { freshdeskDispatchedCallId: call.id, freshdeskDispatchedAttempt: 1 },
      { freshdeskDispatchedCallId: call.id, freshdeskRetryAttempt: 2 },
    ]);
  });

  it('retries an address-policy refusal and permits a later create attempt', async () => {
    const client = new FreshdeskClient({ domain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('inertKey') }) }, {
      send: async () => { throw new AddressRefusal('resolved address violates policy'); },
    });
    const { call, heartbeats } = testCall();
    const attempt = await client.createTicket().invoke(call, createInput);
    expect(attempt.kind).toBe('retry');
    expect(attempt.failure?.kind).toBe('TRANSPORT');
    expect(heartbeats).toEqual([
      { freshdeskDispatchedCallId: call.id, freshdeskDispatchedAttempt: 1 },
      { freshdeskDispatchedCallId: call.id, freshdeskRetryAttempt: 2 },
    ]);
  });

  it('records the ticket ID on get and update rejection receipts', async () => {
    const client = new FreshdeskClient({ domain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('inertKey') }) }, {
      send: async () => ({ status: 404, headers: {}, body: Buffer.from('{}') }),
    });
    const { call } = testCall();
    const getAttempt = await client.getTicket().invoke(call, { ticketId: 1201 });
    const updateAttempt = await client.updateTicket().invoke(call, { ticketId: 1201, status: 3 });
    expect(getAttempt.kind).toBe('branch');
    expect(updateAttempt.kind).toBe('branch');
    if (getAttempt.kind !== 'branch' || updateAttempt.kind !== 'branch') throw new Error('expected rejection branches');
    expect(getAttempt.receipt.providerObjectId).toBe('1201');
    expect(updateAttempt.receipt.providerObjectId).toBe('1201');
  });

  it('does not resend when an earlier write may have reached Freshdesk', async () => {
    let sent = 0;
    const marker = { freshdeskDispatchedCallId: '703d7ec0-692e-50db-a914-86d5fecf7dcc', freshdeskDispatchedAttempt: 1 };
    const { call } = testCall();
    Object.assign(call.context, { attempt: 2, hasLastHeartbeatValue: () => true, getLastHeartbeatValue: () => marker });
    const client = new FreshdeskClient({ domain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('inertKey') }) }, {
      send: async () => { sent++; throw new Error('unexpected request'); },
    });
    const attempt = await client.createTicket().invoke(call, createInput);
    expect(attempt.kind).toBe('uncertain');
    expect(sent).toBe(0);
  });

  it('retries a refused loopback connection without treating it as a local defect', async () => {
    const listener = createServer();
    listener.listen(0, '127.0.0.1');
    await once(listener, 'listening');
    const address = listener.address();
    if (!address || typeof address === 'string') throw new Error('free loopback port unavailable');
    await new Promise<void>(resolve => listener.close(() => resolve()));
    const client = new FreshdeskClient({ domain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('inertKey') }) }, { apiBaseURL: `http://127.0.0.1:${address.port}/api/v2` });
    const { call } = testCall();
    const attempt = await client.getTicket().invoke(call, { ticketId: 1201 });
    expect(attempt.kind).toBe('retry');
    expect(attempt.failure?.kind).toBe('TRANSPORT');
  });

  it('classifies an oversized 429 error body as a rate-limit retry', async () => {
    const client = new FreshdeskClient({ domain: 'acme' }, { resolve: async () => ({ apiKey: new SecretString('inertKey') }) }, {
      send: async () => { throw new BodyTooLargeError(429, { 'retry-after': '2' }); },
    });
    const { call } = testCall();
    const attempt = await client.getTicket().invoke(call, { ticketId: 1201 });
    expect(attempt.kind).toBe('retry');
    expect(attempt.failure?.kind).toBe('RATE_LIMIT');
  });
});
