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
import { StripeClient } from '../../src/client.js';
import { newStripeConnection } from '../../src/connection.js';
import type { CheckoutSession } from '../../src/models.js';
import { CardCheckoutFlow } from '../../examples/create-card-checkout-session/flow/workflow.js';

it('retries a lost response and a key-in-use conflict with real Dex, then fails an unwired rejection', async () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');

  const keys: string[] = [];
  const objectsByKey = new Map<string, string>();
  const server = createServer((request, response) => {
    if (request.method !== 'POST' || request.url !== '/v1/checkout/sessions') {
      response.writeHead(404).end();
      return;
    }
    const key = String(request.headers['idempotency-key'] ?? '');
    const chunks: Buffer[] = [];
    request.on('data', (chunk: Buffer) => chunks.push(chunk));
    request.on('end', () => {
      const form = new URLSearchParams(Buffer.concat(chunks).toString('utf8'));
      if (form.get('client_reference_id') === 'rejected') {
        response.writeHead(400, { 'Content-Type': 'application/json' });
        response.end(JSON.stringify({ error: { type: 'invalid_request_error', code: 'bad_request' } }));
        return;
      }
      keys.push(key);
      if (!objectsByKey.has(key)) objectsByKey.set(key, 'cs_test_replayed');
      if (keys.length === 1) { response.destroy(); return; }
      if (keys.length === 2) {
        response.writeHead(409, { 'Content-Type': 'application/json', 'Retry-After': '1' });
        response.end(JSON.stringify({ error: { code: 'idempotency_key_in_use' } }));
        return;
      }
      response.writeHead(200, { 'Content-Type': 'application/json' });
      response.end(JSON.stringify({ id: objectsByKey.get(key), object: 'checkout.session', status: 'open' }));
    });
  });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const providerAddress = server.address();
  if (!providerAddress || typeof providerAddress === 'string') throw new Error('Stripe fixture port unavailable');

  const directory = await mkdtemp(join(tmpdir(), 'dex-stripe-integration-'));
  const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
  const provider = new StripeClient({ endpoint: `http://127.0.0.1:${providerAddress.port}/v1` }, {
    resolve: async () => ({ authMethodId: 'api-key-only', secretKey: new SecretString('rk_test_private') }),
  });
  const flow = new CardCheckoutFlow(newStripeConnection(provider, { provider: 'stripe', name: 'stripe-payments' }));
  const registry = new Registry([flow]);
  const worker = new Worker(registry, cache, { serverAddress: address });
  const client = new Client(registry, cache, { serverAddress: address });
  try {
    await worker.start();
    await worker.waitPullRegistered({ timeoutMs: 30_000 });
    const input = {
      clientReferenceId: 'registration-123', customerEmail: 'attendee@example.com',
      successUrl: 'https://events.example.com/success', cancelUrl: 'https://events.example.com/cancel',
      currency: 'usd', unitAmount: 7500, productName: 'Community Event',
    };
    const successfulFlowId = `stripe-card-retry-${randomUUID()}`;
    await client.startFlow(flow, successfulFlowId, input);
    const successful = await client.waitForFlow(successfulFlowId, 60_000);
    expect(successful.status, successful.errorMessage).toBe('completed');
    expect(successful.singleOutput<CheckoutSession>().id).toBe('cs_test_replayed');
    expect(keys).toHaveLength(3);
    expect(keys.every(key => key && key === keys[0])).toBe(true);
    expect(objectsByKey.size).toBe(1);

    const rejectedFlowId = `stripe-card-rejected-${randomUUID()}`;
    await client.startFlow(flow, rejectedFlowId, { ...input, clientReferenceId: 'rejected' });
    const rejected = await client.waitForFlow(rejectedFlowId, 60_000);
    expect(rejected.status).toBe('failed');
    expect(rejected.errorMessage).toContain('providerRejected');
    expect(rejected.errorMessage).toContain('PROVIDER_REJECTION');
  } finally {
    await client.close();
    await worker.close();
    cache.close();
    await rm(directory, { recursive: true, force: true });
    await new Promise<void>(resolve => server.close(() => resolve()));
  }
}, 90_000);
