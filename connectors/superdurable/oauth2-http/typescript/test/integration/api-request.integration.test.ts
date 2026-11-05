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
import { OAuth2HTTPClient } from '../../src/client.js';
import { newConnection, type SendRequestOutput } from '../../src/index.js';
import { APIRequestFlow, ConnectionName } from '../../examples/api-request/flow/workflow.js';

it('calls a local OAuth API through real Dex and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer local-test-access-token') { response.writeHead(401).end(); return; }
    if (request.url === '/v1/rejected') { response.writeHead(403).end('forbidden'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json', 'X-Request-Id': 'local-001' }).end('{"sub":"user-1"}');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('provider fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-oauth2-http-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials = new StaticCredentialProvider([[{ provider: 'oauth2-http', name: ConnectionName }, {
    oauthClientId: 'local-client', oauthClientSecret: new SecretString('local-client-secret'),
    accessToken: new SecretString('local-test-access-token'), refreshToken: new SecretString(''),
  }]]);
  const provider = new OAuth2HTTPClient({
    authorizationEndpoint: `http://127.0.0.1:${address.port}/auth`, tokenEndpoint: `http://127.0.0.1:${address.port}/token`,
    scopes: ['profile'], apiBaseUrl: `http://127.0.0.1:${address.port}/v1`,
  }, credentials);
  const flow = new APIRequestFlow(newConnection(provider, { provider: 'oauth2-http', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `oauth2-http-${randomUUID()}`;
    await client.startFlow(flow, successId, { path: '/userinfo' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<SendRequestOutput>()).toEqual({ statusCode: 200, contentType: 'application/json', body: '{"sub":"user-1"}' });
    const rejectedId = `oauth2-http-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { path: '/rejected' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('rejected');
    expect(requests).toEqual(['GET /v1/userinfo', 'GET /v1/rejected']);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
