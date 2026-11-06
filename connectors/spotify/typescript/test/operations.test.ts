// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { SecretString, StaticCredentialProvider, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { SpotifyClient } from '../src/client.js';

const connection = { provider: 'spotify', name: 'playlist-reader' };
const playlistId = '3cEYpjA9oz9GiPac4AsH4n';
const call: Call = {
  id: 'spotify-unit-call', connection, operation: { connectorId: 'spotify', operationId: 'listPlaylistTracks' },
  context: { flowId: 'spotify-unit', cancellationSignal: new AbortController().signal } as Call['context'],
  reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
};
const credentials = () => new StaticCredentialProvider([[connection, { accessToken: new SecretString('unit-token') }]]);

it('uses connection timeout and rejects invalid configuration', () => {
  expect(() => new SpotifyClient({ endpoint: 'http://api.spotify.com/v1' }, credentials())).toThrow();
  expect(() => new SpotifyClient({ timeoutMs: -1 }, credentials())).toThrow();
});

it('classifies malformed provider data as invalidResponse without leaking provider text', async () => {
  let request: ProviderRequest | undefined;
  const client = new SpotifyClient({ endpoint: 'http://127.0.0.1:37801/v1', timeoutMs: 2100 }, credentials(), {
    send: async sent => { request = sent; return { status: 200, headers: {}, body: Buffer.from('{"limit":"wrong","items":[]}') }; },
  });
  const attempt = await client.listPlaylistTracks().invoke(call, { playlistId });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind !== 'branch') throw new TypeError('expected invalid response branch');
  expect(attempt.branch).toBe('invalidResponse');
  expect(attempt.failure?.kind).toBe('PROTOCOL');
  expect(JSON.stringify(attempt)).not.toContain('wrong');
  expect(request?.timeoutMs).toBe(2100);
  expect(request?.headers?.Authorization).toBe('Bearer unit-token');
});

it('keeps Go response-size precedence for a large rate-limit body', async () => {
  const client = new SpotifyClient({ endpoint: 'http://127.0.0.1:37801/v1' }, credentials(), {
    send: async () => { throw new BodyTooLargeError(429, { 'retry-after': '7' }); },
  });
  const attempt = await client.listPlaylistTracks().invoke(call, { playlistId });
  expect(attempt.kind).toBe('branch');
  if (attempt.kind !== 'branch') throw new TypeError('expected oversized response branch');
  expect(attempt.branch).toBe('invalidResponse');
  expect(attempt.failure?.kind).toBe('RESPONSE_TOO_LARGE');
});
