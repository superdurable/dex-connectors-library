// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, expect, it } from 'vitest';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { CainiaoClient, newConnection, type WaybillTrace } from '../../src/index.js';
import { CainiaoWaybillTraceFlow, ConnectionName, waybillTraceAttribute } from '../../examples/waybill-trace/flow.js';

const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801';
let requests = 0;
let isRejecting = false;
const provider = createServer(async (request, response) => {
  const chunks: Buffer[] = [];
  for await (const chunk of request) chunks.push(Buffer.from(chunk));
  const fields = new URLSearchParams(Buffer.concat(chunks).toString());
  if (request.method !== 'POST' || request.url !== '/router/rest' || fields.get('app_key') !== '123456' ||
      fields.get('cp_code') !== 'YTO' || fields.get('waybill_code') !== '12345' || fields.get('timestamp') !== '2026-10-22 15:26:40' ||
      fields.get('sign') !== 'D8666C943359C9756A69F6992557452E') { response.writeHead(400).end(); return; }
  requests++;
  if (isRejecting) { response.writeHead(200).end(JSON.stringify({ error_response: { code: 15, msg: 'denied' } })); return; }
  if (requests === 1) { response.writeHead(503).end(); return; }
  response.writeHead(200).end(JSON.stringify({ cainiao_waybill_logisticstrace_get_response: { logistics_detail_dto_list: { logistics_detail_dto: [
    { action: 'GOT', description: '已揽收', time: '2016-07-14 17:00:42' },
  ] } } }));
});
afterAll(() => provider.close());

it('runs the checked-in Flow through Dex, retries, and persists the public trace', async () => {
  provider.listen(0, '127.0.0.1'); await once(provider, 'listening');
  const address = provider.address();
  if (!address || typeof address === 'string') throw new TypeError('local provider address unavailable');
  const reference = { provider: 'cainiao', name: ConnectionName };
  const credentials = new StaticCredentialProvider([[reference, {
    appKey: '123456', appSecret: new SecretString('secretKey'), sessionKey: new SecretString('sessionToken'),
  }]]);
  const connector = new CainiaoClient({ baseUrl: `http://127.0.0.1:${address.port}/router/rest` }, credentials, { now: () => new Date('2026-10-22T07:26:40Z') });
  const flow = new CainiaoWaybillTraceFlow(newConnection(connector, reference));
  const registry = new Registry([flow]);
  const directory = await mkdtemp(join(tmpdir(), 'dex-cainiao-ts-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start(); await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `cainiao-ts-${randomUUID()}`;
    await client.startFlow(flow, flowId, { carrierCode: 'YTO', waybillCode: '12345' });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const trace: WaybillTrace = { carrierCode: 'YTO', waybillCode: '12345', events: [{ action: 'GOT', description: '已揽收', time: '2016-07-14 17:00:42' }] };
    expect(result.singleOutput<WaybillTrace>()).toEqual(trace);
    expect(await client.getAttributes(flowId, waybillTraceAttribute)).toEqual([trace]);
    expect(requests).toBe(2);
    isRejecting = true;
    const rejectedId = `cainiao-ts-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedId, { carrierCode: 'YTO', waybillCode: '12345' });
    const rejected = await client.waitForFlow(rejectedId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toMatch(/providerRejected/);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
  }
}, 120_000);
