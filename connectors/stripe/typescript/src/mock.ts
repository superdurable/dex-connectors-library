// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { branchCase, newMutation, newQuery, retryCase, uncertainCase, type Case, type MockMutation, type MockQuery } from '@superdurable/connectors-sdk/connectormock';
import type { Failure, FailureKind } from '@superdurable/connectors-sdk';
import type { CheckoutSession, PaymentIntent, Refund } from './models.js';
import type { CreateACHCheckoutSessionInput, CreateCardCheckoutSessionInput, CreateRefundInput, GetCheckoutSessionInput, GetPaymentIntentInput } from './models.js';
import { CreateACHCheckoutSessionDefinition, CreateCardCheckoutSessionDefinition, CreateRefundDefinition, GetCheckoutSessionDefinition, GetPaymentIntentDefinition } from './definitions.js';
import { newStripeConnectionWithOperations, type StripeConnection } from './connection.js';

const emptySession: CheckoutSession = { id: '' };
const emptyIntent: PaymentIntent = { id: '', amount: 0, currency: '', status: '', amountRefunded: 0 };
const emptyRefund: Refund = { refundId: '', status: '', amount: 0, currency: '' };

function failure(operation: string, kind: FailureKind, message: string): Failure {
  return { kind, provider: 'stripe', operation, message };
}

/** Script a safe retry of one card Checkout attempt. */
export function createCardCheckoutSessionRetry(providerFailure: Failure, retryAfterMs = 0): Case<CheckoutSession> { return retryCase(providerFailure, retryAfterMs); }
/** Script an unconfirmable card Checkout attempt. */
export function createCardCheckoutSessionUncertain(value: CheckoutSession, providerFailure: Failure): Case<CheckoutSession> { return uncertainCase(value, providerFailure); }
/** Manifest mock createdCardSession, also the default card scenario. */
export function createCardCheckoutSessionCreatedCardSession(): Case<CheckoutSession> { return branchCase('created', { id: 'cs_test_card123', url: 'https://checkout.stripe.com/c/pay/cs_test_card123', clientReferenceId: 'registration-card-42', paymentStatus: 'unpaid', status: 'open', currency: 'usd', amountTotal: 7500, metadata: { registration_id: 'registration-card-42' } }); }
/** Manifest mock invalidCardRequest. */
export function createCardCheckoutSessionInvalidCardRequest(): Case<CheckoutSession> { return branchCase('providerRejected', emptySession, failure('createCardCheckoutSession', 'PROVIDER_REJECTION', 'Stripe rejected the request')); }
/** Manifest mock unconfirmedCardSession. */
export function createCardCheckoutSessionUnconfirmedCardSession(): Case<CheckoutSession> { return uncertainCase(emptySession, failure('createCardCheckoutSession', 'AVAILABILITY', 'Stripe Checkout Session outcome cannot be confirmed after a server error')); }
/** Manifest mock malformedCardSession. */
export function createCardCheckoutSessionMalformedCardSession(): Case<CheckoutSession> { return branchCase('invalidResponse', emptySession, failure('createCardCheckoutSession', 'PROTOCOL', 'Stripe response is invalid: Stripe returned an invalid Checkout Session')); }
/** Manifest mock invalidCardCurrency. */
export function createCardCheckoutSessionInvalidCardCurrency(): Case<CheckoutSession> { return branchCase('defect', emptySession, failure('createCardCheckoutSession', 'VALIDATION', 'currency must be a lowercase three-letter code')); }
/** Default card Checkout scenario. */
export function createCardCheckoutSessionDefault(): Case<CheckoutSession> { return createCardCheckoutSessionCreatedCardSession(); }

/** Script a safe retry of one ACH Checkout attempt. */
export function createACHCheckoutSessionRetry(providerFailure: Failure, retryAfterMs = 0): Case<CheckoutSession> { return retryCase(providerFailure, retryAfterMs); }
/** Script an unconfirmable ACH Checkout attempt. */
export function createACHCheckoutSessionUncertain(value: CheckoutSession, providerFailure: Failure): Case<CheckoutSession> { return uncertainCase(value, providerFailure); }
/** Manifest mock createdSession, also the default ACH scenario. */
export function createACHCheckoutSessionCreatedSession(): Case<CheckoutSession> { return branchCase('created', { id: 'cs_test_a1b2c3', url: 'https://checkout.stripe.com/c/pay/cs_test_a1b2c3', clientReferenceId: 'registration-42', paymentStatus: 'unpaid', status: 'open', currency: 'usd', amountTotal: 7500, metadata: { registration_id: 'registration-42' } }); }
/** Manifest mock invalidRequest. */
export function createACHCheckoutSessionInvalidRequest(): Case<CheckoutSession> { return branchCase('providerRejected', emptySession, failure('createACHCheckoutSession', 'PROVIDER_REJECTION', 'Stripe rejected the request')); }
/** Manifest mock unconfirmedSession. */
export function createACHCheckoutSessionUnconfirmedSession(): Case<CheckoutSession> { return uncertainCase(emptySession, failure('createACHCheckoutSession', 'AVAILABILITY', 'Stripe Checkout Session outcome cannot be confirmed after a server error')); }
/** Manifest mock malformedSession. */
export function createACHCheckoutSessionMalformedSession(): Case<CheckoutSession> { return branchCase('invalidResponse', emptySession, failure('createACHCheckoutSession', 'PROTOCOL', 'Stripe response is invalid: Stripe returned an invalid Checkout Session')); }
/** Manifest mock invalidCurrency. */
export function createACHCheckoutSessionInvalidCurrency(): Case<CheckoutSession> { return branchCase('defect', emptySession, failure('createACHCheckoutSession', 'VALIDATION', 'currency must be a lowercase three-letter code')); }
/** Default ACH Checkout scenario. */
export function createACHCheckoutSessionDefault(): Case<CheckoutSession> { return createACHCheckoutSessionCreatedSession(); }

/** Script a safe retry of one Checkout Session query. */
export function getCheckoutSessionRetry(providerFailure: Failure, retryAfterMs = 0): Case<CheckoutSession> { return retryCase(providerFailure, retryAfterMs); }
/** Manifest mock paidSession, also the default query scenario. */
export function getCheckoutSessionPaidSession(): Case<CheckoutSession> { return branchCase('found', { id: 'cs_test_a1b2c3', paymentStatus: 'paid', status: 'complete', paymentIntentId: 'pi_3Pabc123', currency: 'usd', amountTotal: 7500 }); }
/** Manifest mock missingSession. */
export function getCheckoutSessionMissingSession(): Case<CheckoutSession> { return branchCase('notFound', emptySession, failure('getCheckoutSession', 'NOT_FOUND', 'Stripe Checkout Session was not found')); }
/** Manifest mock unauthorizedKey. */
export function getCheckoutSessionUnauthorizedKey(): Case<CheckoutSession> { return branchCase('providerRejected', emptySession, failure('getCheckoutSession', 'AUTHENTICATION', 'Stripe rejected the request')); }
/** Manifest mock malformedSession. */
export function getCheckoutSessionMalformedSession(): Case<CheckoutSession> { return branchCase('invalidResponse', emptySession, failure('getCheckoutSession', 'PROTOCOL', 'Stripe response is invalid: Stripe returned an invalid Checkout Session')); }
/** Manifest mock invalidSessionID. */
export function getCheckoutSessionInvalidSessionID(): Case<CheckoutSession> { return branchCase('defect', emptySession, failure('getCheckoutSession', 'VALIDATION', 'Stripe Checkout Session ID is required')); }
/** Default Checkout Session query scenario. */
export function getCheckoutSessionDefault(): Case<CheckoutSession> { return getCheckoutSessionPaidSession(); }

/** Script a safe retry of one PaymentIntent query. */
export function getPaymentIntentRetry(providerFailure: Failure, retryAfterMs = 0): Case<PaymentIntent> { return retryCase(providerFailure, retryAfterMs); }
/** Manifest mock partiallyRefundedPayment, also the default query scenario. */
export function getPaymentIntentPartiallyRefundedPayment(): Case<PaymentIntent> { return branchCase('found', { id: 'pi_3Pabc123', amount: 7500, currency: 'usd', status: 'succeeded', amountRefunded: 2500, customerEmail: 'person@example.com', latestChargeId: 'ch_3Pabc123' }); }
/** Manifest mock missingPaymentIntent. */
export function getPaymentIntentMissingPaymentIntent(): Case<PaymentIntent> { return branchCase('notFound', emptyIntent, failure('getPaymentIntent', 'NOT_FOUND', 'Stripe PaymentIntent was not found: resource_missing')); }
/** Manifest mock unauthorizedKey. */
export function getPaymentIntentUnauthorizedKey(): Case<PaymentIntent> { return branchCase('providerRejected', emptyIntent, failure('getPaymentIntent', 'AUTHENTICATION', 'Stripe rejected the request')); }
/** Manifest mock malformedPaymentIntent. */
export function getPaymentIntentMalformedPaymentIntent(): Case<PaymentIntent> { return branchCase('invalidResponse', emptyIntent, failure('getPaymentIntent', 'PROTOCOL', 'Stripe response is invalid: Stripe returned an invalid PaymentIntent')); }
/** Manifest mock invalidPaymentIntentID. */
export function getPaymentIntentInvalidPaymentIntentID(): Case<PaymentIntent> { return branchCase('defect', emptyIntent, failure('getPaymentIntent', 'VALIDATION', 'Stripe PaymentIntent ID must be a pi_ identifier')); }
/** Default PaymentIntent query scenario. */
export function getPaymentIntentDefault(): Case<PaymentIntent> { return getPaymentIntentPartiallyRefundedPayment(); }

/** Script a safe retry of one refund attempt. */
export function createRefundRetry(providerFailure: Failure, retryAfterMs = 0): Case<Refund> { return retryCase(providerFailure, retryAfterMs); }
/** Script an unconfirmable refund attempt. */
export function createRefundUncertain(value: Refund, providerFailure: Failure): Case<Refund> { return uncertainCase(value, providerFailure); }
/** Manifest mock succeededRefund, also the default refund scenario. */
export function createRefundSucceededRefund(): Case<Refund> { return branchCase('refunded', { refundId: 're_3Pabc123', status: 'succeeded', amount: 2500, currency: 'usd' }); }
/** Manifest mock alreadyRefunded. */
export function createRefundAlreadyRefunded(): Case<Refund> { return branchCase('providerRejected', emptyRefund, failure('createRefund', 'PROVIDER_REJECTION', 'Stripe rejected the refund: charge_already_refunded: Charge ch_3Pabc123 has already been refunded.')); }
/** Manifest mock unconfirmedRefund. */
export function createRefundUnconfirmedRefund(): Case<Refund> { return uncertainCase(emptyRefund, failure('createRefund', 'AVAILABILITY', 'Stripe refund outcome cannot be confirmed after a server error')); }
/** Manifest mock malformedRefund. */
export function createRefundMalformedRefund(): Case<Refund> { return branchCase('invalidResponse', emptyRefund, failure('createRefund', 'PROTOCOL', 'Stripe response is invalid: Stripe returned an invalid refund')); }
/** Manifest mock invalidAmount. */
export function createRefundInvalidAmount(): Case<Refund> { return branchCase('defect', emptyRefund, failure('createRefund', 'VALIDATION', 'refund amount must be positive')); }
/** Default refund scenario. */
export function createRefundDefault(): Case<Refund> { return createRefundSucceededRefund(); }

/** Scripted Stripe connection with every Go manifest default installed. */
export class StripeMock {
  private readonly card: MockMutation<CreateCardCheckoutSessionInput, CheckoutSession>;
  private readonly ach: MockMutation<CreateACHCheckoutSessionInput, CheckoutSession>;
  private readonly checkout: MockQuery<GetCheckoutSessionInput, CheckoutSession>;
  private readonly payment: MockQuery<GetPaymentIntentInput, PaymentIntent>;
  private readonly refund: MockMutation<CreateRefundInput, Refund>;
  private readonly namedConnection: StripeConnection;

  /** Construct the same five default scenarios as Go's stripemock.New. */
  constructor(connectionName: string) {
    this.card = newMutation<CreateCardCheckoutSessionInput, CheckoutSession>(CreateCardCheckoutSessionDefinition).default(createCardCheckoutSessionDefault());
    this.ach = newMutation<CreateACHCheckoutSessionInput, CheckoutSession>(CreateACHCheckoutSessionDefinition).default(createACHCheckoutSessionDefault());
    this.checkout = newQuery<GetCheckoutSessionInput, CheckoutSession>(GetCheckoutSessionDefinition).default(getCheckoutSessionDefault());
    this.payment = newQuery<GetPaymentIntentInput, PaymentIntent>(GetPaymentIntentDefinition).default(getPaymentIntentDefault());
    this.refund = newMutation<CreateRefundInput, Refund>(CreateRefundDefinition).default(createRefundDefault());
    this.namedConnection = newStripeConnectionWithOperations({
      createCardCheckoutSession: () => this.card,
      createACHCheckoutSession: () => this.ach,
      getCheckoutSession: () => this.checkout,
      getPaymentIntent: () => this.payment,
      createRefund: () => this.refund,
    }, { provider: 'stripe', name: connectionName });
  }

  /** Return the operation connection used by Dex Step factories. */
  connection(): StripeConnection { return this.namedConnection; }
  /** Script the card Checkout mutation. */ createCardCheckoutSession(): MockMutation<CreateCardCheckoutSessionInput, CheckoutSession> { return this.card; }
  /** Script the ACH Checkout mutation. */ createACHCheckoutSession(): MockMutation<CreateACHCheckoutSessionInput, CheckoutSession> { return this.ach; }
  /** Script the Checkout Session query. */ getCheckoutSession(): MockQuery<GetCheckoutSessionInput, CheckoutSession> { return this.checkout; }
  /** Script the PaymentIntent query. */ getPaymentIntent(): MockQuery<GetPaymentIntentInput, PaymentIntent> { return this.payment; }
  /** Script the refund mutation. */ createRefund(): MockMutation<CreateRefundInput, Refund> { return this.refund; }

  /** Fail test verification if any operation had an invalid or unscripted answer. */
  assertNoFailures(): void {
    this.card.assertNoFailures();
    this.ach.assertNoFailures();
    this.checkout.assertNoFailures();
    this.payment.assertNoFailures();
    this.refund.assertNoFailures();
  }
}

/** Construct one scripted Stripe connection with manifest defaults. */
export function newStripeMock(connectionName: string): StripeMock { return new StripeMock(connectionName); }


/** Select the created route for createCardCheckoutSession. */
export function createCardCheckoutSessionCreated(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('created', value, providerFailure); }

/** Select the providerRejected route for createCardCheckoutSession. */
export function createCardCheckoutSessionProviderRejected(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('providerRejected', value, providerFailure); }

/** Select the invalidResponse route for createCardCheckoutSession. */
export function createCardCheckoutSessionInvalidResponse(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('invalidResponse', value, providerFailure); }

/** Select the defect route for createCardCheckoutSession. */
export function createCardCheckoutSessionDefect(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('defect', value, providerFailure); }

/** Select the created route for createACHCheckoutSession. */
export function createACHCheckoutSessionCreated(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('created', value, providerFailure); }

/** Select the providerRejected route for createACHCheckoutSession. */
export function createACHCheckoutSessionProviderRejected(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('providerRejected', value, providerFailure); }

/** Select the invalidResponse route for createACHCheckoutSession. */
export function createACHCheckoutSessionInvalidResponse(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('invalidResponse', value, providerFailure); }

/** Select the defect route for createACHCheckoutSession. */
export function createACHCheckoutSessionDefect(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('defect', value, providerFailure); }

/** Select the found route for getCheckoutSession. */
export function getCheckoutSessionFound(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('found', value, providerFailure); }

/** Select the notFound route for getCheckoutSession. */
export function getCheckoutSessionNotFound(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('notFound', value, providerFailure); }

/** Select the providerRejected route for getCheckoutSession. */
export function getCheckoutSessionProviderRejected(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('providerRejected', value, providerFailure); }

/** Select the invalidResponse route for getCheckoutSession. */
export function getCheckoutSessionInvalidResponse(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('invalidResponse', value, providerFailure); }

/** Select the defect route for getCheckoutSession. */
export function getCheckoutSessionDefect(value: CheckoutSession, providerFailure?: Failure): Case<CheckoutSession> { return branchCase('defect', value, providerFailure); }

/** Select the found route for getPaymentIntent. */
export function getPaymentIntentFound(value: PaymentIntent, providerFailure?: Failure): Case<PaymentIntent> { return branchCase('found', value, providerFailure); }

/** Select the notFound route for getPaymentIntent. */
export function getPaymentIntentNotFound(value: PaymentIntent, providerFailure?: Failure): Case<PaymentIntent> { return branchCase('notFound', value, providerFailure); }

/** Select the providerRejected route for getPaymentIntent. */
export function getPaymentIntentProviderRejected(value: PaymentIntent, providerFailure?: Failure): Case<PaymentIntent> { return branchCase('providerRejected', value, providerFailure); }

/** Select the invalidResponse route for getPaymentIntent. */
export function getPaymentIntentInvalidResponse(value: PaymentIntent, providerFailure?: Failure): Case<PaymentIntent> { return branchCase('invalidResponse', value, providerFailure); }

/** Select the defect route for getPaymentIntent. */
export function getPaymentIntentDefect(value: PaymentIntent, providerFailure?: Failure): Case<PaymentIntent> { return branchCase('defect', value, providerFailure); }

/** Select the refunded route for createRefund. */
export function createRefundRefunded(value: Refund, providerFailure?: Failure): Case<Refund> { return branchCase('refunded', value, providerFailure); }

/** Select the providerRejected route for createRefund. */
export function createRefundProviderRejected(value: Refund, providerFailure?: Failure): Case<Refund> { return branchCase('providerRejected', value, providerFailure); }

/** Select the invalidResponse route for createRefund. */
export function createRefundInvalidResponse(value: Refund, providerFailure?: Failure): Case<Refund> { return branchCase('invalidResponse', value, providerFailure); }

/** Select the defect route for createRefund. */
export function createRefundDefect(value: Refund, providerFailure?: Failure): Case<Refund> { return branchCase('defect', value, providerFailure); }
