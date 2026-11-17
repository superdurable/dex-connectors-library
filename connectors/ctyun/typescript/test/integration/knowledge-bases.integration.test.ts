// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { CTYunClient, signAuthorization, newConnection, type ListKnowledgeBasesOutput } from '../../src/index.js';
import { CTYunKnowledgeBasesFlow, ConnectionName, knowledgeBasesAttribute } from '../../examples/knowledge-bases/flow.js';

it('retries a signed 503 through a real Dex Worker and persists resource metadata', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    if (request.method !== 'GET' || request.url !== '/openapi/v1/infobases/list' ||
      request.headers.tenantid !== 'tenant-example' ||
      request.headers['ctyun-eop-request-id'] !== '123e4567-e89b-42d3-a456-426614174000' ||
      request.headers['eop-date'] !== '20261116T203456Z' ||
      request.headers['eop-authorization'] !== signAuthorization('AKIDEXAMPLE', 'SKEXAMPLE', '123e4567-e89b-42d3-a456-426614174000', '20261116T203456Z')) {
      response.writeHead(401).end(); return;
    }
    requests++;
    if (requests === 1) { response.writeHead(503, { 'Retry-After': '1' }).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ statusCode: 200, returnObj: [{ id: 280, name: 'Example knowledge base', count: 4, private: 'omitted' }] }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-ctyun-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new CTYunClient({}, { resolve: async () => ({ accessKey: new SecretString('AKIDEXAMPLE'), securityKey: new SecretString('SKEXAMPLE'), tenantId: new SecretString('tenant-example') }) }, {
    now: () => new Date('2026-11-16T12:34:56Z'), requestId: () => '123e4567-e89b-42d3-a456-426614174000',
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/openapi/v1/infobases/list`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new CTYunKnowledgeBasesFlow(newConnection(provider, { provider: 'ctyun', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `ctyun-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ListKnowledgeBasesOutput>()).toEqual({ knowledgeBases: [{ id: 280, name: 'Example knowledge base', fileCount: 4 }] });
    const [persistedPage] = await client.getAttributes(flowId, knowledgeBasesAttribute);
    expect(persistedPage).toEqual({ knowledgeBases: [{ id: 280, name: 'Example knowledge base', fileCount: 4 }] });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
