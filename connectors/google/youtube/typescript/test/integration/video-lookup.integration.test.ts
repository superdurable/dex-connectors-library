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
import { YouTubeClient } from '../../src/client.js';
import { newConnection, type Video } from '../../src/index.js';
import { ConnectionName, VideoLookupFlow } from '../../examples/video-lookup/flow/workflow.js';

const videoId = 'AbCdEfGhI12';
const channelId = 'UC1234567890123456789012';

it('reads a video through a real Dex Worker and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer one-use-token') { response.writeHead(401).end(); return; }
    if (request.url?.includes('id=ZxYwVuTsR90')) { response.writeHead(404).end('{}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ items: [{ id: videoId, snippet: { channelId, channelTitle: 'Compiler Lab', title: 'Compiler release walkthrough', description: 'A short tour', publishedAt: '2026-10-20T10:00:00Z' }, contentDetails: { duration: 'PT4M13S' }, status: { privacyStatus: 'public' }, statistics: { viewCount: '1200' } }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('YouTube stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-youtube-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'google', name: ConnectionName };
  const provider = new YouTubeClient({ endpoint: `http://127.0.0.1:${address.port}/youtube/v3` }, new StaticCredentialProvider([[connection, { authMethodId: 'google-oauth-readonly', accessToken: new SecretString('one-use-token'), oauthClientId: 'client.apps.googleusercontent.com', oauthClientSecret: new SecretString('secret'), refreshToken: new SecretString('refresh') }]]));
  const flow = new VideoLookupFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `youtube-video-${randomUUID()}`;
    await client.startFlow(flow, successId, { videoId });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Video>().title).toBe('Compiler release walkthrough');
    const rejectedId = `youtube-missing-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { videoId: 'ZxYwVuTsR90' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('notFound');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
