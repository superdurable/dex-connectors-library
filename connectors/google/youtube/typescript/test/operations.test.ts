// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { HeartbeatContext, loadScenarios, scenarioCall } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import type { ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
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

it('keeps the insert claim after an ambiguous resend that follows a credential refresh', async () => {
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
  expect(kinds).toEqual(['retry', 'uncertain', 'uncertain']);
  expect(inserts).toEqual(['rejected-token', 'replacement-token']);
  expect(forcedRefreshes).toBe(1);
  expect(context.marker).toEqual({ callId: scenario.callId, attempt: 1, state: 'claimed' });
});
