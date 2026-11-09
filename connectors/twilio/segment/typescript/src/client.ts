// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetSourceDefinition } from './generated/definitions.js';
import type { Config, Credentials, SourceProfile, GetSourceInput } from './generated/models.js';

const defaultBaseURL = 'https://api.segmentapis.com';
const emptySource = (): SourceProfile => ({ id: '', slug: '', name: '' });

/** Injectable provider sender for tests and local applications. */
export interface SegmentClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface Exchange {
  status: number;
  body: Uint8Array;
  retryAfterMs: number;
  error?: 'credential' | 'request' | 'tooLarge';
}

/** Authenticated Segment REST API client for source metadata. */
export class SegmentClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate provider configuration without contacting Segment. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SegmentClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Segment configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 ||
      !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Segment request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the getSource Query. */ getSource(): GetSourceOperation { return new GetSourceOperation(this); }

  /** Resolve the API key and send one bounded source lookup. */
  async get(call: Call, sourceID: string): Promise<Exchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return emptyExchange('credential'); }
    if (!apiKey || !isHeaderSafeCredential(apiKey)) return emptyExchange('credential');
    const request: ProviderRequest = {
      url: `${this.baseURL}/sources/${encodeURIComponent(sourceID)}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` },
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

/** Read one Source's ID, slug, and display name. */
export class GetSourceOperation implements Query<GetSourceInput, SourceProfile> {
  /** Bind the provider client. */ constructor(private readonly client: SegmentClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetSourceDefinition; }
  /** Classify provider outcomes consistently with the Go implementation. */
  async invoke(call: Call, input: GetSourceInput): Promise<QueryAttempt<SourceProfile>> {
    if (!isValidSourceID(input?.sourceId))
      return terminal('defect', 'VALIDATION', 'sourceId must contain 1–255 URL-safe characters');
    const exchange = await this.client.get(call, input.sourceId);
    if (exchange.error === 'credential') return terminal('defect', 'AUTHENTICATION', 'Segment credentials are unavailable or invalid');
    if (exchange.error === 'request') return retry('TRANSPORT', 'Segment request failed');
    if (exchange.status === 429) return retry('RATE_LIMIT', 'Segment rate limited the request', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return retry('AVAILABILITY', 'Segment is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 404) return terminal('notFound', 'NOT_FOUND', 'Segment did not expose the source');
    if (exchange.status !== 200) return terminal('providerRejected', 'PROVIDER_REJECTION', `Segment rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Segment response exceeded the configured limit');
    const sourceProfile = decodeSource(exchange.body, input.sourceId);
    if (!sourceProfile) return terminal('invalidResponse', 'PROTOCOL', 'Segment returned invalid source JSON');
    return newQueryBranch('found', sourceProfile);
  }
}

function isValidSourceID(value: unknown): value is string {
  return typeof value === 'string' && value.length > 0 && value.length <= 255 && /^[A-Za-z0-9_-]+$/.test(value);
}
function emptyExchange(error: NonNullable<Exchange['error']>): Exchange { return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error }; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'segment', operation: 'getSource', kind, message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<SourceProfile> { return newQueryBranch(branch, emptySource(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<SourceProfile> { return newQueryRetry(failure(kind, message), delay); }
function header(headers: ProviderResponse['headers'], name: string): string {
  const entry = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return Array.isArray(entry) ? entry[0] ?? '' : entry ?? '';
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function decodeSource(body: Uint8Array, sourceID: string): SourceProfile | undefined {
  let raw: unknown;
  try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; }
  if (!isObject(raw) || !isObject(raw.data)) return undefined;
  const source = raw.data.source;
  if (!isObject(source) || source.id !== sourceID || typeof source.slug !== 'string' || !source.slug.trim() || typeof source.name !== 'string') return undefined;
  return { id: sourceID, slug: source.slug, name: source.name };
}
