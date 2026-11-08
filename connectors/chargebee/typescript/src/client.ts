// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetSubscriptionDefinition } from './generated/definitions.js';
import type { Config, Credentials, SubscriptionStatus, GetSubscriptionInput } from './generated/models.js';

const emptySubscription = (): SubscriptionStatus => ({ id: '', status: '', currencyCode: '' });

/** Injectable provider sender for tests and local applications. */
export interface ChargebeeClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Chargebee REST API client for subscription metadata. */
export class ChargebeeClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Chargebee. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ChargebeeClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Chargebee configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Chargebee request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getSubscription Query. */ getSubscription(): GetSubscriptionOperation { return new GetSubscriptionOperation(this); }

  /** Resolve the API key and send one bounded subscription lookup. */
  async get(call: Call, subscriptionID: string): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiKey || !isHeaderSafeCredential(apiKey) || apiKey.includes(':')) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/subscriptions/${encodeURIComponent(subscriptionID)}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${apiKey}:`).toString('base64')}` },
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

/** Read one subscription's ID, status, and currency code. */
export class GetSubscriptionOperation implements Query<GetSubscriptionInput, SubscriptionStatus> {
  /** Bind the provider client. */ constructor(private readonly client: ChargebeeClient) {}
  /** Return the generated Query subscription. */ definition(): QueryDefinition { return GetSubscriptionDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetSubscriptionInput): Promise<QueryAttempt<SubscriptionStatus>> {
    if (!isValidSubscriptionID(input?.subscriptionId))
      return terminal('defect', 'VALIDATION', 'subscription ID must be 1–50 URL-safe characters');
    const exchange = await this.client.get(call, input.subscriptionId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Chargebee credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Chargebee request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Chargebee rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Chargebee is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Chargebee did not expose the subscription');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Chargebee rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Chargebee response exceeded the configured limit');
    const subscriptionStatus = decodeSubscription(exchange.body, input.subscriptionId);
    if (!subscriptionStatus) return terminal('invalidResponse', 'PROTOCOL', 'Chargebee returned invalid subscription JSON');
    return newQueryBranch('found', subscriptionStatus);
  }
}

function isValidSubscriptionID(value: unknown): value is string {
  return typeof value === 'string' && /^[a-zA-Z0-9_-]{1,50}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'chargebee', operation: 'getSubscription', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<SubscriptionStatus> { return newQueryBranch(branch, emptySubscription(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<SubscriptionStatus> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeSubscription(body: Uint8Array, subscriptionID: string): SubscriptionStatus | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !isObject(raw.subscription)) return undefined;
  const data = raw.subscription;
  if (data.id !== subscriptionID || typeof data.status !== 'string' || !data.status.trim() ||
    typeof data.currency_code !== 'string' || data.currency_code.length !== 3) return undefined;
  return { id: subscriptionID, status: data.status, currencyCode: data.currency_code };
}
