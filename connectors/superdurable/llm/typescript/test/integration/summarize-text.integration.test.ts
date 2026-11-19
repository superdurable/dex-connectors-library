// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client as DexClient, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { Client, newConnection } from '../../src/index.js';
import { ConnectionName, SummarizeTextFlow, generatedText } from '../../examples/summarize-text/flow.js';

it('retries a rate limit, persists streamed text, and fails an unwired defect through Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  let requestCount = 0;
  const server = createServer((_request, response) => {
    requestCount++;
    if (requestCount === 1) {
      response.writeHead(429, { 'Content-Type': 'application/json' });
      response.end('{"error":{"type":"rate_limit_error"}}');
      return;
    }
    response.writeHead(200, { 'Content-Type': 'text/event-stream' });
    response.write('data: {"id":"resp-dex-1","model":"fixture-model","choices":[{"index":0,"delta":{"content":"Hello "},"finish_reason":null}]}\n\n');
    response.write('data: {"id":"resp-dex-1","model":"fixture-model","choices":[{"index":0,"delta":{"content":"Dex"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}\n\n');
    response.end('data: [DONE]\n\n');
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('provider fixture address is unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-llm-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 << 20 });
  const provider = new Client({
    provider: 'openai-compatible', model: 'fixture-model', baseUrl: `http://127.0.0.1:${address.port}/v1`,
  }, { resolve: async () => ({ apiKey: new SecretString('sk-llm-integration-test') }) });
  const flow = new SummarizeTextFlow(newConnection(provider, ConnectionName));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new DexClient(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successID = 'llm-summary-' + randomUUID();
    await client.startFlow(flow, successID, { messages: [{ role: 'user', text: 'Say hello.' }] });
    const result = await client.waitForFlow(successID, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<{ text: string }>().text).toBe('Hello Dex');
    expect(requestCount).toBe(2);
    const deadline = Date.now() + 5_000;
    let messages: readonly { value: string }[] = [];
    while (Date.now() < deadline) {
      messages = (await client.listStreamMessages(successID, generatedText, 10)).messages;
      if (messages.length) break;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    expect(messages.map(message => message.value).join('')).toContain('Hello Dex');
    const failureID = 'llm-summary-' + randomUUID();
    await client.startFlow(flow, failureID, { messages: [{ role: 'user', text: 'Say hello.' }], reasoningEffort: 'low' });
    const failed = await client.waitForFlow(failureID, 30_000);
    expect(failed.status).toBe('failed');
    expect(failed.errorMessage).toContain('defect');
    expect(requestCount).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
