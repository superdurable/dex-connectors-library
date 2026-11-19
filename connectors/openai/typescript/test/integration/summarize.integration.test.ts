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
import { OpenAIClient } from '../../src/client.js';
import { newConnection } from '../../src/generated/connection.js';
import { ConnectionName, SummarizeTextFlow, type SummaryOutcome } from '../../examples/summarize-text/workflow.js';

it('runs the checked-in summary Flow through a real Dex Worker and local Responses provider', async () => {
  const serverAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!serverAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requestIds: string[] = [];
  const provider = createServer((request, response) => {
    if (request.method !== 'POST' || request.url !== '/v1/responses') { response.writeHead(404).end(); return; }
    const chunks: Buffer[] = [];
    request.on('data', (chunk: Buffer) => chunks.push(chunk));
    request.on('end', () => {
      requestIds.push(String(request.headers['x-client-request-id'] ?? ''));
      const payload = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { input: string; model: string };
      if (payload.input === 'reject') { response.writeHead(400).end('{}'); return; }
      response.writeHead(200, { 'Content-Type': 'application/json', 'X-Request-Id': 'req_test' });
      response.end(JSON.stringify({ id: 'resp_summary001', model: payload.model, status: 'completed', output: [{ content: [{ type: 'output_text', text: 'A short summary.' }] }], usage: { input_tokens: 8, output_tokens: 4, total_tokens: 12 } }));
    });
  });
  provider.listen(0, '127.0.0.1');
  await once(provider, 'listening');
  const address = provider.address();
  if (!address || typeof address === 'string') throw new Error('provider fixture address is unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-openai-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const openai = new OpenAIClient({ endpoint: `http://127.0.0.1:${address.port}/v1`, model: 'gpt-test' }, { resolve: async () => ({ apiKey: new SecretString('test-secret-key') }) });
  const flow = new SummarizeTextFlow(newConnection(openai, { provider: 'openai', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress });
  const client = new Client(registry, cache, { serverAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `openai-summary-${randomUUID()}`;
    await client.startFlow(flow, successId, { text: 'A long article.' });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<SummaryOutcome>()).toMatchObject({ responseId: 'resp_summary001', summary: 'A short summary.', model: 'gpt-test', usage: { inputTokens: 8, outputTokens: 4, totalTokens: 12 } });
    expect(requestIds).toHaveLength(1);
    expect(requestIds[0]).toMatch(/^[0-9a-f-]{36}$/);
    const rejectedId = `openai-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { text: 'reject' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => provider.close(() => resolve()));
  }
}, 90_000);
