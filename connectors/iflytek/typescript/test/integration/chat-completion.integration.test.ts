// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { SecretString } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { expect, it } from 'vitest';
import { ConnectionName, ChatCompletionFlow } from '../../examples/chat-completion/flow.js';
import { IFLYTEKClient } from '../../src/client.js';
import { newConnection, type ChatCompletion } from '../../src/index.js';

it('retries Spark 503 through a real Dex Worker and persists the answer', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const server = createServer((request, response) => {
    requests++;
    if (request.method !== 'POST' || request.url !== '/v1/chat/completions' || request.headers.authorization !== 'Bearer spark-test-password' || request.headers.accept !== 'application/json' || request.headers['content-type'] !== 'application/json') {
      response.writeHead(400).end(); return;
    }
    if (requests === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ code: 0, choices: [{ message: { role: 'assistant', content: '你好！' } }], usage: { prompt_tokens: 4, completion_tokens: 3, total_tokens: 7 } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('IFLYTEK fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-iflytek-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new IFLYTEKClient({}, { resolve: async () => ({ apiPassword: new SecretString('spark-test-password') }) }, {
    send: request => sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}/v1/chat/completions`, addressPolicy: 'privateNetworkHTTP' }),
  });
  const flow = new ChatCompletionFlow(newConnection(provider, { provider: 'iflytek', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `iflytek-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { prompt: '你好' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<ChatCompletion>()).toEqual({ content: '你好！', model: 'lite', promptTokens: 4, completionTokens: 3, totalTokens: 7 });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
