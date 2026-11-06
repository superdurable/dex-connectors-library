// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, RetryError, resolveCredential, resolveCredentialAfterRejection, SecretString, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { GetObjectOperation, SearchObjectsOperation, UpdateObjectOperation, UpsertObjectOperation } from './operations.js';

const defaultEndpoint = 'https://api.hubapi.com';
const knownCategories = new Set(['BAD_REQUEST', 'CONFLICT', 'EXPIRED_AUTHENTICATION', 'INVALID_AUTHENTICATION', 'MISSING_SCOPES', 'OBJECT_ALREADY_EXISTS', 'OBJECT_NOT_FOUND', 'RATE_LIMITS', 'VALIDATION_ERROR']);

/** Replace the provider sender or clock in deterministic tests. */
export interface HubSpotClientOptions {
  /** Caller-owned provider sender. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock used for retry dates and receipts. */
  now?: () => Date;
}

/** Classified unsuccessful exchange. */
export interface ExchangeFailure {
  /** Retry or terminal disposition. */
  disposition: 'retry' | 'notFound' | 'conflict' | 'rejected' | 'invalidResponse' | 'defect';
  /** Safe failure. */
  failure: Failure;
  /** Provider-directed retry delay. */
  retryAfterMs: number;
  /** Provider receipt. */
  receipt: ReceiptDraft;
}

/** Successful response or classified failure. */
export type ExchangeResult = { response: ProviderResponse } | { outcome: ExchangeFailure };

/** HubSpot CRM client using host-owned renewable credentials. */
export class HubSpotClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'HubSpot', auth: Authorization, credentialFields: CredentialFields });

  /** Validate settings and bind a credential source without contacting HubSpot. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: HubSpotClientOptions = {}) {
    if (!credentials) throw new TypeError('HubSpot credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('HubSpot maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the search query. */ searchObjects(): SearchObjectsOperation { return new SearchObjectsOperation(this); }
  /** Bind the record query. */ getObject(): GetObjectOperation { return new GetObjectOperation(this); }
  /** Bind the unique-property upsert. */ upsertObject(): UpsertObjectOperation { return new UpsertObjectOperation(this); }
  /** Bind the absolute update. */ updateObject(): UpdateObjectOperation { return new UpdateObjectOperation(this); }

  /** Return a safe receipt for a completed provider attempt. */
  receipt(call: Call, response?: ProviderResponse, objectId = ''): ReceiptDraft {
    const correlation = response ? header(response, 'x-hubspot-correlation-id') : '';
    return { callId: call.id, provider: 'hubspot', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}),
      ...(/^[A-Za-z0-9-]{1,64}$/.test(correlation) ? { providerRequestId: correlation } : {}) };
  }

  /** Resolve, send, refresh after one OAuth 401, then classify the response. */
  async exchange(call: Call, operation: string, objectId: string, method: string, path: string, query?: string, body?: unknown): Promise<ExchangeResult> {
    let credentials: Credentials;
    try {
      credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      this.validateCredentials(credentials);
    } catch (error) {
      if (error instanceof ReauthorizationRequiredError) return { outcome: this.failure('rejected', operation, 'AUTHENTICATION', 'HubSpot authorization must be renewed') };
      if (error instanceof RetryError) return { outcome: this.failure('retry', operation, 'AVAILABILITY', 'HubSpot OAuth token refresh is temporarily unavailable') };
      return { outcome: this.failure('defect', operation, 'AUTHENTICATION', 'HubSpot connection credentials are unavailable') };
    }
    let encoded: Uint8Array | undefined;
    try { if (body !== undefined) encoded = Buffer.from(JSON.stringify(body)); }
    catch { return { outcome: this.failure('defect', operation, 'LOCAL_DEFECT', 'HubSpot request could not be built') }; }
    let response: ProviderResponse;
    try {
      response = await this.sendRequest(call, credentials, method, path, query, encoded);
      if (response.status === 401 && credentials.authMethodId === 'hubspot-oauth' && 'resolveAfterRejection' in this.credentials) {
        let replacement: Credentials | undefined;
        try {
          const resolved = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
          this.validateCredentials(resolved);
          replacement = resolved;
        } catch { /* The original rejection remains authoritative when refresh cannot recover it. */ }
        if (replacement) response = await this.sendRequest(call, replacement, method, path, query, encoded);
      }
    } catch (error) {
      const receipt = this.receipt(call, error instanceof BodyTooLargeError && error.headers ? { status: error.status ?? 0, headers: error.headers, body: new Uint8Array() } : undefined, objectId);
      if (error instanceof BodyTooLargeError) return { outcome: this.failure('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'HubSpot response exceeds the configured size limit', receipt) };
      return { outcome: this.failure('retry', operation, 'TRANSPORT', 'HubSpot did not return a complete response', receipt) };
    }
    if (response.status >= 200 && response.status < 300) return { response };
    return { outcome: this.classify(operation, response, this.receipt(call, response, objectId)) };
  }

  private validateCredentials(credentials: Credentials): void {
    if (!(credentials.accessToken instanceof SecretString) || !isHeaderSafeCredential(credentials.accessToken.reveal())) throw new TypeError('HubSpot access token is missing or malformed');
  }

  private sendRequest(call: Call, credentials: Credentials, method: string, path: string, query?: string, body?: Uint8Array): Promise<ProviderResponse> {
    return this.send({ url: `${this.endpoint}/${path}${query ? `?${query}` : ''}`, method,
      headers: { Accept: 'application/json', Authorization: `Bearer ${credentials.accessToken.reveal()}`, ...(body ? { 'Content-Type': 'application/json' } : {}) },
      ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.endpoint.startsWith('http://') ? 'privateNetworkHTTP' : 'publicHTTPS', timeoutMs: 25_000,
      signal: call.context.cancellationSignal });
  }

  /** Classify a batch upsert error using the same status rules as an HTTP error. */
  classifyBatchError(operation: string, response: ProviderResponse, receipt: ReceiptDraft): ExchangeFailure { return this.classify(operation, response, receipt); }

  private classify(operation: string, response: ProviderResponse, receipt: ReceiptDraft): ExchangeFailure {
    const status = response.status;
    const errorBody = decodeErrorBody(response.body);
    const retryAfter = parseRetryAfter(header(response, 'retry-after'), this.now());
    const detail = safeDetail(errorBody);
    if (status === 429 && errorBody.policyName === 'DAILY') return this.failure('rejected', operation, 'QUOTA_EXHAUSTED', "HubSpot's daily API request limit is exhausted", receipt);
    if (status === 429) return this.failure('retry', operation, 'RATE_LIMIT', 'HubSpot rate limited the request', receipt, Math.max(retryAfter, rateLimitDelay(header(response, 'x-hubspot-ratelimit-interval-milliseconds'))));
    if (status === 423) return this.failure('retry', operation, 'RATE_LIMIT', 'HubSpot locked the records for a high-volume sync', receipt, Math.max(retryAfter, 2_000));
    if (status === 408 || status === 477 || (status >= 500 && status !== 501)) return this.failure('retry', operation, 'AVAILABILITY', `HubSpot is temporarily unavailable (HTTP ${status})`, receipt, retryAfter);
    if (status === 401) return this.failure('rejected', operation, 'AUTHENTICATION', `HubSpot rejected the access token${detail}`, receipt);
    if (status === 403 && errorBody.category === 'MISSING_SCOPES') return this.failure('rejected', operation, 'AUTHORIZATION', `HubSpot access token lacks a required scope${detail}`, receipt);
    if (status === 403) return this.failure('rejected', operation, 'AUTHORIZATION', `HubSpot denied access to the record${detail}`, receipt);
    if (status === 404) return this.failure('notFound', operation, 'NOT_FOUND', `HubSpot found no such record${detail}`, receipt);
    if (status === 409) return this.failure('conflict', operation, 'CONFLICT', `HubSpot reported a conflict with another record${detail}`, receipt);
    if (status === 400 || status === 422 || errorBody.category === 'VALIDATION_ERROR') return this.failure('rejected', operation, 'VALIDATION', `HubSpot rejected the request as invalid${detail}`, receipt);
    if (status >= 300 && status < 400) return this.failure('rejected', operation, 'PROTOCOL', `HubSpot redirected the request (HTTP ${status})`, receipt);
    return this.failure('rejected', operation, 'PROVIDER_REJECTION', `HubSpot rejected the request (HTTP ${status})${detail}`, receipt);
  }

  private failure(disposition: ExchangeFailure['disposition'], operation: string, kind: Failure['kind'], message: string, receipt: ReceiptDraft = {}, retryAfterMs = 0): ExchangeFailure {
    return { disposition, failure: { kind, provider: 'hubspot', operation, message }, receipt, retryAfterMs };
  }
}

function header(response: ProviderResponse, name: string): string {
  const value = response.headers[name]; return Array.isArray(value) ? (value[0] ?? '') : (value ?? '');
}
function rateLimitDelay(value: string): number { if (!/^[+-]?[0-9]+$/.test(value)) return 0; const milliseconds = BigInt(value); return milliseconds > 0n && milliseconds <= 9_223_372_036_854_775_807n ? Number(milliseconds > 10_000n ? 10_000n : milliseconds) : 0; }
function decodeErrorBody(body: Uint8Array): { category?: string; policyName?: string; errors?: Array<{ code?: string }> } {
  if (body.length > 65_536) return {};
  try { const parsed: unknown = JSON.parse(Buffer.from(body).toString()); return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as Record<string, unknown> : {}; }
  catch { return {}; }
}
function safeDetail(body: ReturnType<typeof decodeErrorBody>): string {
  const tokens: string[] = [];
  if (body.category && knownCategories.has(body.category)) tokens.push(body.category);
  const code = body.errors?.[0]?.code;
  if (typeof code === 'string' && /^[A-Z][A-Z0-9_]{1,63}$/.test(code) && code !== body.category) tokens.push(code);
  return tokens.length ? ` (${tokens.join(', ')})` : '';
}
