// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  newCreateCardCheckoutSessionStep as newGeneratedCreateCardCheckoutSessionStep,
  newCreateACHCheckoutSessionStep as newGeneratedCreateACHCheckoutSessionStep,
  newGetCheckoutSessionStep as newGeneratedGetCheckoutSessionStep,
  newGetPaymentIntentStep as newGeneratedGetPaymentIntentStep,
  newCreateRefundStep as newGeneratedCreateRefundStep,
  type CreateCardCheckoutSessionStepConfig as GeneratedCardConfig,
  type CreateACHCheckoutSessionStepConfig as GeneratedACHConfig,
  type GetCheckoutSessionStepConfig as GeneratedCheckoutConfig,
  type GetPaymentIntentStepConfig as GeneratedIntentConfig,
  type CreateRefundStepConfig as GeneratedRefundConfig,
} from './generated/steps.js';
import type { Connection } from './connection.js';
import type { CreateACHCheckoutSessionInput, CreateCardCheckoutSessionInput, CreateRefundInput, GetCheckoutSessionInput, GetPaymentIntentInput } from './models.js';

type StripeStepConfig<StepInput, GeneratedConfig, OperationInput> = Omit<GeneratedConfig, 'connection' | 'mapToOperationInput'> & {
  connection: Connection;
  mapToOperationInput(input: StepInput): OperationInput;
};

/** Card Checkout Step configuration with the public input and typed Stripe connection. */
export type CreateCardCheckoutSessionStepConfig<StepInput> = StripeStepConfig<StepInput, GeneratedCardConfig<StepInput>, CreateCardCheckoutSessionInput>;
/** ACH Checkout Step configuration with the public input and typed Stripe connection. */
export type CreateACHCheckoutSessionStepConfig<StepInput> = StripeStepConfig<StepInput, GeneratedACHConfig<StepInput>, CreateACHCheckoutSessionInput>;
/** Checkout lookup Step configuration with the typed Stripe connection. */
export type GetCheckoutSessionStepConfig<StepInput> = StripeStepConfig<StepInput, GeneratedCheckoutConfig<StepInput>, GetCheckoutSessionInput>;
/** PaymentIntent lookup Step configuration with the typed Stripe connection. */
export type GetPaymentIntentStepConfig<StepInput> = StripeStepConfig<StepInput, GeneratedIntentConfig<StepInput>, GetPaymentIntentInput>;
/** Refund Step configuration preserving the public refund reason choices. */
export type CreateRefundStepConfig<StepInput> = StripeStepConfig<StepInput, GeneratedRefundConfig<StepInput>, CreateRefundInput>;

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
