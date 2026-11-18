// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
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
});
