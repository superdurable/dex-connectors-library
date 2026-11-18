// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { FreshdeskClient } from '../src/client.js';
import type { AddNoteOutput, CreateTicketOutput, Credentials } from '../src/generated/models.js';

const callID = '703d7ec0-692e-50db-a914-86d5fecf7dcc';
const createInput = { subject: 'Billing issue', description: 'Charged twice', requester: { email: 'jane@example.com' } };
const noteInput = { ticketId: 1201, body: 'Hello' };
const ticketWire = { id: 1207, subject: 'Billing issue', description_text: 'Charged twice', status: 2, priority: 1, requester_id: 6007738334, created_at: '2026-01-29T10:00:00Z', updated_at: '2026-01-29T10:00:00Z' };
const staticCredentials: CredentialProvider<Credentials> = { resolve: async () => ({ apiKey: new SecretString('inertKey') }) };

type Responder = (request: ProviderRequest, index: number) => Promise<ProviderResponse> | ProviderResponse;

/** Execute attempts of one Step that keep the last heartbeat any earlier attempt stored, as Dex does. */
class AttemptSequence {
  attempt = 0;
  lastHeartbeat: unknown = undefined;
  heartbeatWrites = 0;
  cancellationSignal: AbortSignal = new AbortController().signal;
  readonly requests: ProviderRequest[] = [];

  constructor(private readonly respond: Responder) {}

  /** Run the next attempt of an operation with optional failing credentials. */
  async run(operation: 'createTicket' | 'addNote' | 'updateTicket', input: unknown, credentials: CredentialProvider<Credentials> = staticCredentials) {
    this.attempt++;
    const client = new FreshdeskClient({ domain: 'acme', maxResponseBytes: 2048 }, credentials, {
      send: async request => { this.requests.push(request); return this.respond(request, this.requests.length - 1); },
    });
    const call = {
      id: callID, connection: { provider: 'freshdesk', name: 'freshdesk-test' }, operation: { connector: 'freshdesk', name: operation },
      context: {
        attempt: this.attempt, cancellationSignal: this.cancellationSignal,
        hasLastHeartbeatValue: () => this.lastHeartbeat !== undefined, getLastHeartbeatValue: () => this.lastHeartbeat,
        recordHeartbeat: async (value: unknown) => { this.lastHeartbeat = value; this.heartbeatWrites++; },
      },
    } as unknown as Call;
    if (operation === 'createTicket') return client.createTicket().invoke(call, input as typeof createInput);
    if (operation === 'addNote') return client.addNote().invoke(call, input as typeof noteInput);
    return client.updateTicket().invoke(call, input as { ticketId: number; status: number });
  }

  /** Return the last heartbeat as JSON for marker assertions. */
  heartbeatJSON(): string { return JSON.stringify(this.lastHeartbeat ?? null); }
}

function json(status: number, body: unknown, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers, body: Buffer.from(JSON.stringify(body)) };
}

describe('Freshdesk accepted and possibly sent writes', () => {
  for (const [name, failure] of [['transport', new ProviderTransportError(false, 201, {})], ['stall', new ProviderStalledError(201, {})]] as const) {
    for (const operation of ['createTicket', 'addNote'] as const) {
      it(`treats 2xx headers followed by a broken body (${name}) as an accepted ${operation} that is never resent`, async () => {
        const sequence = new AttemptSequence(() => { throw failure; });
        const attempt = await sequence.run(operation, operation === 'createTicket' ? createInput : noteInput);
        expect(attempt.kind).toBe('uncertain');
        expect(attempt.failure?.kind).toBe('TRANSPORT');
        expect(sequence.requests).toHaveLength(1);
      });
    }
  }

  it('reads an accepted ticket ID back in the same attempt', async () => {
    const sequence = new AttemptSequence(request => request.method === 'POST' ? json(201, { id: 1207, created_at: 'SENTINEL' }) : json(200, ticketWire));
    const attempt = await sequence.run('createTicket', createInput);
    expect(attempt.kind).toBe('branch');
    if (attempt.kind !== 'branch') throw new Error('expected created');
    expect(attempt.branch).toBe('created');
    expect((attempt.value as CreateTicketOutput).ticket.id).toBe(1207);
    expect(sequence.requests.map(request => `${request.method} ${new URL(request.url).pathname}`)).toEqual(['POST /api/v2/tickets', 'GET /api/v2/tickets/1207']);
  });

  it('reads a saved ticket ID back on a later attempt and never resends it', async () => {
    const sequence = new AttemptSequence((request, index) => request.method === 'POST' ? json(201, { id: 1207 }) : index === 1 ? json(503, {}) : json(200, ticketWire));
    const first = await sequence.run('createTicket', createInput);
    expect(first.kind).toBe('retry');
    expect(sequence.heartbeatJSON()).toContain('"providerId":"1207"');
    const second = await sequence.run('createTicket', createInput);
    expect(second.kind).toBe('branch');
    if (second.kind !== 'branch') throw new Error('expected created');
    expect(second.branch).toBe('created');
    expect(sequence.requests.map(request => request.method)).toEqual(['POST', 'GET', 'GET']);
  });

  it('selects uncertain when the accepted ticket reads back as missing', async () => {
    const sequence = new AttemptSequence(request => request.method === 'POST' ? json(201, { id: 1207 }) : json(404, {}));
    const attempt = await sequence.run('createTicket', createInput);
    expect(attempt.kind).toBe('uncertain');
    expect(attempt.failure?.kind).toBe('NOT_FOUND');
  });

  it('finds a saved note ID across conversation pages', async () => {
    const sequence = new AttemptSequence((request, index) => {
      if (request.method === 'POST') return json(201, { id: 9003, created_at: 'SENTINEL' });
      if (index === 1) return json(500, {});
      if (new URL(request.url).searchParams.get('page') === '1') return json(200, [{ id: 9001, ticket_id: 1201, created_at: '2026-01-27T10:00:00Z' }], { link: '<https://acme.freshdesk.com/api/v2/tickets/1201/conversations?page=2>; rel="next"' });
      return json(200, [{ id: 9003, ticket_id: 1201, source: 2, private: true, body_text: 'Hello', created_at: '2026-01-27T10:00:00Z' }]);
    });
    expect((await sequence.run('addNote', noteInput)).kind).toBe('retry');
    const second = await sequence.run('addNote', noteInput);
    expect(second.kind).toBe('branch');
    if (second.kind !== 'branch') throw new Error('expected added');
    expect(second.branch).toBe('added');
    expect((second.value as AddNoteOutput).conversation).toMatchObject({ id: 9003, isPrivate: true, body: 'Hello' });
    expect(sequence.requests.map(request => request.method)).toEqual(['POST', 'GET', 'GET', 'GET']);
    const pages = sequence.requests.slice(2).map(request => new URL(request.url).searchParams);
    expect(pages.map(page => [page.get('per_page'), page.get('page')])).toEqual([['100', '1'], ['100', '2']]);
  });

  async function unknownSendThenRateLimitedResend(sequence: AttemptSequence): Promise<void> {
    expect((await sequence.run('createTicket', createInput)).kind).toBe('retry');
    expect((await sequence.run('createTicket', createInput)).kind).toBe('retry');
    expect(sequence.heartbeatJSON()).toContain('"state":"canResend"');
    expect(sequence.heartbeatJSON()).toContain('"sendCount":1');
  }

  function unknownThenRateLimited(third: ProviderResponse): Responder {
    return (_, index) => index === 0 ? json(500, {}) : index === 1 ? json(429, {}) : third;
  }

  it('selects uncertain for a refusal after an unknown send and a rate-limited resend', async () => {
    const sequence = new AttemptSequence(unknownThenRateLimited(json(400, { errors: [{ field: 'group_id' }] })));
    await unknownSendThenRateLimitedResend(sequence);
    const third = await sequence.run('createTicket', createInput);
    expect(third.kind).toBe('uncertain');
    expect(sequence.requests).toHaveLength(3);
  });

  for (const [name, error, kind] of [
    ['terminal', new ReauthorizationRequiredError(), 'uncertain'], ['missing connection object', new ObjectNotFoundError(), 'uncertain'],
    ['unselected connection', new ConnectionNotSelectedError(), 'uncertain'], ['broker refusal', new CredentialBrokerRejectedError(), 'uncertain'],
    ['transient', new Error('store unavailable'), 'retry'], ['broker outage', new CredentialBrokerUnavailableError(), 'retry'],
    ['unconfigured static connection', new Error('connection is not configured'), 'retry'],
  ] as const) {
    it(`records nothing for a ${name} credential failure after a possible send`, async () => {
      const sequence = new AttemptSequence(unknownThenRateLimited(json(201, ticketWire)));
      await unknownSendThenRateLimitedResend(sequence);
      const writes = sequence.heartbeatWrites;
      const third = await sequence.run('createTicket', createInput, { resolve: async () => { throw error; } });
      expect(third.kind).toBe(kind);
      expect(sequence.heartbeatWrites).toBe(writes);
      expect(sequence.requests).toHaveLength(2);
    });
  }

  it('passes the fence write-window signal to the provider request', async () => {
    let signal: AbortSignal | undefined;
    const sequence = new AttemptSequence(request => { signal = request.signal; return json(201, ticketWire); });
    expect((await sequence.run('createTicket', createInput)).kind).toBe('branch');
    expect(signal).toBeDefined();
    expect(signal).not.toBe(sequence.cancellationSignal);
  });

  it('treats an oversized refusal as an unknown outcome', async () => {
    const created = new AttemptSequence(() => { throw new BodyTooLargeError(400, {}); });
    expect((await created.run('createTicket', createInput)).kind).toBe('retry');
    expect(created.heartbeatJSON()).toContain('"state":"claimed"');
    const updated = new AttemptSequence(request => { if (request.method === 'GET') return json(200, { ...ticketWire, status: 3 }); throw new BodyTooLargeError(400, {}); });
    expect((await updated.run('updateTicket', { ticketId: 1207, status: 2 })).kind).toBe('retry');
  });

  for (const [status, body, branch] of [
    [404, {}, 'notFound'], [403, {}, 'providerRejected'], [200, { id: 1207 }, ''], [409, {}, ''], [500, {}, ''], [301, {}, ''],
    [400, { description: 'inertKey' }, ''],
  ] as const) {
    it(`selects an update branch on the read only for a definite answer: HTTP ${status} ${JSON.stringify(body)}`, async () => {
      const sequence = new AttemptSequence(() => json(status, body));
      const attempt = await sequence.run('updateTicket', { ticketId: 1207, status: 2 });
      expect(sequence.requests.map(request => request.method)).toEqual(['GET']);
      if (!branch) expect(attempt.kind).toBe('retry');
      else expect(attempt.kind === 'branch' && attempt.branch).toBe(branch);
    });
  }

  it('selects defect for a terminal credential failure only on the first update attempt', async () => {
    const reconnect: CredentialProvider<Credentials> = { resolve: async () => { throw new ReauthorizationRequiredError(); } };
    const sequence = new AttemptSequence(() => { throw new Error('no request is expected'); });
    const first = await sequence.run('updateTicket', { ticketId: 1207, status: 2 }, reconnect);
    expect(first.kind === 'branch' && first.branch).toBe('defect');
    // A later attempt may follow a PUT that took effect, so it retries instead.
    const second = await sequence.run('updateTicket', { ticketId: 1207, status: 2 }, reconnect);
    expect(second.kind).toBe('retry');
    expect(sequence.requests).toHaveLength(0);
  });
});
