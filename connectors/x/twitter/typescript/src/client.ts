// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetPostDefinition } from './generated/definitions.js';
import type { Config, Credentials, PostMetadata, GetPostInput } from './generated/models.js';

const defaultBaseURL = 'https://api.x.com/2';
const emptyPost = (): PostMetadata => ({ id: '', text: '' });

/** Injectable provider sender for tests and local applications. */
export interface XClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated X REST API v2 client for post metadata. */
export class XClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting X. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: XClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('X configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('X request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getPost Query. */ getPost(): GetPostOperation { return new GetPostOperation(this); }

  /** Resolve the token and send one bounded post lookup. */
  async get(call: Call, postID: string): Promise<Exchange> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!token || !isHeaderSafeCredential(token)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/tweets/${encodeURIComponent(postID)}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${token}` },
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

/** Read one Post's ID and text. */
export class GetPostOperation implements Query<GetPostInput, PostMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: XClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetPostDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetPostInput): Promise<QueryAttempt<PostMetadata>> {
    if (!isValidPostID(input?.postId))
      return terminal('defect', 'VALIDATION', 'Post ID must be 1–20 decimal digits');
    const exchange = await this.client.get(call, input.postId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'X credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'X request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'X rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'X is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'X did not expose the Post');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `X rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'X response exceeded the configured limit');
    const post = decodePost(exchange.body, input.postId);
    if (!post) return terminal('invalidResponse', 'PROTOCOL', 'X returned invalid Post JSON');
    return newQueryBranch('found', post);
  }
}

function isValidPostID(value: unknown): value is string {
  return typeof value === 'string' && /^[1-9][0-9]{0,19}$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'x-twitter', operation: 'getPost', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<PostMetadata> { return newQueryBranch(branch, emptyPost(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<PostMetadata> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodePost(body: Uint8Array, postID: string): PostMetadata | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !isObject(raw.data) || raw.data.id !== postID ||
    typeof raw.data.text !== 'string' || !raw.data.text.trim()) return undefined;
  return { id: postID, text: raw.data.text };
}
