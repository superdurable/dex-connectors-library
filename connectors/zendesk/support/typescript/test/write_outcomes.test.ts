// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { z } from 'zod';
import { ReauthorizationRequiredError, RetryError, runMutation, SecretString, type Call, type MutationResult } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { ZendeskClient } from '../src/client.js';
import { CreateTicketDefinition, UpdateTicketDefinition } from '../src/generated/definitions.js';
import { CredentialMaterialError, type CreateTicketOutput, type UpdateTicketInput, type UpdateTicketOutput } from '../src/generated/models.js';

const callId = '6a0e7f8c-2f0b-5d7e-9a51-2f4c8e1b3d77';
const update: UpdateTicketInput = { ticketId: 5512, status: 'open', addTags: ['escalated'], comment: { body: 'Customer called again.', isInternalNote: true } };
const emptyAudits = { audits: [] };
const keyedAudits = { audits: [{ id: 10, metadata: { custom: { dex_idempotency_key: callId } } }] };

function ticket(status = 'pending'): unknown {
  return { ticket: { id: 5512, status, tags: ['billing'], created_at: '2026-01-26T14:02:00Z', updated_at: '2026-01-28T08:45:00Z' } };
}

interface MarkerView { state?: string; providerId?: string; sendCount?: number }

class AttemptContext {
  attempt = 1;
  heartbeat: unknown;
  heartbeatCount = 0;
  readonly runId = 'run';
  readonly flowId = 'zendesk-write-outcomes';
  readonly flowStartedAt = new Date('1970-01-01T00:00:01Z');
  readonly deadline: Date;
  readonly cancellationSignal = new AbortController().signal;
  constructor(deadlineMs = 30_000) { this.deadline = new Date(Date.now() + deadlineMs); }
  hasLastHeartbeatValue(): boolean { return this.heartbeat !== undefined; }
  getLastHeartbeatValue(): unknown { return this.heartbeat; }
  async recordHeartbeat(value: unknown): Promise<void> { this.heartbeat = value; this.heartbeatCount++; }
  /** Decode this call's marker from a flat or per-call map heartbeat. */
  get marker(): MarkerView | undefined {
    const value = this.heartbeat as Record<string, unknown> | undefined;
    if (!value) return undefined;
    const markers = value.singleDispatchMarkers as Record<string, MarkerView> | undefined;
    return markers ? markers[callId] : value as MarkerView;
  }
}

type Answer = (request: ProviderRequest, index: number) => Promise<ProviderResponse> | ProviderResponse;

function json(status: number, body: unknown): ProviderResponse { return { status, headers: {}, body: Buffer.from(JSON.stringify(body)) }; }

/** Answer each attempt's ticket read, audit page, and PUT; audits and writes are numbered from 1 across attempts. */
function updateAnswers(write: (count: number) => Promise<ProviderResponse> | ProviderResponse, audits: (count: number) => unknown): Answer {
  let auditCount = 0;
  let writeCount = 0;
  return request => {
    if (request.method === 'PUT') return write(++writeCount);
    if (request.url.includes('/audits')) return json(200, audits(++auditCount));
    return json(200, ticket());
  };
}

class SwitchableCredentials {
  error: Error | undefined;
  async resolve() {
    if (this.error) throw this.error;
    return { email: 'agent@example.com', apiToken: new SecretString('inert-token') };
  }
}

async function run(operation: 'updateTicket' | 'createTicket', answer: Answer, context: AttemptContext, requests: ProviderRequest[],
  credentials = new SwitchableCredentials(), maxResponseBytes?: number): Promise<MutationResult<UpdateTicketOutput> | MutationResult<CreateTicketOutput> | RetryError> {
  const client = new ZendeskClient({ subdomain: 'acme', ...(maxResponseBytes ? { maxResponseBytes } : {}) }, credentials, {
    apiBaseURL: 'http://127.0.0.1:37001/api/v2',
    send: async request => { requests.push(request); return answer(request, requests.length - 1); },
  });
  const definition = operation === 'updateTicket' ? UpdateTicketDefinition : CreateTicketDefinition;
  const call = { id: callId, connection: { provider: 'zendesk', name: 'desk' }, operation: definition.operation, context } as unknown as Call;
  try {
    if (operation === 'createTicket') return await runMutation(client.createTicket(), call, { subject: 'Double charge', comment: { body: 'Charged twice' } });
    return await runMutation(client.updateTicket(), call, update);
  } catch (error) {
    if (error instanceof RetryError) return error;
    throw error;
  }
}

function branchOf(result: MutationResult<unknown> | RetryError): string { return result instanceof RetryError ? 'retry' : result.branch; }
const brokenBody = () => { throw new ProviderTransportError(false, 200, {}); };
const stalledBody = () => { throw new ProviderStalledError(200, {}); };
const undecodableBody = () => json(200, { wrong: true });

describe('updateTicket after a possible or accepted write', () => {
  it('reads back before resending after a rate-limited resend', async () => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    const answer = updateAnswers(count => json(count === 1 ? 500 : 429, { error: 'x' }), count => (count === 3 ? keyedAudits : emptyAudits));
    for (const attempt of [1, 2]) { context.attempt = attempt; expect(branchOf(await run('updateTicket', answer, context, requests))).toBe('retry'); }
    expect(context.marker?.state).toBe('canResend');
    context.attempt = 3;
    const result = await run('updateTicket', answer, context, requests);
    expect(branchOf(result)).toBe('updated');
    expect((result as MutationResult<UpdateTicketOutput>).value.wasAlreadyApplied).toBe(true);
    expect(requests.map(request => request.method).slice(6)).toEqual(['GET', 'GET']);
  });

  it.each([
    ['terminal', new ReauthorizationRequiredError(), 'uncertain'],
    ['missing connection object', new ObjectNotFoundError(), 'uncertain'],
    ['unselected connection', new ConnectionNotSelectedError(), 'uncertain'],
    ['broker refusal', new CredentialBrokerRejectedError(), 'uncertain'],
    ['transient', new Error('broker unavailable'), 'retry'],
    ['broker outage', new CredentialBrokerUnavailableError(), 'retry'],
    ['unconfigured static connection', new Error('connection is not configured'), 'retry'],
    ['malformed stored material', new CredentialMaterialError('credential api_token is required'), 'uncertain'],
  ])('never selects defect for a %s credential failure after a possible send', async (_name, error, branch) => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    const credentials = new SwitchableCredentials();
    const answer = updateAnswers(() => json(500, { error: 'x' }), () => emptyAudits);
    expect(branchOf(await run('updateTicket', answer, context, requests, credentials))).toBe('retry');
    const heartbeats = context.heartbeatCount;
    context.attempt = 2;
    credentials.error = error;
    expect(branchOf(await run('updateTicket', answer, context, requests, credentials))).toBe(branch);
    expect(context.heartbeatCount).toBe(heartbeats);
    expect(requests).toHaveLength(3);
  });

  it.each([
    ['terminal', new ReauthorizationRequiredError(), 'defect'],
    ['missing connection object', new ObjectNotFoundError(), 'defect'],
    ['unselected connection', new ConnectionNotSelectedError(), 'defect'],
    ['broker refusal', new CredentialBrokerRejectedError(), 'defect'],
    ['transient', new Error('broker unavailable'), 'retry'],
    ['broker outage', new CredentialBrokerUnavailableError(), 'retry'],
    ['malformed stored material', new CredentialMaterialError('credential api_token is required'), 'defect'],
    ['SDK-wrapped malformed material', new TypeError('project credential decoding failed'), 'defect'],
    ['project material the schema rejects', new z.ZodError([]), 'defect'],
  ])('records notSent for a %s credential failure on a first send', async (_name, error, branch) => {
    const credentials = new SwitchableCredentials();
    credentials.error = error;
    const context = new AttemptContext();
    const requests: ProviderRequest[] = [];
    expect(branchOf(await run('updateTicket', () => json(200, ticket()), context, requests, credentials))).toBe(branch);
    expect(context.marker?.state).toBe('notSent');
    expect(requests).toHaveLength(0);
  });

  it.each([
    ['terminal', new ReauthorizationRequiredError(), 'defect'],
    ['broker refusal', new CredentialBrokerRejectedError(), 'defect'],
    ['malformed stored material', new CredentialMaterialError('credential api_token is required'), 'defect'],
    ['SDK-wrapped malformed material', new TypeError('project credential decoding failed'), 'defect'],
    ['broker outage', new CredentialBrokerUnavailableError(), 'retry'],
    ['unconfigured static connection', new Error('connection is not configured'), 'retry'],
  ])('selects defect in a Query only for a %s credential failure', async (_name, error, expected) => {
    const credentials = new SwitchableCredentials();
    credentials.error = error;
    const requests: ProviderRequest[] = [];
    const client = new ZendeskClient({ subdomain: 'acme' }, credentials, {
      apiBaseURL: 'http://127.0.0.1:37001/api/v2', send: async request => { requests.push(request); return json(500, {}); },
    });
    const context = new AttemptContext();
    const get = await client.getTicket().invoke({ id: callId, connection: { provider: 'zendesk', name: 'desk' }, operation: { connectorId: 'zendesk-support', operationId: 'getTicket' }, context } as unknown as Call, { ticketId: 5512 });
    const search = await client.searchTickets().invoke({ id: callId, connection: { provider: 'zendesk', name: 'desk' }, operation: { connectorId: 'zendesk-support', operationId: 'searchTickets' }, context } as unknown as Call, { statuses: ['open'] });
    for (const result of [get, search]) expect(result.kind === 'branch' ? result.branch : result.kind).toBe(expected);
    expect(requests).toHaveLength(0);
  });

  it.each([
    ['body breaks after 2xx headers', brokenBody],
    ['2xx body stalls', stalledBody],
    ['undecodable 2xx', undecodableBody],
  ])('reads back an accepted write in the same attempt when %s', async (_name, write) => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    const result = await run('updateTicket', updateAnswers(write, count => (count === 2 ? keyedAudits : emptyAudits)), context, requests);
    expect(branchOf(result)).toBe('updated');
    expect((result as MutationResult<UpdateTicketOutput>).value.wasAlreadyApplied).toBeUndefined();
    expect(requests).toHaveLength(5);
    expect(context.marker?.providerId).toBe('5512');
  });

  it('selects uncertain without resending when an accepted write cannot be confirmed', async () => {
    const requests: ProviderRequest[] = [];
    expect(branchOf(await run('updateTicket', updateAnswers(undecodableBody, () => emptyAudits), new AttemptContext(), requests))).toBe('uncertain');
    expect(requests.filter(request => request.method === 'PUT')).toHaveLength(1);
  });

  it('retries a transient read-back of an accepted write and later only reads back', async () => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    const base = updateAnswers(undecodableBody, count => (count > 1 ? keyedAudits : emptyAudits));
    const answer: Answer = (request, index) => (index === 3 ? json(503, { error: 'Unavailable' }) : base(request, index));
    expect(branchOf(await run('updateTicket', answer, context, requests))).toBe('retry');
    expect(context.marker?.providerId).toBe('5512');
    const heartbeats = context.heartbeatCount;
    context.attempt = 2;
    const result = await run('updateTicket', answer, context, requests);
    expect(branchOf(result)).toBe('updated');
    expect((result as MutationResult<UpdateTicketOutput>).value.wasAlreadyApplied).toBe(true);
    expect(requests).toHaveLength(6);
    expect(requests.filter(request => request.method === 'PUT')).toHaveLength(1);
    expect(context.heartbeatCount).toBe(heartbeats);
  });

  it('selects uncertain when a saved ticket ID reads back a missing ticket', async () => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    const base = updateAnswers(undecodableBody, () => emptyAudits);
    const answer: Answer = (request, index) => (index === 3 ? json(502, { error: 'BadGateway' }) : index > 3 ? json(404, { error: 'RecordNotFound' }) : base(request, index));
    expect(branchOf(await run('updateTicket', answer, context, requests))).toBe('retry');
    context.attempt = 2;
    expect(branchOf(await run('updateTicket', answer, context, requests))).toBe('uncertain');
    expect(requests).toHaveLength(5);
  });

  it('treats an aborted write window as an unknown outcome', async () => {
    const context = new AttemptContext();
    const answer = updateAnswers(() => { throw new ProviderTransportError(false); }, () => emptyAudits);
    expect(branchOf(await run('updateTicket', answer, context, []))).toBe('retry');
    expect(context.marker?.state).toBe('claimed');
  });

  it('reads back without resending when the write deadline cuts the body after 2xx headers', async () => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext(1_000);
    // Like providerhttp, an abort during the body keeps the 2xx status.
    const cutBody = (request: ProviderRequest) => new Promise<ProviderResponse>((_resolve, reject) => {
      request.signal?.addEventListener('abort', () => reject(new ProviderTransportError(false, 200, {})), { once: true });
    });
    const base = updateAnswers(() => json(500, {}), count => (count === 2 ? keyedAudits : emptyAudits));
    const answer: Answer = (request, index) => (request.method === 'PUT' ? cutBody(request) : base(request, index));
    const result = await run('updateTicket', answer, context, requests);
    expect(branchOf(result)).toBe('updated');
    expect(requests.map(request => request.method)).toEqual(['GET', 'GET', 'PUT', 'GET', 'GET']);
    expect(context.marker?.providerId).toBe('5512');
  });

  it.each([
    ['finds the change', true, 'updated'],
    ['finds no change', false, 'uncertain'],
  ])('reads back after an unknown outcome of the last allowed send and %s', async (_name, isChangeFound, branch) => {
    const requests: ProviderRequest[] = [];
    const context = new AttemptContext();
    const answer = updateAnswers(count => json([502, 409, 500][count - 1]!, { error: 'x' }), count => (isChangeFound && count === 4 ? keyedAudits : emptyAudits));
    for (const attempt of [1, 2, 3]) { context.attempt = attempt; expect(branchOf(await run('updateTicket', answer, context, requests))).toBe('retry'); }
    expect(requests.filter(request => request.method === 'PUT')).toHaveLength(3);
    context.attempt = 4;
    expect(branchOf(await run('updateTicket', answer, context, requests))).toBe(branch);
    expect(requests.map(request => request.method).slice(9)).toEqual(['GET', 'GET']);
  });
});

describe('refusals with an unreadable or contradictory body', () => {
  const oversized = (): ProviderResponse => { throw new BodyTooLargeError(422, {}); };
  const carryingTicket = (): ProviderResponse => json(422, { error: 'RecordInvalid', ticket: { id: 5512 } });
  it.each([
    ['updateTicket', 'oversized', oversized],
    ['updateTicket', 'carrying a ticket', carryingTicket],
    ['createTicket', 'oversized', oversized],
    ['createTicket', 'carrying a ticket', carryingTicket],
  ] as const)('%s retries a 422 %s', async (operation, _name, refusal) => {
    const answer: Answer = request => {
      if (request.method === 'PUT' || request.method === 'POST') return refusal();
      if (request.url.includes('/audits')) return json(200, emptyAudits);
      return json(200, ticket());
    };
    const context = new AttemptContext();
    expect(branchOf(await run(operation, answer, context, []))).toBe('retry');
    if (operation === 'updateTicket') expect(context.marker?.state).toBe('claimed');
  });
});

describe('createTicket credential failures', () => {
  it.each([
    ['terminal on the first attempt', new ReauthorizationRequiredError(), 1, 'defect'],
    ['transient on the first attempt', new Error('broker unavailable'), 1, 'retry'],
    ['terminal on a later attempt', new ReauthorizationRequiredError(), 2, 'retry'],
  ])('maps a credential failure %s', async (_name, error, attempt, branch) => {
    const credentials = new SwitchableCredentials();
    credentials.error = error;
    const context = new AttemptContext();
    context.attempt = attempt;
    const requests: ProviderRequest[] = [];
    expect(branchOf(await run('createTicket', () => json(201, ticket()), context, requests, credentials))).toBe(branch);
    expect(requests).toHaveLength(0);
  });
});
