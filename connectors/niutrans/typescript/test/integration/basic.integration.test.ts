// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { NiutransClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, NiutransInspectionFlow } from '../../examples/basic/flow.js';

it('retries provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const providerBody = new TextEncoder().encode("{\"from\":\"zh\",\"to\":\"en\",\"tgt_text\":\"Hello.\",\"apikey\":\"private\",\"src_text\":\"private\"}");
  const directory = await mkdtemp(join(tmpdir(), 'dex-niutrans-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { apiKey: new SecretString("test-translation-api-key") };
  const provider = new NiutransClient({}, { resolve: async () => credentials }, { send: async () => {
    attempts++;
    return attempts === 1 ? { status: 503, headers: {}, body: new Uint8Array() } : { status: 200, headers: {}, body: providerBody };
  } });
  const connection = newConnection(provider, { provider: 'niutrans', name: ConnectionName });
  const flow = new NiutransInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `niutrans-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {"from": "zh", "text": "你好。", "to": "en"});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({"translatedText": "Hello."});
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
