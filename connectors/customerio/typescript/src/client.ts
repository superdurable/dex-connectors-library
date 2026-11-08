// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCustomersByEmailDefinition } from './generated/definitions.js';
import type { Config, Credentials, CustomerLookupPage, GetCustomersByEmailInput } from './generated/models.js';

const emptyCustomers = (): CustomerLookupPage => ({ email: '', customerIDs: [] });

/** Injectable provider sender for tests and local applications. */
export interface CustomerioClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Customer.io App API client for customer lookups. */
export class CustomerioClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Customer.io. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: CustomerioClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Customer.io configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl ?? 'https://api.customer.io').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Customer.io request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getCustomersByEmail Query. */ getCustomersByEmail(): GetCustomersByEmailOperation { return new GetCustomersByEmailOperation(this); }

  /** Resolve credentials and send one bounded customer lookup. */
  async get(call: Call, email: string): Promise<Exchange> {
    let privateKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      privateKey = credentials.privateKey instanceof SecretString ? credentials.privateKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!isHeaderSafeCredential(privateKey)) return emptyExchange('credential');
    const url = new URL(`${this.baseURL}/v1/customers`);
    url.searchParams.set('email', email);
    const request: ProviderRequest = {
      url: url.toString(), method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${privateKey}` },
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

/** Look up Customer.io profile IDs by exact email address. */
export class GetCustomersByEmailOperation implements Query<GetCustomersByEmailInput, CustomerLookupPage> {
  /** Bind the provider client. */ constructor(private readonly client: CustomerioClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetCustomersByEmailDefinition; }
  /** Classify provider outcomes consistently with Go. */
  async invoke(call: Call, input: GetCustomersByEmailInput): Promise<QueryAttempt<CustomerLookupPage>> {
    if (!isValidEmail(input?.email))
      return terminal('defect', 'VALIDATION', 'email must be one valid address of at most 320 bytes');
    const exchange = await this.client.get(call, input.email);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Customer.io credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Customer.io request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Customer.io rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Customer.io is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Customer.io rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Customer.io response exceeded the configured limit');
    const customers = decodeCustomers(exchange.body, input.email);
    if (!customers) return terminal('invalidResponse', 'PROTOCOL', 'Customer.io returned invalid customer-lookup JSON');
    return newQueryBranch('found', customers);
  }
}

function isValidEmail(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.trim() === value &&
    new TextEncoder().encode(value).length <= 320 && /^[^\s@]+@[^\s@]+$/.test(value) &&
    !/(?:[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF])/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'customerio', operation: 'getCustomersByEmail', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<CustomerLookupPage> { return newQueryBranch(branch, emptyCustomers(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<CustomerLookupPage> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeCustomers(body: Uint8Array, email: string): CustomerLookupPage | undefined {
  let raw: unknown;
  try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !Array.isArray(raw.results)) return undefined;
  const customerIDs: string[] = [];
  for (const result of raw.results) {
    if (!isObject(result) || typeof result.cio_id !== 'string' || !result.cio_id) return undefined;
    customerIDs.push(result.cio_id);
  }
  return { email, customerIDs };
}
