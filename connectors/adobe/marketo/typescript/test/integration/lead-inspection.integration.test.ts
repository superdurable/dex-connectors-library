// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { randomUUID } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Client, Registry, Worker, openBlobCache } from '@superdurable/dex';
import { SecretString, StaticCredentialProvider } from '@superdurable/connectors-sdk';
import { sendProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { MarketoClient } from '../../src/client.js';
import { newConnection, type LeadProfile, type Credentials } from '../../src/index.js';
import { ConnectionName, LeadInspectionFlow } from '../../examples/lead-inspection/flow.js';

it('retries a provider 503 and persists the lead through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const leadId = 318581;
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== 'Bearer sample-access-token') { response.writeHead(401).end(); return; }
    if (!request.url?.startsWith(`/rest/v1/lead/${leadId}.json?`)) { response.writeHead(404).end(); return; }
    const query = new URL(request.url, 'http://localhost').searchParams;
    expect(Object.fromEntries(query)).toEqual({ fields: 'id,firstName,lastName' });
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ success: true, result: [{ id: leadId, firstName: 'Sample', lastName: 'Lead' }] }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('Marketo fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-adobe-marketo-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { clientId: 'sample-client-id', clientSecret: new SecretString('sample-client-secret'), accessToken: new SecretString('sample-access-token') };
  const provider = new MarketoClient({ restEndpoint: 'https://123-ABC-456.mktorest.com/rest', tokenEndpoint: 'https://123-ABC-456.mktorest.com/identity/oauth/token' }, new StaticCredentialProvider([[{ provider: 'adobe', name: ConnectionName }, credentials]]), {
    send: request => {
      const target = new URL(request.url);
      return sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}${target.pathname}${target.search}`, addressPolicy: 'privateNetworkHTTP' });
    },
  });
  const connection = newConnection(provider, { provider: 'adobe', name: ConnectionName });
  const flow = new LeadInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `adobe-marketo-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { leadId });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<LeadProfile>().id).toBe(leadId);
    expect(result.singleOutput<LeadProfile>()).toEqual({ id: leadId, firstName: 'Sample', lastName: 'Lead' });
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
