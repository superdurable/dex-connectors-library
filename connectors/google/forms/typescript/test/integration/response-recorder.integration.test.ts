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
import { GoogleFormsClient } from '../../src/client.js';
import { newConnection, type FormResponse } from '../../src/index.js';
import { ConnectionName, ResponseRecorderFlow } from '../../examples/response-recorder/flow/workflow.js';

it('reads a form, page and response through a real Dex Worker', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer one-use-token') { response.writeHead(401).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    if (request.url === '/v1/forms/form_123') response.end(JSON.stringify({ formId: 'form_123', info: { title: 'Vendor intake' }, items: [] }));
    else if (request.url === '/v1/forms/form_123/responses?pageSize=50') response.end(JSON.stringify({ responses: [{ responseId: 'response_1', lastSubmittedTime: '2026-09-01T12:30:00Z' }] }));
    else if (request.url === '/v1/forms/form_123/responses/response_1') response.end(JSON.stringify({ responseId: 'response_1', formId: 'form_123', lastSubmittedTime: '2026-09-01T12:30:00Z', answers: {} }));
    else response.writeHead(404).end();
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Google Forms stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-forms-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'google', name: ConnectionName };
  const provider = new GoogleFormsClient({ endpoint: `http://127.0.0.1:${address.port}` }, new StaticCredentialProvider([[connection, { accessToken: new SecretString('one-use-token') }]]));
  const flow = new ResponseRecorderFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `forms-response-${randomUUID()}`;
    await client.startFlow(flow, flowId, { formId: 'form_123' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<FormResponse>().responseId).toBe('response_1');
    expect(requests).toEqual(['GET /v1/forms/form_123', 'GET /v1/forms/form_123/responses?pageSize=50', 'GET /v1/forms/form_123/responses/response_1']);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
