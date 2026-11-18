// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { GorgiasClient, isBareEmail } from '../src/client.js';
import { decodeTicket } from '../src/wire.js';

const credential = { email: 'agent@example.com', apiKey: new SecretString('testKey0123456789') };
const call = {
  id: 'call-test', connection: { provider: 'gorgias', name: 'gorgias-test' }, operation: { connectorId: 'gorgias', operationId: 'getTicket' },
  context: { attempt: 1, cancellationSignal: new AbortController().signal, hasLastHeartbeatValue: () => false },
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
} as unknown as Call;

const mutationCallID = '3e3a7a5b-fac4-5d1b-a1bf-06bd4a3a9269';

function mutationCall(attempt: number, previousHeartbeat?: unknown): Call {
  return {
    ...call, id: mutationCallID, idempotencyKey: `dex-${mutationCallID}`,
    context: {
      ...call.context, attempt, hasLastHeartbeatValue: () => previousHeartbeat !== undefined,
      getLastHeartbeatValue: () => previousHeartbeat ?? null,
      recordHeartbeat: async () => {},
    },
  } as Call;
}

/** A mutation call whose Fence heartbeats are appended to heartbeats. */
function recordingMutationCall(attempt: number, heartbeats: unknown[], previousHeartbeat?: unknown): Call {
  const recorded = mutationCall(attempt, previousHeartbeat);
  return { ...recorded, context: { ...recorded.context, recordHeartbeat: async (value: unknown) => { heartbeats.push(value); } } } as Call;
}

/** A transient credential read failure and the terminal ones another attempt cannot repair. */
const credentialFailures = [
  ['transient credential read failure', async () => { throw new Error('credential store unavailable'); }, false],
  ['reauthorization requirement', async () => { throw new ReauthorizationRequiredError(); }, true],
  ['unusable credential', async () => ({ email: 'not an address', apiKey: new SecretString('testKey0123456789') }), true],
] as const;

/** A claim after three possible sends, which reaches the manifest send cap of three. */
const claimAtSendCap = { callId: mutationCallID, attempt: 3, state: 'claimed', sendCount: 3 };

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
  it('skips a malformed ticket while finding a matching external ID', async () => {
    let requestCount = 0;
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async () => {
      requestCount++;
      return response(200, { data: [{ id: 1 }, { id: 7001, status: 'open', created_datetime: '2026-10-20T09:00:00Z', external_id: `dex-${mutationCallID}` }] });
    } });
    const attempt = await client.createTicket().invoke(mutationCall(2), createInput);
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') {
      expect(attempt.branch).toBe('created');
      expect(attempt.value.ticket.id).toBe(7001);
    }
    expect(requestCount).toBe(1);
  });

  it.each([
    ['oversized response', async () => { throw new BodyTooLargeError(200, {}); }],
    ['reflected credential', async () => response(200, { apiKey: credential.apiKey.reveal() })],
  ] as const)('routes a %s during public-reply ticket read to providerRejected', async (_description, send) => {
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send });
    const attempt = await client.addNote().invoke(mutationCall(1), { ...noteInput, isPublicReply: true });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') expect(attempt.branch).toBe('providerRejected');
  });

  it.each([
    ['createTicket', createInput],
    ['addNote', noteInput],
  ] as const)('resends below the cap after an invalid %s lookup and never resends an unusable accepted answer', async (operation, input) => {
    const methods: string[] = [];
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async request => { methods.push(request.method); return response(200, { malformed: true }); } });
    const attempt = operation === 'createTicket'
      ? await client.createTicket().invoke(mutationCall(2), input)
      : await client.addNote().invoke(mutationCall(2), input);
    expect(attempt.kind).toBe('uncertain');
    expect(attempt.failure?.kind).toBe('PROTOCOL');
    expect(methods).toEqual(['GET', 'POST', 'GET']);
  });

  it.each([
    ['createTicket', createInput],
    ['addNote', noteInput],
  ] as const)('reports %s as uncertain after a rejected lookup at the send cap', async (operation, input) => {
    let requestCount = 0;
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async () => { requestCount++; return response(403); } });
    const attempt = operation === 'createTicket'
      ? await client.createTicket().invoke(mutationCall(4, claimAtSendCap), input)
      : await client.addNote().invoke(mutationCall(4, claimAtSendCap), input);
    expect(attempt.kind).toBe('uncertain');
    expect(attempt.failure?.message).toContain('Gorgias could not confirm it:');
    expect(requestCount).toBe(1);
  });

  it.each(credentialFailures)('maps a %s after a possible earlier send without recording a checkpoint', async (_name, resolve, isTerminal) => {
    const heartbeats: unknown[] = [];
    const attempt = await new GorgiasClient({ domain: 'acme' }, { resolve }).createTicket()
      .invoke(recordingMutationCall(2, heartbeats, { callId: mutationCallID, attempt: 1, state: 'claimed', sendCount: 1 }), createInput);
    expect(heartbeats).toEqual([]);
    expect(attempt.kind).toBe(isTerminal ? 'uncertain' : 'retry');
    if (isTerminal) expect(attempt.failure?.message).toContain('credentials are unusable to read it back');
  });

  it.each(credentialFailures)('maps a %s on a first send after recording notSent', async (_name, resolve, isTerminal) => {
    const heartbeats: unknown[] = [];
    const attempt = await new GorgiasClient({ domain: 'acme' }, { resolve }).addNote().invoke(recordingMutationCall(1, heartbeats), noteInput);
    expect(JSON.stringify(heartbeats)).toContain('"state":"notSent"');
    if (!isTerminal) { expect(attempt.kind).toBe('retry'); return; }
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') expect(attempt.branch).toBe('defect');
  });

  it.each([
    ['transport failure', () => new ProviderTransportError(false, 201, {})],
    ['stall', () => new ProviderStalledError(201, {})],
  ] as const)('reads back a ticket whose 2xx body breaks with a %s and never resends it', async (_name, bodyFailure) => {
    const methods: string[] = [];
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async request => {
      methods.push(request.method);
      if (request.method === 'POST') throw bodyFailure();
      return response(200, { data: [{ id: 7001, status: 'open', created_datetime: '2026-10-20T09:00:00Z', external_id: `dex-${mutationCallID}` }] });
    } });
    const attempt = await client.createTicket().invoke(mutationCall(1), createInput);
    expect(methods).toEqual(['POST', 'GET']);
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') expect(attempt.branch).toBe('created');
  });

  it('reads back a note whose 2xx body breaks and selects uncertain when it is absent', async () => {
    const methods: string[] = [];
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async request => {
      methods.push(request.method);
      if (request.method === 'POST') throw new ProviderTransportError(false, 201, {});
      return response(200, { id: 7001, status: 'open', created_datetime: '2026-10-20T09:00:00Z', messages: [] });
    } });
    const attempt = await client.addNote().invoke(mutationCall(1), noteInput);
    expect(methods).toEqual(['POST', 'GET']);
    expect(attempt.kind).toBe('uncertain');
  });

  it.each([true, false])('reads back a write whose 2xx body the write window cuts off and never resends it (found: %s)', async isFound => {
    const methods: string[] = [];
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async request => {
      methods.push(request.method);
      if (request.method !== 'POST') return response(200, { data: isFound ? [{ id: 7001, status: 'open', created_datetime: '2026-10-20T09:00:00Z', external_id: `dex-${mutationCallID}` }] : [] });
      // The headers arrived; the SDK sender reports the cut body with the status it saw.
      await new Promise(resolve => request.signal?.addEventListener('abort', resolve, { once: true }));
      throw new ProviderTransportError(false, 201, {});
    } });
    const shortCall = mutationCall(1);
    const attempt = await client.createTicket().invoke({ ...shortCall, context: { ...shortCall.context, deadline: new Date(Date.now() + 600) } } as Call, createInput);
    expect(methods).toEqual(['POST', 'GET']);
    expect(attempt.kind).toBe(isFound ? 'branch' : 'uncertain');
  });

  it.each([
    ['an invalid ticket', async () => response(200, { id: 'SENTINEL' })],
    ['an oversized refusal', async () => { throw new BodyTooLargeError(400, {}); }],
    ['a redirect', async () => response(301)],
    ['a conflict', async () => response(409)],
  ] as const)('retries an updateTicket whose first read returns %s', async (_name, send) => {
    const methods: string[] = [];
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async request => { methods.push(request.method); return send(); } });
    const attempt = await client.updateTicket().invoke(mutationCall(1), { ticketId: 7001, status: 'closed' });
    expect(methods).toEqual(['GET']);
    expect(attempt.kind).toBe('retry');
  });

  it.each([[404, 'notFound'], [403, 'providerRejected']] as const)('selects a branch when the updateTicket first read gets a definite %s', async (status, branch) => {
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async () => response(status) });
    const attempt = await client.updateTicket().invoke(mutationCall(1), { ticketId: 7001, status: 'closed' });
    expect(attempt.kind).toBe('branch');
    if (attempt.kind === 'branch') expect(attempt.branch).toBe(branch);
  });

  it('treats a write refusal with an oversized body as an unknown outcome', async () => {
    const heartbeats: unknown[] = [];
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async () => { throw new BodyTooLargeError(400, {}); } });
    const attempt = await client.createTicket().invoke(recordingMutationCall(1, heartbeats), createInput);
    expect(attempt.kind).toBe('retry');
    expect(JSON.stringify(heartbeats.at(-1))).toContain('"state":"claimed"');
  });

  it('retries an updateTicket refusal with an oversized body', async () => {
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async request => {
      if (request.method === 'GET') return response(200, { id: 7001, status: 'open', created_datetime: '2026-10-20T09:00:00Z' });
      throw new BodyTooLargeError(400, {});
    } });
    const attempt = await client.updateTicket().invoke(mutationCall(1), { ticketId: 7001, status: 'closed' });
    expect(attempt.kind).toBe('retry');
  });

  it.each(credentialFailures)('retries an updateTicket %s on a later attempt, when an earlier change may have applied', async (_name, resolve) => {
    const attempt = await new GorgiasClient({ domain: 'acme' }, { resolve }).updateTicket().invoke(mutationCall(2), { ticketId: 7001, status: 'closed' });
    expect(attempt.kind).toBe('retry');
  });

  it.each(credentialFailures)('maps an updateTicket %s', async (_name, resolve, isTerminal) => {
    const attempt = await new GorgiasClient({ domain: 'acme' }, { resolve }).updateTicket().invoke(mutationCall(1), { ticketId: 7001, status: 'closed' });
    expect(attempt.kind).toBe(isTerminal ? 'branch' : 'retry');
  });

  it('retries without sending after a marker the previous release wrote', async () => {
    let requestCount = 0;
    const client = new GorgiasClient({ domain: 'acme' }, { resolve: async () => credential }, { send: async () => { requestCount++; return response(201); } });
    const attempt = await client.addNote().invoke(mutationCall(2, { gorgiasDispatchedCallId: mutationCallID }), noteInput);
    expect(attempt.kind).toBe('retry');
    expect(attempt.failure?.kind).toBe('AVAILABILITY');
    expect(requestCount).toBe(0);
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
