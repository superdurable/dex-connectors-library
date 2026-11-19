// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, SecretString, type Call, type MutationAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, MaxErrorBodyBytes, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { describe, expect, it } from 'vitest';
import { ZohoDeskClient } from '../src/client.js';
import { CredentialMaterialError, type CredentialSource, type Credentials } from '../src/generated/models.js';

const callId = '6b0f5c4e-3d1e-5c55-9d0e-6e8a7f0c2b11';
const ticketId = '1892000000099004';
const commentId = '4000000529001';
const ticketBody = { id: ticketId, status: 'Open', statusType: 'OPEN', subject: 'Billing question', departmentId: '1892000000006907', createdTime: '2026-01-28T09:00:00.000Z', modifiedTime: '2026-01-28T09:03:00.000Z' };
const commentBody = { id: commentId, isPublic: false, content: 'Triaged.', contentType: 'plainText', commentedTime: '2026-01-28T09:04:00.000Z' };
const createInput = { subject: 'Billing question', description: 'Invoice', departmentId: '1892000000006907', contactId: '1892000000042032' };
const commentInput = { ticketId, content: 'Triaged.' };

const credential = (token = '1000.synthetic-access'): Credentials => ({ authMethodId: 'zoho-us-oauth', accessToken: new SecretString(token) }) as Credentials;
const staticSource: CredentialSource = { resolve: async () => credential(), resolveWithRefresh: async () => credential() };
const refreshFailing = (error: Error): CredentialSource => ({
  resolve: async () => credential('1000.rejected-token'), resolveWithRefresh: async () => credential('1000.rejected-token'),
  resolveAfterRejection: async () => { throw error; },
}) as CredentialSource;
const revokedSource: CredentialSource = {
  resolve: async () => { throw new ReauthorizationRequiredError(); },
  resolveWithRefresh: async () => { throw new ReauthorizationRequiredError(); },
};

/** One Step execution whose last heartbeat persists across attempts, as Dex supplies it. */
class StepExecution {
  heartbeat: unknown = undefined;
  writes = 0;
  rejectsHeartbeatAfter = Infinity;
  attempt = 1;

  call(operationId: string): Call {
    return {
      id: callId, idempotencyKey: callId, connection: { provider: 'zoho', name: 'zoho-desk-test' }, operation: { connectorId: 'zoho-desk', operationId },
      context: {
        attempt: this.attempt, firstAttemptAt: new Date(1_000), cancellationSignal: new AbortController().signal,
        hasLastHeartbeatValue: () => this.heartbeat !== undefined && this.heartbeat !== null,
        getLastHeartbeatValue: () => this.heartbeat,
        recordHeartbeat: async (value: unknown) => {
          if (this.writes >= this.rejectsHeartbeatAfter) throw new Error('heartbeat stream closed');
          this.writes++;
          this.heartbeat = value;
        },
      },
      reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
    } as unknown as Call;
  }

  /** This call's marker in either SDK heartbeat format. */
  get marker(): Record<string, unknown> | undefined {
    const value = this.heartbeat as Record<string, unknown> | undefined;
    if (!value) return undefined;
    if (typeof value.callId === 'string') return value;
    return (value.singleDispatchMarkers as Record<string, Record<string, unknown>> | undefined)?.[callId];
  }

  /** Replace this call's marker fields, keeping the heartbeat format the SDK wrote. */
  rewriteMarker(fields: Record<string, unknown>): void {
    const marker = this.marker;
    if (!marker) throw new Error('no marker to rewrite');
    Object.assign(marker, fields);
  }
}

function json(status: number, body: unknown = {}): ProviderResponse {
  return { status, headers: {}, body: Buffer.from(JSON.stringify(body)) };
}

function deskClient(requests: ProviderRequest[], reply: (request: ProviderRequest, index: number) => Promise<ProviderResponse> | ProviderResponse, credentials = staticSource): ZohoDeskClient {
  return new ZohoDeskClient({ orgId: '2389290' }, credentials, {
    apiBaseURL: 'http://127.0.0.1:12345/api/v1',
    send: async request => { requests.push(request); return reply(request, requests.length - 1); },
  });
}

function outcomeOf(attempt: MutationAttempt<unknown>): string { return attempt.kind === 'branch' ? attempt.branch : attempt.kind; }

describe('Zoho Desk write recovery', () => {
  it('counts an earlier send after a 429 resend records canResend', async () => {
    const statuses = [500, 429, 422];
    const requests: ProviderRequest[] = [];
    const client = deskClient(requests, (_request, index) => json(statuses[index]!));
    const step = new StepExecution();
    expect(outcomeOf(await client.createTicket().invoke(step.call('createTicket'), createInput))).toBe('retry');
    step.attempt = 2;
    expect(outcomeOf(await client.createTicket().invoke(step.call('createTicket'), createInput))).toBe('retry');
    expect(step.marker).toMatchObject({ state: 'canResend', sendCount: 1 });
    step.attempt = 3;
    expect(outcomeOf(await client.createTicket().invoke(step.call('createTicket'), createInput))).toBe('uncertain');
    expect(requests).toHaveLength(3);
  });

  it('never selects a terminal refusal for credential failures after a possible send', async () => {
    const requests: ProviderRequest[] = [];
    const step = new StepExecution();
    const first = deskClient(requests, () => json(500));
    expect(outcomeOf(await first.createTicket().invoke(step.call('createTicket'), createInput))).toBe('retry');
    const writes = step.writes;
    step.attempt = 2;
    const revoked = deskClient(requests, () => json(500), revokedSource);
    expect(outcomeOf(await revoked.createTicket().invoke(step.call('createTicket'), createInput))).toBe('uncertain');
    expect(step.writes).toBe(writes);

    const outage = deskClient(requests, () => json(401), refreshFailing(new Error('Zoho token endpoint returned HTTP 503')));
    const retried = await outage.createTicket().invoke(step.call('createTicket'), createInput);
    expect(retried).toMatchObject({ kind: 'retry', failure: { message: 'Zoho Desk rejected the access token (HTTP 401); credential refresh is temporarily unavailable' } });
    expect(step.marker).toMatchObject({ state: 'claimed' });
  });

  it.each([
    ['missing connection object', () => new ObjectNotFoundError(), 'uncertain'],
    ['unselected connection', () => new ConnectionNotSelectedError(), 'uncertain'],
    ['broker refusal', () => new CredentialBrokerRejectedError(), 'uncertain'],
    ['broker outage', () => new CredentialBrokerUnavailableError(), 'retry'],
    ['unconfigured static connection', () => new Error('connection is not configured'), 'retry'],
    ['malformed stored material', () => new CredentialMaterialError('credential access_token is required'), 'uncertain'],
    ['SDK-wrapped malformed material', () => new TypeError('project credential decoding failed'), 'uncertain'],
  ] as const)('maps a %s after a possible send without recording a checkpoint', async (_name, error, outcome) => {
    const requests: ProviderRequest[] = [];
    const step = new StepExecution();
    expect(outcomeOf(await deskClient(requests, () => json(500)).createTicket().invoke(step.call('createTicket'), createInput))).toBe('retry');
    const writes = step.writes;
    step.attempt = 2;
    const failing: CredentialSource = { resolve: async () => { throw error(); }, resolveWithRefresh: async () => { throw error(); } };
    expect(outcomeOf(await deskClient(requests, () => json(500), failing).createTicket().invoke(step.call('createTicket'), createInput))).toBe(outcome);
    expect(step.writes).toBe(writes);
    expect(requests).toHaveLength(1);
  });

  it('records canResend and retries a first 401 whose refresh fails transiently', async () => {
    const step = new StepExecution();
    const client = deskClient([], () => json(401), refreshFailing(new Error('Zoho token endpoint returned HTTP 503')));
    expect(outcomeOf(await client.createTicket().invoke(step.call('createTicket'), createInput))).toBe('retry');
    expect(step.marker).toMatchObject({ state: 'canResend' });
  });

  it('never resends a 2xx whose body breaks after the headers', async () => {
    const requests: ProviderRequest[] = [];
    const client = deskClient(requests, request => { request.onResponseChunk?.(Buffer.from('{"id":'), 200, {}); throw new ProviderTransportError(false, 200, {}); });
    const step = new StepExecution();
    expect(outcomeOf(await client.createTicket().invoke(step.call('createTicket'), createInput))).toBe('uncertain');
    expect(requests).toHaveLength(1);
  });

  it('never resends a 2xx whose body breaks before any byte', async () => {
    const client = deskClient([], () => { throw new ProviderTransportError(false, 201, {}); });
    expect(outcomeOf(await client.addComment().invoke(new StepExecution().call('addComment'), commentInput))).toBe('uncertain');
  });

  it('never resends a 2xx without a usable ID', async () => {
    const client = deskClient([], () => json(200, {}));
    expect(outcomeOf(await client.addComment().invoke(new StepExecution().call('addComment'), commentInput))).toBe('uncertain');
  });

  it('reads an accepted ticket ID back in the same attempt and on later attempts', async () => {
    const requests: ProviderRequest[] = [];
    const client = deskClient(requests, (request, index) => request.method === 'POST' ? json(200, { id: ticketId, subject: 'Billing question' }) : index === 1 ? json(500) : json(200, ticketBody));
    const step = new StepExecution();
    expect(outcomeOf(await client.createTicket().invoke(step.call('createTicket'), createInput))).toBe('retry');
    expect(step.marker).toMatchObject({ providerId: ticketId });
    step.attempt = 2;
    expect(outcomeOf(await client.createTicket().invoke(step.call('createTicket'), createInput))).toBe('created');
    expect(requests.map(request => request.method)).toEqual(['POST', 'GET', 'GET']);
    expect(new URL(requests[2]!.url).pathname).toBe(`/api/v1/tickets/${ticketId}`);
  });

  it.each([
    ['createTicket', ticketBody, `/api/v1/tickets/${ticketId}`, 'created'],
    ['addComment', commentBody, `/api/v1/tickets/${ticketId}/comments/${commentId}`, 'added'],
  ] as const)('reads a saved %s ID back at the send cap', async (operationId, body, path, success) => {
    for (const [status, expected] of [[200, success], [503, 'retry'], [404, 'uncertain'], [403, 'uncertain']] as const) {
      const step = new StepExecution();
      const accepting = deskClient([], () => json(200, body));
      const write = (client: ZohoDeskClient) => operationId === 'createTicket'
        ? client.createTicket().invoke(step.call(operationId), createInput)
        : client.addComment().invoke(step.call(operationId), commentInput);
      await write(accepting);
      step.rewriteMarker({ attempt: 3, sendCount: 3 });
      step.attempt = 4;
      const requests: ProviderRequest[] = [];
      const reading = deskClient(requests, () => json(status, status === 200 ? body : {}));
      expect(outcomeOf(await write(reading)), `${operationId} ${status}`).toBe(expected);
      expect(requests.map(request => [request.method, new URL(request.url).pathname])).toEqual([['GET', path]]);
    }
  });

  it('never retries a failed read-back when the provider ID was not saved', async () => {
    const requests: ProviderRequest[] = [];
    const client = deskClient(requests, request => request.method === 'POST' ? json(201, { id: commentId, isPublic: 'maybe' }) : json(500));
    const step = new StepExecution();
    step.rejectsHeartbeatAfter = 1;
    expect(outcomeOf(await client.addComment().invoke(step.call('addComment'), commentInput))).toBe('uncertain');
    expect(requests).toHaveLength(2);
  });

  it.each([
    ['a body truncated at the error body limit', () => ({ status: 400, headers: {}, body: Buffer.alloc(MaxErrorBodyBytes, 0x78) })],
    ['an oversized body', () => { throw new BodyTooLargeError(422, {}); }],
    ['a body that reflects the token', () => json(404, { errorCode: 'URL_NOT_FOUND', message: '1000.synthetic-access' })],
  ] as const)('treats a refusal with %s as unknown: resend below the cap, uncertain at it', async (_name, reply) => {
    const requests: ProviderRequest[] = [];
    const client = deskClient(requests, () => reply());
    const step = new StepExecution();
    for (const attempt of [1, 2]) {
      step.attempt = attempt;
      expect(outcomeOf(await client.addComment().invoke(step.call('addComment'), commentInput))).toBe('retry');
      expect(step.marker).toMatchObject({ state: 'claimed' });
    }
    step.attempt = 3;
    expect(outcomeOf(await client.addComment().invoke(step.call('addComment'), commentInput))).toBe('uncertain');
    expect(requests).toHaveLength(3);
  });

  it.each([
    ['createTicket', ticketId, ticketBody, `/api/v1/tickets/${ticketId}`, 'created'],
    ['addComment', commentId, commentBody, `/api/v1/tickets/${ticketId}/comments/${commentId}`, 'added'],
  ] as const)('reads %s back by the created id a refusal body carries', async (operationId, createdId, body, path, success) => {
    const requests: ProviderRequest[] = [];
    const client = deskClient(requests, request => request.method === 'POST' ? json(422, { id: createdId, errorCode: 'INVALID_DATA' }) : json(200, body));
    const step = new StepExecution();
    const attempt = operationId === 'createTicket'
      ? await client.createTicket().invoke(step.call(operationId), createInput)
      : await client.addComment().invoke(step.call(operationId), commentInput);
    expect(outcomeOf(attempt)).toBe(success);
    expect(step.marker).toMatchObject({ providerId: createdId });
    expect(requests.map(request => [request.method, new URL(request.url).pathname])).toEqual([['POST', operationId === 'createTicket' ? '/api/v1/tickets' : `/api/v1/tickets/${ticketId}/comments`], ['GET', path]]);
  });

  it('records canResend and retries a rate limit whose body breaks', async () => {
    const client = deskClient([], () => { throw new ProviderTransportError(false, 429, {}); });
    const step = new StepExecution();
    expect(outcomeOf(await client.createTicket().invoke(step.call('createTicket'), createInput))).toBe('retry');
    expect(step.marker).toMatchObject({ state: 'canResend' });
  });

  it('retries a revoked updateTicket credential after the first attempt', async () => {
    const requests: ProviderRequest[] = [];
    const client = deskClient(requests, () => json(200, ticketBody), revokedSource);
    const step = new StepExecution();
    expect(outcomeOf(await client.updateTicket().invoke(step.call('updateTicket'), { ticketId, status: 'Closed' }))).toBe('providerRejected');
    step.attempt = 2;
    expect(outcomeOf(await client.updateTicket().invoke(step.call('updateTicket'), { ticketId, status: 'Closed' }))).toBe('retry');
    expect(requests).toHaveLength(0);
  });

  it('repeats the updateTicket change when its refusal body cannot prove a refusal', async () => {
    const requests: ProviderRequest[] = [];
    const client = deskClient(requests, request => request.method === 'GET' ? json(200, ticketBody) : { status: 400, headers: {}, body: Buffer.alloc(MaxErrorBodyBytes, 0x78) });
    const attempt = await client.updateTicket().invoke(new StepExecution().call('updateTicket'), { ticketId, status: 'Closed' });
    expect(attempt.kind).toBe('retry');
    expect(requests).toHaveLength(2);
  });

  it('retries an unusable read before updateTicket changes anything', async () => {
    const requests: ProviderRequest[] = [];
    const client = deskClient(requests, () => json(200, {}));
    const attempt = await client.updateTicket().invoke(new StepExecution().call('updateTicket'), { ticketId, status: 'Closed' });
    expect(attempt.kind).toBe('retry');
    expect(requests).toHaveLength(1);
  });
});
