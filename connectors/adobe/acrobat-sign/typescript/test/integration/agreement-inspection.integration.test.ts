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
import { AcrobatSignClient } from '../../src/client.js';
import { newConnection, type AgreementStatus, type Credentials } from '../../src/index.js';
import { ConnectionName, AgreementInspectionFlow } from '../../examples/agreement-inspection/flow.js';

it('retries a provider 503 and persists the agreement through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const agreementId = '3AAABLblqZNOTREALAGREEMENTID5_BjiH';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer sample-acrobat-sign-integration-key') { response.writeHead(401).end(); return; }
    attempts++;
    if (request.url === '/api/rest/v6/baseUris') {
      if (attempts === 1) { response.writeHead(503).end(); return; }
      const serverAddress = server.address();
      if (!serverAddress || typeof serverAddress === 'string') throw new Error('fixture port unavailable');
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ apiAccessPoint: `http://127.0.0.1:${serverAddress.port}/` }));
      return;
    }
    if (request.url !== `/api/rest/v6/agreements/${encodeURIComponent(agreementId)}`) { response.writeHead(404).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      id: agreementId, name: 'Example agreement', status: 'SIGNED',
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('AcrobatSign fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-adobe-acrobat-sign-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { integrationKey: new SecretString('sample-acrobat-sign-integration-key') };
  const provider = new AcrobatSignClient({ baseUrl: `http://127.0.0.1:${address.port}/api/rest/v6` }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'adobe-acrobat-sign', name: ConnectionName });
  const flow = new AgreementInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `adobe-acrobat-sign-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { agreementId });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<AgreementStatus>().id).toBe(agreementId);
    expect(result.singleOutput<AgreementStatus>().status).toBe('SIGNED');
    expect(attempts).toBe(3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
