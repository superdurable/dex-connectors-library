// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { runMutation, SecretString, type Call, type MutationAttempt, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { StripeClient } from '../src/client.js';
import type { CheckoutSession, CreateACHCheckoutSessionInput, Refund, PaymentIntent } from '../src/models.js';

const stableCallId = '11111111-1111-5111-8111-111111111111';
const checkoutInput: CreateACHCheckoutSessionInput = {
  clientReferenceId: 'registration-42', customerEmail: 'person@example.com',
  successUrl: 'https://example.com/success', cancelUrl: 'https://example.com/cancel',
  currency: 'usd', unitAmount: 7500, productName: 'Ticket',
};
const checkoutBody = { id: 'cs_123', object: 'checkout.session', status: 'open' };
const refundBody = { id: 're_123', object: 'refund', status: 'pending', amount: 2500, currency: 'usd' };

function call(operationId: string, keyed = false): Call {
  return {
    context: { flowId: 'test-flow', cancellationSignal: new AbortController().signal } as Call['context'],
    id: stableCallId, ...(keyed ? { idempotencyKey: stableCallId } : {}),
    connection: { provider: 'stripe', name: 'stripe-payments' }, operation: { connectorId: 'stripe', operationId },
  };
}

function response(status: number, body: unknown = {}, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers, body: Buffer.from(JSON.stringify(body)) };
}

function oversized(status: number, headers: Record<string, string> = {}): BodyTooLargeError {
  return new BodyTooLargeError(status, headers);
}

function client(responses: Array<ProviderResponse | Error>, requests: ProviderRequest[] = []): StripeClient {
  return new StripeClient({ endpoint: 'http://localhost:12121/v1' }, {
    resolve: async () => ({ authMethodId: 'api-key-only', secretKey: new SecretString('rk_test_private') }),
  }, { send: async request => {
    requests.push(request);
    const next = responses.shift();
    if (!next) throw new Error('unscripted provider request');
    if (next instanceof Error) throw next;
    return next;
  } });
}

type WriteValue = CheckoutSession | Refund;
type WriteCase = {
  operationId: 'createCardCheckoutSession' | 'createACHCheckoutSession' | 'createRefund';
  successBody: unknown;
  invoke: (stripe: StripeClient) => Promise<MutationAttempt<WriteValue>>;
};
const writes: WriteCase[] = [
  { operationId: 'createCardCheckoutSession', successBody: checkoutBody,
    invoke: stripe => stripe.createCardCheckoutSession().invoke(call('createCardCheckoutSession', true), checkoutInput) },
  { operationId: 'createACHCheckoutSession', successBody: checkoutBody,
    invoke: stripe => stripe.createACHCheckoutSession().invoke(call('createACHCheckoutSession', true), checkoutInput) },
  { operationId: 'createRefund', successBody: refundBody,
    invoke: stripe => stripe.createRefund().invoke(call('createRefund', true), { paymentIntentId: 'pi_123', amount: 2500 }) },
];

function refundReadback(): ProviderResponse { return response(200, { object: 'list', data: [], has_more: false }); }
function withReadback(write: WriteCase, first: ProviderResponse | Error, readback = refundReadback()): Array<ProviderResponse | Error> {
  return write.operationId === 'createRefund' ? [first, readback] : [first];
}

for (const write of writes) {
  describe(`${write.operationId} Go write parity`, () => {
    for (const status of [401, 403]) {
      it(`classifies ${status} credential rejection without echoing the key`, async () => {
        const result = await write.invoke(client([response(status, { error: { message: 'rk_test_private' } })]));
        expect(result).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: status === 401 ? 'AUTHENTICATION' : 'AUTHORIZATION' } });
        expect(JSON.stringify(result)).not.toContain('rk_test_private');
      });
    }

    it('retries an in-progress 409 with the identical idempotency key', async () => {
      const requests: ProviderRequest[] = [];
      const stripe = client([response(409, { error: { code: 'idempotency_key_in_use' } }, { 'retry-after': '2' }), response(200, write.successBody)], requests);
      expect(await write.invoke(stripe)).toMatchObject({ kind: 'retry', failure: { kind: 'CONFLICT' }, retryAfterMs: 2000 });
      expect(await write.invoke(stripe)).toMatchObject({ kind: 'branch', branch: write.operationId === 'createRefund' ? 'refunded' : 'created' });
      expect(requests.map(request => request.headers?.['Idempotency-Key'])).toEqual([stableCallId, stableCallId]);
    });

    it('caps Retry-After on a keyed 409 conflict', async () => {
      expect(await write.invoke(client([response(409, {}, { 'retry-after': '9999999999999999999999' })])))
        .toMatchObject({ kind: 'retry', failure: { kind: 'CONFLICT' }, retryAfterMs: 3_600_000 });
    });

    for (const [name, headers, body] of [
      ['parameter mismatch', {}, { error: { type: 'idempotency_error' } }],
      ['Stripe no-retry hint', { 'stripe-should-retry': 'false' }, {}],
    ] as const) {
      it(`selects providerRejected for 409 ${name}`, async () => {
        expect(await write.invoke(client([response(409, body, headers)]))).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'CONFLICT' } });
      });
    }

    for (const [status, expectedKind] of [[429, 'RATE_LIMIT'], [503, 'AVAILABILITY']] as const) {
      for (const [hint, delay] of [['3', 3000], ['9999999999999999999999', 3_600_000]] as const) {
        it(`retries ${status} with Retry-After ${hint} capped at ${delay}ms`, async () => {
          expect(await write.invoke(client([response(status, {}, { 'retry-after': hint })]))).toMatchObject({ kind: 'retry', failure: { kind: expectedKind }, retryAfterMs: delay });
        });
      }
    }

    for (const isProvablyNotSent of [true, false]) {
      it(`retries transport error ${isProvablyNotSent ? 'before' : 'after'} dispatch`, async () => {
        expect(await write.invoke(client([new ProviderTransportError(isProvablyNotSent)]))).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
      });
    }

    it('retries a hinted 500 and treats a replayed 500 as unconfirmed', async () => {
      expect(await write.invoke(client([response(500, {}, { 'stripe-should-retry': 'true' })]))).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
      const replayed = client(withReadback(write, response(500, {}, { 'stripe-should-retry': 'true', 'idempotent-replayed': 'true' })));
      expect(await write.invoke(replayed)).toMatchObject({ kind: 'uncertain', failure: { kind: 'AVAILABILITY' } });
    });

    it('treats 503 with an explicit no-retry hint as unconfirmed', async () => {
      const noRetry = client(withReadback(write, response(503, {}, { 'stripe-should-retry': 'false' })));
      expect(await write.invoke(noRetry)).toMatchObject({ kind: 'uncertain', failure: { kind: 'AVAILABILITY' } });
    });

    for (const [status, expected] of [
      [200, { kind: 'branch', branch: 'invalidResponse', failure: { kind: 'RESPONSE_TOO_LARGE' } }],
      [400, { kind: 'branch', branch: 'providerRejected', failure: { kind: 'PROVIDER_REJECTION' } }],
      [429, { kind: 'retry', failure: { kind: 'RATE_LIMIT' } }],
      [409, { kind: 'retry', failure: { kind: 'CONFLICT' } }],
      [500, { kind: 'uncertain', failure: { kind: 'AVAILABILITY' } }],
      [503, { kind: 'retry', failure: { kind: 'AVAILABILITY' } }],
    ] as const) {
      it(`classifies oversized ${status} by status and headers`, async () => {
        const oversizedResponse = client(withReadback(write, oversized(status)));
        expect(await write.invoke(oversizedResponse)).toMatchObject(expected);
      });
    }

    it('uses an oversized 409 no-retry hint without reading its truncated body', async () => {
      expect(await write.invoke(client([oversized(409, { 'stripe-should-retry': 'false' })])))
        .toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'CONFLICT' } });
    });

    it('selects invalidResponse for malformed success JSON', async () => {
      expect(await write.invoke(client([{ status: 200, headers: {}, body: Buffer.from('{bad') }]))).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
    });
  });
}

type QueryValue = CheckoutSession | PaymentIntent;
type QueryCase = {
  operationId: 'getCheckoutSession' | 'getPaymentIntent';
  invoke: (stripe: StripeClient) => Promise<QueryAttempt<QueryValue>>;
};
const queries: QueryCase[] = [
  { operationId: 'getCheckoutSession', invoke: stripe => stripe.getCheckoutSession().invoke(call('getCheckoutSession'), { sessionId: 'cs_123' }) },
  { operationId: 'getPaymentIntent', invoke: stripe => stripe.getPaymentIntent().invoke(call('getPaymentIntent'), { paymentIntentId: 'pi_123' }) },
];

for (const query of queries) {
  describe(`${query.operationId} Go query parity`, () => {
    for (const [status, branch, kind] of [[401, 'providerRejected', 'AUTHENTICATION'], [403, 'providerRejected', 'AUTHORIZATION'], [404, 'notFound', 'NOT_FOUND'], [409, 'providerRejected', 'CONFLICT']] as const) {
      it(`classifies ${status} as ${branch}`, async () => {
        expect(await query.invoke(client([response(status)]))).toMatchObject({ kind: 'branch', branch, failure: { kind } });
        expect(await query.invoke(client([oversized(status)]))).toMatchObject({ kind: 'branch', branch, failure: { kind } });
      });
    }
    it('retries 429 with Retry-After and caps overflow', async () => {
      expect(await query.invoke(client([response(429, {}, { 'retry-after': '3' })]))).toMatchObject({ kind: 'retry', failure: { kind: 'RATE_LIMIT' }, retryAfterMs: 3000 });
      expect(await query.invoke(client([response(429, {}, { 'retry-after': '9999999999999999999999' })]))).toMatchObject({ kind: 'retry', retryAfterMs: 3_600_000 });
    });
    it('retries 503 and 500 availability regardless of Retry-After', async () => {
      expect(await query.invoke(client([response(503, {}, { 'retry-after': '3' })]))).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' }, retryAfterMs: 0 });
      expect(await query.invoke(client([response(500)]))).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    });
    for (const isProvablyNotSent of [true, false]) {
      it(`retries transport error ${isProvablyNotSent ? 'before' : 'after'} dispatch`, async () => {
        expect(await query.invoke(client([new ProviderTransportError(isProvablyNotSent)]))).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
      });
    }
    it('classifies oversized success and malformed JSON as invalidResponse', async () => {
      expect(await query.invoke(client([oversized(200)]))).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'RESPONSE_TOO_LARGE' } });
      expect(await query.invoke(client([{ status: 200, headers: {}, body: Buffer.from('{bad') }]))).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
    });
  });
}

it('the connector SDK supplies the Stripe idempotency key before a direct mutation', async () => {
  const requests: ProviderRequest[] = [];
  const result = await runMutation(client([response(200, refundBody)], requests).createRefund(), call('createRefund'), { paymentIntentId: 'pi_123', amount: 2500 });
  expect(result).toMatchObject({ branch: 'refunded', receipt: { idempotencyKey: stableCallId } });
  expect(requests[0]?.headers?.['Idempotency-Key']).toBe(stableCallId);
});
