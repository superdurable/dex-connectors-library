// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutation, newQuery, type MockMutation, type MockQuery } from '@superdurable/connectors-sdk/connectormock';
import type { CheckoutSession, CreateACHCheckoutSessionInput, CreateCardCheckoutSessionInput, CreateRefundInput, GetCheckoutSessionInput, GetPaymentIntentInput, PaymentIntent, Refund } from './models.js';
import { CreateACHCheckoutSessionDefinition, CreateCardCheckoutSessionDefinition, CreateRefundDefinition, GetCheckoutSessionDefinition, GetPaymentIntentDefinition } from './definitions.js';
import { newConnectionWithOperations, type Connection } from './connection.js';
import * as generatedMock from './generated/mock.js';

export {
  createCardCheckoutSessionCreated, createCardCheckoutSessionProviderRejected, createCardCheckoutSessionInvalidResponse,
  createCardCheckoutSessionDefect, createCardCheckoutSessionRetry, createCardCheckoutSessionUncertain,
  createCardCheckoutSessionCreatedCardSession, createCardCheckoutSessionInvalidCardRequest,
  createCardCheckoutSessionUnconfirmedCardSession, createCardCheckoutSessionMalformedCardSession,
  createCardCheckoutSessionInvalidCardCurrency, createCardCheckoutSessionDefault,
  createACHCheckoutSessionCreated, createACHCheckoutSessionProviderRejected, createACHCheckoutSessionInvalidResponse,
  createACHCheckoutSessionDefect, createACHCheckoutSessionRetry, createACHCheckoutSessionUncertain,
  createACHCheckoutSessionCreatedSession, createACHCheckoutSessionInvalidRequest,
  createACHCheckoutSessionUnconfirmedSession, createACHCheckoutSessionMalformedSession,
  createACHCheckoutSessionInvalidCurrency, createACHCheckoutSessionDefault,
  getCheckoutSessionFound, getCheckoutSessionNotFound, getCheckoutSessionProviderRejected,
  getCheckoutSessionInvalidResponse, getCheckoutSessionDefect, getCheckoutSessionRetry,
  getCheckoutSessionPaidSession, getCheckoutSessionMissingSession, getCheckoutSessionUnauthorizedKey,
  getCheckoutSessionMalformedSession, getCheckoutSessionInvalidSessionID, getCheckoutSessionDefault,
  getPaymentIntentFound, getPaymentIntentNotFound, getPaymentIntentProviderRejected,
  getPaymentIntentInvalidResponse, getPaymentIntentDefect, getPaymentIntentRetry,
  getPaymentIntentPartiallyRefundedPayment, getPaymentIntentMissingPaymentIntent,
  getPaymentIntentUnauthorizedKey, getPaymentIntentMalformedPaymentIntent,
  getPaymentIntentInvalidPaymentIntentID, getPaymentIntentDefault,
  createRefundRefunded, createRefundProviderRejected, createRefundInvalidResponse,
  createRefundDefect, createRefundRetry, createRefundUncertain,
  createRefundSucceededRefund, createRefundAlreadyRefunded, createRefundUnconfirmedRefund,
  createRefundMalformedRefund, createRefundInvalidAmount, createRefundDefault,
} from './generated/mock.js';


/** Scripted Stripe connection with manifest scenarios and public request types. */
export class StripeMock {
  readonly #createCardCheckoutSession: MockMutation<CreateCardCheckoutSessionInput, CheckoutSession>;
  readonly #createACHCheckoutSession: MockMutation<CreateACHCheckoutSessionInput, CheckoutSession>;
  readonly #getCheckoutSession: MockQuery<GetCheckoutSessionInput, CheckoutSession>;
  readonly #getPaymentIntent: MockQuery<GetPaymentIntentInput, PaymentIntent>;
  readonly #createRefund: MockMutation<CreateRefundInput, Refund>;
  readonly #connection: Connection;

  /** Construct the five manifest defaults with the published input types. */
  constructor(connectionName: string) {
    this.#createCardCheckoutSession = newMutation<CreateCardCheckoutSessionInput, CheckoutSession>(CreateCardCheckoutSessionDefinition).default(generatedMock.createCardCheckoutSessionDefault());
    this.#createACHCheckoutSession = newMutation<CreateACHCheckoutSessionInput, CheckoutSession>(CreateACHCheckoutSessionDefinition).default(generatedMock.createACHCheckoutSessionDefault());
    this.#getCheckoutSession = newQuery<GetCheckoutSessionInput, CheckoutSession>(GetCheckoutSessionDefinition).default(generatedMock.getCheckoutSessionDefault());
    this.#getPaymentIntent = newQuery<GetPaymentIntentInput, PaymentIntent>(GetPaymentIntentDefinition).default(generatedMock.getPaymentIntentDefault());
    this.#createRefund = newMutation<CreateRefundInput, Refund>(CreateRefundDefinition).default(generatedMock.createRefundDefault());
    this.#connection = newConnectionWithOperations(this, { provider: 'stripe', name: connectionName });
  }

  /** Return the operation connection used by Dex Step factories. */
  connection(): Connection { return this.#connection; }
  /** Script card Checkout attempts. */
  createCardCheckoutSession(): MockMutation<CreateCardCheckoutSessionInput, CheckoutSession> { return this.#createCardCheckoutSession; }
  /** Script ACH Checkout attempts. */
  createACHCheckoutSession(): MockMutation<CreateACHCheckoutSessionInput, CheckoutSession> { return this.#createACHCheckoutSession; }
  /** Script Checkout Session lookups. */
  getCheckoutSession(): MockQuery<GetCheckoutSessionInput, CheckoutSession> { return this.#getCheckoutSession; }
  /** Script PaymentIntent lookups. */
  getPaymentIntent(): MockQuery<GetPaymentIntentInput, PaymentIntent> { return this.#getPaymentIntent; }
  /** Script refund attempts. */
  createRefund(): MockMutation<CreateRefundInput, Refund> { return this.#createRefund; }
  /** Assert every script was consumed without mock failures. */
  assertNoFailures(): void {
    this.#createCardCheckoutSession.assertNoFailures();
    this.#createACHCheckoutSession.assertNoFailures();
    this.#getCheckoutSession.assertNoFailures();
    this.#getPaymentIntent.assertNoFailures();
    this.#createRefund.assertNoFailures();
  }
}

/** Construct one scripted Stripe connection with manifest defaults. */
export function newStripeMock(connectionName: string): StripeMock { return new StripeMock(connectionName); }
