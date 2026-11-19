// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { OkkiClient } from '../../src/client.js';
import { newConnection, type Credentials } from '../../src/index.js';
import { ConnectionName, OkkiInspectionFlow } from '../../examples/basic/flow.js';

it('retries provider 503 and persists the result through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const providerBody = new TextEncoder().encode("{\"code\":200,\"message\":\"success\",\"data\":[{\"id\":\"origin_list\",\"name\":\"客户来源\",\"field_type\":7,\"default\":\"private@example.com\",\"ext_info\":[\"private\"]},{\"id\":5200812401,\"name\":\"测试日期\",\"field_type\":4,\"default\":\"private\"},{\"id\":\"\",\"name\":\"来源详情\",\"field_type\":7}]}");
  const directory = await mkdtemp(join(tmpdir(), 'dex-okki-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { clientId: "client123", clientSecret: new SecretString("secretKey"), accessToken: new SecretString("jwt123") };
  const provider = new OkkiClient({}, new StaticCredentialProvider([[{ provider: 'okki', name: ConnectionName }, credentials]]), { send: async () => {
    attempts++;
    return attempts === 1 ? { status: 503, headers: {}, body: new Uint8Array() } : { status: 200, headers: {}, body: providerBody };
  } });
  const connection = newConnection(provider, { provider: 'okki', name: ConnectionName });
  const flow = new OkkiInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `okki-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, {});
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({"fields": [{"fieldType": 7, "id": "origin_list", "name": "客户来源"}, {"fieldType": 4, "id": "5200812401", "name": "测试日期"}]});
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
