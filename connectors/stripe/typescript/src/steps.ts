// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { StepTarget } from '@superdurable/dex';
import { goToBranch, newMutationStep, newQueryStep, type MutationResult, type MutationStep, type MutationStepConfig, type QueryResult, type QueryStep, type QueryStepConfig } from '@superdurable/connectors-sdk';
import type { StripeConnection } from './connection.js';
import type { CheckoutSession, CreateACHCheckoutSessionInput, CreateCardCheckoutSessionInput, CreateRefundInput, GetCheckoutSessionInput, GetPaymentIntentInput, PaymentIntent, Refund } from './models.js';

/** Card Checkout Step settings with named manifest branches. */
export interface CreateCardCheckoutSessionStepConfig<StepInput> extends Omit<MutationStepConfig<StepInput, CreateCardCheckoutSessionInput, CheckoutSession>, 'operation' | 'connection' | 'branches'> {
  /** Bound Stripe connection. */ connection: StripeConnection;
  /** Static connection name; must match the bound connection. */ connectionName: string;
  /** Required success destination. */ created: StepTarget<MutationResult<CheckoutSession>>;
  /** Optional conclusive rejection destination. */ providerRejected?: StepTarget<MutationResult<CheckoutSession>>;
  /** Optional unknown-outcome destination. */ uncertain?: StepTarget<MutationResult<CheckoutSession>>;
  /** Optional invalid-response destination. */ invalidResponse?: StepTarget<MutationResult<CheckoutSession>>;
  /** Optional local-defect destination. */ defect?: StepTarget<MutationResult<CheckoutSession>>;
}

/** Create a card Checkout Step from the manifest operation. */
export function newCreateCardCheckoutSessionStep<StepInput>(configuration: CreateCardCheckoutSessionStepConfig<StepInput>): MutationStep<StepInput, CreateCardCheckoutSessionInput, CheckoutSession> {
  validateConnectionName(configuration?.connection, configuration?.connectionName);
  const { connection, connectionName, created, providerRejected, uncertain, invalidResponse, defect, ...stepConfiguration } = configuration;
  void connectionName;
  return newMutationStep({ ...stepConfiguration, connection: connection.reference, operation: connection.operations.createCardCheckoutSession(),
    branches: [goToBranch('created', created), goToBranch('providerRejected', providerRejected), goToBranch('uncertain', uncertain), goToBranch('invalidResponse', invalidResponse), goToBranch('defect', defect)] });
}

/** ACH Checkout Step settings with named manifest branches. */
export interface CreateACHCheckoutSessionStepConfig<StepInput> extends Omit<MutationStepConfig<StepInput, CreateACHCheckoutSessionInput, CheckoutSession>, 'operation' | 'connection' | 'branches'> {
  /** Bound Stripe connection. */ connection: StripeConnection;
  /** Static connection name; must match the bound connection. */ connectionName: string;
  /** Required success destination. */ created: StepTarget<MutationResult<CheckoutSession>>;
  /** Optional conclusive rejection destination. */ providerRejected?: StepTarget<MutationResult<CheckoutSession>>;
  /** Optional unknown-outcome destination. */ uncertain?: StepTarget<MutationResult<CheckoutSession>>;
  /** Optional invalid-response destination. */ invalidResponse?: StepTarget<MutationResult<CheckoutSession>>;
  /** Optional local-defect destination. */ defect?: StepTarget<MutationResult<CheckoutSession>>;
}

/** Create an ACH Checkout Step from the manifest operation. */
export function newCreateACHCheckoutSessionStep<StepInput>(configuration: CreateACHCheckoutSessionStepConfig<StepInput>): MutationStep<StepInput, CreateACHCheckoutSessionInput, CheckoutSession> {
  validateConnectionName(configuration?.connection, configuration?.connectionName);
  const { connection, connectionName, created, providerRejected, uncertain, invalidResponse, defect, ...stepConfiguration } = configuration;
  void connectionName;
  return newMutationStep({ ...stepConfiguration, connection: connection.reference, operation: connection.operations.createACHCheckoutSession(),
    branches: [goToBranch('created', created), goToBranch('providerRejected', providerRejected), goToBranch('uncertain', uncertain), goToBranch('invalidResponse', invalidResponse), goToBranch('defect', defect)] });
}

/** Checkout Session query Step settings with named manifest branches. */
export interface GetCheckoutSessionStepConfig<StepInput> extends Omit<QueryStepConfig<StepInput, GetCheckoutSessionInput, CheckoutSession>, 'operation' | 'connection' | 'branches'> {
  /** Bound Stripe connection. */ connection: StripeConnection;
  /** Static connection name; must match the bound connection. */ connectionName: string;
  /** Required found destination. */ found: StepTarget<QueryResult<CheckoutSession>>;
  /** Optional missing-session destination. */ notFound?: StepTarget<QueryResult<CheckoutSession>>;
  /** Optional conclusive rejection destination. */ providerRejected?: StepTarget<QueryResult<CheckoutSession>>;
  /** Optional invalid-response destination. */ invalidResponse?: StepTarget<QueryResult<CheckoutSession>>;
  /** Optional local-defect destination. */ defect?: StepTarget<QueryResult<CheckoutSession>>;
}

/** Create a Checkout Session query Step from the manifest operation. */
export function newGetCheckoutSessionStep<StepInput>(configuration: GetCheckoutSessionStepConfig<StepInput>): QueryStep<StepInput, GetCheckoutSessionInput, CheckoutSession> {
  validateConnectionName(configuration?.connection, configuration?.connectionName);
  const { connection, connectionName, found, notFound, providerRejected, invalidResponse, defect, ...stepConfiguration } = configuration;
  void connectionName;
  return newQueryStep({ ...stepConfiguration, connection: connection.reference, operation: connection.operations.getCheckoutSession(),
    branches: [goToBranch('found', found), goToBranch('notFound', notFound), goToBranch('providerRejected', providerRejected), goToBranch('invalidResponse', invalidResponse), goToBranch('defect', defect)] });
}

/** PaymentIntent query Step settings with named manifest branches. */
export interface GetPaymentIntentStepConfig<StepInput> extends Omit<QueryStepConfig<StepInput, GetPaymentIntentInput, PaymentIntent>, 'operation' | 'connection' | 'branches'> {
  /** Bound Stripe connection. */ connection: StripeConnection;
  /** Static connection name; must match the bound connection. */ connectionName: string;
  /** Required found destination. */ found: StepTarget<QueryResult<PaymentIntent>>;
  /** Optional missing-intent destination. */ notFound?: StepTarget<QueryResult<PaymentIntent>>;
  /** Optional conclusive rejection destination. */ providerRejected?: StepTarget<QueryResult<PaymentIntent>>;
  /** Optional invalid-response destination. */ invalidResponse?: StepTarget<QueryResult<PaymentIntent>>;
  /** Optional local-defect destination. */ defect?: StepTarget<QueryResult<PaymentIntent>>;
}

/** Create a PaymentIntent query Step from the manifest operation. */
export function newGetPaymentIntentStep<StepInput>(configuration: GetPaymentIntentStepConfig<StepInput>): QueryStep<StepInput, GetPaymentIntentInput, PaymentIntent> {
  validateConnectionName(configuration?.connection, configuration?.connectionName);
  const { connection, connectionName, found, notFound, providerRejected, invalidResponse, defect, ...stepConfiguration } = configuration;
  void connectionName;
  return newQueryStep({ ...stepConfiguration, connection: connection.reference, operation: connection.operations.getPaymentIntent(),
    branches: [goToBranch('found', found), goToBranch('notFound', notFound), goToBranch('providerRejected', providerRejected), goToBranch('invalidResponse', invalidResponse), goToBranch('defect', defect)] });
}

/** Refund Step settings with named manifest branches. */
export interface CreateRefundStepConfig<StepInput> extends Omit<MutationStepConfig<StepInput, CreateRefundInput, Refund>, 'operation' | 'connection' | 'branches'> {
  /** Bound Stripe connection. */ connection: StripeConnection;
  /** Static connection name; must match the bound connection. */ connectionName: string;
  /** Required refund destination. */ refunded: StepTarget<MutationResult<Refund>>;
  /** Optional conclusive rejection destination. */ providerRejected?: StepTarget<MutationResult<Refund>>;
  /** Optional unknown-outcome destination. */ uncertain?: StepTarget<MutationResult<Refund>>;
  /** Optional invalid-response destination. */ invalidResponse?: StepTarget<MutationResult<Refund>>;
  /** Optional local-defect destination. */ defect?: StepTarget<MutationResult<Refund>>;
}

/** Create a refund Step from the manifest operation. */
export function newCreateRefundStep<StepInput>(configuration: CreateRefundStepConfig<StepInput>): MutationStep<StepInput, CreateRefundInput, Refund> {
  validateConnectionName(configuration?.connection, configuration?.connectionName);
  const { connection, connectionName, refunded, providerRejected, uncertain, invalidResponse, defect, ...stepConfiguration } = configuration;
  void connectionName;
  return newMutationStep({ ...stepConfiguration, connection: connection.reference, operation: connection.operations.createRefund(),
    branches: [goToBranch('refunded', refunded), goToBranch('providerRejected', providerRejected), goToBranch('uncertain', uncertain), goToBranch('invalidResponse', invalidResponse), goToBranch('defect', defect)] });
}

function validateConnectionName(connection: StripeConnection | undefined, connectionName: string | undefined): void {
  if (!connection) throw new TypeError('Stripe connection is required');
  if (connectionName !== connection.reference.name) throw new TypeError(`Stripe connector Step connectionName ${JSON.stringify(connectionName)} must equal its connection name ${JSON.stringify(connection.reference.name)}`);
}
