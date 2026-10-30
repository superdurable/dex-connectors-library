// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { expect, it } from 'vitest';
import { ConnectionName, prepareCardCheckoutInput } from '../examples/create-card-checkout-session/flow/workflow.js';

it('uses a static connection name and trims the example start input', () => {
  expect(ConnectionName).toBe('stripe-payments');
  expect(prepareCardCheckoutInput({
    clientReferenceId: ' registration-123 ', customerEmail: ' attendee@example.com ',
    successUrl: ' https://events.example.com/success ', cancelUrl: ' https://events.example.com/cancel ',
    currency: ' USD ', unitAmount: 7500, productName: ' Community Event ',
  })).toEqual({
    clientReferenceId: 'registration-123', customerEmail: 'attendee@example.com',
    successUrl: 'https://events.example.com/success', cancelUrl: 'https://events.example.com/cancel',
    currency: 'usd', unitAmount: 7500, productName: 'Community Event',
  });
  expect(prepareCardCheckoutInput({
    clientReferenceId: '', customerEmail: 'attendee@example.com',
    successUrl: 'https://events.example.com/success', cancelUrl: 'https://events.example.com/cancel',
    currency: 'usd', unitAmount: 0, productName: 'Community Event',
  })).toBeUndefined();
});
