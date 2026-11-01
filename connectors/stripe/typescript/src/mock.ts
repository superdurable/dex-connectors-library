// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { branchCase, retryCase, uncertainCase, type Case } from '@superdurable/connectors-sdk/connectormock';
import type { Failure } from '@superdurable/connectors-sdk';
import type { CheckoutSession, PaymentIntent, Refund } from './models.js';
import { newConnectionWithOperations, type Connection } from './connection.js';
import { GeneratedMock } from './generated/mock.js';
export {
  createCardCheckoutSessionCreatedCardSession,
  createCardCheckoutSessionInvalidCardRequest,
  createCardCheckoutSessionUnconfirmedCardSession,
  createCardCheckoutSessionMalformedCardSession,
  createCardCheckoutSessionInvalidCardCurrency,
  createCardCheckoutSessionDefault,
  createACHCheckoutSessionCreatedSession,
  createACHCheckoutSessionInvalidRequest,
  createACHCheckoutSessionUnconfirmedSession,
  createACHCheckoutSessionMalformedSession,
  createACHCheckoutSessionInvalidCurrency,
  createACHCheckoutSessionDefault,
  getCheckoutSessionPaidSession,
  getCheckoutSessionMissingSession,
  getCheckoutSessionUnauthorizedKey,
  getCheckoutSessionMalformedSession,
  getCheckoutSessionInvalidSessionID,
  getCheckoutSessionDefault,
  getPaymentIntentPartiallyRefundedPayment,
  getPaymentIntentMissingPaymentIntent,
  getPaymentIntentUnauthorizedKey,
  getPaymentIntentMalformedPaymentIntent,
  getPaymentIntentInvalidPaymentIntentID,
  getPaymentIntentDefault,
  createRefundSucceededRefund,
  createRefundAlreadyRefunded,
  createRefundUnconfirmedRefund,
  createRefundMalformedRefund,
  createRefundInvalidAmount,
  createRefundDefault,
} from './generated/mock.js';

/** Script a safe retry of one card Checkout attempt. */
export function createCardCheckoutSessionRetry(providerFailure: Failure, retryAfterMs = 0): Case<CheckoutSession> { return retryCase(providerFailure, retryAfterMs); }
/** Script an unconfirmable card Checkout attempt. */
export function createCardCheckoutSessionUncertain(value: CheckoutSession, providerFailure: Failure): Case<CheckoutSession> { return uncertainCase(value, providerFailure); }

/** Script a safe retry of one ACH Checkout attempt. */
export function createACHCheckoutSessionRetry(providerFailure: Failure, retryAfterMs = 0): Case<CheckoutSession> { return retryCase(providerFailure, retryAfterMs); }
/** Script an unconfirmable ACH Checkout attempt. */
export function createACHCheckoutSessionUncertain(value: CheckoutSession, providerFailure: Failure): Case<CheckoutSession> { return uncertainCase(value, providerFailure); }

/** Script a safe retry of one Checkout Session query. */
export function getCheckoutSessionRetry(providerFailure: Failure, retryAfterMs = 0): Case<CheckoutSession> { return retryCase(providerFailure, retryAfterMs); }

/** Script a safe retry of one PaymentIntent query. */
export function getPaymentIntentRetry(providerFailure: Failure, retryAfterMs = 0): Case<PaymentIntent> { return retryCase(providerFailure, retryAfterMs); }

/** Script a safe retry of one refund attempt. */
export function createRefundRetry(providerFailure: Failure, retryAfterMs = 0): Case<Refund> { return retryCase(providerFailure, retryAfterMs); }
/** Script an unconfirmable refund attempt. */
export function createRefundUncertain(value: Refund, providerFailure: Failure): Case<Refund> { return uncertainCase(value, providerFailure); }

/** Scripted Stripe connection with every Go manifest default installed. */
export class StripeMock extends GeneratedMock {
  private readonly namedConnection: Connection;

  /** Construct the same five default scenarios as Go's stripemock.New. */
  constructor(connectionName: string) {
    super(connectionName);
    this.namedConnection = newConnectionWithOperations(this, { provider: 'stripe', name: connectionName });
  }

  /** Return the operation connection used by Dex Step factories. */
  override connection(): Connection { return this.namedConnection; }
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
