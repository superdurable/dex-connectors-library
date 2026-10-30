// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { MutationDefinition, QueryDefinition } from '@superdurable/connectors-sdk';

/** Stripe's stable connector identity. */
export const ConnectorID = 'stripe';

const stepDefaults = { executeMethodTimeoutMs: 30_000, executeDurability: 'async' } as const;
const defect = { id: 'defect', description: 'Local input, connection configuration, or connector definition is invalid.', optional: true } as const;
const invalidSession = { id: 'invalidResponse', description: 'Stripe returned an invalid or oversized Checkout Session response.', optional: true } as const;
const sessionRejection = { id: 'providerRejected', description: 'Stripe conclusively rejected the Checkout Session request.', optional: true } as const;
const sessionUncertain = { id: 'uncertain', description: 'A Stripe 500 or non-retryable server response left the Checkout Session outcome unconfirmed.', optional: true } as const;

/** Hosted card Checkout mutation and its manifest branches. */
export const CreateCardCheckoutSessionDefinition: MutationDefinition = {
  operation: { connectorId: ConnectorID, operationId: 'createCardCheckoutSession' }, stepDefaults,
  branches: [{ id: 'created', description: 'Stripe created the Checkout Session.' }, sessionRejection, sessionUncertain, invalidSession, defect],
};

/** Hosted ACH Checkout mutation and its manifest branches. */
export const CreateACHCheckoutSessionDefinition: MutationDefinition = {
  operation: { connectorId: ConnectorID, operationId: 'createACHCheckoutSession' }, stepDefaults,
  branches: [{ id: 'created', description: 'Stripe created the Checkout Session.' }, sessionRejection, sessionUncertain, invalidSession, defect],
};

/** Checkout Session query and its manifest branches. */
export const GetCheckoutSessionDefinition: QueryDefinition = {
  operation: { connectorId: ConnectorID, operationId: 'getCheckoutSession' }, stepDefaults,
  branches: [{ id: 'found', description: 'The Checkout Session exists and was returned.' },
    { id: 'notFound', description: 'The Checkout Session does not exist.', optional: true },
    { id: 'providerRejected', description: 'Stripe conclusively rejected the Checkout Session query.', optional: true },
    invalidSession, defect],
};

/** PaymentIntent query and its manifest branches. */
export const GetPaymentIntentDefinition: QueryDefinition = {
  operation: { connectorId: ConnectorID, operationId: 'getPaymentIntent' }, stepDefaults,
  branches: [{ id: 'found', description: 'The PaymentIntent exists and was returned.' },
    { id: 'notFound', description: 'Stripe reported resource_missing for the PaymentIntent.', optional: true },
    { id: 'providerRejected', description: 'Stripe conclusively rejected the PaymentIntent query.', optional: true },
    { id: 'invalidResponse', description: 'Stripe returned an invalid or oversized PaymentIntent response.', optional: true }, defect],
};

/** Keyed refund mutation and its manifest branches. */
export const CreateRefundDefinition: MutationDefinition = {
  operation: { connectorId: ConnectorID, operationId: 'createRefund' }, stepDefaults,
  branches: [{ id: 'refunded', description: 'Stripe accepted the refund; read its status to tell succeeded from pending.' },
    { id: 'providerRejected', description: 'Stripe conclusively rejected the refund with a 4xx error such as charge_already_refunded or an amount that is too large.', optional: true },
    { id: 'uncertain', description: 'A non-retryable Stripe server response left the refund outcome unconfirmed after bounded read-back.', optional: true },
    { id: 'invalidResponse', description: 'Stripe returned an invalid or oversized refund response.', optional: true }, defect],
};
