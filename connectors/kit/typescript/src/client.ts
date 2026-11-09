// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetSubscriberDefinition, ListSubscribersDefinition } from './generated/definitions.js';
import type { Config, SubscriberMetadata, SubscribersPage, Credentials, GetSubscriberInput, ListSubscribersInput } from './generated/models.js';

const emptySubscribers = (): SubscribersPage => ({ subscribers: [], hasNextPage: false, nextCursor: '', perPage: 0 });
const emptySubscriber = (): SubscriberMetadata => ({ id: 0, emailAddress: '', firstName: '', state: '' });

/** Optional transport dependency for provider tests and local Dex examples. */
export interface KitClientOptions {
  /** One bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Kit API v4 client with call-time API Key resolution. */
export class KitClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate account configuration without contacting Kit. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: KitClientOptions = {}) {
    if (!credentials) throw new TypeError('Kit credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.kit.com/v4');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Kit timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Kit response limit is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the subscriber page Query. */ listSubscribers(): ListSubscribersOperation { return new ListSubscribersOperation(this); }
  /** Bind the subscriber metadata Query. */ getSubscriber(): GetSubscriberOperation { return new GetSubscriberOperation(this); }

  /** Send one read and classify transport, HTTP, and response-size outcomes. */
  async exchange(call: Call, operation: string, path: string, hasNotFound: boolean): Promise<ProviderOutcome> {
    let apiKey: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = resolved.apiKey instanceof SecretString ? resolved.apiKey.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') }; }
    if (!apiKey || !isHeaderSafeCredential(apiKey)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable or invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', 'X-Kit-Api-Key': apiKey },
      maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Kit response exceeded the configured limit') };
      } else return { retry: true, failure: failure(operation, 'TRANSPORT', 'Kit request failed') };
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(headerValue(response.headers['retry-after']));
    if (status === 429) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'RATE_LIMIT', 'Kit rate limited the request') };
    if (status === 408 || status >= 500) return { retry: true, retryAfterMs: retryAfter, failure: failure(operation, 'AVAILABILITY', 'Kit is temporarily unavailable') };
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'Kit did not find the resource') };
    if (status !== 200) return { branch: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', `Kit rejected HTTP ${status}`) };
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Kit response exceeded the configured limit') };
    return { body: response.body };
  }
}

/** List one cursor page of subscribers accessible to the API user. */
export class ListSubscribersOperation implements Query<ListSubscribersInput, SubscribersPage> {
  /** Bind the provider client. */ constructor(private readonly client: KitClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return ListSubscribersDefinition; }
  /** Validate pagination and read subscriber metadata. */
  async invoke(call: Call, input: ListSubscribersInput): Promise<QueryAttempt<SubscribersPage>> {
    const after = input.after ?? '';
    const perPage = input.perPage || 20;
    if (typeof after !== 'string' || !isNonnegativeInteger(input.perPage ?? 0) || perPage > 1000 || Buffer.byteLength(after, 'utf8') > 2048 || !isValidUnicode(after)) return terminal('listSubscribers', 'defect', 'VALIDATION', 'cursor or perPage is invalid', emptySubscribers());
    const cursor = after ? `after=${encodeURIComponent(after)}&` : '';
    const result = await this.client.exchange(call, 'listSubscribers', `/subscribers?${cursor}per_page=${perPage}&slim=true`, false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptySubscribers(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !Array.isArray(document.subscribers) || !isObject(document.pagination)) return invalid('listSubscribers', emptySubscribers());
    const pagination = document.pagination;
    const hasNextPage = pagination.has_next_page ?? false;
    if (typeof hasNextPage !== 'boolean' || (hasNextPage && (typeof pagination.end_cursor !== 'string' || !pagination.end_cursor))) return invalid('listSubscribers', emptySubscribers());
    const subscribers: SubscriberMetadata[] = [];
    for (const item of document.subscribers) {
      if (!isSubscriber(item)) return invalid('listSubscribers', emptySubscribers());
      subscribers.push(subscriberMetadata(item));
    }
    return newQueryBranch('listed', { subscribers, hasNextPage, nextCursor: hasNextPage ? pagination.end_cursor as string : '', perPage });
  }
}

/** Read one subscriber without embedded custom fields. */
export class GetSubscriberOperation implements Query<GetSubscriberInput, SubscriberMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: KitClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return GetSubscriberDefinition; }
  /** Validate the subscriber ID and read its metadata. */
  async invoke(call: Call, input: GetSubscriberInput): Promise<QueryAttempt<SubscriberMetadata>> {
    if (!isPositiveInteger(input.subscriberId)) return terminal('getSubscriber', 'defect', 'VALIDATION', 'subscriber ID must be positive', emptySubscriber());
    const result = await this.client.exchange(call, 'getSubscriber', `/subscribers/${input.subscriberId}`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptySubscriber(), {}, result.failure);
    const document = parseJSON(result.body!);
    if (!isObject(document) || !isSubscriber(document.subscriber)) return invalid('getSubscriber', emptySubscriber());
    return newQueryBranch('found', subscriberMetadata(document.subscriber));
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'kit', operation, kind, message }; }
function terminal<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: Output): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function invalid<Output>(operation: string, empty: Output): QueryAttempt<Output> { return terminal(operation, 'invalidResponse', 'PROTOCOL', 'Kit returned invalid JSON', empty); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isPositiveInteger(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value > 0; }
function isNonnegativeInteger(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0; }
function isValidUnicode(value: string): boolean { try { encodeURIComponent(value); return true; } catch { return false; } }
function isSubscriber(value: unknown): value is Record<string, unknown> { return isObject(value) && isPositiveInteger(value.id) && typeof value.email_address === 'string' && value.email_address.length > 0 && typeof value.state === 'string' && value.state.length > 0 && (value.first_name === undefined || value.first_name === null || typeof value.first_name === 'string'); }
function subscriberMetadata(value: Record<string, unknown>): SubscriberMetadata { return { id: value.id as number, emailAddress: value.email_address as string, firstName: (value.first_name as string | null | undefined) ?? '', state: value.state as string }; }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function headerValue(value: string | string[] | undefined): string { return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
