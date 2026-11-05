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
import { SpreadsheetClient } from '../../src/client.js';
import { newConnection, type UpsertRowOutput } from '../../src/index.js';
import { ConnectionName, UpsertContactFlow } from '../../examples/upsert-contact/flow/workflow.js';

it('upserts a row through real Dex and fails an unwired provider rejection', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const requests: string[] = [];
  const rows = [['email', 'name']];
  const server = createServer(async (request, response) => {
    requests.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer fixture-token') { response.writeHead(401).end(); return; }
    if (request.method === 'GET') {
      if (request.url?.includes('REJECT')) { response.writeHead(403).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ range: "'Contacts'!A1:B2", majorDimension: 'ROWS', values: rows }));
      return;
    }
    if (request.method !== 'POST') { response.writeHead(404).end(); return; }
    const chunks: Buffer[] = [];
    for await (const chunk of request) chunks.push(Buffer.from(chunk));
    const payload = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { values: string[][] };
    rows.push(payload.values[0]!);
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ updates: { updatedRange: "'Contacts'!A2:B2" } }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new TypeError('Google Sheets fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-google-sheets-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credential = { accessToken: new SecretString('fixture-token'), oauthClientId: 'fixture-client', oauthClientSecret: new SecretString('fixture-secret'), refreshToken: new SecretString('fixture-refresh') };
  const provider = new SpreadsheetClient({ endpoint: `http://127.0.0.1:${address.port}` }, { resolve: async () => credential, resolveWithRefresh: async () => credential });
  const flow = new UpsertContactFlow(newConnection(provider, { provider: 'google', name: ConnectionName }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const input = { spreadsheetId: 'fixture-sheet', sheetName: 'Contacts', keyColumn: 'email', keyValue: 'ada@example.test', values: { email: 'ada@example.test', name: 'Ada' } };
    const successId = `google-sheets-upsert-${randomUUID()}`;
    await client.startFlow(flow, successId, input);
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<UpsertRowOutput>()).toMatchObject({ action: 'inserted', rowNumber: 2 });
    const rejectionId = `google-sheets-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectionId, { ...input, sheetName: 'REJECT' });
    const rejected = await client.waitForFlow(rejectionId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(requests).toHaveLength(3);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
