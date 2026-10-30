// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { StripeClient, type StripeRequestSender } from '../src/client.js';
import type { CreateACHCheckoutSessionInput, CreateRefundInput, Credentials } from '../src/models.js';

const callId = '11111111-1111-5111-8111-111111111111';
const checkoutInput: CreateACHCheckoutSessionInput = {
  clientReferenceId: 'registration-42', customerEmail: 'person@example.com',
  successUrl: 'https://example.com/success', cancelUrl: 'https://example.com/cancel',
  currency: 'usd', unitAmount: 7500, productName: 'Event ~*', metadata: { registration_id: 'registration-42' },
};
const refundInput: CreateRefundInput = { paymentIntentId: 'pi_123', amount: 2500 };
const checkoutResponse = { object: 'checkout.session', id: 'cs_123', status: 'open', payment_status: 'unpaid', amount_total: 7500 };
const intentResponse = { object: 'payment_intent', id: 'pi_123', amount: 7500, currency: 'usd', status: 'succeeded', latest_charge: { id: 'ch_123', amount_refunded: 2500, billing_details: { email: 'person@example.com' } } };
const refundResponse = { object: 'refund', id: 're_123', status: 'succeeded', amount: 2500, currency: 'usd' };

function call(operationId: string, keyed = false): Call {
  return { id: callId, context: { flowId: 'test-flow' } as Call['context'], connection: { provider: 'stripe', name: 'stripe-payments' }, operation: { connectorId: 'stripe', operationId }, ...(keyed ? { idempotencyKey: callId } : {}) };
}

function response(status: number, body: unknown = {}, headers: Record<string, string> = {}): ProviderResponse {
  return { status, headers, body: Buffer.from(JSON.stringify(body)) };
}

function client(responses: Array<ProviderResponse | Error>, requests: ProviderRequest[] = [], credential?: Credentials): StripeClient {
  const sender: StripeRequestSender = async request => {
    requests.push(request);
    const next = responses.shift();
    if (!next) throw new Error('unscripted provider request');
    if (next instanceof Error) throw next;
    return next;
  };
  return new StripeClient({ endpoint: 'http://localhost:12121/v1' }, {
    resolve: async () => credential ?? { authMethodId: 'api-key-only', secretKey: new SecretString('rk_test_example') },
  }, { send: sender, now: () => new Date('2026-01-02T03:04:05Z') });
}

function oversized(status: number, headers: Record<string, string> = {}): BodyTooLargeError {
  return Object.assign(new BodyTooLargeError(), { status, headers });
}

describe('hosted Checkout mutations', () => {
  for (const [operationId, method] of [
    ['createCardCheckoutSession', 'card'], ['createACHCheckoutSession', 'us_bank_account'],
  ] as const) {
    it(`${operationId} sends Go-compatible keyed form and returns created`, async () => {
      const requests: ProviderRequest[] = [];
      const stripe = client([response(200, checkoutResponse, { 'request-id': 'req_123' })], requests);
      const attempt = method === 'card'
        ? await stripe.createCardCheckoutSession().invoke(call(operationId, true), checkoutInput)
        : await stripe.createACHCheckoutSession().invoke(call(operationId, true), checkoutInput);
      expect(attempt).toMatchObject({ kind: 'branch', branch: 'created', value: { id: 'cs_123' }, receipt: { callId, idempotencyKey: callId, providerRequestId: 'req_123' } });
      const request = requests[0];
      expect(request).toBeDefined();
      expect(request?.headers?.['Idempotency-Key']).toBe(callId);
      expect(request?.headers?.Authorization).toMatch(/^Basic /);
      expect(request?.url).toBe('http://localhost:12121/v1/checkout/sessions');
      const form = new URLSearchParams(request?.body?.toString());
      expect(form.get('payment_method_types[0]')).toBe(method);
      expect(form.get('line_items[0][price_data][unit_amount]')).toBe('7500');
      expect(Buffer.from(request?.body ?? []).toString('utf8')).toContain('product_data%5D%5Bname%5D=Event+~%2A');
      expect(form.get('payment_intent_data[metadata][registration_id]')).toBe('registration-42');
    });

    it(`${operationId} selects each terminal branch`, async () => {
      const invoke = (stripe: StripeClient, input = checkoutInput) => method === 'card'
        ? stripe.createCardCheckoutSession().invoke(call(operationId, true), input)
        : stripe.createACHCheckoutSession().invoke(call(operationId, true), input);
      expect(await invoke(client([response(400)]))).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'PROVIDER_REJECTION' } });
      expect(await invoke(client([response(200, { wrong: true })]))).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
      expect(await invoke(client([]), { ...checkoutInput, currency: 'USD' })).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION' } });
      expect(await invoke(client([oversized(200)]))).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'RESPONSE_TOO_LARGE' } });
      expect(await invoke(client([response(401)]))).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHENTICATION' } });
      expect(await invoke(client([response(403)]))).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHORIZATION' } });
    });

    it(`${operationId} retries only safe keyed responses and marks unconfirmed outcomes`, async () => {
      const invoke = (stripe: StripeClient) => method === 'card'
        ? stripe.createCardCheckoutSession().invoke(call(operationId, true), checkoutInput)
        : stripe.createACHCheckoutSession().invoke(call(operationId, true), checkoutInput);
      expect(await invoke(client([response(429, {}, { 'retry-after': '3' })]))).toMatchObject({ kind: 'retry', failure: { kind: 'RATE_LIMIT' }, retryAfterMs: 3000 });
      expect(await invoke(client([response(409)]))).toMatchObject({ kind: 'retry', failure: { kind: 'CONFLICT' } });
      expect(await invoke(client([response(409, { error: { type: 'idempotency_error' } })]))).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'CONFLICT' } });
      expect(await invoke(client([response(503, {}, { 'retry-after': '999999' })]))).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' }, retryAfterMs: 3600000 });
      expect(await invoke(client([response(500)]))).toMatchObject({ kind: 'uncertain', failure: { kind: 'AVAILABILITY' } });
      expect(await invoke(client([response(503, {}, { 'idempotent-replayed': 'true' })]))).toMatchObject({ kind: 'uncertain' });
      expect(await invoke(client([oversized(500)]))).toMatchObject({ kind: 'uncertain' });
      expect(await invoke(client([new Error('socket closed')]))).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
    });
  }
});

describe('Checkout Session query', () => {
  it('returns found, notFound, providerRejected, invalidResponse and defect', async () => {
    const invoke = (stripe: StripeClient, sessionId = 'cs_123') => stripe.getCheckoutSession().invoke(call('getCheckoutSession'), { sessionId });
    expect(await invoke(client([response(200, checkoutResponse)]))).toMatchObject({ kind: 'branch', branch: 'found', value: { id: 'cs_123' } });
    expect(await invoke(client([response(404)]))).toMatchObject({ kind: 'branch', branch: 'notFound', failure: { kind: 'NOT_FOUND' } });
    expect(await invoke(client([response(401)]))).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHENTICATION' } });
    expect(await invoke(client([response(200, {})]))).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
    expect(await invoke(client([oversized(200)]))).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'RESPONSE_TOO_LARGE' } });
    expect(await invoke(client([]), 'wrong')).toMatchObject({ kind: 'branch', branch: 'defect' });
  });
  it('retries rate limits and availability with the provider delay', async () => {
    const invoke = (stripe: StripeClient) => stripe.getCheckoutSession().invoke(call('getCheckoutSession'), { sessionId: 'cs_123' });
    expect(await invoke(client([response(429, {}, { 'retry-after': '2' })]))).toMatchObject({ kind: 'retry', retryAfterMs: 2000 });
    expect(await invoke(client([response(503)]))).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(await invoke(client([new Error('closed')]))).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
  });
});

describe('PaymentIntent query', () => {
  it('sends expand and classifies all branches', async () => {
    const requests: ProviderRequest[] = [];
    const invoke = (stripe: StripeClient, paymentIntentId = 'pi_123') => stripe.getPaymentIntent().invoke(call('getPaymentIntent'), { paymentIntentId });
    expect(await invoke(client([response(200, intentResponse)], requests))).toMatchObject({ kind: 'branch', branch: 'found', value: { amountRefunded: 2500, customerEmail: 'person@example.com' } });
    expect(requests[0]?.url).toContain('expand%5B%5D=latest_charge');
    expect(await invoke(client([response(404, { error: { code: 'resource_missing' } })]))).toMatchObject({ kind: 'branch', branch: 'notFound', failure: { kind: 'NOT_FOUND', message: 'Stripe PaymentIntent was not found: resource_missing' } });
    expect(await invoke(client([response(403)]))).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHORIZATION' } });
    expect(await invoke(client([response(200, {})]))).toMatchObject({ kind: 'branch', branch: 'invalidResponse' });
    expect(await invoke(client([oversized(200)]))).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'RESPONSE_TOO_LARGE' } });
    expect(await invoke(client([]), 'wrong')).toMatchObject({ kind: 'branch', branch: 'defect' });
    expect(await invoke(client([response(429, {}, { 'retry-after': '5' })]))).toMatchObject({ kind: 'retry', retryAfterMs: 5000 });
    expect(await invoke(client([response(500)]))).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
  });
});

describe('refund mutation', () => {
  const invoke = (stripe: StripeClient, input = refundInput) => stripe.createRefund().invoke(call('createRefund', true), input);
  it('sends the keyed request and returns a refund', async () => {
    const requests: ProviderRequest[] = [];
    expect(await invoke(client([response(200, refundResponse)], requests))).toMatchObject({ kind: 'branch', branch: 'refunded', value: { refundId: 're_123' } });
    const form = new URLSearchParams(requests[0]?.body?.toString());
    expect(form.get('metadata[dex_call_id]')).toBe(callId);
    expect(requests[0]?.headers?.['Idempotency-Key']).toBe(callId);
  });
  it('classifies rejection, invalid response, validation, and credentials', async () => {
    expect(await invoke(client([response(400, { error: { code: 'charge_already_refunded', message: 'Already refunded' } })]))).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'PROVIDER_REJECTION', message: 'Stripe rejected the refund: charge_already_refunded: Already refunded' } });
    expect(await invoke(client([response(401, { error: { message: 'rk_test_sensitive' } })]))).toMatchObject({ kind: 'branch', branch: 'providerRejected', failure: { kind: 'AUTHENTICATION', message: 'Stripe rejected the refund' } });
    expect(await invoke(client([response(200, {})]))).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'PROTOCOL' } });
    expect(await invoke(client([oversized(200)]))).toMatchObject({ kind: 'branch', branch: 'invalidResponse', failure: { kind: 'RESPONSE_TOO_LARGE' } });
    expect(await invoke(client([]), { ...refundInput, amount: 0 })).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'VALIDATION' } });
    const noKey = client([], [], { authMethodId: 'api-key-only', secretKey: new SecretString('') });
    expect(await invoke(noKey)).toMatchObject({ kind: 'branch', branch: 'defect', failure: { kind: 'AUTHENTICATION' } });
  });
  it('retries safe keyed failures and read-back availability', async () => {
    expect(await invoke(client([response(429, {}, { 'retry-after': '3' })]))).toMatchObject({ kind: 'retry', failure: { kind: 'RATE_LIMIT' }, retryAfterMs: 3000 });
    expect(await invoke(client([response(409)]))).toMatchObject({ kind: 'retry', failure: { kind: 'CONFLICT' } });
    expect(await invoke(client([response(503)]))).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' } });
    expect(await invoke(client([response(500), response(429, {}, { 'retry-after': '4' })]))).toMatchObject({ kind: 'retry', failure: { kind: 'AVAILABILITY' }, retryAfterMs: 4000 });
    expect(await invoke(client([new Error('closed')]))).toMatchObject({ kind: 'retry', failure: { kind: 'TRANSPORT' } });
  });
  it('reconciles a cached 500 by call ID across bounded pages', async () => {
    const requests: ProviderRequest[] = [];
    const result = await invoke(client([
      response(500),
      response(200, { object: 'list', data: [{ id: 're_other', metadata: {} }], has_more: true }),
      response(200, { object: 'list', data: [{ ...refundResponse, metadata: { dex_call_id: callId } }], has_more: false }),
    ], requests));
    expect(result).toMatchObject({ kind: 'branch', branch: 'refunded', value: { refundId: 're_123' } });
    expect(requests[2]?.url).toContain('starting_after=re_other');
    expect(await invoke(client([response(500), response(200, { object: 'list', data: [], has_more: false })]))).toMatchObject({ kind: 'uncertain', failure: { kind: 'AVAILABILITY' } });
    expect(await invoke(client([response(500), response(403)]))).toMatchObject({ kind: 'uncertain', failure: { kind: 'AUTHORIZATION' } });
    expect(await invoke(client([response(500), response(401)]))).toMatchObject({ kind: 'uncertain', failure: { kind: 'AUTHENTICATION' } });
  });
  it('stops read-back after three full pages without a matching call ID', async () => {
    const requests: ProviderRequest[] = [];
    const pages = Array.from({ length: 3 }, (_, page) => response(200, {
      object: 'list', has_more: true, data: [{ id: `re_other_${page}`, metadata: {} }],
    }));
    const result = await invoke(client([response(500), ...pages], requests));
    expect(result).toMatchObject({ kind: 'uncertain', failure: { kind: 'AVAILABILITY' } });
    expect(requests).toHaveLength(4);
    expect(requests[3]?.url).toContain('starting_after=re_other_1');
  });
});

describe('Go response normalization mirrors', () => {
  it('retains only the safe Checkout fields and expands the PaymentIntent ID', async () => {
    const stripe = client([response(200, {
      object: 'checkout.session', id: 'cs_123', url: 'https://checkout.stripe.com/c/pay/cs_123',
      client_reference_id: 'registration-42', payment_status: 'paid', status: 'complete',
      payment_intent: { id: 'pi_123', client_secret: 'must-not-persist' },
      currency: 'usd', amount_total: 7500, expires_at: 1_767_318_245,
      metadata: { registration_id: 'registration-42' }, customer_details: { email: 'private@example.com' },
    })]);
    const result = await stripe.getCheckoutSession().invoke(call('getCheckoutSession'), { sessionId: 'cs_123' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'found', value: {
      id: 'cs_123', paymentIntentId: 'pi_123', paymentStatus: 'paid', status: 'complete', currency: 'usd', amountTotal: 7500,
      metadata: { registration_id: 'registration-42' },
    } });
    expect(JSON.stringify(result)).not.toContain('client_secret');
    expect(JSON.stringify(result)).not.toContain('private@example.com');
  });

  it('prefers the PaymentIntent receipt email over charge billing email', async () => {
    const stripe = client([response(200, { ...intentResponse, receipt_email: 'receipt@example.com' })]);
    const result = await stripe.getPaymentIntent().invoke(call('getPaymentIntent'), { paymentIntentId: 'pi_123' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'found', value: { customerEmail: 'receipt@example.com', amountRefunded: 2500 } });
  });

  it('accepts a PaymentIntent with no latest charge', async () => {
    const stripe = client([response(200, { object: 'payment_intent', id: 'pi_123', amount: 7500, currency: 'usd', status: 'requires_payment_method', latest_charge: null })]);
    const result = await stripe.getPaymentIntent().invoke(call('getPaymentIntent'), { paymentIntentId: 'pi_123' });
    expect(result).toMatchObject({ kind: 'branch', branch: 'found', value: { amountRefunded: 0 } });
    if (result.kind !== 'branch') throw new Error('PaymentIntent did not return a branch');
    expect(result.value).not.toHaveProperty('latestChargeId');
    expect(result.value).not.toHaveProperty('customerEmail');
  });
});

describe('refund read-back Go mirrors', () => {
  const invoke = (stripe: StripeClient) => stripe.createRefund().invoke(call('createRefund', true), refundInput);
  for (const [name, initial] of [
    ['unhinted 500', response(500)],
    ['503 with no-retry hint', response(503, {}, { 'stripe-should-retry': 'false' })],
    ['replayed 500 with retry hint', response(500, {}, { 'stripe-should-retry': 'true', 'idempotent-replayed': 'true' })],
  ] as const) {
    it(`finds a matching refund after ${name}`, async () => {
      const requests: ProviderRequest[] = [];
      const stripe = client([initial, response(200, { object: 'list', has_more: false,
        data: [{ object: 'refund', id: 're_found', status: 'pending', amount: 2500, currency: 'usd', metadata: { dex_call_id: callId } }],
      })], requests);
      expect(await invoke(stripe)).toMatchObject({ kind: 'branch', branch: 'refunded', value: { refundId: 're_found' } });
      expect(requests[1]?.method).toBe('GET');
      expect(requests[1]?.headers?.['Idempotency-Key']).toBeUndefined();
      expect(requests[1]?.url).toContain('payment_intent=pi_123');
      expect(requests[1]?.url).toContain('limit=100');
    });
    it(`returns uncertain when no matching refund follows ${name}`, async () => {
      expect(await invoke(client([initial, response(200, { object: 'list', data: [], has_more: false })])))
        .toMatchObject({ kind: 'uncertain', failure: { kind: 'AVAILABILITY', message: 'Stripe refund outcome cannot be confirmed after a server error' } });
    });
  }

  for (const [name, readback, expected] of [
    ['malformed list', response(200, { wrong: true }), { kind: 'uncertain', failure: { kind: 'AVAILABILITY' } }],
    ['malformed candidate', response(200, { object: 'list', data: [{ id: 'bad' }], has_more: false }), { kind: 'uncertain', failure: { kind: 'AVAILABILITY' } }],
    ['oversized list', oversized(200), { kind: 'uncertain', failure: { kind: 'AVAILABILITY' } }],
    ['temporary unavailability', response(503, {}, { 'retry-after': '7' }), { kind: 'retry', failure: { kind: 'AVAILABILITY' }, retryAfterMs: 7000 }],
    ['oversized temporary unavailability', oversized(503), { kind: 'retry', failure: { kind: 'AVAILABILITY' } }],
    ['read-back transport loss', new Error('closed'), { kind: 'retry', failure: { kind: 'AVAILABILITY' } }],
  ] as const) {
    it(`classifies ${name} after a cached 500`, async () => {
      expect(await invoke(client([response(500), readback]))).toMatchObject(expected);
    });
  }

  for (const [status, kind, message] of [
    [401, 'AUTHENTICATION', 'Stripe refund outcome cannot be confirmed after a server error because Stripe rejected the API key for the refund read-back; check that the key is still valid'],
    [403, 'AUTHORIZATION', 'Stripe refund outcome cannot be confirmed after a server error because the API key lacks permission for the refund read-back; grant it Refund read permission'],
  ] as const) {
    it(`redacts the API key and explains a ${status} read-back denial`, async () => {
      const result = await invoke(client([response(500), response(status, { error: { message: 'rk_test_private' } })]));
      expect(result).toMatchObject({ kind: 'uncertain', failure: { kind, message } });
      expect(JSON.stringify(result)).not.toContain('rk_test_private');
    });
  }
});
