// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetPaymentDefinition } from './generated/definitions.js';
import type { Config, Credentials, PaymentStatus, GetPaymentInput } from './generated/models.js';

const defaultBaseURL = 'https://api.razorpay.com/v1';
const emptyPayment = (): PaymentStatus => ({ id: '', status: '', amount: 0, currencyCode: '' });

/** Injectable provider sender for tests and local applications. */
export interface RazorpayClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Razorpay REST API client for payment metadata. */
export class RazorpayClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Razorpay. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: RazorpayClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Razorpay configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Razorpay request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getPayment Query. */ getPayment(): GetPaymentOperation { return new GetPaymentOperation(this); }

  /** Resolve the API key and send one bounded payment lookup. */
  async get(call: Call, paymentID: string): Promise<Exchange> {
    let keyID: string;
    let keySecret: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      keyID = credentials.keyId instanceof SecretString ? credentials.keyId.reveal() : '';
      keySecret = credentials.keySecret instanceof SecretString ? credentials.keySecret.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!keyID || !keySecret || !isHeaderSafeCredential(keyID) || keyID.includes(':') || !isHeaderSafeCredential(keySecret)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/payments/${encodeURIComponent(paymentID)}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(keyID + ':' + keySecret).toString('base64')}` },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    let isTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        isTooLarge = true;
      } else return emptyExchange('request');
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (isTooLarge || response.body.length > this.maxResponseBytes)
      return { status: response.status, body: new Uint8Array(), retryAfterMs, error: 'tooLarge' };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read one payment's ID, status, amount, and currency. */
export class GetPaymentOperation implements Query<GetPaymentInput, PaymentStatus> {
  /** Bind the provider client. */ constructor(private readonly client: RazorpayClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetPaymentDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetPaymentInput): Promise<QueryAttempt<PaymentStatus>> {
    if (!isValidPaymentID(input?.paymentId))
      return terminal('defect', 'VALIDATION', 'payment ID must start with pay_ and contain 6–64 letters or digits');
    const exchange = await this.client.get(call, input.paymentId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Razorpay credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Razorpay request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Razorpay rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Razorpay is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404 || (exchange.status === 400 && !exchange.error && isPaymentMissing(exchange.body))) return terminal('notFound', 'NOT_FOUND', 'Razorpay did not expose the payment');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Razorpay rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Razorpay response exceeded the configured limit');
    const paymentStatus = decodePayment(exchange.body, input.paymentId);
    if (!paymentStatus) return terminal('invalidResponse', 'PROTOCOL', 'Razorpay returned invalid payment JSON');
    return newQueryBranch('found', paymentStatus);
  }
}

function isValidPaymentID(value: unknown): value is string {
  return typeof value === 'string' && /^pay_[A-Za-z0-9]{6,64}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'razorpay', operation: 'getPayment', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<PaymentStatus> { return newQueryBranch(branch, emptyPayment(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<PaymentStatus> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
/** Report Razorpay's documented HTTP 400 error for an unknown payment ID. */
function isPaymentMissing(body: Uint8Array): boolean {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return false; }
  return isObject(raw) && isObject(raw.error) && raw.error.code === 'BAD_REQUEST_ERROR' &&
    raw.error.description === 'The id provided does not exist';
}
function decodePayment(body: Uint8Array, paymentID: string): PaymentStatus | undefined {
  let data: unknown;
  try { data = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(data) || data.id !== paymentID || data.entity !== 'payment' ||
    typeof data.status !== 'string' || !data.status.trim() ||
    typeof data.amount !== 'number' || !Number.isSafeInteger(data.amount) || data.amount <= 0 ||
    typeof data.currency !== 'string' || data.currency.length !== 3) return undefined;
  return { id: paymentID, status: data.status, amount: data.amount, currencyCode: data.currency };
}
