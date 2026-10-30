// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import { newStripeMock, createRefundAlreadyRefunded, createRefundUnconfirmedRefund, createRefundRetry } from '../src/mock.js';

function call(operationId: string, callId: string, flowId: string): Call {
  return {
    context: { flowId } as Call['context'], id: callId, idempotencyKey: callId,
    connection: { provider: 'stripe', name: 'stripe-payments' }, operation: { connectorId: 'stripe', operationId },
  };
}

const first = '11111111-1111-5111-8111-111111111111';
const second = '22222222-2222-5222-8222-222222222222';

describe('Stripe mock', () => {
  it('installs every manifest default on the named connection', () => {
    const mock = newStripeMock('stripe-payments');
    const operations = mock.connection().operations;
    expect(mock.connection().reference).toEqual({ provider: 'stripe', name: 'stripe-payments' });
    expect(operations.createCardCheckoutSession().invoke(call('createCardCheckoutSession', first, 'flow-a'), {} as never)).toMatchObject({ kind: 'branch', branch: 'created', value: { id: 'cs_test_card123' } });
    expect(operations.createACHCheckoutSession().invoke(call('createACHCheckoutSession', first, 'flow-a'), {} as never)).toMatchObject({ kind: 'branch', branch: 'created', value: { id: 'cs_test_a1b2c3' } });
    expect(operations.getCheckoutSession().invoke(call('getCheckoutSession', first, 'flow-a'), { sessionId: 'cs_test_a1b2c3' })).toMatchObject({ kind: 'branch', branch: 'found' });
    expect(operations.getPaymentIntent().invoke(call('getPaymentIntent', first, 'flow-a'), { paymentIntentId: 'pi_3Pabc123' })).toMatchObject({ kind: 'branch', branch: 'found' });
    expect(operations.createRefund().invoke(call('createRefund', first, 'flow-a'), { paymentIntentId: 'pi_3Pabc123', amount: 2500 })).toMatchObject({ kind: 'branch', branch: 'refunded' });
    mock.assertNoFailures();
  });

  it('retries, selects branches, and replays one terminal keyed result', () => {
    const mock = newStripeMock('stripe-payments');
    const refund = mock.createRefund();
    refund.respond(createRefundRetry({ kind: 'AVAILABILITY', provider: 'stripe', operation: 'createRefund', message: 'temporary' }), createRefundAlreadyRefunded());
    const input = { paymentIntentId: 'pi_123', amount: 2500 };
    expect(refund.invoke(call('createRefund', first, 'flow-a'), input)).toMatchObject({ kind: 'retry' });
    expect(refund.invoke(call('createRefund', first, 'flow-a'), input)).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
    expect(refund.invoke(call('createRefund', first, 'flow-a'), input)).toMatchObject({ kind: 'branch', branch: 'providerRejected' });
    expect(refund.calls()[2]?.replay).toBe(true);
    expect(refund.invoke(call('createRefund', second, 'flow-b'), input)).toMatchObject({ kind: 'branch', branch: 'refunded' });
    mock.assertNoFailures();
  });

  it('applies Flow-specific uncertainty before defaults', () => {
    const mock = newStripeMock('stripe-payments');
    const refund = mock.createRefund();
    refund.forFlow('selected-').respond(createRefundUnconfirmedRefund());
    expect(refund.invoke(call('createRefund', first, 'selected-1'), { paymentIntentId: 'pi_123', amount: 1 })).toMatchObject({ kind: 'uncertain' });
    expect(refund.invoke(call('createRefund', second, 'other-1'), { paymentIntentId: 'pi_123', amount: 1 })).toMatchObject({ kind: 'branch', branch: 'refunded' });
    mock.assertNoFailures();
  });
});
