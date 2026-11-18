// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { HeartbeatContext } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { HiverClient } from '../src/client.js';
import type { Credentials } from '../src/generated/models.js';

const callId = '53fa38a4-5e96-56e2-81ac-2b8ddb6e38f2';
const connection = { provider: 'hiver', name: 'hiver-test' };
const noteInput = { inboxId: '105902', conversationId: '573741352', content: 'Triaged.' };
const draftInput = { inboxId: '105902', hiverMessageId: '834466048', body: 'Thanks.' };
const staticCredentials = (apiKey = 'hiverTestKey0123456789'): CredentialProvider<Credentials> => ({ resolve: async () => ({ apiKey: new SecretString(apiKey) }) });
const missingCredentials: CredentialProvider<Credentials> = { resolve: async () => { throw new Error('connection is not configured'); } };
const revokedCredentials: CredentialProvider<Credentials> = { resolve: async () => { throw new ReauthorizationRequiredError(); } };
const json = (status: number, body = '{}'): ProviderResponse => ({ status, headers: { 'content-type': 'application/json' }, body: Buffer.from(body) });

/** Answers each request with the next reply, repeating the last one. */
class Provider {
  requestCount = 0;
  constructor(private readonly replies: Array<() => ProviderResponse | Promise<ProviderResponse>>) {}
  send = async (): Promise<ProviderResponse> => this.replies[Math.min(this.requestCount++, this.replies.length - 1)]!();
}

function newClient(provider: Provider, credentials = staticCredentials()): HiverClient {
  return new HiverClient({ requestIntervalMilliseconds: 1 }, credentials, { apiBaseURL: 'http://127.0.0.1:12345/v1', send: provider.send });
}

function call(context: HeartbeatContext, operationId: string): Call {
  return { id: callId, connection, operation: { connectorId: 'hiver', operationId }, context } as unknown as Call;
}

/** Reads this call's marker state from a flat marker or a per-call checkpoint map. */
function markerState(context: HeartbeatContext): string {
  const value = context.marker as Record<string, unknown>;
  const markers = value.singleDispatchMarkers as Record<string, Record<string, unknown>> | undefined;
  return String((markers?.[callId] ?? value).state);
}

function nextAttempt(context: HeartbeatContext): void {
  context.attempt++;
  context.hasWrittenHeartbeat = false;
}

async function unknownThenRateLimited(provider: Provider, context: HeartbeatContext): Promise<void> {
  expect((await newClient(provider).addNote().invoke(call(context, 'addNote'), noteInput)).kind).toBe('retry');
  nextAttempt(context);
  expect((await newClient(provider).addNote().invoke(call(context, 'addNote'), noteInput)).kind).toBe('retry');
  expect(markerState(context)).toBe('canResend');
  nextAttempt(context);
}

describe('accepted writes are never resent', () => {
  it.each([
    ['success headers, then the body breaks', () => { throw new ProviderTransportError(false, 201, {}); }],
    ['success headers, then the body stalls', () => { throw new ProviderStalledError(201, {}); }],
    ['oversized success', () => { throw new BodyTooLargeError(201, {}); }],
    ['undecodable success without an ID', () => json(201, '{"data":{}}')],
    ['success that reflects the credential', () => json(201, '{"data":{"id":"1","content":"hiverTestKey0123456789"}}')],
  ])('%s is uncertain', async (_name, reply) => {
    const provider = new Provider([reply]);
    const context = new HeartbeatContext(undefined, undefined, '', callId);
    const attempt = await newClient(provider).createSharedDraft().invoke(call(context, 'createSharedDraft'), draftInput);
    expect(attempt.kind).toBe('uncertain');
    expect(markerState(context)).toBe('claimed');
    expect(provider.requestCount).toBe(1);
  });
});

/** Reads this call's saved provider ID from a flat marker or a per-call checkpoint map. */
function markerProviderId(context: HeartbeatContext): string {
  const value = context.marker as Record<string, unknown>;
  const markers = value.singleDispatchMarkers as Record<string, Record<string, unknown>> | undefined;
  return String((markers?.[callId] ?? value).providerId ?? '');
}

describe('a refusal status is definite only with a readable body and no created ID', () => {
  it.each([
    ['refusal headers, then the body breaks', () => { throw new ProviderTransportError(false, 400, {}); }],
    ['not found headers, then the body stalls', () => { throw new ProviderStalledError(404, {}); }],
    ['oversized refusal body', () => { throw new BodyTooLargeError(422, {}); }],
  ])('%s resends below the cap and is uncertain at the cap', async (_name, reply) => {
    const provider = new Provider([reply]);
    const context = new HeartbeatContext(undefined, undefined, '', callId);
    for (let sendCount = 1; sendCount < 3; sendCount++) {
      expect((await newClient(provider).addNote().invoke(call(context, 'addNote'), noteInput)).kind).toBe('retry');
      expect(markerState(context)).toBe('claimed');
      nextAttempt(context);
    }
    expect((await newClient(provider).addNote().invoke(call(context, 'addNote'), noteInput)).kind).toBe('uncertain');
    expect(provider.requestCount).toBe(3);
  });

  it('keeps a rate limit whose body breaks a Retry that records canResend', async () => {
    const provider = new Provider([() => { throw new ProviderTransportError(false, 429, {}); }]);
    const context = new HeartbeatContext(undefined, undefined, '', callId);
    expect((await newClient(provider).addNote().invoke(call(context, 'addNote'), noteInput)).kind).toBe('retry');
    expect(markerState(context)).toBe('canResend');
  });

  it('reads the whole error body of a write but not of a read', async () => {
    const requests: Array<Record<string, unknown>> = [];
    const client = new HiverClient({ requestIntervalMilliseconds: 1 }, staticCredentials(), { apiBaseURL: 'http://127.0.0.1:12345/v1',
      send: async request => { requests.push(request as unknown as Record<string, unknown>); return json(400); } });
    const context = new HeartbeatContext(undefined, undefined, '', callId);
    await client.addNote().invoke(call(context, 'addNote'), noteInput);
    await client.getConversation().invoke(call(new HeartbeatContext(undefined, undefined, '', callId), 'getConversation'), { inboxId: '105902', conversationId: '573741352' });
    expect(requests[0]).not.toHaveProperty('ignoreErrorBodyReadFailure');
    expect(requests[0]).not.toHaveProperty('maxErrorResponseBytes');
    expect(requests[1]).toMatchObject({ ignoreErrorBodyReadFailure: true, maxErrorResponseBytes: 0 });
  });

  it.each([
    ['addNote', '{"data":{"id":"35042650"}}', '35042650'],
    ['createSharedDraft', '{"data":{"id":573741352,"shared_draft_id":901}}', '901'],
  ] as const)('never resends %s when the refusal body carries the created ID', async (operationId, body, providerId) => {
    const provider = new Provider([() => json(400, body)]);
    const context = new HeartbeatContext(undefined, undefined, '', callId);
    const invoke = () => operationId === 'addNote'
      ? newClient(provider).addNote().invoke(call(context, operationId), noteInput)
      : newClient(provider).createSharedDraft().invoke(call(context, operationId), draftInput);
    const attempt = await invoke();
    expect(attempt.kind).toBe('uncertain');
    expect(attempt.kind === 'uncertain' && attempt.receipt.providerObjectId).toBe(providerId);
    expect(markerProviderId(context)).toBe(providerId);
    nextAttempt(context);
    expect((await invoke()).kind).toBe('uncertain');
    expect(provider.requestCount).toBe(1);
  });

  it('repeats the updateConversation change when a refusal body cannot be read', async () => {
    const update = { inboxId: '105902', conversationId: '573741352', status: 'open' as const };
    const provider = new Provider([() => { throw new ProviderTransportError(false, 404, {}); }]);
    const context = new HeartbeatContext(undefined, undefined, '', callId);
    const attempt = await newClient(provider).updateConversation().invoke(call(context, 'updateConversation'), update);
    expect(attempt.kind).toBe('retry');
    expect(provider.requestCount).toBe(1);
  });
});

describe('a canResend marker after a possible send', () => {
  it('selects uncertain for a refusal on attempt 3', async () => {
    const provider = new Provider([() => json(502), () => json(429), () => json(400)]);
    const context = new HeartbeatContext(undefined, undefined, '', callId);
    await unknownThenRateLimited(provider, context);
    const attempt = await newClient(provider).addNote().invoke(call(context, 'addNote'), noteInput);
    expect(attempt.kind).toBe('uncertain');
    expect(provider.requestCount).toBe(3);
  });

  it.each([
    ['transient', missingCredentials, 'retry'],
    ['revoked', revokedCredentials, 'uncertain'],
    ['malformed', staticCredentials('bad key'), 'uncertain'],
  ] as const)('records nothing for a %s credential failure', async (_name, credentials, kind) => {
    const provider = new Provider([() => json(502), () => json(429)]);
    const context = new HeartbeatContext(undefined, undefined, '', callId);
    await unknownThenRateLimited(provider, context);
    const attempt = await newClient(provider, credentials).addNote().invoke(call(context, 'addNote'), noteInput);
    expect(attempt.kind).toBe(kind);
    expect(context.hasWrittenHeartbeat).toBe(false);
    expect(provider.requestCount).toBe(2);
  });
});

describe('credential failures on a first send', () => {
  it('selects defect for a terminal failure after recording notSent', async () => {
    const provider = new Provider([() => json(201)]);
    const context = new HeartbeatContext(undefined, undefined, '', callId);
    const attempt = await newClient(provider, revokedCredentials).createSharedDraft().invoke(call(context, 'createSharedDraft'), draftInput);
    expect(attempt.kind === 'branch' && attempt.branch).toBe('defect');
    expect(markerState(context)).toBe('notSent');
    expect(provider.requestCount).toBe(0);
  });

  it('retries a transient failure after recording notSent', async () => {
    const context = new HeartbeatContext(undefined, undefined, '', callId);
    const attempt = await newClient(new Provider([() => json(201)]), missingCredentials).addNote().invoke(call(context, 'addNote'), noteInput);
    expect(attempt.kind).toBe('retry');
    expect(markerState(context)).toBe('notSent');
  });

  it('selects updateConversation defect only for a terminal failure on the first attempt', async () => {
    const update = { inboxId: '105902', conversationId: '573741352', status: 'open' as const };
    for (const [credentials, attemptNumber, kind] of [[revokedCredentials, 1, 'branch'], [revokedCredentials, 2, 'retry'], [missingCredentials, 1, 'retry']] as const) {
      const context = new HeartbeatContext(undefined, undefined, '', callId);
      context.attempt = attemptNumber;
      const attempt = await newClient(new Provider([() => json(500)]), credentials).updateConversation().invoke(call(context, 'updateConversation'), update);
      expect(attempt.kind).toBe(kind);
    }
  });

  it.each([
    ['reauthorization', new ReauthorizationRequiredError(), true],
    ['missing connection object', new ObjectNotFoundError(), true],
    ['unselected connection', new ConnectionNotSelectedError(), true],
    ['broker refusal', new CredentialBrokerRejectedError(), true],
    // The SDK project provider replaces a malformed-material decoder error with this TypeError.
    ['malformed stored material', new TypeError('project credential decoding failed'), true],
    ['broker outage', new CredentialBrokerUnavailableError(), false],
    ['unlisted error', new TypeError('unlisted'), false],
  ] as const)('classifies a %s credential failure on a first send', async (_name, error, isTerminal) => {
    const context = new HeartbeatContext(undefined, undefined, '', callId);
    const provider = new Provider([() => json(201)]);
    const attempt = await newClient(provider, { resolve: async () => { throw error; } }).addNote().invoke(call(context, 'addNote'), noteInput);
    expect(provider.requestCount).toBe(0);
    expect(markerState(context)).toBe('notSent');
    if (isTerminal) expect(attempt).toMatchObject({ kind: 'branch', branch: 'defect' });
    else expect(attempt.kind).toBe('retry');
  });
});

describe('updateConversation pre-write lookups', () => {
  it.each([
    [404, '{}', 'notFound'], [403, '{}', 'providerRejected'], [200, '{"data":"SENTINEL"}', ''], [409, '{}', ''], [500, '{}', ''], [301, '{}', ''],
    [400, '{"message":"hiverTestKey0123456789"}', ''],
  ] as const)('select a branch only for a definite answer: HTTP %s %s', async (status, body, branch) => {
    for (const update of [
      { inboxId: '105902', conversationId: '573741352', assigneeEmail: 'phoebe@acme.example.com' },
      { inboxId: '105902', conversationId: '573741352', applyTagNames: ['billing'] },
    ]) {
      const provider = new Provider([() => json(status, body)]);
      const context = new HeartbeatContext(undefined, undefined, '', callId);
      const attempt = await newClient(provider).updateConversation().invoke(call(context, 'updateConversation'), update);
      expect(provider.requestCount).toBe(1);
      if (!branch) expect(attempt.kind).toBe('retry');
      else expect(attempt).toMatchObject({ kind: 'branch', branch });
    }
  });
});
