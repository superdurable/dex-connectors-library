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
import { DropboxSignClient } from '../../src/client.js';
import { newConnection, type SignatureRequestStatus, type Credentials } from '../../src/index.js';
import { ConnectionName, SignatureRequestInspectionFlow } from '../../examples/signature-request-inspection/flow.js';

it('retries a provider 503 and persists the signature request through real Dex', async () => {
  const dexAddress = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!dexAddress) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev service');
  const signatureRequestId = 'fa5c8a0b0f492d768749333ad6fcc214c111e967';
  let attempts = 0;
  const server = createServer((request, response) => {
    if (request.headers.authorization !== `Basic ${Buffer.from('sample-dropbox-sign-api-key:').toString('base64')}`) { response.writeHead(401).end(); return; }
    if (request.url !== `/v3/signature_request/${encodeURIComponent(signatureRequestId)}`) { response.writeHead(404).end(); return; }
    attempts++;
    if (attempts === 1) { response.writeHead(503).end(); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({
      signature_request: { signature_request_id: signatureRequestId, title: 'Example agreement', is_complete: true, is_declined: false, has_error: false },
    }));
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('DropboxSign fixture port unavailable');
  const directory = await mkdtemp(join(tmpdir(), 'dex-dropbox-sign-ts-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const credentials: Credentials = { apiKey: new SecretString('sample-dropbox-sign-api-key') };
  const provider = new DropboxSignClient({ baseUrl: `http://127.0.0.1:${address.port}/v3` }, {
    resolve: async () => credentials,
  });
  const connection = newConnection(provider, { provider: 'dropbox-sign', name: ConnectionName });
  const flow = new SignatureRequestInspectionFlow(connection);
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: dexAddress });
  const client = new Client(registry, cache, { serverAddress: dexAddress });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const flowId = `dropbox-sign-typescript-${randomUUID()}`;
    await client.startFlow(flow, flowId, { signatureRequestId });
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    expect(result.singleOutput<SignatureRequestStatus>().id).toBe(signatureRequestId);
    expect(result.singleOutput<SignatureRequestStatus>().isComplete).toBe(true);
    expect(attempts).toBe(2);
  } finally {
    await client.close(); await worker.close(); cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 120_000);
