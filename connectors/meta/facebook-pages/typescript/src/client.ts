// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type CallID, type CredentialProvider, type Failure, type Mutation, type MutationAttempt, type MutationDefinition, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, ProviderTransportError, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { CheckpointError, InsufficientTimeError, newFence } from '@superdurable/connectors-sdk/singledispatch';
import { GetPostDefinition, ListPostsDefinition, PublishTextPostDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetPostInput, ListPostsInput, PagePostMetadata, PagePostsPage, PublishedPost, PublishTextPostInput } from './generated/models.js';

const emptyPage = (): PagePostsPage => ({ posts: [], nextCursor: '' });
const emptyPost = (): PagePostMetadata => ({ id: '', message: '', createdTime: '', permalinkUrl: '' });
const emptyPublishedPost = (): PublishedPost => ({ postId: '' });
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
  /** Bind the Page publishing Mutation. */ publishTextPost(): PublishTextPostOperation {
    return new PublishTextPostOperation((call, input) => this.publishTextPostAttempt(call, input));
  }

  private async publishTextPostAttempt(call: Call, input: PublishTextPostInput): Promise<MutationAttempt<PublishedPost>> {
    const operation = 'publishTextPost';
    const zero = emptyPublishedPost();
    if (!input || typeof input.pageId !== 'string' || !numericID.test(input.pageId) || typeof input.message !== 'string' || !input.message.trim() || Buffer.from(input.message, 'utf8').toString('utf8') !== input.message || Buffer.byteLength(input.message, 'utf8') > 65_536) {
      return newMutationBranch('defect', zero, {}, failure(operation, 'VALIDATION', 'Page ID or message is invalid'));
    }
    let fence: ReturnType<typeof newFence>;
    try { fence = newFence(call, PublishTextPostDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000); }
    catch { return newMutationBranch('defect', zero, {}, failure(operation, 'LOCAL_DEFECT', 'Meta dispatch fence is unavailable')); }
    if (fence.decide().kind !== 'sendAllowed') return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'Meta publish outcome cannot be confirmed; inspect the Page before retrying'));
    let accessToken: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessToken = resolved.pageAccessToken instanceof SecretString ? resolved.pageAccessToken.reveal() : '';
    } catch { return newMutationBranch('defect', zero, {}, failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid')); }
    if (!accessToken || !isHeaderSafeCredential(accessToken)) return newMutationBranch('defect', zero, {}, failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid'));
    const form = new URLSearchParams({ message: input.message }).toString();
    const request: ProviderRequest = {
      url: `${this.baseUrl}/${input.pageId}/feed`, method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded', Authorization: `Bearer ${accessToken}`, 'User-Agent': 'Dex Connector (https://superdurable.io)' },
      body: Buffer.from(form), maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await fence.dispatch(signal => this.sendRequest({ ...request, signal })); }
    catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError) return newMutationRetry(failure(operation, 'TRANSPORT', 'Meta dispatch was not started'));
      if (error instanceof ProviderTransportError && error.isProvablyNotSent) {
        try { await fence.recordCanResend(); return newMutationRetry(failure(operation, 'TRANSPORT', 'Meta could not be reached before sending')); }
        catch { return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'Meta publish outcome is unknown')); }
      }
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 200) return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'Meta publish response could not be confirmed'));
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
      }
      else return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'Meta publish outcome is unknown'));
    }
    // A Graph throttling code means Meta refused the post before processing it.
    const isRateLimited = response.status === 429 || (response.status >= 400 && response.status < 500 && decodeGraphError(response.body)?.isThrottled === true);
    if (isRateLimited || response.status === 503) {
      try { await fence.recordCanResend(); }
      catch { return newMutationRetry(failure(operation, 'AVAILABILITY', 'Meta retry checkpoint is unavailable')); }
      const kind = isRateLimited ? 'RATE_LIMIT' : 'AVAILABILITY';
      return newMutationRetry(failure(operation, kind, 'Meta did not process the post'), parseRetryAfter(headerValue(response.headers['retry-after'])));
    }
    if (response.status >= 500 || response.status === 408 || response.status === 409) return newMutationUncertain(zero, failure(operation, 'AVAILABILITY', 'Meta publish outcome is unknown'));
    if (response.status >= 400 && response.status < 500) {
      const kind = response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
      return newMutationBranch('providerRejected', zero, {}, failure(operation, kind, `Meta rejected HTTP ${response.status}`));
    }
    if (response.body.length > this.maxResponseBytes) return newMutationUncertain(zero, failure(operation, 'TRANSPORT', 'Meta publish response could not be confirmed'));
    const document = parseJSON(response.body);
    if (response.status !== 200 || !isObject(document) || typeof document.id !== 'string' || !postID.test(document.id)) return newMutationUncertain(zero, failure(operation, 'PROTOCOL', 'Meta publish response is incomplete'));
    try { await fence.recordProviderId(document.id); }
    catch { return newMutationUncertain(zero, failure(operation, 'LOCAL_DEFECT', 'Meta post ID could not be checkpointed')); }
    return newMutationBranch('published', { postId: document.id });
  }

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
    const graphError = status >= 400 ? decodeGraphError(response.body) : undefined;
    if (graphError?.isThrottled) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Meta rate limited the request') };
    if (graphError?.isTransient) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Meta reported a transient error') };
    if (status === 404) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Meta did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Meta rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Meta response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** Publish one plain-text Page post through a durable dispatch fence. */
export class PublishTextPostOperation implements Mutation<PublishTextPostInput, PublishedPost> {
  /** Bind a client's single-dispatch publishing callback. */
  constructor(private readonly publish: (call: Call, input: PublishTextPostInput) => Promise<MutationAttempt<PublishedPost>>) {}
  /** Return the manifest-generated publishing contract. */ definition(): MutationDefinition { return PublishTextPostDefinition; }
  /** Record the stable Call ID; Meta does not deduplicate on this key. */ idempotencyKey(callID: CallID): string { return callID; }
  /** Publish once, returning uncertainty when the result cannot be confirmed. */ invoke(call: Call, input: PublishTextPostInput): Promise<MutationAttempt<PublishedPost>> { return this.publish(call, input); }
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
/** Decode Meta's Graph API error envelope; undefined means the body is not one. */
function decodeGraphError(body: Uint8Array): { isThrottled: boolean; isTransient: boolean } | undefined {
  const document = parseJSON(body);
  if (!isObject(document) || !isObject(document.error)) return undefined;
  const code = document.error.code ?? 0;
  const isTransient = document.error.is_transient ?? false;
  if (!Number.isSafeInteger(code) || typeof isTransient !== 'boolean') return undefined;
  const numericCode = code as number;
  // Meta documents these rate-limit codes, which Graph returns with HTTP 4xx.
  const isThrottled = [4, 17, 32, 341, 613].includes(numericCode) || (numericCode >= 80_000 && numericCode <= 80_014);
  return { isThrottled, isTransient };
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
