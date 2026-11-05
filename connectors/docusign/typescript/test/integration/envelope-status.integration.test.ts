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
import { sendProviderRequest, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { expect, it } from 'vitest';
import { DocuSignClient } from '../../src/client.js';
import { newConnection, type Envelope } from '../../src/index.js';
import { ConnectionName, EnvelopeStatusFlow } from '../../examples/envelope-status/flow/workflow.js';

const accountId = 'a4ec37d6-1111-2222-3333-143885c220e1';
const envelopeId = '93be49ab-1111-2222-3333-f752070d71ec';

it('reads status through a real Dex Worker and fails an unwired missing envelope', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  const paths: string[] = [];
  const server = createServer((request, response) => {
    paths.push(`${request.method} ${request.url}`);
    if (request.headers.authorization !== 'Bearer inert-access-token') { response.writeHead(401).end(); return; }
    response.setHeader('Content-Type', 'application/json');
    if (request.url === '/oauth/userinfo') {
      response.writeHead(200).end(JSON.stringify({ accounts: [{ account_id: accountId, is_default: true, base_uri: 'https://na3.docusign.net' }] })); return;
    }
    if (request.url?.includes('000000000404')) { response.writeHead(404).end(JSON.stringify({ errorCode: 'ENVELOPE_DOES_NOT_EXIST' })); return; }
    response.writeHead(200).end(JSON.stringify({ envelopeId, status: 'completed', emailSubject: 'Meridian MSA', statusChangedDateTime: '2026-10-04T11:00:00Z' }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('DocuSign stand-in port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-docusign-status-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const connection = { provider: 'docusign', name: ConnectionName };
  const credentials = { authMethodId: 'docusign-oauth', oauthClientId: 'integration-key', oauthClientSecret: new SecretString('inert-client-secret'), accessToken: new SecretString('inert-access-token'), refreshToken: new SecretString('inert-refresh-token') };
  const provider = new DocuSignClient({}, new StaticCredentialProvider([[connection, credentials]]), {
    send: (request: ProviderRequest) => {
      const target = new URL(request.url);
      return sendProviderRequest({ ...request, url: `http://127.0.0.1:${address.port}${target.pathname}${target.search}`, addressPolicy: 'privateNetworkHTTP' });
    },
  });
  const flow = new EnvelopeStatusFlow(newConnection(provider, connection));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const successId = `docusign-status-${randomUUID()}`;
    await client.startFlow(flow, successId, { envelopeId });
    const success = await client.waitForFlow(successId, 60_000);
    expect(success.status, success.errorMessage).toBe('completed');
    expect(success.singleOutput<Envelope>().status).toBe('completed');
    const missingId = `docusign-missing-${randomUUID()}`;
    await client.startFlow(flow, missingId, { envelopeId: '93be49ab-1111-2222-3333-000000000404' });
    const missing = await client.waitForFlow(missingId, 60_000);
    expect(missing.status).toBe('failed');
    expect(missing.errorMessage).toContain('notFound');
    expect(paths).toEqual(['GET /oauth/userinfo', `GET /restapi/v2.1/accounts/${accountId}/envelopes/${envelopeId}?include=custom_fields`, `GET /restapi/v2.1/accounts/${accountId}/envelopes/93be49ab-1111-2222-3333-000000000404?include=custom_fields`]);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
