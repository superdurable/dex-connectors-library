// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListApplicationTagsDefinition } from './generated/definitions.js';
import type { ApplicationTags, Config, Credentials, ListApplicationTagsInput } from './generated/models.js';

/** Optional transport for local provider tests. */
export interface JiguangClientOptions {
  /** Request sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Resolve JPush credentials for each tag query. */
export class JiguangClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting JPush. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: JiguangClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('JPush configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://device.jpush.cn');
    if (new URL(this.baseURL).pathname !== '/') throw new TypeError('JPush base URL must be a host root');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('JPush request limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the application tag Query. */ listApplicationTags(): ListApplicationTagsOperation { return new ListApplicationTagsOperation(this); }

  /** Send a bounded JPush Device API request. */
  async getTags(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let appKey = '';
    let masterSecret = '';
    try {
      const credential = await this.credentials.resolve(call, call.context.cancellationSignal);
      appKey = credential.appKey;
      masterSecret = credential.masterSecret instanceof SecretString ? credential.masterSecret.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!appKey || appKey.length > 128 || /[^\x21-\x7e]|:/.test(appKey) || !masterSecret) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${this.baseURL}/v3/tags/`, method: 'GET', headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${appKey}:${masterSecret}`).toString('base64')}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after')) || (response.status === 429 ? rateReset(header(response.headers, 'x-rate-limit-reset')) : 0);
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs };
  }
}

/** Read application tag names without device identifiers. */
export class ListApplicationTagsOperation implements Query<ListApplicationTagsInput, ApplicationTags> {
  /** Bind the provider client. */ constructor(private readonly client: JiguangClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListApplicationTagsDefinition; }
  /** Classify one JPush response using the Go connector contract. */
  async invoke(call: Call, input: ListApplicationTagsInput): Promise<QueryAttempt<ApplicationTags>> {
    if (!input || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getTags(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('TRANSPORT');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    const text = new TextDecoder('utf-8', { fatal: true });
    let wire: unknown;
    try { wire = JSON.parse(text.decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire) || !('tags' in wire)) return branch('invalidResponse', 'PROTOCOL');
    const tags = (wire as { tags: unknown }).tags;
    if (tags !== null && (!Array.isArray(tags) || tags.length > 1000 || tags.some(tag => typeof tag !== 'string' || !tag || Buffer.byteLength(tag) > 40))) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('listed', { tags: tags ?? [] });
  }
}

function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function rateReset(value: string): number { const seconds = Number(value); return Number.isInteger(seconds) && seconds > 0 && seconds <= 3600 ? seconds * 1000 : 0; }
function failure(kind: Failure['kind']): Failure { return { provider: 'jiguang', operation: 'listApplicationTags', kind, message: 'JPush tag query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ApplicationTags> { return newQueryBranch(name, { tags: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ApplicationTags> { return newQueryRetry(failure(kind), delay); }
