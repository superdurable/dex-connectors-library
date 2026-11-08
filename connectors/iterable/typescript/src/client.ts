// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetUserDefinition } from './generated/definitions.js';
import type { Config, Credentials, UserIdentity, GetUserInput } from './generated/models.js';

const defaultBaseURL = 'https://api.iterable.com/api';
const emptyUser = (): UserIdentity => ({ email: '', userId: '' });

/** Injectable provider sender for tests and local applications. */
export interface IterableClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Iterable REST API client for user metadata. */
export class IterableClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Iterable. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: IterableClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Iterable configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Iterable request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getUser Query. */ getUser(): GetUserOperation { return new GetUserOperation(this); }

  /** Resolve the API key and send one bounded user lookup. */
  async get(call: Call, email: string): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiKey || !isHeaderSafeCredential(apiKey)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/users/${encodeURIComponent(email)}`, method: 'GET',
      headers: { Accept: 'application/json', 'Api-Key': apiKey },
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

/** Read one user's ID, name, and status. */
export class GetUserOperation implements Query<GetUserInput, UserIdentity> {
  /** Bind the provider client. */ constructor(private readonly client: IterableClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetUserDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetUserInput): Promise<QueryAttempt<UserIdentity>> {
    if (!isValidEmail(input?.email))
      return terminal('defect', 'VALIDATION', 'email must be 1–256 printable characters with @');
    const exchange = await this.client.get(call, input.email);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Iterable credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Iterable request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Iterable rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Iterable is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Iterable did not expose the user');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Iterable rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Iterable response exceeded the configured limit');
    const userIdentity = decodeUser(exchange.body, input.email);
    if (!userIdentity) return terminal('invalidResponse', 'PROTOCOL', 'Iterable returned invalid user JSON');
    return newQueryBranch('found', userIdentity);
  }
}

function isValidEmail(value: unknown): value is string {
  if (typeof value !== 'string' || !value || !value.includes('@') || Buffer.byteLength(value, 'utf8') > 256) return false;
  for (const character of value) {
    const codePoint = character.codePointAt(0) ?? 0;
    if (codePoint < 32 || (codePoint >= 127 && codePoint <= 159) || /\s/u.test(character) || '/?#'.includes(character)) return false;
  }
  return true;
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'iterable', operation: 'getUser', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<UserIdentity> { return newQueryBranch(branch, emptyUser(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<UserIdentity> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeUser(body: Uint8Array, email: string): UserIdentity | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !isObject(raw.user)) return undefined;
  const user = raw.user;
  if (!isValidEmail(user.email) || user.email.toLowerCase() !== email.toLowerCase() ||
    (user.userId !== undefined && typeof user.userId !== 'string')) return undefined;
  return { email: user.email, userId: typeof user.userId === 'string' ? user.userId : '' };
}
