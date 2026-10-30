// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';
import { BodyTooLargeError, isHeaderSafeCredential, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Call, CredentialProvider, Receipt } from '@superdurable/connectors-sdk';
import type { CheckoutSession, Config, Credentials, PaymentIntent, Refund } from './models.js';
import { CreateACHCheckoutSessionOperation, CreateCardCheckoutSessionOperation, CreateRefundOperation, GetCheckoutSessionOperation, GetPaymentIntentOperation } from './operations.js';

const defaultEndpoint = 'https://api.stripe.com/v1';
const defaultMaxResponseBytes = 1_048_576;
const defaultTimeoutMs = 25_000;

/** HTTP response with the status and headers retained for bounded-body classification. */
export interface StripeResponse {
  /** HTTP status, including non-2xx responses. */ status: number;
  /** Provider headers, excluding secret request headers. */ headers: ProviderResponse['headers'];
  /** Complete bounded body, empty when too large. */ body: Uint8Array;
  /** Whether the body exceeded maxResponseBytes. */ isTooLarge: boolean;
}

/** Injectable HTTP sender; production uses providerhttp's pinned DNS and no-redirect transport. */
export type StripeRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Constructor dependencies for deterministic tests and private gateways. */
export interface StripeClientOptions {
  /** Substitute a safe sender for tests; production should omit this. */ send?: StripeRequestSender;
  /** Receipt clock, called after a provider response. */ now?: () => Date;
}

/** Authenticated Stripe API client. Secrets are resolved only at invocation time. */
export class StripeClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: StripeRequestSender;
  private readonly now: () => Date;

  /** Validate connector configuration and own immutable provider dependencies. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: StripeClientOptions = {}) {
    if (!credentials) throw new TypeError('credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    const size = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(size) || size < 0) throw new TypeError('configuration maxResponseBytes cannot be negative');
    this.maxResponseBytes = size;
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the card Checkout mutation to this client. */
  createCardCheckoutSession(): CreateCardCheckoutSessionOperation { return new CreateCardCheckoutSessionOperation(this); }
  /** Bind the ACH Checkout mutation to this client. */
  createACHCheckoutSession(): CreateACHCheckoutSessionOperation { return new CreateACHCheckoutSessionOperation(this); }
  /** Bind the Checkout Session query to this client. */
  getCheckoutSession(): GetCheckoutSessionOperation { return new GetCheckoutSessionOperation(this); }
  /** Bind the PaymentIntent query to this client. */
  getPaymentIntent(): GetPaymentIntentOperation { return new GetPaymentIntentOperation(this); }
  /** Bind the refund mutation to this client. */
  createRefund(): CreateRefundOperation { return new CreateRefundOperation(this); }

  /** Resolve one current API key without permitting secret-bearing errors. */
  async resolveSecretKey(call: Call): Promise<string | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (credentials.authMethodId !== 'api-key-only' && credentials.authMethodId !== 'api-key-with-webhook') return undefined;
      if (credentials.authMethodId === 'api-key-with-webhook' && !credentials.webhookSecret?.reveal()) return undefined;
      const key = credentials.secretKey?.reveal();
      if (!key || !isHeaderSafeCredential(key)) return undefined;
      return key;
    } catch { return undefined; }
  }

  /** Make one provider request with Go's sorted form encoding and Stripe headers. */
  async sendStripeRequest(call: Call, secretKey: string, method: 'GET' | 'POST', path: string, values?: URLSearchParams): Promise<StripeResponse> {
    const encoded = values ? sortedForm(values) : '';
    const url = `${this.endpoint}/${path}${method === 'GET' && encoded ? `?${encoded}` : ''}`;
    const headers: Record<string, string> = {
      Authorization: `Basic ${Buffer.from(`${secretKey}:`, 'utf8').toString('base64')}`,
      Accept: 'application/json',
    };
    if (method === 'POST') {
      headers['Content-Type'] = 'application/x-www-form-urlencoded';
      headers['Idempotency-Key'] = call.idempotencyKey ?? '';
    }
    const request: ProviderRequest = {
      url, method, headers, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.endpoint === defaultEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP',
      timeoutMs: defaultTimeoutMs,
      signal: call.context.cancellationSignal,
      ...(method === 'POST' && values ? { body: Buffer.from(encoded, 'utf8') } : {}),
    };
    try {
      const response = await this.send(request);
      return { status: response.status, headers: response.headers, body: response.body, isTooLarge: false };
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.headers !== undefined) {
          return { status: error.status, headers: error.headers, body: new Uint8Array(), isTooLarge: true };
        }
      }
      throw error;
    }
  }

  /** Construct one secret-safe provider receipt. */
  receipt(call: Call, response: StripeResponse, objectId: string): Receipt {
    return {
      callId: call.id, provider: 'stripe', observedAt: goUTCDate(this.now()),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}),
      ...(stripeResponseHeader(response, 'request-id') ? { providerRequestId: stripeResponseHeader(response, 'request-id') } : {}),
    };
  }
}

/** Read one case-insensitive provider header without revealing request headers. */
export function stripeResponseHeader(response: StripeResponse, name: string): string {
  const value = response.headers[name.toLowerCase()];
  return Array.isArray(value) ? (value[0] ?? '') : (value ?? '');
}

/** Parse only integer Retry-After seconds, capped at one hour as in Go. */
export function stripeRetryAfter(response: StripeResponse): number {
  const raw = stripeResponseHeader(response, 'retry-after');
  if (!/^[0-9]+$/.test(raw) || raw === '0') return 0;
  return Math.min(Number(raw) * 1000, 3_600_000);
}

/** Retry keyed server failures only when Stripe has not cached or ruled out the key. */
export function shouldRetryKeyedServerFailure(response: StripeResponse): boolean {
  if (stripeResponseHeader(response, 'idempotent-replayed').toLowerCase() === 'true' || stripeResponseHeader(response, 'stripe-should-retry').toLowerCase() === 'false') return false;
  if (stripeResponseHeader(response, 'stripe-should-retry').toLowerCase() === 'true') return true;
  return [502, 503, 504].includes(response.status);
}

/** Retry 409 unless Stripe rules it out or reports parameters reused with one key. */
export function shouldRetryKeyedConflict(response: StripeResponse): boolean {
  return stripeResponseHeader(response, 'stripe-should-retry').toLowerCase() !== 'false' && parseStripeAPIError(response.body).type !== 'idempotency_error';
}

interface StripeAPIError { type: string; code: string; message: string }

/** Parse Stripe's public error code and message without retaining raw response bodies. */
function parseStripeAPIError(body: Uint8Array): StripeAPIError {
  try {
    const parsed = z.object({ error: z.object({ type: z.string().optional(), code: z.string().optional(), message: z.string().optional() }).optional() }).parse(JSON.parse(Buffer.from(body).toString('utf8')) as unknown);
    return { type: parsed.error?.type ?? '', code: parsed.error?.code ?? '', message: parsed.error?.message ?? '' };
  } catch { return { type: '', code: '', message: '' }; }
}

/** Include bounded Stripe error detail except for credential errors that can echo the API key. */
export function describeStripeRejection(summary: string, response: StripeResponse): string {
  if (response.status === 401 || response.status === 403) return summary;
  const error = parseStripeAPIError(response.body);
  return `${summary}${error.code ? `: ${error.code}` : ''}${error.message ? `: ${Array.from(error.message).slice(0, 300).join('')}` : ''}`;
}

/** Parse and validate a Stripe Checkout Session into the public safe shape. */
export function decodeCheckoutSession(body: Uint8Array): CheckoutSession {
  const parsed = parseProviderJSON(body, 'Checkout Session');
  const result = z.object({
    id: z.string(), object: z.string(), url: z.string().nullable().optional(), client_reference_id: z.string().nullable().optional(),
    payment_status: z.string().nullable().optional(), status: z.string().nullable().optional(),
    payment_intent: z.union([z.string(), z.object({ id: z.string().nullable().optional() }), z.null()]).optional(),
    currency: z.string().nullable().optional(), amount_total: z.number().int().nullable().optional(),
    expires_at: z.number().int().nullable().optional(), metadata: z.record(z.string(), z.string()).nullable().optional(),
  }).safeParse(parsed);
  if (!result.success || result.data.object !== 'checkout.session' || !result.data.id.startsWith('cs_')) throw new Error('Stripe response is invalid: Stripe returned an invalid Checkout Session');
  const session = result.data;
  const paymentIntentId = typeof session.payment_intent === 'string' ? session.payment_intent : session.payment_intent?.id;
  return {
    id: session.id,
    ...(session.url ? { url: session.url } : {}),
    ...(session.client_reference_id ? { clientReferenceId: session.client_reference_id } : {}),
    ...(session.payment_status ? { paymentStatus: session.payment_status } : {}),
    ...(session.status ? { status: session.status } : {}),
    ...(paymentIntentId ? { paymentIntentId } : {}),
    ...(session.currency ? { currency: session.currency } : {}),
    ...(session.amount_total ? { amountTotal: session.amount_total } : {}),
    ...(session.expires_at && session.expires_at > 0 ? { expiresAt: goUTCDate(new Date(session.expires_at * 1000)) } : {}),
    ...(session.metadata ? { metadata: session.metadata } : {}),
  };
}

/** Parse and validate the latest charge expanded PaymentIntent summary. */
export function decodePaymentIntent(body: Uint8Array): PaymentIntent {
  const parsed = parseProviderJSON(body, 'PaymentIntent');
  const result = z.object({
    id: z.string(), object: z.string(), amount: z.number().int().nullable().optional(), currency: z.string(), status: z.string(),
    receipt_email: z.string().nullable().optional(),
    latest_charge: z.union([z.string(), z.object({ id: z.string().nullable().optional(), amount_refunded: z.number().int().nullable().optional(), billing_details: z.object({ email: z.string().nullable().optional() }).optional() }), z.null()]).optional(),
  }).safeParse(parsed);
  if (!result.success || result.data.object !== 'payment_intent' || !result.data.id.startsWith('pi_') || !result.data.currency || !result.data.status) throw new Error('Stripe response is invalid: Stripe returned an invalid PaymentIntent');
  const intent = result.data;
  const charge = intent.latest_charge && typeof intent.latest_charge === 'object' ? intent.latest_charge : undefined;
  const chargeId = typeof intent.latest_charge === 'string' ? intent.latest_charge : charge?.id;
  const email = intent.receipt_email || charge?.billing_details?.email;
  return { id: intent.id, amount: intent.amount ?? 0, currency: intent.currency, status: intent.status,
    amountRefunded: charge?.amount_refunded ?? 0, ...(email ? { customerEmail: email } : {}), ...(chargeId ? { latestChargeId: chargeId } : {}) };
}

/** Parse and validate a Stripe refund into the public safe shape. */
export function decodeRefund(body: Uint8Array): Refund {
  const parsed = parseProviderJSON(body, 'refund');
  const result = z.object({ id: z.string(), object: z.string(), status: z.string(), amount: z.number().int().nullable().optional(), currency: z.string() }).safeParse(parsed);
  if (!result.success || result.data.object !== 'refund' || !result.data.id.startsWith('re_') || !result.data.currency ||
    !['succeeded', 'pending', 'failed', 'requires_action', 'canceled'].includes(result.data.status)) {
    throw new Error('Stripe response is invalid: Stripe returned an invalid refund');
  }
  return { refundId: result.data.id, status: result.data.status, amount: result.data.amount ?? 0, currency: result.data.currency };
}

function parseProviderJSON(body: Uint8Array, subject: string): unknown {
  if (body.length === 0) throw new Error(`Stripe response is invalid: Stripe returned an empty ${subject}`);
  try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; }
  catch { throw new Error(`Stripe response is invalid: Stripe returned malformed ${subject} JSON`); }
}

function sortedForm(values: URLSearchParams): string {
  return [...values.entries()]
    .sort(([left], [right]) => Buffer.compare(Buffer.from(left), Buffer.from(right)))
    .map(([key, value]) => `${goQueryEscape(key)}=${goQueryEscape(value)}`)
    .join('&');
}

function goUTCDate(value: Date): string { return value.toISOString().replace(/\.000Z$/, 'Z'); }

function goQueryEscape(value: string): string {
  let encoded = '';
  for (const byte of Buffer.from(value, 'utf8')) {
    if ((byte >= 65 && byte <= 90) || (byte >= 97 && byte <= 122) ||
      (byte >= 48 && byte <= 57) || byte === 45 || byte === 46 || byte === 95 || byte === 126) {
      encoded += String.fromCharCode(byte);
    } else if (byte === 32) {
      encoded += '+';
    } else {
      encoded += `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
    }
  }
  return encoded;
}
