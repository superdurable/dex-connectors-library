// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListPostsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListPostsInput, ListPostsOutput } from './generated/models.js';

const postsPath = '/apis/content.halo.run/v1alpha1/posts';

/** Optional provider sender for local tests. */
export interface Fit2cloudClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** List article metadata authorized by a Halo personal access token. */
export class Fit2cloudClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting Halo. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: Fit2cloudClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Halo configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Halo request limits are invalid');
    let parsedUrl: URL;
    try { parsedUrl = new URL(config.baseUrl); } catch { throw new TypeError('Halo baseUrl must be an HTTPS origin'); }
    if (parsedUrl.protocol !== 'https:' || !parsedUrl.hostname || parsedUrl.username || parsedUrl.password || parsedUrl.search || parsedUrl.hash || (parsedUrl.pathname !== '/' && parsedUrl.pathname !== '') || parsedUrl.hostname === 'localhost') throw new TypeError('Halo baseUrl must be an HTTPS origin');
    this.baseUrl = parsedUrl.origin;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the article page Query. */ listPosts(): ListPostsOperation { return new ListPostsOperation(this); }

  /** Send one bounded article page request. */
  async list(call: Call, page: number, pageSize: number): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey.startsWith('pat_') || apiKey.length > 512 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const query = new URLSearchParams({ page: String(page), size: String(pageSize) });
    const request: ProviderRequest = { url: this.baseUrl + postsPath + '?' + query.toString(), method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read one Halo article page. */
export class ListPostsOperation implements Query<ListPostsInput, ListPostsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: Fit2cloudClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListPostsDefinition; }
  /** Classify one page using the Go connector contract. */
  async invoke(call: Call, input: ListPostsInput): Promise<QueryAttempt<ListPostsOutput>> {
    const selectedPage = input?.page ?? 0;
    const selectedPageSize = input?.pageSize ?? 0;
    const page = selectedPage || 1;
    const pageSize = selectedPageSize || 20;
    if (!Number.isSafeInteger(page) || !Number.isSafeInteger(pageSize) || page < 1 || page > 50_000 || pageSize < 1 || pageSize > 100) return branch('defect', 'VALIDATION');
    const exchange = await this.client.list(call, page, pageSize);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { wire = undefined; }
    const total = isObject(wire) ? wire.total ?? 0 : undefined;
    if (!isObject(wire) || !Array.isArray(wire.items) || typeof wire.hasNext !== 'boolean' || wire.page !== page || wire.size !== pageSize || typeof total !== 'number' || !Number.isSafeInteger(total) || total < 0 || wire.items.length > pageSize) return branch('invalidResponse', 'PROTOCOL');
    const posts: Array<{ name: string; title: string; slug: string; published: boolean; visibility: string }> = [];
    for (const item of wire.items) {
      if (!isObject(item) || !isObject(item.metadata) || !isObject(item.spec)) return branch('invalidResponse', 'PROTOCOL');
      const { name } = item.metadata;
      const { title, slug, publish, visible } = item.spec;
      if (typeof name !== 'string' || !name || name.length > 512 || typeof title !== 'string' || !title || title.length > 1024 || typeof slug !== 'string' || !slug || slug.length > 1024 || slug.trim() !== slug || (publish !== undefined && typeof publish !== 'boolean') || !['PUBLIC', 'INTERNAL', 'PRIVATE'].includes(String(visible))) return branch('invalidResponse', 'PROTOCOL');
      posts.push({ name, title, slug, published: publish === true, visibility: visible as string });
    }
    if (wire.hasNext && page === 50_000) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('listed', { posts, nextPage: wire.hasNext ? page + 1 : 0 });
  }
}

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'fit2cloud', operation: 'listPosts', kind, message: 'Halo article query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListPostsOutput> { return newQueryBranch(name, { posts: [], nextPage: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListPostsOutput> { return newQueryRetry(failure(kind), delay); }
