// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetKeyDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetKeyInput, KeyLookup } from './generated/models.js';

const emptyLookup = (): KeyLookup => ({ key: '', value: '', exists: false });
/** Injectable provider sender for tests and local applications. */
export interface UpstashRedisClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}
interface Exchange { status: number; body: Uint8Array; retryAfterMs: number; error?: 'credential' | 'request' | 'tooLarge' }

/** Authenticated Upstash Redis REST client for string GET commands. */
export class UpstashRedisClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate the database REST URL and limits without contacting Upstash. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: UpstashRedisClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Upstash configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl).replace(/\/+$/, '');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Upstash request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getKey Query. */ getKey(): GetKeyOperation { return new GetKeyOperation(this); }

  /** Send one bounded Redis GET command with a resolved token. */
  async readKey(call: Call, key: string): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.restToken instanceof SecretString ? credentials.restToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!token || !isHeaderSafeCredential(token)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/`, method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      body: Buffer.from(JSON.stringify(['GET', key])),
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
    if (Buffer.from(response.body).includes(token)) return { ...emptyExchange('credential'), status: response.status, retryAfterMs };
    return { status: response.status, body: response.body, retryAfterMs };
  }
}

/** Read one Redis string value, distinguishing an absent key. */
export class GetKeyOperation implements Query<GetKeyInput, KeyLookup> {
  /** Bind the provider client. */ constructor(private readonly client: UpstashRedisClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetKeyDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetKeyInput): Promise<QueryAttempt<KeyLookup>> {
    if (!input || typeof input.key !== 'string' || !input.key || Buffer.byteLength(input.key) > 512 || input.key.includes('\0'))
      return terminal('defect', 'VALIDATION', 'Redis key is invalid');
    const exchange = await this.client.readKey(call, input.key);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Upstash token is unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Upstash request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Upstash rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Upstash is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Upstash rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Upstash response exceeded the configured limit');
    let decoded: unknown;
    try { decoded = JSON.parse(Buffer.from(exchange.body).toString('utf8')) as unknown; }
    catch { return terminal('invalidResponse', 'PROTOCOL', 'Upstash returned invalid JSON'); }
    if (!isObject(decoded) || Object.keys(decoded).length !== 1) return terminal('invalidResponse', 'PROTOCOL', 'Upstash returned invalid JSON');
    if (typeof decoded.error === 'string') return terminal('providerRejected', 'PROVIDER_REJECTION', 'Upstash rejected the GET command');
    if (!Object.hasOwn(decoded, 'result') || (decoded.result !== null && typeof decoded.result !== 'string'))
      return terminal('invalidResponse', 'PROTOCOL', 'Upstash returned invalid JSON');
    if (decoded.result === null) return newQueryBranch('missing', { key: input.key, value: '', exists: false });
    return newQueryBranch('found', { key: input.key, value: decoded.result as string, exists: true });
  }
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'upstash-redis', operation: 'getKey', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<KeyLookup> { return newQueryBranch(branch, emptyLookup(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<KeyLookup> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
