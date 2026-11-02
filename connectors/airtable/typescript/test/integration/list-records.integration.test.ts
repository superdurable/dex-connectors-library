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
import { AirtableClient } from '../../src/client.js';
import { newConnection, type RecordPage } from '../../src/index.js';
import { ConnectionName, ListRecordsFlow } from '../../examples/list-records/flow/workflow.js';

it('lists a provider page through real Dex and fails an unwired rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const server = createServer((request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.method !== 'POST' || request.url !== '/v0/appRefundBase0001/tblPolicies000001/listRecords') {
      response.writeHead(404).end(); return;
    }
    if (request.headers.authorization !== 'Bearer patTESTtoken.0123456789abcdef') { response.writeHead(401).end(); return; }
    const chunks: Buffer[] = [];
    request.on('data', (chunk: Buffer) => chunks.push(chunk));
    request.on('end', () => {
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { filterByFormula?: string };
      if (body.filterByFormula?.includes('REJECT')) {
        response.writeHead(403, { 'Content-Type': 'application/json' });
        response.end(JSON.stringify({ error: { type: 'INVALID_PERMISSIONS' } }));
        return;
      }
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ records: [{ id: 'recPolicyStandard', createdTime: '2026-09-01T09:00:00Z', fields: { 'Policy Key': 'refund-standard' } }] }));
    });
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Airtable fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-airtable-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new AirtableClient({ endpoint: `http://127.0.0.1:${address.port}` }, {
    resolve: async () => ({ personalAccessToken: new SecretString('patTESTtoken.0123456789abcdef') }),
  });
  const flow = new ListRecordsFlow(newConnection(provider, { provider: 'airtable', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const input = { baseId: 'appRefundBase0001', tableIdOrName: 'tblPolicies000001', pageSize: 2 };
    const successId = `airtable-list-${randomUUID()}`;
    await client.startFlow(flow, successId, input);
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<RecordPage>().records?.[0]?.id).toBe('recPolicyStandard');
    const rejectionId = `airtable-list-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { ...input, formula: 'REJECT' });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
