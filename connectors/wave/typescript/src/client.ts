// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCustomerDefinition } from './generated/definitions.js';
import type { Config, Credentials, CustomerProfile, GetCustomerInput } from './generated/models.js';

const emptyCustomer = (): CustomerProfile => ({ businessId: '', id: '', name: '' });
const getCustomerGraphQL = 'query GetCustomer($businessId: ID!, $customerId: ID!) { business(id: $businessId) { customer(id: $customerId) { id name } } }';

/** Injectable provider sender for tests and local applications. */
export interface WaveClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Wave App API client for customer lookups. */
export class WaveClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Wave. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: WaveClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Wave configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl ?? 'https://gql.waveapps.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Wave request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getCustomer Query. */ getCustomer(): GetCustomerOperation { return new GetCustomerOperation(this); }

  /** Resolve credentials and send one bounded customer lookup. */
  async get(call: Call, input: GetCustomerInput): Promise<Exchange> {
    let accessToken: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessToken = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!isHeaderSafeCredential(accessToken)) return emptyExchange('credential');
    const url = new URL(`${this.baseURL}/graphql/public`);
    const request: ProviderRequest = {
      url: url.toString(), method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: `Bearer ${accessToken}` },
      body: new TextEncoder().encode(JSON.stringify({ query: getCustomerGraphQL, variables: { businessId: input.businessId, customerId: input.customerId } })),
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

/** Read one customer by GraphQL ID from one Wave business. */
export class GetCustomerOperation implements Query<GetCustomerInput, CustomerProfile> {
  /** Bind the provider client. */ constructor(private readonly client: WaveClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetCustomerDefinition; }
  /** Classify provider outcomes consistently with Go. */
  async invoke(call: Call, input: GetCustomerInput): Promise<QueryAttempt<CustomerProfile>> {
    if (!isValidGraphQLID(input?.businessId) || !isValidGraphQLID(input?.customerId))
      return terminal('defect', 'VALIDATION', 'businessId and customerId must each contain 1–128 printable characters');
    const exchange = await this.client.get(call, input);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Wave credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Wave request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Wave rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Wave is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Wave rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Wave response exceeded the configured limit');
    const customer = decodeCustomer(exchange.body, input);
    if (customer === undefined) return terminal('invalidResponse', 'PROTOCOL', 'Wave returned invalid customer JSON');
    if (customer === null) return terminal('notFound', 'NOT_FOUND', 'Wave did not expose the customer');
    return newQueryBranch('found', customer);
  }
}

function isValidGraphQLID(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.trim() === value &&
    new TextEncoder().encode(value).length <= 128 && ![...value].some(character => character.codePointAt(0)! < 0x20 || character.codePointAt(0)! === 0x7f) &&
    !/(?:[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF])/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'wave', operation: 'getCustomer', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<CustomerProfile> { return newQueryBranch(branch, emptyCustomer(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<CustomerProfile> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeCustomer(body: Uint8Array, input: GetCustomerInput): CustomerProfile | null | undefined {
  let raw: unknown;
  try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; }
  if (!isObject(raw) || (Array.isArray(raw.errors) && raw.errors.length > 0) || !isObject(raw.data)) return undefined;
  const business = raw.data.business;
  if (business === null) return null;
  if (!isObject(business)) return undefined;
  const customer = business.customer;
  if (customer === null) return null;
  if (!isObject(customer) || customer.id !== input.customerId || typeof customer.name !== 'string' || !customer.name) return undefined;
  return { businessId: input.businessId, id: customer.id, name: customer.name };
}
