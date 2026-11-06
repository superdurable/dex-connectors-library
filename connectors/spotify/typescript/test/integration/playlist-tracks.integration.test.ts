// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { SpotifyClient } from '../../src/client.js';
import { newConnection, type PlaylistTrackPage } from '../../src/index.js';
import { ConnectionName, PlaylistTracksFlow } from '../../examples/playlist-tracks/flow/workflow.js';

const playlistId = '3cEYpjA9oz9GiPac4AsH4n';

it('lists a provider page through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer one-use-token') { response.writeHead(401).end(); return; }
    if (request.url?.includes('offset=7')) { response.writeHead(403).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ limit: 2, offset: 5, total: 6, next: null, items: [
      { item: { type: 'track', id: '4iV5W9uYEdYUVa79Axb7Rh', name: 'A Song', artists: [], album: { name: 'An Album', artists: [] }, duration_ms: 213000, disc_number: 1, track_number: 2 } },
      { item: null },
    ] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Spotify stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-spotify-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'spotify', name: ConnectionName };
  const provider = new SpotifyClient({ endpoint: `http://127.0.0.1:${address.port}/v1` }, new StaticCredentialProvider([[connection, { accessToken: new SecretString('one-use-token') }]]));
  const flow = new PlaylistTracksFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `spotify-list-${randomUUID()}`;
    await client.startFlow(flow, successId, { playlistId, limit: 2, offset: 5 });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    const page = success.singleOutput<PlaylistTrackPage>();
    expect(page.tracks?.[0]?.name).toBe('A Song');
    expect(page.skippedItems).toBe(1);
    const rejectedId = `spotify-denied-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { playlistId, limit: 2, offset: 7 });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('accessDenied');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
