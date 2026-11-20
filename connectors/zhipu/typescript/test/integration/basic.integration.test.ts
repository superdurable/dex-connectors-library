// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { ZhipuClient } from '../../src/client.js';
import { newConnection } from '../../src/index.js';
import { ConnectionName, ZhipuInspectionFlow } from '../../examples/basic/flow.js';

it('retries Zhipu 503 and persists generated text through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const body = new TextEncoder().encode(JSON.stringify({ id: 'chat-1', model: 'glm-4.7-flash', choices: [{ message: { role: 'assistant', content: 'world' }, finish_reason: 'stop' }], usage: { prompt_tokens: 2, completion_tokens: 3, total_tokens: 5 }, private: 'must not persist' }));
  const directory = await mkdtemp(join(tmpdir(), 'dex-zhipu-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new ZhipuClient({}, { resolve: async () => ({ apiKey: new SecretString('REDACTED_ZHIPU_API_KEY') }) }, { send: async () => {
    requests++;
    return requests === 1 ? { status: 503, headers: {}, body: new Uint8Array() } : { status: 200, headers: {}, body };
  } });
  const flow = new ZhipuInspectionFlow(newConnection(provider, { provider: 'zhipu', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `zhipu-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { prompt: 'hello', maxOutputTokens: 64 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ text: 'world', model: 'glm-4.7-flash', responseId: 'chat-1', finishReason: 'stop', usage: { promptTokens: 2, completionTokens: 3, totalTokens: 5 } });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
