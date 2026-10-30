// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { goToBranch, stepRef } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { newStripeMock } from '../src/mock.js';
import { newCreateACHCheckoutSessionStep, newCreateCardCheckoutSessionStep, newCreateRefundStep, newGetCheckoutSessionStep, newGetPaymentIntentStep } from '../src/steps.js';

const annotations = { groupId: 'payments', groupLabel: 'Payments', explanation: 'Call the selected Stripe operation.' };

it('binds every operation-specific factory to its manifest defaults and required branch', () => {
  const connection = newStripeMock('stripe-payments').connection();
  const settings = { annotations, connection, mapToOperationInput: (input: never) => input };
  const completion = stepRef('CompleteStripeOperation');
  const steps = [
    newCreateCardCheckoutSessionStep({ ...settings, stepType: 'CreateCardCheckout', branches: [goToBranch('created', completion)] }),
    newCreateACHCheckoutSessionStep({ ...settings, stepType: 'CreateACHCheckout', branches: [goToBranch('created', completion)] }),
    newGetCheckoutSessionStep({ ...settings, stepType: 'GetCheckoutSession', branches: [goToBranch('found', completion)] }),
    newGetPaymentIntentStep({ ...settings, stepType: 'GetPaymentIntent', branches: [goToBranch('found', completion)] }),
    newCreateRefundStep({ ...settings, stepType: 'CreateRefund', branches: [goToBranch('refunded', completion)] }),
  ];
  expect(steps.map(step => step.getStepType())).toEqual([
    'CreateCardCheckout', 'CreateACHCheckout', 'GetCheckoutSession', 'GetPaymentIntent', 'CreateRefund',
  ]);
  for (const step of steps) {
    expect(step.getStepOptions()).toMatchObject({ executeMethodTimeoutMs: 30_000, executeDurability: 'async', executeRetry: { totalDurationMs: 3_600_000 } });
    expect(step.annotations()).toEqual(annotations);
  }
});
