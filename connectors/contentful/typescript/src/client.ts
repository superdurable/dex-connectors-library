// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError, MaxErrorBodyBytes, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetEntryDefinition, ListEntriesDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetEntryInput, GetEntryOutput, ListEntriesInput, ListEntriesOutput } from './generated/models.js';

const identifier = /^[A-Za-z0-9_-]+$/;
const emptyOutput = { data: null };

/** Injectable request sender for deterministic provider tests. */
export type ContentfulRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional constructor dependencies; production uses the Connector SDK transport. */
export interface ContentfulClientOptions {
  /** Provider HTTP sender. */ send?: ContentfulRequestSender;
}

interface ProviderOutcome {
  body?: Uint8Array;
  branch?: string;
  failure?: Failure;
  retry?: boolean;
  retryAfterMs?: number;
}

/** Authenticated Contentful client with call-time credential resolution. */
export class ContentfulClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly sendRequest: ContentfulRequestSender;

  /** Validate configuration without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: ContentfulClientOptions = {}) {
    if (!credentials) throw new TypeError('contentful credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://cdn.contentful.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0) throw new TypeError('contentful timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) throw new TypeError('contentful maxResponseBytes is invalid');
    this.sendRequest = options.send ?? sendProviderRequest;
  }

  /** Bind the getEntry Query. */ getEntry(): GetEntryOperation { return new GetEntryOperation(this); }
  /** Bind the listEntries Query. */ listEntries(): ListEntriesOperation { return new ListEntriesOperation(this); }

  /** Send one bounded request and classify its provider outcome. */
  async exchange(call: Call, operation: string, path: string, hasNotFound: boolean): Promise<ProviderOutcome> {
    let token: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = resolved.deliveryToken instanceof SecretString ? resolved.deliveryToken.reveal() : '';
    } catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable') }; }
    if (!token || !isHeaderSafeCredential(token)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credential is invalid') };
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method: 'GET',
      headers: { Accept: 'application/json', 'User-Agent': 'superdurable-contentful/0.1', Authorization: `Bearer ${token}` },
      maxResponseBytes: Math.max(this.maxResponseBytes, MaxErrorBodyBytes), timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl === 'https://cdn.contentful.com' ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.sendRequest(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        if (response.status === 200) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit') };
      } else if (error instanceof AddressRefusal) {
        return { branch: 'defect', failure: failure(operation, 'LOCAL_DEFECT', 'request could not be built') };
      } else return { retry: true, failure: failure(operation, 'AVAILABILITY', 'provider request failed') };
    }
    const status = response.status;
    if (status === 429 || status === 408 || status === 409 || status >= 500) {
      const retryAfter = response.headers['retry-after'];
      return { retry: true, retryAfterMs: parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? ''),
        failure: failure(operation, status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'provider is temporarily unavailable') };
    }
    if (status === 404 && hasNotFound) return { branch: 'notFound', failure: failure(operation, 'NOT_FOUND', 'resource was not found') };
    if (status !== 200) {
      const kind = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION';
      return { branch: 'providerRejected', failure: failure(operation, kind, `provider rejected the request with HTTP ${status}`) };
    }
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit') };
    try { JSON.parse(Buffer.from(response.body).toString('utf8')); }
    catch { return { branch: 'invalidResponse', failure: failure(operation, 'PROTOCOL', 'provider returned malformed JSON') }; }
    return { body: response.body };
  }
}

/** Read one published Contentful entry. */
export class GetEntryOperation implements Query<GetEntryInput, GetEntryOutput> {
  /** Bind an authenticated client. */ constructor(private readonly client: ContentfulClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return GetEntryDefinition; }
  /** Validate IDs, send one request, and classify the result. */
  async invoke(call: Call, input: GetEntryInput): Promise<QueryAttempt<GetEntryOutput>> {
    if (!isIdentifier(input.spaceId) || !isIdentifier(input.environmentId) || !isIdentifier(input.entryId)) {
      return terminal('getEntry', 'defect', 'VALIDATION', 'space, environment, and entry IDs are required');
    }
    const result = await this.client.exchange(call, 'getEntry', `/spaces/${input.spaceId}/environments/${input.environmentId}/entries/${input.entryId}`, true);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyOutput, {}, result.failure);
    const data = JSON.parse(Buffer.from(result.body!).toString('utf8')) as unknown;
    if (!isObject(data) || !isObject(data.sys)) return terminal('getEntry', 'invalidResponse', 'PROTOCOL', 'provider returned an incomplete response');
    return newQueryBranch('found', { data });
  }
}

/** List one page of published Contentful entries. */
export class ListEntriesOperation implements Query<ListEntriesInput, ListEntriesOutput> {
  /** Bind an authenticated client. */ constructor(private readonly client: ContentfulClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return ListEntriesDefinition; }
  /** Validate selection, send one request, and classify the page. */
  async invoke(call: Call, input: ListEntriesInput): Promise<QueryAttempt<ListEntriesOutput>> {
    if (!isIdentifier(input.spaceId) || !isIdentifier(input.environmentId) || !Number.isInteger(input.limit ?? 0) || (input.limit ?? 0) < 0 || (input.limit ?? 0) > 1000 || !Number.isInteger(input.skip ?? 0) || (input.skip ?? 0) < 0) {
      return terminal('listEntries', 'defect', 'VALIDATION', 'invalid space, environment, limit, or skip');
    }
    const query = new URLSearchParams();
    if (input.limit) query.set('limit', String(input.limit));
    if (input.skip) query.set('skip', String(input.skip));
    const suffix = query.size ? `?${query}` : '';
    const result = await this.client.exchange(call, 'listEntries', `/spaces/${input.spaceId}/environments/${input.environmentId}/entries${suffix}`, false);
    if (result.retry) return newQueryRetry(result.failure!, result.retryAfterMs);
    if (result.failure) return newQueryBranch(result.branch!, emptyOutput, {}, result.failure);
    const data = JSON.parse(Buffer.from(result.body!).toString('utf8')) as unknown;
    if (!isObject(data) || !Array.isArray(data.items)) return terminal('listEntries', 'invalidResponse', 'PROTOCOL', 'provider returned an incomplete response');
    return newQueryBranch('listed', { data });
  }
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'contentful', operation, kind, message }; }
function terminal(operation: string, branch: string, kind: Failure['kind'], message: string): QueryAttempt<{ data: null }> {
  return newQueryBranch(branch, emptyOutput, {}, failure(operation, kind, message));
}
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isIdentifier(value: unknown): value is string { return typeof value === 'string' && identifier.test(value); }
