// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetPaymentLinkDefinition } from './generated/definitions.js';
import type { Config, Credentials, PaymentLinkStatus, GetPaymentLinkInput } from './generated/models.js';

const defaultBaseURL = 'https://checkout-test.adyen.com/v72';
const emptyPaymentLink = (): PaymentLinkStatus => ({ id: '', status: '', currencyCode: '' });

/** Injectable provider sender for tests and local applications. */
export interface AdyenClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Adyen REST API client for payment link metadata. */
export class AdyenClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Adyen. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: AdyenClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Adyen configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Adyen request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getPaymentLink Query. */ getPaymentLink(): GetPaymentLinkOperation { return new GetPaymentLinkOperation(this); }

  /** Resolve the API key and send one bounded payment link lookup. */
  async get(call: Call, paymentLinkID: string): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiKey || !isHeaderSafeCredential(apiKey)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/paymentLinks/${encodeURIComponent(paymentLinkID)}`, method: 'GET',
      headers: { Accept: 'application/json', 'X-API-Key': apiKey },
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

/** Read one payment link's ID, name, and status. */
export class GetPaymentLinkOperation implements Query<GetPaymentLinkInput, PaymentLinkStatus> {
  /** Bind the provider client. */ constructor(private readonly client: AdyenClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetPaymentLinkDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetPaymentLinkInput): Promise<QueryAttempt<PaymentLinkStatus>> {
    if (!isValidPaymentLinkID(input?.paymentLinkId))
      return terminal('defect', 'VALIDATION', 'payment link ID must contain 1–128 URL-safe characters');
    const exchange = await this.client.get(call, input.paymentLinkId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Adyen credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Adyen request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Adyen rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Adyen is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Adyen did not expose the payment link');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Adyen rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Adyen response exceeded the configured limit');
    const paymentLinkStatus = decodePaymentLink(exchange.body, input.paymentLinkId);
    if (!paymentLinkStatus) return terminal('invalidResponse', 'PROTOCOL', 'Adyen returned invalid payment link JSON');
    return newQueryBranch('found', paymentLinkStatus);
  }
}

function isValidPaymentLinkID(value: unknown): value is string {
  return typeof value === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'adyen', operation: 'getPaymentLink', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<PaymentLinkStatus> { return newQueryBranch(branch, emptyPaymentLink(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<PaymentLinkStatus> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodePaymentLink(body: Uint8Array, paymentLinkID: string): PaymentLinkStatus | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !isObject(raw.amount)) return undefined;
  const amount = raw.amount;
  if (raw.id !== paymentLinkID || typeof raw.status !== 'string' || !raw.status.trim() ||
    typeof amount.currency !== 'string' || amount.currency.length !== 3) return undefined;
  return { id: paymentLinkID, status: raw.status, currencyCode: amount.currency };
}
