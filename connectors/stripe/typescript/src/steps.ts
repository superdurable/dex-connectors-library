// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  newCreateCardCheckoutSessionStep as newGeneratedCreateCardCheckoutSessionStep,
  newCreateACHCheckoutSessionStep as newGeneratedCreateACHCheckoutSessionStep,
  newGetCheckoutSessionStep as newGeneratedGetCheckoutSessionStep,
  newGetPaymentIntentStep as newGeneratedGetPaymentIntentStep,
  newCreateRefundStep as newGeneratedCreateRefundStep,
  type CreateCardCheckoutSessionStepConfig,
  type CreateACHCheckoutSessionStepConfig,
  type GetCheckoutSessionStepConfig,
  type GetPaymentIntentStepConfig,
  type CreateRefundStepConfig,
} from './generated/steps.js';

export type { CreateCardCheckoutSessionStepConfig, CreateACHCheckoutSessionStepConfig, GetCheckoutSessionStepConfig, GetPaymentIntentStepConfig, CreateRefundStepConfig };

function validateStripeStepConnection(connection: { reference: { name: string } }, connectionName: string): void {
  if (!connection) throw new TypeError('Stripe connection is required');
  if (connectionName !== connection.reference.name) throw new TypeError(`Stripe connector Step connectionName ${JSON.stringify(connectionName)} must equal its connection name ${JSON.stringify(connection.reference.name)}`);
}

/** Build the card Checkout Step with Stripe's existing connection validation. */
export function newCreateCardCheckoutSessionStep<StepInput>(configuration: CreateCardCheckoutSessionStepConfig<StepInput>) {
  validateStripeStepConnection(configuration.connection, configuration.connectionName);
  return newGeneratedCreateCardCheckoutSessionStep(configuration);
}

/** Build the ACH Checkout Step with Stripe's existing connection validation. */
export function newCreateACHCheckoutSessionStep<StepInput>(configuration: CreateACHCheckoutSessionStepConfig<StepInput>) {
  validateStripeStepConnection(configuration.connection, configuration.connectionName);
  return newGeneratedCreateACHCheckoutSessionStep(configuration);
}

/** Build the Checkout lookup Step with Stripe's existing connection validation. */
export function newGetCheckoutSessionStep<StepInput>(configuration: GetCheckoutSessionStepConfig<StepInput>) {
  validateStripeStepConnection(configuration.connection, configuration.connectionName);
  return newGeneratedGetCheckoutSessionStep(configuration);
}

/** Build the PaymentIntent lookup Step with Stripe's existing connection validation. */
export function newGetPaymentIntentStep<StepInput>(configuration: GetPaymentIntentStepConfig<StepInput>) {
  validateStripeStepConnection(configuration.connection, configuration.connectionName);
  return newGeneratedGetPaymentIntentStep(configuration);
}

/** Build the refund Step with Stripe's existing connection validation. */
export function newCreateRefundStep<StepInput>(configuration: CreateRefundStepConfig<StepInput>) {
  validateStripeStepConnection(configuration.connection, configuration.connectionName);
  return newGeneratedCreateRefundStep(configuration);
}
