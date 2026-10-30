// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationStep, newQueryStep, type MutationStep, type MutationStepConfig, type QueryStep, type QueryStepConfig } from '@superdurable/connectors-sdk';
import type { StripeConnection } from './connection.js';
import type { CheckoutSession, CreateACHCheckoutSessionInput, CreateCardCheckoutSessionInput, CreateRefundInput, GetCheckoutSessionInput, GetPaymentIntentInput, PaymentIntent, Refund } from './models.js';

/** Card Checkout Step settings with one named Stripe connection. */
export interface CreateCardCheckoutSessionStepConfig<StepInput> extends Omit<MutationStepConfig<StepInput, CreateCardCheckoutSessionInput, CheckoutSession>, 'operation' | 'connection'> {
  /** Bound, static Stripe connection. */ connection: StripeConnection;
}

/** Create a card Checkout Step from the manifest operation. */
export function newCreateCardCheckoutSessionStep<StepInput>(configuration: CreateCardCheckoutSessionStepConfig<StepInput>): MutationStep<StepInput, CreateCardCheckoutSessionInput, CheckoutSession> {
  if (!configuration?.connection) throw new TypeError('Stripe connection is required');
  return newMutationStep({ ...configuration, connection: configuration.connection.reference, operation: configuration.connection.operations.createCardCheckoutSession() });
}

/** ACH Checkout Step settings with one named Stripe connection. */
export interface CreateACHCheckoutSessionStepConfig<StepInput> extends Omit<MutationStepConfig<StepInput, CreateACHCheckoutSessionInput, CheckoutSession>, 'operation' | 'connection'> {
  /** Bound, static Stripe connection. */ connection: StripeConnection;
}

/** Create an ACH Checkout Step from the manifest operation. */
export function newCreateACHCheckoutSessionStep<StepInput>(configuration: CreateACHCheckoutSessionStepConfig<StepInput>): MutationStep<StepInput, CreateACHCheckoutSessionInput, CheckoutSession> {
  if (!configuration?.connection) throw new TypeError('Stripe connection is required');
  return newMutationStep({ ...configuration, connection: configuration.connection.reference, operation: configuration.connection.operations.createACHCheckoutSession() });
}

/** Checkout Session query Step settings with one named Stripe connection. */
export interface GetCheckoutSessionStepConfig<StepInput> extends Omit<QueryStepConfig<StepInput, GetCheckoutSessionInput, CheckoutSession>, 'operation' | 'connection'> {
  /** Bound, static Stripe connection. */ connection: StripeConnection;
}

/** Create a Checkout Session query Step from the manifest operation. */
export function newGetCheckoutSessionStep<StepInput>(configuration: GetCheckoutSessionStepConfig<StepInput>): QueryStep<StepInput, GetCheckoutSessionInput, CheckoutSession> {
  if (!configuration?.connection) throw new TypeError('Stripe connection is required');
  return newQueryStep({ ...configuration, connection: configuration.connection.reference, operation: configuration.connection.operations.getCheckoutSession() });
}

/** PaymentIntent query Step settings with one named Stripe connection. */
export interface GetPaymentIntentStepConfig<StepInput> extends Omit<QueryStepConfig<StepInput, GetPaymentIntentInput, PaymentIntent>, 'operation' | 'connection'> {
  /** Bound, static Stripe connection. */ connection: StripeConnection;
}

/** Create a PaymentIntent query Step from the manifest operation. */
export function newGetPaymentIntentStep<StepInput>(configuration: GetPaymentIntentStepConfig<StepInput>): QueryStep<StepInput, GetPaymentIntentInput, PaymentIntent> {
  if (!configuration?.connection) throw new TypeError('Stripe connection is required');
  return newQueryStep({ ...configuration, connection: configuration.connection.reference, operation: configuration.connection.operations.getPaymentIntent() });
}

/** Refund Step settings with one named Stripe connection. */
export interface CreateRefundStepConfig<StepInput> extends Omit<MutationStepConfig<StepInput, CreateRefundInput, Refund>, 'operation' | 'connection'> {
  /** Bound, static Stripe connection. */ connection: StripeConnection;
}

/** Create a refund Step from the manifest operation. */
export function newCreateRefundStep<StepInput>(configuration: CreateRefundStepConfig<StepInput>): MutationStep<StepInput, CreateRefundInput, Refund> {
  if (!configuration?.connection) throw new TypeError('Stripe connection is required');
  return newMutationStep({ ...configuration, connection: configuration.connection.reference, operation: configuration.connection.operations.createRefund() });
}
