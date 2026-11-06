// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { loadScenarios, scenarioCall } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { YouTubeClient } from '../src/client.js';

const connection = { provider: 'google', name: 'youtube' };
const recorded = loadScenarios(new URL('../../../../../conformance/connectors/google/youtube/scenarios.json', import.meta.url));
const call = scenarioCall(recorded[0]!, connection, 'google-youtube', { flowId: 'youtube-unit', cancellationSignal: new AbortController().signal }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
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
