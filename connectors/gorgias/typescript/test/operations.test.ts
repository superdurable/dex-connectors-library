// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GorgiasClient, isBareEmail } from '../src/client.js';
import { decodeTicket } from '../src/wire.js';

const credential = { email: 'agent@example.com', apiKey: new SecretString('testKey0123456789') };
const call = {
  id: 'call-test', connection: { provider: 'gorgias', name: 'gorgias-test' }, operation: { connectorId: 'gorgias', operationId: 'getTicket' },
  context: { attempt: 1, cancellationSignal: new AbortController().signal, hasLastHeartbeatValue: () => false },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
} as unknown as Call;

function mutationCall(attempt: number, hasEarlierDispatch = false): Call {
  return {
    ...call, idempotencyKey: 'dex-call-test',
    context: {
      ...call.context, attempt, hasLastHeartbeatValue: () => hasEarlierDispatch,
      getLastHeartbeatValue: () => hasEarlierDispatch ? { gorgiasDispatchedCallId: 'call-test' } : null,
      recordHeartbeat: async () => {},
    },
  } as Call;
}

function response(status: number, body: unknown = {}): { status: number; headers: Record<string, string>; body: Uint8Array } {
  return { status, headers: {}, body: Buffer.from(JSON.stringify(body)) };
}

const createInput = { subject: 'Issue', description: 'Please help', requester: { email: 'ada@example.com' } };
const noteInput = { ticketId: 7001, body: 'Please help' };

describe('Gorgias client', () => {
  it('rejects malformed domains and limits', () => {
    expect(() => new GorgiasClient({ domain: 'acme.gorgias.com' }, { resolve: async () => credential })).toThrow();
    expect(() => new GorgiasClient({ domain: 'acme', maxResponseBytes: -1 }, { resolve: async () => credential })).toThrow();
  });

  it('classifies a refused connection as safe to retry', async () => {
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, {
      send: async () => { throw new ProviderTransportError(true); },
    });
    const attempt = await client.getTicket().invoke(call, { ticketId: 7001 });
    expect(attempt.kind).toBe('retry');
    expect(attempt.failure?.kind).toBe('TRANSPORT');
  });

  it('treats unavailable credentials as a terminal defect', async () => {
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => { throw new Error('secret-bearing error'); } });
    const attempt = await client.getTicket().invoke(call, { ticketId: 7001 });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new Error('expected a terminal branch');
    expect(attempt.branch).toBe('defect');
    expect(attempt.failure?.kind).toBe('AUTHENTICATION');
    expect(attempt.failure?.message).not.toContain('secret');
  });
});

it.each(['agent@example.com', 'first.last+triage@example.com'])('accepts a bare email: %s', email => expect(isBareEmail(email)).toBe(true));
it.each(['.first@example.com', 'first.@example.com', 'first..last@example.com', 'Name <first@example.com>', 'first@example-.com'])('rejects a malformed email: %s', email => expect(isBareEmail(email)).toBe(false));

it('preserves Go timestamp precision and normalizes offsets', () => {
  const ticket = decodeTicket({ id: 7001, status: 'open', created_datetime: '2026-01-26T14:02:00.384938', updated_datetime: '2026-01-28T09:45:00.932637+01:00' });
  expect(ticket.createdAt).toBe('2026-01-26T14:02:00.384938Z');
  expect(ticket.updatedAt).toBe('2026-01-28T08:45:00.932637Z');
});

describe('Gorgias retry and routing parity', () => {
  it.each([
    ['createTicket', createInput],
    ['addNote', noteInput],
  ] as const)('retries an invalid unsent %s lookup', async (operation, input) => {
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async () => response(200, { malformed: true }) });
    const attempt = operation === 'createTicket'
      ? await client.createTicket().invoke(mutationCall(2), input)
      : await client.addNote().invoke(mutationCall(2), input);
    expect(attempt.kind).toBe('retry');
    expect(attempt.failure?.kind).toBe('PROTOCOL');
  });

  it.each([
    ['createTicket', createInput],
    ['addNote', noteInput],
  ] as const)('reports an earlier %s send as uncertain after a rejected lookup', async (operation, input) => {
    let requestCount = 0;
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async () => { requestCount++; return response(403); } });
    const attempt = operation === 'createTicket'
      ? await client.createTicket().invoke(mutationCall(2, true), input)
      : await client.addNote().invoke(mutationCall(2, true), input);
    expect(attempt.kind).toBe('uncertain');
    expect(attempt.failure?.message).toContain('Gorgias could not confirm it, so it is not sent again:');
    expect(requestCount).toBe(1);
  });

  it('reports unavailable credentials after an earlier dispatch with the reconciliation message', async () => {
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => { throw new Error('unavailable'); } });
    const attempt = await client.createTicket().invoke(mutationCall(4, true), createInput);
    expect(attempt.kind).toBe('uncertain');
    expect(attempt.failure?.message).toContain('credentials are unavailable to confirm it, so it is not sent again');
  });

  it('omits a zero view ID and rejects empty status and priority filters', async () => {
    let searchURL = '';
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async request => { searchURL = request.url; return response(200, { data: [] }); } });
    await client.searchTickets().invoke(call, { viewId: 0 });
    expect(searchURL).not.toContain('view_id=');
    for (const input of [{ statuses: [''] }, { priorities: [''] }]) {
      const attempt = await client.searchTickets().invoke(call, input);
      expect(attempt.kind).toBe('branch');
      if (attempt.kind === 'branch') expect(attempt.branch).toBe('defect');
    }
  });

  it('does not use an agent address with a null integration ID for a public reply', async () => {
    let requestCount = 0;
    const ticket = {
      id: 7001, status: 'open', subject: 'Issue', created_datetime: '2026-10-20T09:00:00Z', updated_datetime: '2026-10-25T09:00:00Z', messages: [
        { id: 1, from_agent: false, channel: 'email', integration_id: null, source: { from: { address: 'ada@example.com' }, to: [{ address: 'help@example.com' }], cc: [{ address: 'other@example.com' }] } },
        { id: 2, from_agent: true, channel: 'email', integration_id: null, source: { from: { address: 'help@example.com' } } },
      ],
    };
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async () => { requestCount++; return response(200, ticket); } });
    const attempt = await client.addNote().invoke(mutationCall(1), { ...noteInput, isPublicReply: true });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') expect(attempt.branch).toBe('providerRejected');
    expect(attempt.failure?.message).toContain("none is known to be the helpdesk's");
    expect(requestCount).toBe(1);
  });
});
