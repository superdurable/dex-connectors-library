// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SearchUsersDefinition } from './generated/definitions.js';
import type { Config, Credentials, UserSearchPage, SearchUsersInput } from './generated/models.js';

const emptyUsers = (): UserSearchPage => ({ user: '', userIDs: [], matchType: '' });

/** Injectable provider sender for tests and local applications. */
export interface AmplitudeClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Amplitude Dashboard REST API client for user searches. */
export class AmplitudeClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Amplitude. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: AmplitudeClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Amplitude configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl ?? 'https://amplitude.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Amplitude request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the searchUsers Query. */ searchUsers(): SearchUsersOperation { return new SearchUsersOperation(this); }

  /** Resolve credentials and send one bounded user search. */
  async get(call: Call, user: string): Promise<Exchange> {
    let apiKey: string;
    let secretKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
      secretKey = credentials.secretKey instanceof SecretString ? credentials.secretKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!isHeaderSafeCredential(apiKey) || !isHeaderSafeCredential(secretKey) || apiKey.includes(':')) return emptyExchange('credential');
    const url = new URL(`${this.baseURL}/api/2/usersearch`);
    url.searchParams.set('user', user);
    const request: ProviderRequest = {
      url: url.toString(), method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${apiKey}:${secretKey}`).toString('base64')}` },
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

/** Search project users by Amplitude or external identity. */
export class SearchUsersOperation implements Query<SearchUsersInput, UserSearchPage> {
  /** Bind the provider client. */ constructor(private readonly client: AmplitudeClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return SearchUsersDefinition; }
  /** Classify provider outcomes consistently with Go. */
  async invoke(call: Call, input: SearchUsersInput): Promise<QueryAttempt<UserSearchPage>> {
    if (!isValidUserSearch(input?.user))
      return terminal('defect', 'VALIDATION', 'user must contain 1–256 printable characters');
    const exchange = await this.client.get(call, input.user);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Amplitude credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Amplitude request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Amplitude rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Amplitude is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Amplitude rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Amplitude response exceeded the configured limit');
    const users = decodeUsers(exchange.body, input.user);
    if (!users) return terminal('invalidResponse', 'PROTOCOL', 'Amplitude returned invalid user-search JSON');
    return newQueryBranch('searched', users);
  }
}

function isValidUserSearch(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.trim() === value &&
    new TextEncoder().encode(value).length <= 256 &&
    [...value].every(character => character.codePointAt(0)! >= 0x20 && character.codePointAt(0)! !== 0x7f) &&
    !/(?:[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF])/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'amplitude', operation: 'searchUsers', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<UserSearchPage> { return newQueryBranch(branch, emptyUsers(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<UserSearchPage> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeUsers(body: Uint8Array, user: string): UserSearchPage | undefined {
  let raw: unknown;
  try { raw = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !Array.isArray(raw.matches) || typeof raw.type !== 'string' || !raw.type) return undefined;
  const userIDs: string[] = [];
  for (const match of raw.matches) {
    if (!isObject(match) || typeof match.user_id !== 'string' || !match.user_id) return undefined;
    userIDs.push(match.user_id);
  }
  return { user, userIDs, matchType: raw.type };
}
