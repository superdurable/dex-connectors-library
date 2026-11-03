// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, SecretString, type Call, type CredentialProvider, type Failure, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials, GetCollectionItemInput, GetCollectionItemOutput, ListCollectionItemsInput, ListCollectionItemsOutput } from './generated/models.js';
import { GetCollectionItemDefinition, ListCollectionItemsDefinition } from './generated/definitions.js';

const objectID = /^[0-9a-fA-F]{24}$/;
const emptyOutput = { data: null };
const defaultBaseURL = 'https://api.webflow.com/v2';

/** Injectable HTTP sender for provider tests. */
export type WebflowRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Dependencies for deterministic provider calls. */
export interface WebflowClientOptions { /** HTTP sender; defaults to the SDK transport. */ send?: WebflowRequestSender; }

/** Webflow CMS client with immutable settings and per-call credentials. */
export class WebflowClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: WebflowRequestSender;

  /** Validate settings without contacting Webflow. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: WebflowClientOptions = {}) {
    if (!credentials) throw new TypeError('Webflow credential source is required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL).replace(/\/+$/, '');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0) throw new TypeError('Webflow timeout is invalid');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) throw new TypeError('Webflow response limit is invalid');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the list query. */ listCollectionItems() { return new ListCollectionItemsOperation(this); }
  /** Bind the item query. */ getCollectionItem() { return new GetCollectionItemOperation(this); }

  /** Send one provider request and classify its response. */
  async request(call: Call, operation: string, path: string, hasNotFound: boolean): Promise<{ body: Uint8Array } | { attempt: QueryAttempt<{ data: unknown | null }> }> {
    let token: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      token = resolved.siteToken instanceof SecretString ? resolved.siteToken.reveal() : '';
    } catch { return { attempt: failed(operation, 'defect', 'AUTHENTICATION', 'connection credentials are unavailable') }; }
    if (!token || !isHeaderSafeCredential(token)) return { attempt: failed(operation, 'defect', 'AUTHENTICATION', 'connection credential is invalid') };
    try {
      const response = await this.send({ url: this.baseURL + path, method: 'GET',
        headers: { Accept: 'application/json', 'User-Agent': 'superdurable-webflow/0.1', Authorization: `Bearer ${token}` },
        timeoutMs: this.timeoutMs, maxResponseBytes: Math.max(this.maxResponseBytes, 65_536),
        addressPolicy: 'privateNetworkHTTP', signal: call.context.cancellationSignal });
      return classifyResponse(operation, response.status, response.body, response.headers['retry-after'], hasNotFound, this.maxResponseBytes);
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.status !== 200) return classifyResponse(operation, error.status, new Uint8Array(), error.headers?.['retry-after'], hasNotFound, this.maxResponseBytes);
        return { attempt: failed(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit') };
      }
      return { attempt: newQueryRetry(failure(operation, 'AVAILABILITY', 'provider request failed')) };
    }
  }
}

/** List one staged CMS collection page. */
export class ListCollectionItemsOperation {
  /** Bind the client. */ constructor(private readonly client: WebflowClient) {}
  /** Return the manifest operation. */ definition() { return ListCollectionItemsDefinition; }
  /** Validate input and invoke Webflow. */
  async invoke(call: Call, input: ListCollectionItemsInput): Promise<QueryAttempt<ListCollectionItemsOutput>> {
    if (!objectID.test(input.collectionId) || !Number.isSafeInteger(input.limit ?? 0) || (input.limit ?? 0) < 0 || (input.limit ?? 0) > 100 || !Number.isSafeInteger(input.offset ?? 0) || (input.offset ?? 0) < 0)
      return failed('listCollectionItems', 'defect', 'VALIDATION', 'invalid collection ID, limit, or offset');
    const query = new URLSearchParams();
    if (input.limit) query.set('limit', String(input.limit));
    if (input.offset) query.set('offset', String(input.offset));
    const suffix = query.size ? `?${query.toString()}` : '';
    const result = await this.client.request(call, 'listCollectionItems', `/collections/${input.collectionId}/items${suffix}`, false);
    if ('attempt' in result) return result.attempt;
    return decodeResponse('listCollectionItems', 'listed', 'items', result.body);
  }
}

/** Read one staged CMS collection item. */
export class GetCollectionItemOperation {
  /** Bind the client. */ constructor(private readonly client: WebflowClient) {}
  /** Return the manifest operation. */ definition() { return GetCollectionItemDefinition; }
  /** Validate input and invoke Webflow. */
  async invoke(call: Call, input: GetCollectionItemInput): Promise<QueryAttempt<GetCollectionItemOutput>> {
    if (!objectID.test(input.collectionId) || !objectID.test(input.itemId)) return failed('getCollectionItem', 'defect', 'VALIDATION', 'collection and item IDs must be 24 hexadecimal characters');
    const result = await this.client.request(call, 'getCollectionItem', `/collections/${input.collectionId}/items/${input.itemId}`, true);
    if ('attempt' in result) return result.attempt;
    return decodeResponse('getCollectionItem', 'found', 'id', result.body);
  }
}

function classifyResponse(operation: string, status: number, body: Uint8Array, retryAfter: string | string[] | undefined, hasNotFound: boolean, maxResponseBytes: number): { body: Uint8Array } | { attempt: QueryAttempt<{ data: unknown | null }> } {
  if (status === 429 || status === 408 || status === 409 || status >= 500) return { attempt: newQueryRetry(failure(operation, status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'provider is temporarily unavailable'), parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '')) };
  if (status === 404 && hasNotFound) return { attempt: failed(operation, 'notFound', 'NOT_FOUND', 'resource was not found') };
  if (status !== 200) return { attempt: failed(operation, 'providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${status}`) };
  if (body.length > maxResponseBytes) return { attempt: failed(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit') };
  return { body };
}
function decodeResponse(operation: string, branch: string, shape: 'items' | 'id', body: Uint8Array): QueryAttempt<{ data: unknown | null }> {
  let decoded: unknown;
  try { decoded = JSON.parse(Buffer.from(body).toString('utf8')); }
  catch { return failed(operation, 'invalidResponse', 'PROTOCOL', 'provider returned malformed JSON'); }
  if (!decoded || typeof decoded !== 'object' || Array.isArray(decoded) || !(shape in decoded) || (shape === 'items' ? !Array.isArray((decoded as Record<string, unknown>).items) : typeof (decoded as Record<string, unknown>).id !== 'string' || !(decoded as Record<string, unknown>).id))
    return failed(operation, 'invalidResponse', 'PROTOCOL', 'provider returned an incomplete response');
  return newQueryBranch(branch, { data: decoded }, {});
}
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'webflow', operation, message }; }
function failed(operation: string, branch: string, kind: Failure['kind'], message: string): QueryAttempt<{ data: unknown | null }> { return newQueryBranch(branch, emptyOutput, {}, failure(operation, kind, message)); }
