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
import { SurveyMonkeyClient } from '../../src/client.js';
import { newConnection, type SurveysPage } from '../../src/index.js';
import { ConnectionName, SurveyInspectionFlow } from '../../examples/survey-inspection/flow.js';

it('retries a provider 503 and persists the survey page through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer synthetic-access-token') { response.writeHead(401).end(); return; }
    if (request.url !== '/v3/surveys?page=1&per_page=50') { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      data: [{ id: '123456789', title: 'Customer feedback' }],
      page: 1, per_page: 50, total: 1,
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('SurveyMonkey fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-surveymonkey-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new SurveyMonkeyClient({ baseUrl: `http://127.0.0.1:${address.port}/v3` }, {
    resolve: async () => ({ accessToken: new SecretString('synthetic-access-token') }),
  });
  const connection = newConnection(provider, { provider: 'surveymonkey', name: ConnectionName });
  const flow = new SurveyInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `surveymonkey-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { page: 1 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<SurveysPage>().surveys?.[0]?.id).toBe('123456789');
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
