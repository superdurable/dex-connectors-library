// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetPostDefinition, ListPostsDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetPostInput, ListPostsInput, PagePostMetadata, PagePostsPage } from './generated/models.js';

const emptyPage = (): PagePostsPage => ({ posts: [], nextCursor: '' });
const emptyPost = (): PagePostMetadata => ({ id: '', message: '', createdTime: '', permalinkUrl: '' });
const numericID = /^[0-9]{1,40}$/;
const postID = /^[0-9]{1,40}(?:_[0-9]{1,40})?$/;

/** Optional provider transport for local tests and examples. */
export interface FacebookPagesClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Meta client with call-time Page token resolution. */
export class FacebookPagesClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate configuration without contacting Meta. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: FacebookPagesClientOptions = {}) {
    if (!credentials) throw new TypeError('Facebook Pages credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://graph.facebook.com/v25.0');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Meta timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Meta response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the Page feed Query. */ listPosts(): ListPostsOperation { return new ListPostsOperation(this); }
  /** Bind the post identity Query. */ getPost(): GetPostOperation { return new GetPostOperation(this); }

  /** Fetch one provider response and classify transport and HTTP failures. */
  async exchange(call: Call, operation: string, path: string): Promise<ProviderOutcome> {
    let accessToken: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessToken = resolved.pageAccessToken instanceof SecretString ? resolved.pageAccessToken.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!accessToken || !isHeaderSafeCredential(accessToken)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Bearer ${accessToken}`, 'User-Agent': 'Dex Connector (https://superdurable.io)' },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Meta response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Meta request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (Buffer.from(response.body).includes(accessToken)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Meta rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Meta is temporarily unavailable') };
    if (status === 404) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Meta did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Meta rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Meta response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** Read one Page feed page without comments or reactions. */
export class ListPostsOperation implements Query<ListPostsInput, PagePostsPage> {
  /** Bind the provider client. */ constructor(private readonly client: FacebookPagesClient) {}
  /** Return the manifest-generated contract. */ definition(): QueryDefinition { return ListPostsDefinition; }
  /** Read one feed page or select a Go-compatible optional branch. */
  async invoke(call: Call, input: ListPostsInput): Promise<QueryAttempt<PagePostsPage>> {
    if (!input || typeof input.pageId !== 'string' || !numericID.test(input.pageId) || (input.after !== undefined && (typeof input.after !== 'string' || input.after.length > 4096 || /[\r\n\0]/.test(input.after)))) {
      return terminal('listPosts', 'defect', 'VALIDATION', 'Page ID or cursor is invalid', emptyPage());
    }
    const query = new URLSearchParams({ fields: 'id,message,created_time,permalink_url', limit: '100' });
    if (input.after) query.set('after', input.after);
    const result = await this.client.exchange(call, 'listPosts', `/${input.pageId}/feed?${query}`);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyPage(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !Array.isArray(document.data) || document.data.length > 100) return invalid('listPosts', emptyPage());
    const paging = document.paging ?? {};
    if (!isObject(paging)) return invalid('listPosts', emptyPage());
    const cursors = paging.cursors ?? {};
    if (!isObject(cursors)) return invalid('listPosts', emptyPage());
    const after = cursors.after ?? '';
    if (typeof after !== 'string' || after.length > 4096) return invalid('listPosts', emptyPage());
    const posts: PagePostMetadata[] = [];
    for (const item of document.data) {
      const post = projectPost(item);
      if (!post) return invalid('listPosts', emptyPage());
      posts.push(post);
    }
    return newQueryBranch('listed', { posts, nextCursor: after });
  }
}

/** Read one Page post's public metadata. */
export class GetPostOperation implements Query<GetPostInput, PagePostMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: FacebookPagesClient) {}
  /** Return the manifest-generated contract. */ definition(): QueryDefinition { return GetPostDefinition; }
  /** Read post metadata or select a Go-compatible optional branch. */
  async invoke(call: Call, input: GetPostInput): Promise<QueryAttempt<PagePostMetadata>> {
    if (!input || typeof input.postId !== 'string' || !postID.test(input.postId)) return terminal('getPost', 'defect', 'VALIDATION', 'post ID is invalid', emptyPost());
    const query = new URLSearchParams({ fields: 'id,message,created_time,permalink_url' });
    const result = await this.client.exchange(call, 'getPost', `/${input.postId}?${query}`);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyPost(), {}, result.failure);
    const post = projectPost(parseJSON(result.body!));
    if (!post) return invalid('getPost', emptyPost());
    return newQueryBranch('found', post);
  }
}

function projectPost(value: unknown): PagePostMetadata | undefined {
  if (!isObject(value) || typeof value.id !== 'string' || !postID.test(value.id)) return undefined;
  const message = value.message ?? '';
  const createdTime = value.created_time;
  const permalinkUrl = value.permalink_url ?? '';
  if (typeof message !== 'string' || typeof createdTime !== 'string' || !createdTime || typeof permalinkUrl !== 'string' || (permalinkUrl && !permalinkUrl.startsWith('https://'))) return undefined;
  return { id: value.id, message, createdTime, permalinkUrl };
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'facebook-pages', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'Meta returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
