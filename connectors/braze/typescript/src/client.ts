// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetUserProfileDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetUserProfileInput, UserProfile } from './generated/models.js';

const fieldsToExport = ['external_id', 'braze_id', 'first_name', 'last_name', 'email'];
const emptyProfile = (): UserProfile => ({ externalId: '' });

/** Injectable provider sender for tests and local applications. */
export interface BrazeClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Braze REST client for selected user profile exports. */
export class BrazeClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate account endpoint and request limits without contacting Braze. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: BrazeClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Braze configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Braze request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getUserProfile Query. */ getUserProfile(): GetUserProfileOperation { return new GetUserProfileOperation(this); }

  /** Resolve the key and send one bounded profile export request. */
  async post(call: Call, externalID: string): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.restApiKey instanceof SecretString ? credentials.restApiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!token || !isHeaderSafeCredential(token)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/users/export/ids`, method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      body: new TextEncoder().encode(JSON.stringify({ external_ids: [externalID], fields_to_export: fieldsToExport })),
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

/** Read one Braze user's selected identity and contact fields. */
export class GetUserProfileOperation implements Query<GetUserProfileInput, UserProfile> {
  /** Bind the provider client. */ constructor(private readonly client: BrazeClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetUserProfileDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetUserProfileInput): Promise<QueryAttempt<UserProfile>> {
    if (!input || typeof input.externalId !== 'string' || !input.externalId.trim() || /[\uD800-\uDFFF]/u.test(input.externalId) || Buffer.byteLength(input.externalId, 'utf8') > 987)
      return terminal('defect', 'VALIDATION', 'external ID must be a nonempty UTF-8 value of at most 987 bytes');
    const exchange = await this.client.post(call, input.externalId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Braze credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Braze profile request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Braze rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Braze is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200 && exchange.status !== 201) return terminal('providerRejected', 'PROVIDER_REJECTION', `Braze rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Braze response exceeded the configured limit');
    const profile = decodeProfile(exchange.body, input.externalId);
    if (profile === undefined) return terminal('invalidResponse', 'PROTOCOL', 'Braze returned an invalid profile response');
    if (profile === null) return terminal('notFound', 'NOT_FOUND', 'Braze did not find the user');
    return newQueryBranch('found', profile);
  }
}

function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'braze', operation: 'getUserProfile', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<UserProfile> { return newQueryBranch(branch, emptyProfile(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<UserProfile> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeProfile(body: Uint8Array, externalID: string): UserProfile | null | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || raw.message !== 'success' || !Array.isArray(raw.users) || raw.users.length > 1) return undefined;
  if (raw.users.length === 0) return null;
  const user: unknown = raw.users[0];
  if (!isObject(user) || user.external_id !== externalID) return undefined;
  for (const field of ['braze_id', 'first_name', 'last_name', 'email'])
    if (user[field] !== undefined && user[field] !== null && typeof user[field] !== 'string') return undefined;
  const result: UserProfile = { externalId: externalID };
  if (user.braze_id) result.brazeId = user.braze_id as string;
  if (user.first_name) result.firstName = user.first_name as string;
  if (user.last_name) result.lastName = user.last_name as string;
  if (user.email) result.email = user.email as string;
  return result;
}
