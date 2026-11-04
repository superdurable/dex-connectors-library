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
import { expect, it } from 'vitest';
import { GeminiClient } from '../../src/client.js';
import { newConnection, type GenerateContentResponse } from '../../src/index.js';
import { ConnectionName, GenerateSummaryFlow } from '../../examples/generate-summary/flow/workflow.js';

it('generates through real Dex and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers['x-goog-api-key'] !== 'AIzaSENTINEL-test-key_0123456789') { response.writeHead(401).end(); return; }
    const chunks: Buffer[] = [];
    request.on('data', (chunk: Buffer) => chunks.push(chunk));
    request.on('end', () => {
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { contents: Array<{ parts: Array<{ text: string }> }> };
      response.setHeader('Content-Type', 'application/json');
      if (body.contents[0]?.parts[0]?.text === 'REJECT') {
        response.writeHead(400).end(JSON.stringify({ error: { status: 'INVALID_ARGUMENT' } }));
      } else {
        response.writeHead(200).end(JSON.stringify({ candidates: [{ content: { parts: [{ text: 'A short summary.' }] }, finishReason: 'STOP' }], responseId: 'resp-summary' }));
      }
    });
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Gemini fixture address unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-gemini-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new GeminiClient({ endpoint: `http://127.0.0.1:${address.port}/v1beta` }, {
    resolve: async () => ({ apiKey: new SecretString('AIzaSENTINEL-test-key_0123456789') }),
  });
  const flow = new GenerateSummaryFlow(newConnection(provider, { provider: 'google', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `gemini-summary-${randomUUID()}`;
    await client.startFlow(flow, successId, 'Summarize launch');
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<GenerateContentResponse>().text).toBe('A short summary.');
    const rejectedId = `gemini-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, 'REJECT');
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
