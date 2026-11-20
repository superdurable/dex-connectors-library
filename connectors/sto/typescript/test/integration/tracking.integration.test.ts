// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { StoClient } from '../../src/client.js';
import { newConnection } from '../../src/index.js';
import { ConnectionName, TrackingFlow } from '../../examples/tracking/flow.js';

it('retries STO 503 and persists the waybill trace through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let requests = 0;
  const body = new TextEncoder().encode(JSON.stringify({ success: true, data: { '884000400421364': [{ waybillNo: '884000400421364', scanType: '收件', opTime: '2020-02-18 17:20:08', opOrgName: '青浦重固网点' }] } }));
  const directory = await mkdtemp(join(tmpdir(), 'dex-sto-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new StoClient({}, { resolve: async () => ({ appKey: 'app123', appSecret: new SecretString('REDACTED_STO_APP_SECRET'), fromCode: 'resource123' }) }, { send: async () => {
    requests++;
    return requests === 1 ? { status: 503, headers: {}, body: new Uint8Array() } : { status: 200, headers: {}, body };
  } });
  const flow = new TrackingFlow(newConnection(provider, { provider: 'sto', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `sto-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { waybillNo: '884000400421364' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput()).toEqual({ waybillNo: '884000400421364', events: [{ scanType: '收件', opTime: '2020-02-18 17:20:08', opOrgName: '青浦重固网点' }] });
    expect(requests).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
