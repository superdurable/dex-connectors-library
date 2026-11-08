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
import { OdooClient } from '../../src/client.js';
import { newConnection, type PartnerSearchPage, type Credentials } from '../../src/index.js';
import { ConnectionName, PartnerSearchFlow } from '../../examples/partner-search/flow.js';

it('retries a provider 503 and persists the worker through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer sample-odoo-api-key' || request.headers['x-odoo-database'] !== 'mycompany') { response.writeHead(401).end(); return; }
    if (request.url !== '/json/2/res.partner/search_read') { response.writeHead(404).end(); return; }
    const chunks: Buffer[] = [];
    request.on('data', chunk => chunks.push(Buffer.from(chunk)));
    request.on('end', () => {
      expect(Buffer.concat(chunks).toString()).toBe('{"domain":[["is_company","=",true],["name","ilike","%Acme%"]],"fields":["id","name"],"limit":5}');
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify([{ id: 42, name: 'Acme Company' }]));
    });
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Odoo fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-odoo-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { apiKey: new SecretString('sample-odoo-api-key') };
  const provider = new OdooClient({ baseUrl: `http://127.0.0.1:${address.port}`, database: 'mycompany' }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'odoo', name: ConnectionName });
  const flow = new PartnerSearchFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `odoo-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { namePattern: '%Acme%', limit: 5 });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<PartnerSearchPage>()).toEqual({ partners: [{ id: 42, name: 'Acme Company' }] });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
