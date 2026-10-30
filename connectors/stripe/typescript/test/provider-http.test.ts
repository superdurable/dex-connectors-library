// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { once } from 'node:events';
import { afterEach, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { StripeClient } from '../src/client.js';
import { noProgress } from './call.js';

const servers: Array<ReturnType<typeof createServer>> = [];
afterEach(async () => { for (const server of servers.splice(0)) { server.close(); await once(server, 'close'); } });

async function localClient(handle: (request: IncomingMessage, response: ServerResponse) => void, maxResponseBytes = 1_048_576): Promise<StripeClient> {
  const server = createServer(handle);
  servers.push(server);
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const address = server.address();
  if (!address || typeof address === 'string') throw new Error('local HTTP port was unavailable');
  return new StripeClient({ endpoint: `http://127.0.0.1:${address.port}/v1`, maxResponseBytes }, {
    resolve: async () => ({ authMethodId: 'api-key-only', secretKey: new SecretString('rk_test_private') }),
  });
}

const call: Call = {
  ...noProgress,
  id: '11111111-1111-5111-8111-111111111111', idempotencyKey: '11111111-1111-5111-8111-111111111111',
  context: { flowId: 'test-flow', cancellationSignal: new AbortController().signal } as Call['context'],
  connection: { provider: 'stripe', name: 'stripe-payments' },
  operation: { connectorId: 'stripe', operationId: 'createRefund' },
};

const checkoutInput = {
  clientReferenceId: 'registration-123', customerEmail: 'attendee@example.com',
  successUrl: 'https://events.example.com/success', cancelUrl: 'https://events.example.com/cancel',
  currency: 'usd', unitAmount: 7500, productName: 'Community Event',
};

it('sends one keyed write through providerhttp without following a redirect', async () => {
  const requests: string[] = [];
  const stripe = await localClient((request, response) => {
    requests.push(`${request.method} ${request.url} ${request.headers['idempotency-key']} ${request.headers.authorization}`);
    response.writeHead(302, { Location: 'http://127.0.0.1:9/unsafe' });
    response.end('redirect');
  });
  const result = await stripe.createRefund().invoke(call, { paymentIntentId: 'pi_123', amount: 1 });
  expect(result).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
  expect(requests).toHaveLength(1);
  expect(requests[0]).toContain('POST /v1/refunds 11111111-1111-5111-8111-111111111111 Basic ');
});

it('classifies an oversized error from status and headers without retaining its body', async () => {
  const stripe = await localClient((_request, response) => {
    response.writeHead(429, { 'Retry-After': '2' });
    response.end('x'.repeat(100));
  }, 16);
  const result = await stripe.createRefund().invoke(call, { paymentIntentId: 'pi_123', amount: 1 });
  expect(result).toMatchObject({ kind: 'retry', failure: { kind: 'RATE_LIMIT' }, retryAfterMs: 2000 });
});

it('selects invalidResponse for an oversized successful body', async () => {
  const stripe = await localClient((_request, response) => {
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end('x'.repeat(100));
  }, 16);
  const result = await stripe.createRefund().invoke(call, { paymentIntentId: 'pi_123', amount: 1 });
  expect(result).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'RESPONSE_TOO_LARGE' } });
});

it('does not forward ACH Checkout credentials across a temporary redirect', async () => {
  let destinationRequests = 0;
  const destination = createServer((_request, response) => { destinationRequests++; response.end(); });
  servers.push(destination);
  destination.listen(0, '127.0.0.1');
  await once(destination, 'listening');
  const address = destination.address();
  if (!address || typeof address === 'string') throw new Error('redirect destination port was unavailable');
  let sourceRequests = 0;
  const stripe = await localClient((request, response) => {
    sourceRequests++;
    expect(request.headers['idempotency-key']).toBe(call.idempotencyKey);
    response.writeHead(307, { Location: `http://127.0.0.1:${address.port}/unsafe` });
    response.end();
  });
  const result = await stripe.createACHCheckoutSession().invoke({ ...call, operation: { connectorId: 'stripe', operationId: 'createACHCheckoutSession' } }, checkoutInput);
  expect(result).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
  expect(sourceRequests).toBe(1);
  expect(destinationRequests).toBe(0);
});

for (const write of [
  { name: 'card Checkout', operationId: 'createCardCheckoutSession', path: '/v1/checkout/sessions', objectId: 'cs_test_card_replayed',
    body: { id: 'cs_test_card_replayed', object: 'checkout.session', status: 'open' },
    invoke: (stripe: StripeClient, writeCall: Call) => stripe.createCardCheckoutSession().invoke(writeCall, checkoutInput) },
  { name: 'ACH Checkout', operationId: 'createACHCheckoutSession', path: '/v1/checkout/sessions', objectId: 'cs_test_ach_replayed',
    body: { id: 'cs_test_ach_replayed', object: 'checkout.session', status: 'open' },
    invoke: (stripe: StripeClient, writeCall: Call) => stripe.createACHCheckoutSession().invoke(writeCall, checkoutInput) },
  { name: 'refund', operationId: 'createRefund', path: '/v1/refunds', objectId: 're_replayed',
    body: { id: 're_replayed', object: 'refund', status: 'pending', amount: 2500, currency: 'usd' },
    invoke: (stripe: StripeClient, writeCall: Call) => stripe.createRefund().invoke(writeCall, { paymentIntentId: 'pi_123', amount: 2500 }) },
] as const) {
  it(`${write.name} replays one keyed object after the first response is lost`, async () => {
    const objectsByKey = new Map<string, typeof write.body>();
    const keys: string[] = [];
    const stripe = await localClient((request, response) => {
      expect(request.method).toBe('POST');
      expect(request.url).toBe(write.path);
      const key = request.headers['idempotency-key'];
      expect(typeof key).toBe('string');
      const idempotencyKey = String(key);
      keys.push(idempotencyKey);
      if (!objectsByKey.has(idempotencyKey)) objectsByKey.set(idempotencyKey, write.body);
      request.resume();
      request.once('end', () => {
        if (keys.length === 1) { response.destroy(); return; }
        response.writeHead(200, { 'Content-Type': 'application/json' });
        response.end(JSON.stringify(objectsByKey.get(idempotencyKey)));
      });
    });
    const writeCall: Call = { ...call, operation: { connectorId: 'stripe', operationId: write.operationId } };
    expect(await write.invoke(stripe, writeCall)).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
    const replay = await write.invoke(stripe, writeCall);
    expect(replay).toMatchObject({ kind: 'branch', branch: write.name === 'refund' ? 'refunded' : 'created' });
    if (replay.kind === 'branch') {
      expect('refundId' in replay.value ? replay.value.refundId : replay.value.id).toBe(write.objectId);
    }
    expect(keys).toEqual([call.idempotencyKey, call.idempotencyKey]);
    expect(objectsByKey.size).toBe(1);
  });
}
