// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { HeartbeatContext, loadScenarios, scenarioCall } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { YouTubeClient } from '../src/client.js';

const connection = { provider: 'google', name: 'youtube' };
const recorded = loadScenarios(new URL('../../../../../conformance/connectors/google/youtube/scenarios.json', import.meta.url));
const call = scenarioCall(recorded.find(scenario => scenario.operation === 'addPlaylistItem')!, connection, 'google-youtube', new HeartbeatContext(undefined, undefined, 'youtube-unit'), { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
const credentials = { authMethodId: 'google-oauth-readonly', accessToken: new SecretString('one-use-token'), oauthClientId: 'client.apps.googleusercontent.com', oauthClientSecret: new SecretString('secret'), refreshToken: new SecretString('refresh') };

it('rejects playlist mutation with a read-only grant before contacting YouTube', async () => {
  let requests = 0;
  const client = new YouTubeClient({ endpoint: 'http://127.0.0.1:42501/youtube/v3' }, new StaticCredentialProvider([[connection, credentials]]), { send: async () => { requests++; throw new Error('unexpected provider call'); } });
  const attempt = await client.addPlaylistItem().invoke(call, { playlistId: 'PLcurated123', videoId: 'AbCdEfGhI12' });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind !== 'branch') return;
  expect(attempt.branch).toBe('providerRejected');
  expect(attempt.failure?.kind).toBe('AUTHORIZATION');
  expect(attempt.receipt.providerObjectId).toBe('PLcurated123');
  expect(requests).toBe(0);
});

it('classifies a wrong-typed video statistic as invalidResponse', async () => {
  const body = new TextEncoder().encode(JSON.stringify({ items: [{ id: 'AbCdEfGhI12', snippet: { channelId: 'UC1234567890123456789012', publishedAt: '2026-10-20T10:00:00Z' }, statistics: { viewCount: 10 } }] }));
  const send = async () => ({ status: 200, headers: {}, body });
  const client = new YouTubeClient({ endpoint: 'http://127.0.0.1:42501/youtube/v3' }, new StaticCredentialProvider([[connection, credentials]]), { send });
  const attempt = await client.getVideo().invoke(call, { videoId: 'AbCdEfGhI12' });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind !== 'branch') return;
  expect(attempt.branch).toBe('invalidResponse');
  expect(attempt.failure?.kind).toBe('PROTOCOL');
});

it('counts an ambiguous resend that follows a credential refresh toward the send cap', async () => {
  const inserts: string[] = [];
  let forcedRefreshes = 0;
  const credentialFor = (token: string) => ({ ...credentials, authMethodId: 'google-oauth-manage', accessToken: new SecretString(token) });
  const provider = {
    resolve: async () => credentialFor('rejected-token'),
    resolveWithRefresh: async () => credentialFor('rejected-token'),
    resolveAfterRejection: async () => { forcedRefreshes++; return credentialFor('replacement-token'); },
  };
  const send = async (request: ProviderRequest) => {
    if (request.method === 'GET') return { status: 200, headers: {}, body: new TextEncoder().encode('{"pageInfo":{"totalResults":0},"items":[]}') };
    inserts.push(String(request.headers?.Authorization).replace('Bearer ', ''));
    // The refreshed insert may have been applied before this server error.
    return { status: inserts.length === 1 ? 401 : 500, headers: {}, body: new Uint8Array() };
  };
  const client = new YouTubeClient({ endpoint: 'http://127.0.0.1:42501/youtube/v3' }, provider, { send });
  const scenario = recorded.find(candidate => candidate.operation === 'addPlaylistItem')!;
  const context = new HeartbeatContext(undefined, undefined, 'youtube-refresh-resend');
  const kinds: string[] = [];
  for (const attempt of [1, 2, 3]) {
    context.attempt = attempt;
    const attemptCall = scenarioCall(scenario, connection, 'google-youtube', context, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
    kinds.push((await client.addPlaylistItem().invoke(attemptCall, { playlistId: 'PLexample123', videoId: 'dQw4w9WgXcQ' })).kind);
  }
  expect(kinds).toEqual(['retry', 'retry', 'uncertain']);
  // The refreshed resend shares one fence claim with the rejected insert.
  expect(inserts).toEqual(['rejected-token', 'replacement-token', 'rejected-token', 'rejected-token']);
  expect(forcedRefreshes).toBe(1);
  expect(context.marker).toEqual({ callId: scenario.callId, attempt: 3, state: 'claimed', sendCount: 3 });
});

const emptyListing = new TextEncoder().encode('{"pageInfo":{"totalResults":0},"items":[]}');
const manageCredentials = { ...credentials, authMethodId: 'google-oauth-manage' };

async function runAttempts(send: (request: ProviderRequest) => Promise<{ status: number; headers: Record<string, string>; body: Uint8Array }>, attempts: number, provider: unknown = new StaticCredentialProvider([[connection, manageCredentials]]), context = new HeartbeatContext(undefined, undefined, `youtube-${Math.random()}`)) {
  const client = new YouTubeClient({ endpoint: 'http://127.0.0.1:42501/youtube/v3' }, provider as never, { send });
  const scenario = recorded.find(candidate => candidate.operation === 'addPlaylistItem')!;
  const results = [];
  for (let attempt = 1; attempt <= attempts; attempt++) {
    context.attempt = attempt;
    const attemptCall = scenarioCall(scenario, connection, 'google-youtube', context, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
    results.push(await client.addPlaylistItem().invoke(attemptCall, { playlistId: 'PLexample123', videoId: 'dQw4w9WgXcQ' }));
  }
  return { results, context };
}

it('reads the playlist back before resending after a rate-limited resend', async () => {
  const methods: string[] = [];
  const statuses = [500, 429, 500];
  const send = async (request: ProviderRequest) => {
    methods.push(request.method ?? 'GET');
    if (request.method === 'GET') return { status: 200, headers: {}, body: emptyListing };
    return { status: statuses[methods.filter(method => method === 'POST').length - 1]!, headers: {}, body: new Uint8Array() };
  };
  const { results } = await runAttempts(send, 3);
  expect(results.map(result => result.kind)).toEqual(['retry', 'retry', 'retry']);
  // Attempt 3 follows a canResend marker yet still lists the playlist before inserting.
  expect(methods).toEqual(['GET', 'POST', 'GET', 'POST', 'GET', 'POST']);
});

it('resends after HTTP 503 without counting the insert', async () => {
  let inserts = 0;
  const send = async (request: ProviderRequest) => {
    if (request.method === 'GET') return { status: 200, headers: {}, body: emptyListing };
    inserts++;
    return { status: 503, headers: {}, body: new Uint8Array() };
  };
  const { results, context } = await runAttempts(send, 4);
  expect(results.map(result => result.kind)).toEqual(['retry', 'retry', 'retry', 'retry']);
  expect(inserts).toBe(4);
  expect(JSON.stringify(context.marker)).toContain('"state":"canResend"');
});

it('never resends an insert whose 2xx body breaks after the headers', async () => {
  let inserts = 0;
  const send = async (request: ProviderRequest) => {
    if (request.method === 'GET') return { status: 200, headers: {}, body: emptyListing };
    inserts++;
    throw new ProviderTransportError(false, 200, {});
  };
  const { results } = await runAttempts(send, 4);
  expect(results.map(result => result.kind)).toEqual(['retry', 'retry', 'retry', 'uncertain']);
  expect(inserts).toBe(1);
});

it('lists again on the next attempt after a transient listing failure follows an accepted insert', async () => {
  let inserts = 0;
  let listings = 0;
  const item = { id: 'UExpdGVt', snippet: { playlistId: 'PLexample123', title: 'Launch', position: 0, publishedAt: '2026-10-13T17:00:00Z', resourceId: { kind: 'youtube#video', videoId: 'dQw4w9WgXcQ' } }, contentDetails: { videoPublishedAt: '2026-10-01T16:00:00Z' } };
  const send = async (request: ProviderRequest) => {
    if (request.method === 'GET') {
      listings++;
      if (listings === 2) return { status: 503, headers: {}, body: new Uint8Array() };
      return { status: 200, headers: {}, body: listings > 2 ? new TextEncoder().encode(JSON.stringify({ pageInfo: { totalResults: 1 }, items: [item] })) : emptyListing };
    }
    inserts++;
    return { status: 200, headers: {}, body: new TextEncoder().encode('{"kind":"youtube#playlistItem"}') };
  };
  const { results } = await runAttempts(send, 2);
  expect(results.map(result => result.kind)).toEqual(['retry', 'branch']);
  expect(results[1]!.kind === 'branch' && results[1]!.branch).toBe('added');
  expect(inserts).toBe(1);
});

it('lists a refusal carrying an item ID and never resends it', async () => {
  let inserts = 0;
  const send = async (request: ProviderRequest) => {
    if (request.method === 'GET') return { status: 200, headers: {}, body: emptyListing };
    inserts++;
    return { status: 409, headers: {}, body: new TextEncoder().encode('{"kind":7,"id":"UExpdGVt"}') };
  };
  const { results } = await runAttempts(send, 4);
  expect(results[3]!.kind).toBe('uncertain');
  expect(inserts).toBe(1);
});

it('treats an oversized refusal body as an unknown outcome', async () => {
  const send = async (request: ProviderRequest) => {
    if (request.method === 'GET') return { status: 200, headers: {}, body: emptyListing };
    throw new BodyTooLargeError(400, {});
  };
  const { results } = await runAttempts(send, 1);
  expect(results[0]!.kind).toBe('retry');
});

it('retries a transient credential failure before the first insert and records notSent', async () => {
  const provider = { resolve: async () => { throw new Error('credential store unavailable'); } };
  const { results, context } = await runAttempts(async () => { throw new Error('no request is expected'); }, 1, provider);
  expect(results[0]!.kind).toBe('retry');
  expect(JSON.stringify(context.marker)).toContain('notSent');
});

it('retries a transient credential failure after a possible insert', async () => {
  let inserts = 0;
  let isUnavailable = false;
  const provider = { resolve: async () => { if (isUnavailable) throw new Error('credential store unavailable'); return manageCredentials; } };
  const send = async (request: ProviderRequest) => {
    if (request.method === 'GET') return { status: 200, headers: {}, body: emptyListing };
    inserts++;
    return { status: 500, headers: {}, body: new Uint8Array() };
  };
  const context = new HeartbeatContext(undefined, undefined, 'youtube-credential-transient');
  const first = await runAttempts(send, 1, provider, context);
  expect(first.results[0]!.kind).toBe('retry');
  isUnavailable = true;
  const client = new YouTubeClient({ endpoint: 'http://127.0.0.1:42501/youtube/v3' }, provider as never, { send });
  context.attempt = 2;
  const scenario = recorded.find(candidate => candidate.operation === 'addPlaylistItem')!;
  const attemptCall = scenarioCall(scenario, connection, 'google-youtube', context, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  expect((await client.addPlaylistItem().invoke(attemptCall, { playlistId: 'PLexample123', videoId: 'dQw4w9WgXcQ' })).kind).toBe('retry');
  expect(inserts).toBe(1);
});

it('retries a transient refresh failure after a 401 on a resend and records canResend', async () => {
  let inserts = 0;
  const provider = { resolve: async () => manageCredentials, resolveWithRefresh: async () => manageCredentials, resolveAfterRejection: async () => { throw new Error('token endpoint unavailable'); } };
  const send = async (request: ProviderRequest) => {
    if (request.method === 'GET') return { status: 200, headers: {}, body: emptyListing };
    inserts++;
    return { status: inserts === 1 ? 500 : 401, headers: {}, body: new Uint8Array() };
  };
  const { results, context } = await runAttempts(send, 2, provider);
  expect(results.map(result => result.kind)).toEqual(['retry', 'retry']);
  expect(inserts).toBe(2);
  expect(JSON.stringify(context.marker)).toContain('"state":"canResend"');
});

it('retries a saved-item read-back below the cap without inserting', async () => {
  const send = async (request: ProviderRequest) => {
    if (request.method !== 'GET') throw new Error('no insert is expected');
    return { status: 200, headers: {}, body: emptyListing };
  };
  const scenario = recorded.find(candidate => candidate.name === 'addPlaylistItem-saved-item-lagging-readback-retries')!;
  const context = new HeartbeatContext(scenario.priorHeartbeat, undefined, 'youtube-saved-item');
  const client = new YouTubeClient({ endpoint: 'http://127.0.0.1:42501/youtube/v3' }, new StaticCredentialProvider([[connection, manageCredentials]]), { send });
  const attemptCall = scenarioCall(scenario, connection, 'google-youtube', context, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  expect((await client.addPlaylistItem().invoke(attemptCall, { playlistId: 'PLexample123', videoId: 'dQw4w9WgXcQ' })).kind).toBe('retry');
});

it('retries a failed listing below the send cap', async () => {
  let gets = 0;
  const send = async (request: ProviderRequest) => {
    if (request.method === 'GET') {
      gets++;
      if (gets === 1) return { status: 200, headers: {}, body: emptyListing };
      return { status: 403, headers: {}, body: new TextEncoder().encode('{"error":{"errors":[{"reason":"quotaExceeded"}]}}') };
    }
    return { status: 500, headers: {}, body: new Uint8Array() };
  };
  const { results } = await runAttempts(send, 2);
  expect(results.map(result => result.kind)).toEqual(['retry', 'retry']);
});
