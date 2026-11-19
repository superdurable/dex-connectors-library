// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SegmentTextDefinition } from './generated/definitions.js';
import type { Config, Credentials, SegmentTextInput, SegmentTextOutput } from './generated/models.js';

/** Optional HTTP transport for local provider tests. */
export interface GuGuDataClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
const emptySegments: SegmentTextOutput = { segmentedText: '' };

/** Resolve GuGuData credentials for each text segmentation query. */
export class GuGuDataClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting GuGuData. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: GuGuDataClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('GuGuData configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('GuGuData request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only text segmentation Query. */ segmentText(): SegmentTextOperation { return new SegmentTextOperation(this); }
  /** Send one bounded text segmentation request. */
  async getSegments(call: Call, text: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let appKey = '';
    try { const credential = await this.credentials.resolve(call, call.context.cancellationSignal); appKey = credential.appKey instanceof SecretString ? credential.appKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!appKey || appKey.length > 1024 || /[^\x21-\x7e]/.test(appKey)) return { error: 'credential', retryAfterMs: 0 };
    const url = new URL('https://api.gugudata.com/text/segment');
    url.searchParams.set('text', text);
    const request: ProviderRequest = { url: url.toString(), method: 'GET', headers: { Accept: 'application/json', 'X-GUGUDATA-APPKEY': appKey }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Segment short Chinese text without modifying provider state. */
export class SegmentTextOperation implements Query<SegmentTextInput, SegmentTextOutput> {
  /** Bind the provider client. */ constructor(private readonly client: GuGuDataClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return SegmentTextDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: SegmentTextInput): Promise<QueryAttempt<SegmentTextOutput>> {
    const text = input?.text;
    if (typeof text !== 'string' || !text.trim() || text.length > 2000 || /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/.test(text)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getSegments(call, text);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const result = wire as { DataStatus?: unknown; Data?: unknown };
    const statusObject = result.DataStatus && typeof result.DataStatus === 'object' && !Array.isArray(result.DataStatus) ? result.DataStatus as Record<string, unknown> : null;
    if (!statusObject || !Number.isInteger(statusObject.StatusCode)) return branch('invalidResponse', 'PROTOCOL');
    if (statusObject.StatusCode !== 100) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!result.Data || typeof result.Data !== 'object' || Array.isArray(result.Data)) return branch('invalidResponse', 'PROTOCOL');
    const data = result.Data as Record<string, unknown>;
    return newQueryBranch('segmented', { segmentedText: typeof data.SegmentaryText === 'string' ? data.SegmentaryText : '' });
  }
}

function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'gugudata', operation: 'segmentText', kind, message: 'GuGuData text segmentation failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<SegmentTextOutput> { return newQueryBranch(name, emptySegments, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<SegmentTextOutput> { return newQueryRetry(failure(kind), delay); }
