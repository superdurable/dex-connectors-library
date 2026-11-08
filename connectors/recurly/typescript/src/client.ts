// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountDefinition } from './generated/definitions.js';
import type { Config, Credentials, AccountIdentity, GetAccountInput } from './generated/models.js';

const defaultBaseURL = 'https://v3.recurly.com';
const emptyAccount = (): AccountIdentity => ({ id: '', code: '' });

/** Injectable provider sender for tests and local applications. */
export interface RecurlyClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Recurly REST API client for account metadata. */
export class RecurlyClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Recurly. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: RecurlyClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Recurly configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Recurly request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getAccount Query. */ getAccount(): GetAccountOperation { return new GetAccountOperation(this); }

  /** Resolve the API key and send one bounded account lookup. */
  async get(call: Call, accountID: string): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiKey || !isHeaderSafeCredential(apiKey)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/accounts/${encodeURIComponent(accountID)}`, method: 'GET',
      headers: { Accept: 'application/vnd.recurly.v2021-02-25+json', Authorization: `Basic ${Buffer.from(`${apiKey}:`).toString('base64')}` },
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

/** Read one account's ID, name, and status. */
export class GetAccountOperation implements Query<GetAccountInput, AccountIdentity> {
  /** Bind the provider client. */ constructor(private readonly client: RecurlyClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetAccountDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetAccountInput): Promise<QueryAttempt<AccountIdentity>> {
    if (!isValidAccountID(input?.accountId))
      return terminal('defect', 'VALIDATION', 'account ID must be 1–55 URL-safe characters');
    const exchange = await this.client.get(call, input.accountId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Recurly credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Recurly request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Recurly rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Recurly is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Recurly did not expose the account');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Recurly rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Recurly response exceeded the configured limit');
    const accountIdentity = decodeAccount(exchange.body, input.accountId);
    if (!accountIdentity) return terminal('invalidResponse', 'PROTOCOL', 'Recurly returned invalid account JSON');
    return newQueryBranch('found', accountIdentity);
  }
}

function isValidAccountID(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.length <= 55 && value !== 'code-' && /^[A-Za-z0-9_-]+$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'recurly', operation: 'getAccount', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<AccountIdentity> { return newQueryBranch(branch, emptyAccount(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<AccountIdentity> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeAccount(body: Uint8Array, accountID: string): AccountIdentity | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !isValidAccountID(raw.id) || typeof raw.code !== 'string' || !raw.code ||
    (accountID.startsWith('code-') ? raw.code !== accountID.slice(5) : raw.id !== accountID)) return undefined;
  return { id: raw.id, code: raw.code };
}
