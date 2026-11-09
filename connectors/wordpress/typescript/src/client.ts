// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry,
  type Call, type CredentialProvider, type Failure, type Mutation, type MutationAttempt,
  type Query, type QueryAttempt,
} from '@superdurable/connectors-sdk';
import {
  BodyTooLargeError, ProviderTransportError, parseRetryAfter, sendProviderRequest, validateBaseURL,
  type ProviderRequest, type ProviderResponse,
} from '@superdurable/connectors-sdk/providerhttp';
import { CheckpointError, InsufficientTimeError, newFence } from '@superdurable/connectors-sdk/singledispatch';
import { CreatePostDefinition, GetPostDefinition, ListPostsDefinition } from './generated/definitions.js';
import type {
  Config, CreatePostInput, CreatedPost, Credentials, GetPostInput, ListPostsInput,
  Post, PostPage,
} from './generated/models.js';

const createdStatuses = new Set(['draft', 'publish', 'pending', 'private', 'future']);
const emptyCreatedPost: CreatedPost = { id: 0, status: '' };
const emptyPost: Post = {
  id: 0, slug: '', status: '', link: '', title: { rendered: '' },
  content: { rendered: '' }, dateGMT: '',
};
const emptyPage: PostPage = { posts: [], page: 0, total: 0, totalPages: 0 };

/** Injectable HTTP sender and clock for deterministic provider replay. */
export interface WordPressClientOptions {
  /** Provider sender; omitted uses the SDK's redirect-safe transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock for Retry-After dates. */
  now?: () => Date;
}

/** One bounded WordPress exchange without credential-bearing URLs or headers. */
export interface WordPressExchange {
  /** HTTP status, or zero when no response arrived. */ status: number;
  /** Complete bounded response body. */ body: Uint8Array;
  /** Provider response headers used for pagination and retry. */ headers: ProviderResponse['headers'];
  /** Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Safe transport or credential classification. */ error?: 'request' | 'tooLarge' | 'auth';
  /** Whether the request may have reached WordPress. */ requestSent: boolean;
}

/** WordPress REST client with call-time application-password credentials. */
export class WordPressClient {
  /** Validated site URL without a trailing slash. */ readonly siteUrl: string;
  /** Maximum complete post request size in bytes. */ readonly maxPostBytes: number;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate site configuration without contacting WordPress. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: WordPressClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('WordPress configuration and credential source are required');
    this.siteUrl = validateBaseURL(config.siteUrl);
    if (this.siteUrl.endsWith('/wp-json')) throw new TypeError('WordPress site URL must omit /wp-json');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    this.maxPostBytes = config.maxPostBytes ?? 1_048_576;
    if (![this.timeoutMs, this.maxResponseBytes, this.maxPostBytes].every(value =>
      Number.isSafeInteger(value) && value > 0))
      throw new TypeError('WordPress request limits must be positive integers');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the single-dispatch post Mutation. */ createPost(): CreatePostOperation {
    return new CreatePostOperation(this);
  }
  /** Bind the published post Query. */ getPost(): GetPostOperation {
    return new GetPostOperation(this);
  }
  /** Bind the published post page Query. */ listPosts(): ListPostsOperation {
    return new ListPostsOperation(this);
  }

  /** Resolve a Basic authorization header without exposing the password in failures. */
  async authorization(call: Call): Promise<string | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const username = credentials.username;
      const password = credentials.applicationPassword.reveal().replaceAll(' ', '');
      if (!username || /[:\r\n\0]/.test(username) || !password || /[\r\n\0]/.test(password)) return undefined;
      return `Basic ${Buffer.from(`${username}:${password}`).toString('base64')}`;
    } catch { return undefined; }
  }

  /** Send one bounded REST request and retain positive no-send evidence. */
  async exchange(call: Call, method: 'GET' | 'POST', url: string,
    headers: Record<string, string>, body?: Uint8Array, signal?: AbortSignal): Promise<WordPressExchange> {
    const request: ProviderRequest = {
      url, method, headers, maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: url.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: signal ?? call.context.cancellationSignal,
      ...(body ? { body } : {}),
    };
    try {
      const response = await this.send(request);
      return { status: response.status, body: response.body, headers: response.headers,
        retryAfterMs: this.retryAfter(response.headers), requestSent: true };
    } catch (error) {
      if (error instanceof BodyTooLargeError)
        return { status: error.status ?? 0, body: new Uint8Array(), headers: error.headers ?? {},
          retryAfterMs: this.retryAfter(error.headers ?? {}), error: 'tooLarge', requestSent: true };
      if (error instanceof ProviderTransportError)
        return { status: 0, body: new Uint8Array(), headers: {}, retryAfterMs: 0,
          error: 'request', requestSent: !error.isProvablyNotSent };
      return { status: 0, body: new Uint8Array(), headers: {}, retryAfterMs: 0,
        error: 'request', requestSent: true };
    }
  }

  private retryAfter(headers: ProviderResponse['headers']): number {
    const value = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '', this.now());
  }
}

/** Create one post without repeating an ambiguous dispatch. */
export class CreatePostOperation implements Mutation<CreatePostInput, CreatedPost> {
  /** Bind the provider client. */ constructor(private readonly client: WordPressClient) {}
  /** Return the generated Mutation contract. */ definition() { return CreatePostDefinition; }
  /** Keep stable Dex call identity; WordPress receives no deduplication key. */
  idempotencyKey(callId: string): string { return callId; }

  /** Claim a checkpoint before dispatch and classify the create response. */
  async invoke(call: Call, input: CreatePostInput): Promise<MutationAttempt<CreatedPost>> {
    let fence: ReturnType<typeof newFence>;
    try { fence = newFence(call, CreatePostDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000); }
    catch { return createFailure('defect', 'LOCAL_DEFECT'); }
    if (fence.decide().kind !== 'sendAllowed') return createUncertain('TRANSPORT');
    const status = input?.status || 'draft';
    if (!input || typeof input.title !== 'string' || !input.title.trim() ||
      typeof input.content !== 'string' || (status !== 'draft' && status !== 'publish'))
      return createFailure('defect', 'VALIDATION');
    const payload = Buffer.from(JSON.stringify({ title: input.title, content: input.content, status }));
    if (payload.byteLength > this.client.maxPostBytes) return createFailure('defect', 'VALIDATION');
    const authorization = await this.client.authorization(call);
    if (!authorization) return createFailure('defect', 'AUTHENTICATION');
    let exchange: WordPressExchange;
    try {
      exchange = await fence.dispatch(signal => this.client.exchange(call, 'POST',
        `${this.client.siteUrl}/wp-json/wp/v2/posts`,
        { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: authorization },
        payload, signal));
    } catch (error) {
      if (error instanceof CheckpointError || error instanceof InsufficientTimeError)
        return createRetry('TRANSPORT');
      return createUncertain('TRANSPORT');
    }
    if (exchange.error === 'request') {
      if (!exchange.requestSent) {
        try { await fence.recordCanResend(); return createRetry('TRANSPORT'); }
        catch { /* The claimed marker still blocks another send. */ }
      }
      return createUncertain('TRANSPORT');
    }
    if (exchange.status === 429 || exchange.status === 503) {
      try { await fence.recordCanResend(); }
      catch { return createRetry('AVAILABILITY'); }
      return createRetry(exchange.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', exchange.retryAfterMs);
    }
    if (exchange.status >= 500 || exchange.status === 408 || exchange.status === 409)
      return createUncertain('AVAILABILITY');
    if (exchange.status === 401) return createFailure('providerRejected', 'AUTHENTICATION');
    if (exchange.status === 403) return createFailure('providerRejected', 'AUTHORIZATION');
    if (exchange.status >= 400 && exchange.status < 500)
      return createFailure('providerRejected', 'PROVIDER_REJECTION');
    if (exchange.status !== 201) return createUncertain('PROTOCOL');
    if (exchange.error) return createUncertain('TRANSPORT');
    const body = parseJSON(exchange.body);
    if (!isRecord(body) || !Number.isSafeInteger(body.id) || (body.id as number) <= 0 ||
      typeof body.status !== 'string' || !createdStatuses.has(body.status))
      return createUncertain('PROTOCOL');
    const id = body.id as number;
    try { await fence.recordProviderId(String(id)); }
    catch { /* The claimed marker still blocks another send. */ }
    return newMutationBranch('created', { id, status: body.status });
  }
}

/** Read one published WordPress post by numeric ID. */
export class GetPostOperation implements Query<GetPostInput, Post> {
  /** Bind the provider client. */ constructor(private readonly client: WordPressClient) {}
  /** Return the generated Query contract. */ definition() { return GetPostDefinition; }
  /** Read one post and exclude drafts from Flow state. */
  async invoke(call: Call, input: GetPostInput): Promise<QueryAttempt<Post>> {
    if (!input || !Number.isSafeInteger(input.postId) || input.postId <= 0)
      return queryFailure('getPost', 'defect', 'VALIDATION', emptyPost);
    const exchange = await readPost(this.client, call, `/wp-json/wp/v2/posts/${input.postId}`);
    const decision = classifyQuery(exchange, 'getPost', emptyPost);
    if (decision) return decision;
    try {
      return newQueryBranch('found', decodePost(parseJSON(exchange.body)));
    } catch (error) {
      return queryFailure('getPost', error instanceof PrivatePostError ? 'notFound' : 'invalidResponse',
        error instanceof PrivatePostError ? 'NOT_FOUND' : 'PROTOCOL', emptyPost);
    }
  }
}

/** Read one bounded page of published WordPress posts. */
export class ListPostsOperation implements Query<ListPostsInput, PostPage> {
  /** Bind the provider client. */ constructor(private readonly client: WordPressClient) {}
  /** Return the generated Query contract. */ definition() { return ListPostsDefinition; }
  /** Validate pagination and an optional publication instant before querying. */
  async invoke(call: Call, input: ListPostsInput): Promise<QueryAttempt<PostPage>> {
    if (!input || !Number.isSafeInteger(input.page ?? 0) || (input.page ?? 0) < 0 ||
      !Number.isSafeInteger(input.perPage ?? 0) || (input.perPage ?? 0) < 0 || (input.perPage ?? 0) > 100 ||
      (input.after !== undefined && input.after !== '' && !validInstant(input.after)))
      return queryFailure('listPosts', 'defect', 'VALIDATION', emptyPage);
    const page = input.page || 1;
    const query = new URLSearchParams({ status: 'publish', page: String(page) });
    if (input.perPage) query.set('per_page', String(input.perPage));
    if (input.after) query.set('after', input.after);
    const exchange = await readPost(this.client, call, `/wp-json/wp/v2/posts?${query}`);
    const decision = classifyQuery(exchange, 'listPosts', emptyPage);
    if (decision) return decision;
    try {
      const body = parseJSON(exchange.body);
      if (!Array.isArray(body)) throw new TypeError('invalid post page');
      const total = Number(headerValue(exchange.headers, 'x-wp-total'));
      const totalPages = Number(headerValue(exchange.headers, 'x-wp-totalpages'));
      if (!Number.isSafeInteger(total) || total < 0 || !Number.isSafeInteger(totalPages) || totalPages < 0 ||
        !headerValue(exchange.headers, 'x-wp-total') || !headerValue(exchange.headers, 'x-wp-totalpages'))
        throw new TypeError('invalid WordPress post totals');
      return newQueryBranch('listed', { posts: body.map(decodePost), page, total, totalPages });
    } catch {
      return queryFailure('listPosts', 'invalidResponse', 'PROTOCOL', emptyPage);
    }
  }
}

async function readPost(client: WordPressClient, call: Call, path: string): Promise<WordPressExchange> {
  const authorization = await client.authorization(call);
  if (!authorization) return { status: 0, body: new Uint8Array(), headers: {}, retryAfterMs: 0,
    error: 'auth', requestSent: false };
  return client.exchange(call, 'GET', `${client.siteUrl}${path}`, {
    Accept: 'application/json', Authorization: authorization,
  });
}

function classifyQuery<T>(exchange: WordPressExchange, operation: 'getPost' | 'listPosts',
  empty: T): QueryAttempt<T> | undefined {
  if (exchange.error === 'auth') return queryFailure(operation, 'defect', 'AUTHENTICATION', empty);
  if (exchange.error === 'request') return queryRetry(operation, 'AVAILABILITY');
  if (exchange.status === 429) return queryRetry(operation, 'RATE_LIMIT', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status === 409 || exchange.status >= 500)
    return queryRetry(operation, 'AVAILABILITY', exchange.retryAfterMs);
  if (exchange.status === 404 && operation === 'getPost')
    return queryFailure(operation, 'notFound', 'NOT_FOUND', empty);
  if (exchange.status === 401) return queryFailure(operation, 'providerRejected', 'AUTHENTICATION', empty);
  if (exchange.status === 403) return queryFailure(operation, 'providerRejected', 'AUTHORIZATION', empty);
  if (exchange.status !== 200) return queryFailure(operation, 'providerRejected', 'PROVIDER_REJECTION', empty);
  if (exchange.error === 'tooLarge') return queryFailure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', empty);
  return undefined;
}

class PrivatePostError extends Error {}

function decodePost(value: unknown): Post {
  if (!isRecord(value)) throw new TypeError('post is not an object');
  if (value.status !== 'publish') throw new PrivatePostError();
  if (!Number.isSafeInteger(value.id) || (value.id as number) <= 0 ||
    typeof value.slug !== 'string' || !value.slug || typeof value.link !== 'string' || !value.link ||
    typeof value.date_gmt !== 'string' || !validDateGMT(value.date_gmt))
    throw new TypeError('post is incomplete');
  const title = renderedText(value.title);
  const content = renderedText(value.content);
  return { id: value.id as number, slug: value.slug, status: 'publish', link: value.link,
    title, content, dateGMT: value.date_gmt };
}

function renderedText(value: unknown): { rendered: string } {
  if (value === undefined || value === null) return { rendered: '' };
  if (!isRecord(value) || (value.rendered !== undefined && typeof value.rendered !== 'string'))
    throw new TypeError('rendered HTML is invalid');
  return { rendered: typeof value.rendered === 'string' ? value.rendered : '' };
}

function validDateGMT(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$/.test(value)) return false;
  return Number.isFinite(Date.parse(`${value}Z`));
}
function validInstant(value: unknown): boolean {
  return typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value) &&
    Number.isFinite(Date.parse(value));
}
function headerValue(headers: ProviderResponse['headers'], name: string): string {
  const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
function parseJSON(body: Uint8Array): unknown {
  try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; }
  catch { return undefined; }
}
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function failure(operation: string, kind: Failure['kind']): Failure {
  return { kind, provider: 'wordpress', operation, message: 'WordPress provider operation failed' };
}
function createFailure(branch: string, kind: Failure['kind']): MutationAttempt<CreatedPost> {
  return newMutationBranch(branch, emptyCreatedPost, {}, failure('createPost', kind));
}
function createRetry(kind: Failure['kind'], after = 0): MutationAttempt<CreatedPost> {
  return newMutationRetry(failure('createPost', kind), after);
}
function createUncertain(kind: Failure['kind']): MutationAttempt<CreatedPost> {
  return newMutationUncertain(emptyCreatedPost, failure('createPost', kind));
}
function queryFailure<T>(operation: string, branch: string, kind: Failure['kind'], empty: T): QueryAttempt<T> {
  return newQueryBranch(branch, empty, {}, failure(operation, kind));
}
function queryRetry<T>(operation: string, kind: Failure['kind'], after = 0): QueryAttempt<T> {
  return newQueryRetry(failure(operation, kind), after);
}
