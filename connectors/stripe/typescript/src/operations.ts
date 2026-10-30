// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry,
  zeroReceipt,
  type Call, type Failure, type FailureKind, type Mutation, type MutationAttempt, type Query, type QueryAttempt,
} from '@superdurable/connectors-sdk';
import { CreateACHCheckoutSessionDefinition, CreateCardCheckoutSessionDefinition, CreateRefundDefinition, GetCheckoutSessionDefinition, GetPaymentIntentDefinition } from './definitions.js';
import { decodeCheckoutSession, decodePaymentIntent, decodeRefund, describeStripeRejection, shouldRetryKeyedConflict, shouldRetryKeyedServerFailure, stripeRetryAfter, type StripeClient, type StripeResponse } from './client.js';
import { CreateCheckoutSessionInputSchema, CreateRefundInputSchema, GetCheckoutSessionInputSchema, GetPaymentIntentInputSchema, type CheckoutSession, type CreateACHCheckoutSessionInput, type CreateCardCheckoutSessionInput, type CreateRefundInput, type GetCheckoutSessionInput, type GetPaymentIntentInput, type PaymentIntent, type Refund } from './models.js';

const zeroSession: CheckoutSession = { id: '' };
const zeroIntent: PaymentIntent = { id: '', amount: 0, currency: '', status: '', amountRefunded: 0 };
const zeroRefund: Refund = { refundId: '', status: '', amount: 0, currency: '' };
const maximumMetadataFields = 50;
const refundCallIDMetadataKey = 'dex_call_id';

/** Card Checkout mutation with Go-compatible keyed retry and uncertainty rules. */
export class CreateCardCheckoutSessionOperation implements Mutation<CreateCardCheckoutSessionInput, CheckoutSession> {
  /** Bind one authenticated Stripe client. */
  constructor(private readonly client: StripeClient) {}
  /** Return the manifest operation identity and routes. */
  definition() { return CreateCardCheckoutSessionDefinition; }
  /** Reuse the stable connector Call ID as Stripe's idempotency key. */
  idempotencyKey(callId: string): string { return callId; }
  /** Create one hosted card Checkout Session. */
  invoke(call: Call, input: CreateCardCheckoutSessionInput): Promise<MutationAttempt<CheckoutSession>> {
    return invokeCheckout(this.client, call, input, 'createCardCheckoutSession', 'card');
  }
}

/** ACH Checkout mutation with Go-compatible keyed retry and uncertainty rules. */
export class CreateACHCheckoutSessionOperation implements Mutation<CreateACHCheckoutSessionInput, CheckoutSession> {
  /** Bind one authenticated Stripe client. */
  constructor(private readonly client: StripeClient) {}
  /** Return the manifest operation identity and routes. */
  definition() { return CreateACHCheckoutSessionDefinition; }
  /** Reuse the stable connector Call ID as Stripe's idempotency key. */
  idempotencyKey(callId: string): string { return callId; }
  /** Create one hosted ACH Checkout Session. */
  invoke(call: Call, input: CreateACHCheckoutSessionInput): Promise<MutationAttempt<CheckoutSession>> {
    return invokeCheckout(this.client, call, input, 'createACHCheckoutSession', 'us_bank_account');
  }
}

/** Checkout Session read with found, notFound, providerRejected, and invalidResponse routes. */
export class GetCheckoutSessionOperation implements Query<GetCheckoutSessionInput, CheckoutSession> {
  /** Bind one authenticated Stripe client. */
  constructor(private readonly client: StripeClient) {}
  /** Return the manifest operation identity and routes. */
  definition() { return GetCheckoutSessionDefinition; }
  /** Retrieve one Checkout Session by Stripe ID. */
  async invoke(call: Call, input: GetCheckoutSessionInput): Promise<QueryAttempt<CheckoutSession>> {
    const operation = 'getCheckoutSession';
    if (!GetCheckoutSessionInputSchema.safeParse(input).success) return newQueryBranch('defect', zeroSession, zeroReceipt(), stripeFailure(operation, 'VALIDATION', 'Stripe Checkout Session ID is required'));
    const sessionId = input.sessionId?.trim();
    if (!sessionId?.startsWith('cs_') || /[/?#]/.test(sessionId)) return newQueryBranch('defect', zeroSession, zeroReceipt(), stripeFailure(operation, 'VALIDATION', 'Stripe Checkout Session ID is required'));
    const secretKey = await this.client.resolveSecretKey(call);
    if (!secretKey) return queryCredentialDefect(operation, zeroSession);
    let response: StripeResponse;
    try { response = await this.client.sendStripeRequest(call, secretKey, 'GET', `checkout/sessions/${encodeURIComponent(sessionId)}`); }
    catch { return newQueryRetry(stripeFailure(operation, 'AVAILABILITY', 'Stripe Checkout Session is temporarily unavailable')); }
    if (response.status === 429) return newQueryRetry(stripeFailure(operation, 'RATE_LIMIT', 'Stripe rate limited the Checkout Session query'), stripeRetryAfter(response));
    if (response.status >= 500) return newQueryRetry(stripeFailure(operation, 'AVAILABILITY', 'Stripe Checkout Session is temporarily unavailable'));
    if (response.status === 404) return newQueryBranch('notFound', zeroSession, this.client.receipt(call, response, sessionId), stripeFailure(operation, 'NOT_FOUND', 'Stripe Checkout Session was not found'));
    if (response.status < 200 || response.status >= 300) return newQueryBranch('providerRejected', zeroSession, this.client.receipt(call, response, sessionId), classifyStripeRejection(operation, response));
    if (response.isTooLarge) return newQueryBranch('invalidResponse', zeroSession, this.client.receipt(call, response, sessionId), stripeFailure(operation, 'RESPONSE_TOO_LARGE', 'Stripe response exceeds configured size limit'));
    try {
      const session = decodeCheckoutSession(response.body);
      return newQueryBranch('found', session, this.client.receipt(call, response, session.id));
    } catch (error) {
      return newQueryBranch('invalidResponse', zeroSession, this.client.receipt(call, response, sessionId), stripeFailure(operation, 'PROTOCOL', errorMessage(error)));
    }
  }
}

/** PaymentIntent read with expanded latest-charge refund data. */
export class GetPaymentIntentOperation implements Query<GetPaymentIntentInput, PaymentIntent> {
  /** Bind one authenticated Stripe client. */
  constructor(private readonly client: StripeClient) {}
  /** Return the manifest operation identity and routes. */
  definition() { return GetPaymentIntentDefinition; }
  /** Retrieve one PaymentIntent and its latest charge. */
  async invoke(call: Call, input: GetPaymentIntentInput): Promise<QueryAttempt<PaymentIntent>> {
    const operation = 'getPaymentIntent';
    if (!GetPaymentIntentInputSchema.safeParse(input).success) return newQueryBranch('defect', zeroIntent, zeroReceipt(), stripeFailure(operation, 'VALIDATION', 'Stripe PaymentIntent ID must be a pi_ identifier'));
    const paymentIntentId = input.paymentIntentId?.trim();
    const inputError = validatePaymentIntentID(paymentIntentId);
    if (inputError) return newQueryBranch('defect', zeroIntent, zeroReceipt(), stripeFailure(operation, 'VALIDATION', inputError));
    const secretKey = await this.client.resolveSecretKey(call);
    if (!secretKey) return queryCredentialDefect(operation, zeroIntent);
    const values = new URLSearchParams({ 'expand[]': 'latest_charge' });
    let response: StripeResponse;
    try { response = await this.client.sendStripeRequest(call, secretKey, 'GET', `payment_intents/${encodeURIComponent(paymentIntentId)}`, values); }
    catch { return newQueryRetry(stripeFailure(operation, 'AVAILABILITY', 'Stripe PaymentIntent is temporarily unavailable')); }
    if (response.status === 429) return newQueryRetry(stripeFailure(operation, 'RATE_LIMIT', 'Stripe rate limited the PaymentIntent query'), stripeRetryAfter(response));
    if (response.status >= 500) return newQueryRetry(stripeFailure(operation, 'AVAILABILITY', 'Stripe PaymentIntent is temporarily unavailable'));
    if (response.status === 404) return newQueryBranch('notFound', zeroIntent, this.client.receipt(call, response, paymentIntentId), stripeFailure(operation, 'NOT_FOUND', describeStripeRejection('Stripe PaymentIntent was not found', response)));
    if (response.status < 200 || response.status >= 300) return newQueryBranch('providerRejected', zeroIntent, this.client.receipt(call, response, paymentIntentId), classifyStripeRejection(operation, response));
    if (response.isTooLarge) return newQueryBranch('invalidResponse', zeroIntent, this.client.receipt(call, response, paymentIntentId), stripeFailure(operation, 'RESPONSE_TOO_LARGE', 'Stripe response exceeds configured size limit'));
    try {
      const intent = decodePaymentIntent(response.body);
      return newQueryBranch('found', intent, this.client.receipt(call, response, intent.id));
    } catch (error) {
      return newQueryBranch('invalidResponse', zeroIntent, this.client.receipt(call, response, paymentIntentId), stripeFailure(operation, 'PROTOCOL', errorMessage(error)));
    }
  }
}

/** Refund mutation with keyed retry and bounded read-back after an unconfirmed server response. */
export class CreateRefundOperation implements Mutation<CreateRefundInput, Refund> {
  /** Bind one authenticated Stripe client. */
  constructor(private readonly client: StripeClient) {}
  /** Return the manifest operation identity and routes. */
  definition() { return CreateRefundDefinition; }
  /** Reuse the stable connector Call ID as Stripe's idempotency key. */
  idempotencyKey(callId: string): string { return callId; }
  /** Create one Stripe refund and reconcile an unconfirmed server response. */
  async invoke(call: Call, input: CreateRefundInput): Promise<MutationAttempt<Refund>> {
    const operation = 'createRefund';
    if (!CreateRefundInputSchema.safeParse(input).success) return newMutationBranch('defect', zeroRefund, zeroReceipt(), stripeFailure(operation, 'VALIDATION', 'Stripe refund input is invalid'));
    const inputError = validateRefundInput(input);
    if (inputError) return newMutationBranch('defect', zeroRefund, zeroReceipt(), stripeFailure(operation, 'VALIDATION', inputError));
    const secretKey = await this.client.resolveSecretKey(call);
    if (!secretKey) return mutationCredentialDefect(operation, zeroRefund);
    const values = new URLSearchParams({ payment_intent: input.paymentIntentId, amount: String(input.amount) });
    if (input.reason) values.set('reason', input.reason);
    for (const [key, value] of Object.entries(input.metadata ?? {})) values.set(`metadata[${key}]`, value);
    values.set(`metadata[${refundCallIDMetadataKey}]`, call.id);
    let response: StripeResponse;
    try { response = await this.client.sendStripeRequest(call, secretKey, 'POST', 'refunds', values); }
    catch { return newMutationRetry(stripeFailure(operation, 'TRANSPORT', 'Stripe refund response is unavailable')); }
    if (response.status === 429) return newMutationRetry(stripeFailure(operation, 'RATE_LIMIT', 'Stripe rate limited the refund request'), stripeRetryAfter(response));
    if (response.status === 409 && shouldRetryKeyedConflict(response)) return newMutationRetry(stripeFailure(operation, 'CONFLICT', 'Stripe is still processing a request with this idempotency key'), stripeRetryAfter(response));
    if (response.status >= 500) {
      if (shouldRetryKeyedServerFailure(response)) return newMutationRetry(stripeFailure(operation, 'AVAILABILITY', 'Stripe refund is temporarily unavailable'), stripeRetryAfter(response));
      return this.readBackRefund(call, secretKey, input.paymentIntentId, response);
    }
    if (response.status < 200 || response.status >= 300) return newMutationBranch('providerRejected', zeroRefund, this.client.receipt(call, response, ''), classifyRefundRejection(response));
    if (response.isTooLarge) return newMutationBranch('invalidResponse', zeroRefund, this.client.receipt(call, response, ''), stripeFailure(operation, 'RESPONSE_TOO_LARGE', 'Stripe response exceeds configured size limit'));
    try {
      const refund = decodeRefund(response.body);
      return newMutationBranch('refunded', refund, this.client.receipt(call, response, refund.refundId));
    } catch (error) {
      return newMutationBranch('invalidResponse', zeroRefund, this.client.receipt(call, response, ''), stripeFailure(operation, 'PROTOCOL', errorMessage(error)));
    }
  }

  private async readBackRefund(call: Call, secretKey: string, paymentIntentId: string, original: StripeResponse): Promise<MutationAttempt<Refund>> {
    const values = new URLSearchParams({ payment_intent: paymentIntentId, limit: '100' });
    for (let page = 0; page < 3; page++) {
      let response: StripeResponse;
      try { response = await this.client.sendStripeRequest(call, secretKey, 'GET', 'refunds', values); }
      catch { return newMutationRetry(stripeFailure('createRefund', 'AVAILABILITY', 'Stripe refund read-back is temporarily unavailable')); }
      if (response.status === 429 || response.status >= 500) return newMutationRetry(stripeFailure('createRefund', 'AVAILABILITY', 'Stripe refund read-back is temporarily unavailable'), stripeRetryAfter(response));
      if (response.status === 401 || response.status === 403) {
        const message = response.status === 401
          ? 'Stripe refund outcome cannot be confirmed after a server error because Stripe rejected the API key for the refund read-back; check that the key is still valid'
          : 'Stripe refund outcome cannot be confirmed after a server error because the API key lacks permission for the refund read-back; grant it Refund read permission';
        return newMutationUncertain(zeroRefund, stripeFailure('createRefund', response.status === 401 ? 'AUTHENTICATION' : 'AUTHORIZATION', message), this.client.receipt(call, original, ''));
      }
      if (response.status < 200 || response.status >= 300 || response.isTooLarge) break;
      let parsed: unknown;
      try { parsed = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown; } catch { break; }
      if (!isRefundList(parsed)) break;
      let lastRefundId = '';
      for (const candidate of parsed.data) {
        if (!candidate || typeof candidate !== 'object' || typeof candidate.id !== 'string' || !candidate.id.startsWith('re_')) return this.uncertainRefund(call, original);
        lastRefundId = candidate.id;
        if (candidate.metadata && typeof candidate.metadata === 'object' && candidate.metadata[refundCallIDMetadataKey] === call.id) {
          try {
            const refund = decodeRefund(Buffer.from(JSON.stringify(candidate)));
            return newMutationBranch('refunded', refund, this.client.receipt(call, response, refund.refundId));
          } catch { return this.uncertainRefund(call, original); }
        }
      }
      if (!parsed.has_more || !lastRefundId) break;
      values.set('starting_after', lastRefundId);
    }
    return this.uncertainRefund(call, original);
  }

  private uncertainRefund(call: Call, response: StripeResponse): MutationAttempt<Refund> {
    return newMutationUncertain(zeroRefund, stripeFailure('createRefund', 'AVAILABILITY', 'Stripe refund outcome cannot be confirmed after a server error'), this.client.receipt(call, response, ''));
  }
}

async function invokeCheckout(client: StripeClient, call: Call, input: CreateACHCheckoutSessionInput, operation: 'createCardCheckoutSession' | 'createACHCheckoutSession', paymentMethod: 'card' | 'us_bank_account'): Promise<MutationAttempt<CheckoutSession>> {
  if (!CreateCheckoutSessionInputSchema.safeParse(input).success) return newMutationBranch('defect', zeroSession, zeroReceipt(), stripeFailure(operation, 'VALIDATION', 'Stripe Checkout Session input is invalid'));
  const inputError = validateCheckoutInput(input);
  if (inputError) return newMutationBranch('defect', zeroSession, zeroReceipt(), stripeFailure(operation, 'VALIDATION', inputError));
  const secretKey = await client.resolveSecretKey(call);
  if (!secretKey) return mutationCredentialDefect(operation, zeroSession);
  const values = new URLSearchParams({
    mode: 'payment', 'payment_method_types[0]': paymentMethod, client_reference_id: input.clientReferenceId,
    customer_email: input.customerEmail, success_url: input.successUrl, cancel_url: input.cancelUrl,
    'line_items[0][quantity]': '1', 'line_items[0][price_data][currency]': input.currency,
    'line_items[0][price_data][unit_amount]': String(input.unitAmount),
    'line_items[0][price_data][product_data][name]': input.productName,
  });
  for (const [key, value] of Object.entries(input.metadata ?? {})) {
    values.set(`metadata[${key}]`, value);
    values.set(`payment_intent_data[metadata][${key}]`, value);
  }
  if (input.expiresAt) values.set('expires_at', String(Math.floor(Date.parse(input.expiresAt) / 1000)));
  let response: StripeResponse;
  try { response = await client.sendStripeRequest(call, secretKey, 'POST', 'checkout/sessions', values); }
  catch { return newMutationRetry(stripeFailure(operation, 'TRANSPORT', 'Stripe Checkout Session response is unavailable')); }
  if (response.status === 429) return newMutationRetry(stripeFailure(operation, 'RATE_LIMIT', 'Stripe rate limited the Checkout Session request'), stripeRetryAfter(response));
  if (response.status === 409 && shouldRetryKeyedConflict(response)) return newMutationRetry(stripeFailure(operation, 'CONFLICT', 'Stripe reported a conflicting Checkout Session request with this idempotency key'), stripeRetryAfter(response));
  if (response.status >= 500) {
    if (shouldRetryKeyedServerFailure(response)) return newMutationRetry(stripeFailure(operation, 'AVAILABILITY', 'Stripe Checkout Session is temporarily unavailable'), stripeRetryAfter(response));
    return newMutationUncertain(zeroSession, stripeFailure(operation, 'AVAILABILITY', 'Stripe Checkout Session outcome cannot be confirmed after a server error'), client.receipt(call, response, ''));
  }
  if (response.status < 200 || response.status >= 300) return newMutationBranch('providerRejected', zeroSession, client.receipt(call, response, ''), classifyStripeRejection(operation, response));
  if (response.isTooLarge) return newMutationBranch('invalidResponse', zeroSession, client.receipt(call, response, ''), stripeFailure(operation, 'RESPONSE_TOO_LARGE', 'Stripe response exceeds configured size limit'));
  try {
    const session = decodeCheckoutSession(response.body);
    return newMutationBranch('created', session, client.receipt(call, response, session.id));
  } catch (error) {
    return newMutationBranch('invalidResponse', zeroSession, client.receipt(call, response, ''), stripeFailure(operation, 'PROTOCOL', errorMessage(error)));
  }
}

function validateCheckoutInput(input: CreateACHCheckoutSessionInput): string | undefined {
  if (!input.clientReferenceId?.trim() || Array.from(input.clientReferenceId).length > 200) return 'client reference ID must be non-empty and at most 200 characters';
  const email = input.customerEmail?.trim();
  // This simplified check differs from Go's mail.ParseAddress on uncommon quoted and comma-containing local parts.
  if (!email || !/^[^\s@<>]+@[^\s@<>]+$/.test(email) || email !== input.customerEmail) return 'customer email must be one plain email address';
  for (const [field, value] of [['success', input.successUrl], ['cancel', input.cancelUrl]] as const) {
    const redirectError = validateRedirectURL(value, field);
    if (redirectError) return redirectError;
  }
  if (!/^[a-z]{3}$/.test(input.currency)) return 'currency must be a lowercase three-letter code';
  if (!Number.isSafeInteger(input.unitAmount) || input.unitAmount <= 0 || input.unitAmount > 99_999_999) return 'unit amount must be from 1 through 99999999';
  if (!input.productName?.trim() || Array.from(input.productName).length > 250) return 'product name must be non-empty and at most 250 characters';
  if (input.expiresAt && !Number.isFinite(Date.parse(input.expiresAt))) return 'expiresAt must be an RFC3339 timestamp';
  return validateMetadata(input.metadata);
}

function validateRedirectURL(value: string, field: string): string | undefined {
  let parsed: URL;
  try { parsed = new URL(value); } catch { return `${field} URL must be absolute and must not contain credentials`; }
  if (!parsed.hostname || parsed.username || parsed.password) return `${field} URL must be absolute and must not contain credentials`;
  if (parsed.protocol !== 'https:' && !['localhost', '127.0.0.1'].includes(parsed.hostname)) return `${field} URL must use HTTPS`;
  return undefined;
}

function validatePaymentIntentID(value: string): string | undefined {
  if (!value?.startsWith('pi_') || /[/?#]/.test(value)) return 'Stripe PaymentIntent ID must be a pi_ identifier';
  return undefined;
}

function validateRefundInput(input: CreateRefundInput): string | undefined {
  const intentError = validatePaymentIntentID(input.paymentIntentId);
  if (intentError) return intentError;
  if (!Number.isSafeInteger(input.amount) || input.amount <= 0) return 'refund amount must be positive';
  if (input.reason && !['duplicate', 'fraudulent', 'requested_by_customer'].includes(input.reason)) return 'refund reason must be duplicate, fraudulent, or requested_by_customer';
  if (Object.hasOwn(input.metadata ?? {}, refundCallIDMetadataKey)) return 'refund metadata key "dex_call_id" is reserved for call reconciliation';
  if (Object.keys(input.metadata ?? {}).length >= maximumMetadataFields) return 'refund metadata cannot contain more than 49 application fields';
  return validateMetadata(input.metadata);
}

function validateMetadata(metadata: Record<string, string> | undefined): string | undefined {
  if (Object.keys(metadata ?? {}).length > maximumMetadataFields) return 'metadata cannot contain more than 50 fields';
  for (const [key, value] of Object.entries(metadata ?? {})) {
    if (!key.trim() || Array.from(key).length > 40 || key.includes('[') || key.includes(']')) return 'metadata keys must be non-empty, at most 40 characters, and contain no square brackets';
    if (Array.from(value).length > 500) return 'metadata values cannot exceed 500 characters';
  }
  return undefined;
}

function stripeFailure(operation: string, kind: FailureKind, message: string): Failure { return { provider: 'stripe', operation, kind, message }; }

function queryCredentialDefect<T>(operation: string, zeroValue: T): QueryAttempt<T> {
  return newQueryBranch('defect', zeroValue, zeroReceipt(), stripeFailure(operation, 'AUTHENTICATION', 'Stripe connection credentials are unavailable'));
}

function mutationCredentialDefect<T>(operation: string, zeroValue: T): MutationAttempt<T> {
  return newMutationBranch('defect', zeroValue, zeroReceipt(), stripeFailure(operation, 'AUTHENTICATION', 'Stripe connection credentials are unavailable'));
}

function classifyStripeRejection(operation: string, response: StripeResponse): Failure {
  const kind: FailureKind = response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : response.status === 404 ? 'NOT_FOUND' : response.status === 409 ? 'CONFLICT' : 'PROVIDER_REJECTION';
  return stripeFailure(operation, kind, 'Stripe rejected the request');
}

function classifyRefundRejection(response: StripeResponse): Failure {
  return { ...classifyStripeRejection('createRefund', response), message: describeStripeRejection('Stripe rejected the refund', response) };
}

function isRefundList(value: unknown): value is { object: 'list'; data: Array<{ id?: string; metadata?: Record<string, string> }>; has_more: boolean } {
  return !!value && typeof value === 'object' && 'object' in value && value.object === 'list' &&
    'data' in value && Array.isArray(value.data) && value.data.length <= 100 &&
    'has_more' in value && typeof value.has_more === 'boolean';
}

function errorMessage(error: unknown): string { return error instanceof Error ? error.message : 'Stripe response is invalid'; }
