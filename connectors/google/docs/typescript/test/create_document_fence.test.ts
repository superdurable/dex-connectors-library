// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ReauthorizationRequiredError, SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, CredentialBrokerUnavailableError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { DocsClient } from '../src/client.js';
import { HeartbeatContext } from '../../../../../conformance/runners/typescript/connector_scenarios.js';

const connection = { provider: 'google', name: 'policy-docs' };
const callId = '4b0d5a43-7e43-5c5b-9a1e-2f4f1d1f0c11';
const emptyLookup = '{"files":[]}';
const document = '{"id":"newDoc1","name":"Acme Refund Policy","mimeType":"application/vnd.google-apps.document"}';
const input = { title: 'Acme Refund Policy' };

function reply(status: number, body: string): ProviderResponse {
  return { status, headers: {}, body: new TextEncoder().encode(body) };
}

function newClient(transport: (request: ProviderRequest) => Promise<ProviderResponse>, provider: unknown = new StaticCredentialProvider([[connection, { accessToken: new SecretString('docs-token') }]])) {
  return new DocsClient({ docsEndpoint: 'http://127.0.0.1:12345', driveEndpoint: 'http://127.0.0.1:12345' }, provider as never, { transport });
}

function callFor(context: HeartbeatContext, maxSendsOverride?: number): Call {
  return { id: callId, idempotencyKey: callId, connection, operation: { connectorId: 'google-docs', operationId: 'createDocument' }, context, ...(maxSendsOverride ? { maxSendsOverride } : {}) } as unknown as Call;
}

async function runAttempts(transport: (request: ProviderRequest) => Promise<ProviderResponse>, attempts: number, options: { firstAttempt?: number; maxSendsOverride?: number; provider?: unknown } = {}) {
  const client = newClient(transport, options.provider);
  const context = new HeartbeatContext(undefined, undefined, `docs-${Math.random()}`, callId);
  const results = [];
  for (let attempt = options.firstAttempt ?? 1; attempt < (options.firstAttempt ?? 1) + attempts; attempt++) {
    context.attempt = attempt;
    results.push(await client.createDocument().invoke(callFor(context, options.maxSendsOverride), input));
  }
  return { results, context };
}

it('looks an accepted create whose body is unusable up until the cap and never resends it', async () => {
  let creates = 0;
  const { results } = await runAttempts(async request => {
    if (request.method === 'GET') return reply(200, emptyLookup);
    creates++;
    return reply(200, '{"name":"Missing ID"}');
  }, 4);
  expect(results.map(result => result.kind)).toEqual(['retry', 'retry', 'retry', 'uncertain']);
  expect(creates).toBe(1);
});

it('finds an accepted create without an ID on a later lookup', async () => {
  let creates = 0;
  let lookups = 0;
  const { results } = await runAttempts(async request => {
    if (request.method === 'GET') {
      lookups++;
      // The first lookup after acceptance fails transiently; the next attempt's lookup finds the document.
      if (lookups === 2) return reply(503, '{}');
      return reply(200, lookups >= 3 ? `{"files":[${document}]}` : emptyLookup);
    }
    creates++;
    return reply(200, '{"name":"Missing ID"}');
  }, 2);
  expect(results.map(result => result.kind)).toEqual(['retry', 'branch']);
  expect(results[1]!.kind === 'branch' && results[1]!.branch).toBe('created');
  expect(creates).toBe(1);
});

it('reads a refusal carrying a document ID back and never resends it', async () => {
  let creates = 0;
  const { results } = await runAttempts(async request => {
    if (request.method === 'POST') { creates++; return reply(400, '{"id":"newDoc1","mimeType":7}'); }
    if (request.url.includes('/files/newDoc1')) return reply(200, document);
    return reply(200, emptyLookup);
  }, 2);
  expect(results.map(result => result.kind)).toEqual(['retry', 'branch']);
  expect(results[1]!.kind === 'branch' && results[1]!.branch).toBe('created');
  expect(creates).toBe(1);
});

it('treats a refusal whose body is oversized as an unknown outcome', async () => {
  const { results } = await runAttempts(async request => {
    if (request.method === 'GET') return reply(200, emptyLookup);
    throw new BodyTooLargeError(400, {});
  }, 1);
  expect(results[0]!.kind).toBe('retry');
});

it('selects defect for a terminal credential failure before a send and uncertain after one', async () => {
  const provider = { resolve: async () => { throw new ReauthorizationRequiredError(); } };
  const noRequest = async (): Promise<ProviderResponse> => { throw new Error('no request is expected'); };
  const first = (await runAttempts(noRequest, 1, { provider })).results[0]!;
  expect(first.kind === 'branch' && first.branch).toBe('defect');
  expect((await runAttempts(noRequest, 1, { firstAttempt: 2, provider })).results[0]!.kind).toBe('uncertain');
});

it.each([
  { name: 'object not found', failure: new ObjectNotFoundError(), isTerminal: true },
  { name: 'connection not selected', failure: new ConnectionNotSelectedError(), isTerminal: true },
  { name: 'broker rejected', failure: new CredentialBrokerRejectedError(), isTerminal: true },
  { name: 'broker unavailable', failure: new CredentialBrokerUnavailableError(), isTerminal: false },
  { name: 'static connection not configured', failure: new Error('connection is not configured'), isTerminal: false },
])('classifies a $name resolve failure before and after a possible send', async ({ failure, isTerminal }) => {
  const provider = { resolve: async () => { throw failure; } };
  const lookupOnly = async (request: ProviderRequest): Promise<ProviderResponse> => {
    if (request.method !== 'GET') throw new Error('no create is expected');
    return reply(200, emptyLookup);
  };
  const first = (await runAttempts(lookupOnly, 1, { provider })).results[0]!;
  const later = (await runAttempts(lookupOnly, 1, { firstAttempt: 2, provider })).results[0]!;
  if (isTerminal) {
    expect(first.kind === 'branch' && first.branch).toBe('defect');
    expect(later.kind).toBe('uncertain');
  } else {
    expect(first.kind).toBe('retry');
    expect(later.kind).toBe('retry');
  }
});

it('reads an accepted create back by the ID its unusable body carries', async () => {
  let creates = 0;
  const { results } = await runAttempts(async request => {
    if (request.method === 'POST') { creates++; return reply(200, '{"id":"newDoc1","mimeType":7}'); }
    if (request.url.includes('/files/newDoc1')) return reply(200, document);
    return reply(200, emptyLookup);
  }, 2);
  expect(results.map(result => result.kind)).toEqual(['retry', 'branch']);
  expect(results[1]!.kind === 'branch' && results[1]!.branch).toBe('created');
  expect(creates).toBe(1);
});

it('never resends a create whose 2xx body breaks after the headers', async () => {
  let creates = 0;
  const { results } = await runAttempts(async request => {
    if (request.method === 'GET') return reply(200, emptyLookup);
    creates++;
    throw new ProviderTransportError(false, 200, {});
  }, 4);
  expect(results.map(result => result.kind)).toEqual(['retry', 'retry', 'retry', 'uncertain']);
  expect(creates).toBe(1);
});

it('looks the document up again after a rate-limited resend', async () => {
  const methods: string[] = [];
  const statuses = [500, 429, 500];
  const { results } = await runAttempts(async request => {
    methods.push(request.method ?? 'GET');
    if (request.method === 'GET') return reply(200, emptyLookup);
    return reply(statuses[methods.filter(method => method === 'POST').length - 1]!, '{}');
  }, 3);
  expect(results.map(result => result.kind)).toEqual(['retry', 'retry', 'retry']);
  expect(methods).toEqual(['GET', 'POST', 'GET', 'GET', 'POST', 'GET', 'GET', 'POST', 'GET']);
});

it('retries a rejected lookup below the cap and selects uncertain at the cap', async () => {
  const rejected = async () => reply(403, '{"error":{"errors":[{"reason":"insufficientPermissions"}]}}');
  expect((await runAttempts(rejected, 1, { firstAttempt: 2 })).results[0]!.kind).toBe('retry');
  expect((await runAttempts(rejected, 1, { firstAttempt: 2, maxSendsOverride: 1 })).results[0]!.kind).toBe('uncertain');
});

it('retries an incomplete lookup at the send cap', async () => {
  const { results } = await runAttempts(async () => reply(200, '{"incompleteSearch":true,"files":[]}'), 1, { firstAttempt: 2, maxSendsOverride: 1 });
  expect(results[0]!.kind).toBe('retry');
});

it('retries missing credentials after a possible send without recording notSent', async () => {
  const provider = { resolve: async () => { throw new Error('credential store unavailable'); } };
  const { results, context } = await runAttempts(async () => { throw new Error('no request is expected'); }, 1, { firstAttempt: 2, provider });
  expect(results[0]!.kind).toBe('retry');
  expect(JSON.stringify(context.marker ?? {})).not.toContain('notSent');
});

it('retries a refresh failure after a 401 unless reauthorization is required', async () => {
  const providerFailing = (error: Error) => ({ resolve: async () => ({ accessToken: new SecretString('docs-token') }), resolveWithRefresh: async () => ({ accessToken: new SecretString('docs-token') }), resolveAfterRejection: async () => { throw error; } });
  const transport = async (request: ProviderRequest) => request.method === 'GET' ? reply(200, emptyLookup) : reply(401, '{}');
  const transient = await runAttempts(transport, 1, { provider: providerFailing(new Error('token endpoint unavailable')) });
  expect(transient.results[0]!.kind).toBe('retry');
  expect(JSON.stringify(transient.context.marker)).toContain('canResend');
  const terminal = await runAttempts(transport, 1, { provider: providerFailing(new ReauthorizationRequiredError()) });
  expect(terminal.results[0]!.kind === 'branch' && terminal.results[0]!.branch).toBe('providerRejected');
});

it('retries a guarded write whose credentials are temporarily unavailable', async () => {
  const provider = { resolve: async () => { throw new Error('credential store unavailable'); } };
  const client = newClient(async () => { throw new Error('no request is expected'); }, provider);
  const context = new HeartbeatContext(undefined, undefined, 'docs-append-credentials', callId);
  const append = { documentId: 'draftDoc1', requiredRevisionId: 'rev-1', text: 'Published by Dex.' };
  expect((await client.appendText().invoke(callFor(context), append)).kind).toBe('retry');
});

it('selects defect for a guarded write that needs reauthorization, and retries after a possible write', async () => {
  const provider = { resolve: async () => { throw new ReauthorizationRequiredError(); } };
  const client = newClient(async () => { throw new Error('no request is expected'); }, provider);
  const context = new HeartbeatContext(undefined, undefined, 'docs-append-reauthorization', callId);
  const append = { documentId: 'draftDoc1', requiredRevisionId: 'rev-1', text: 'Published by Dex.' };
  const first = await client.appendText().invoke(callFor(context), append);
  expect(first.kind === 'branch' && first.branch).toBe('defect');
  context.attempt = 2;
  // A later attempt may follow a write that applied, so defect could hide it.
  expect((await client.appendText().invoke(callFor(context), append)).kind).toBe('retry');
});
