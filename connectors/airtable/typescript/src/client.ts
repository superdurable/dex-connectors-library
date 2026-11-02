// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { GetRecordOperation, ListRecordsOperation, UpdateRecordsOperation, UpsertRecordsOperation } from './operations.js';

const defaultEndpoint = 'https://api.airtable.com';
const defaultMaxResponseBytes = 4_194_304;
const requestTimeoutMs = 25_000;
const rateLimitCooldownMs = 30_000;

/** HTTP sender used by Airtable requests, injectable for deterministic provider tests. */
export type AirtableRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Dependencies for deterministic provider tests. */
export interface AirtableClientOptions {
  /** Sender; omit to use the SDK's pinned DNS, bounded, no redirect transport. */
  send?: AirtableRequestSender;
  /** Clock used for receipts, pacing, and Retry-After dates. */
  now?: () => Date;
}

/** One unsuccessful provider exchange. */
export interface ExchangeFailure {
  /** Retry or terminal classification. */
  disposition: 'retry' | 'notFound' | 'offsetExpired' | 'rejected' | 'invalidResponse' | 'defect';
  /** Secret-safe connector failure. */
  failure: Failure;
  /** Provider-directed retry delay in milliseconds. */
  retryAfterMs: number;
  /** Correlation receipt after a provider attempt. */
  receipt: ReceiptDraft;
}

/** Successful bounded response or classified failure. */
export type ExchangeResult = { response: ProviderResponse } | { outcome: ExchangeFailure };

/** Validated Airtable API client with per-base pacing and live credential resolution. */
export class AirtableClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: AirtableRequestSender;
  private readonly now: () => Date;
  private readonly nextRequestAt = new Map<string, number>();

  /** Construct one client without contacting Airtable. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: AirtableClientOptions = {}) {
    if (!credentials) throw new TypeError('Airtable credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Airtable maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the list query. */
  listRecords(): ListRecordsOperation { return new ListRecordsOperation(this); }
  /** Bind the record query. */
  getRecord(): GetRecordOperation { return new GetRecordOperation(this); }
  /** Bind the upsert mutation. */
  upsertRecords(): UpsertRecordsOperation { return new UpsertRecordsOperation(this); }
  /** Bind the update mutation. */
  updateRecords(): UpdateRecordsOperation { return new UpdateRecordsOperation(this); }

  /** Produce one safe receipt after an exchange. */
  receipt(call: Call, objectId: string): ReceiptDraft {
    return {
      callId: call.id, provider: 'airtable', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}),
    };
  }

  /** Resolve credentials, pace one base, send a bounded request, and classify non-2xx responses. */
  async exchange(call: Call, operation: string, baseId: string, objectId: string, method: string, path: string, body?: unknown): Promise<ExchangeResult> {
    const slot = this.reserveRequestSlot(baseId);
    if (slot > 2_000) return { outcome: this.failure('retry', operation, 'RATE_LIMIT', 'Airtable requests to this base are paced to its rate limit', {}, slot) };
    if (slot > 0) {
      try { await waitForSlot(slot, call.context.cancellationSignal); }
      catch { return { outcome: this.failure('retry', operation, 'AVAILABILITY', 'the Step ended while waiting for an Airtable request slot') }; }
    }
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.personalAccessToken instanceof SecretString)) throw new TypeError('invalid Airtable token');
      token = credentials.personalAccessToken.reveal();
      if (!isHeaderSafeCredential(token)) throw new TypeError('invalid Airtable token');
    } catch (error) {
      if (error instanceof ReauthorizationRequiredError) return { outcome: this.failure('rejected', operation, 'AUTHENTICATION', 'the Airtable connection must be authorized again') };
      return { outcome: this.failure('defect', operation, 'AUTHENTICATION', 'Airtable connection credentials are unavailable') };
    }
    let encoded: Uint8Array | undefined;
    try { if (body !== undefined) encoded = Buffer.from(JSON.stringify(body), 'utf8'); }
    catch { return { outcome: this.failure('defect', operation, 'LOCAL_DEFECT', `${operation} request could not be encoded`) }; }
    const receipt = this.receipt(call, objectId);
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: this.endpoint + path, method,
        headers: { Accept: 'application/json', Authorization: `Bearer ${token}`, ...(encoded ? { 'Content-Type': 'application/json' } : {}) },
        ...(encoded ? { body: encoded } : {}), maxResponseBytes: this.maxResponseBytes,
        addressPolicy: this.endpoint === defaultEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP',
        timeoutMs: requestTimeoutMs, signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError) return { outcome: this.failure('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'Airtable response exceeds the configured size limit', receipt) };
      return { outcome: this.failure('retry', operation, 'TRANSPORT', 'Airtable did not return a complete response', receipt) };
    }
    if (response.status >= 200 && response.status < 300) return { response };
    return { outcome: this.classifyResponse(operation, baseId, response, receipt) };
  }

  private reserveRequestSlot(baseId: string): number {
    const now = this.now().getTime();
    const slot = Math.max(now, this.nextRequestAt.get(baseId) ?? now);
    const delay = slot - now;
    if (delay <= 2_000) this.nextRequestAt.set(baseId, slot + 200);
    return delay;
  }

  private classifyResponse(operation: string, baseId: string, response: ProviderResponse, receipt: ReceiptDraft): ExchangeFailure {
    const status = response.status;
    const retryAfterMs = parseRetryAfter(header(response, 'retry-after'), this.now());
    if (status === 429) {
      this.nextRequestAt.set(baseId, Math.max(this.nextRequestAt.get(baseId) ?? 0, this.now().getTime() + rateLimitCooldownMs));
      return this.failure('retry', operation, 'RATE_LIMIT', 'Airtable rate limited requests to this base', receipt, Math.max(retryAfterMs, rateLimitCooldownMs));
    }
    if (status === 408 || (status >= 500 && status !== 501)) return this.failure('retry', operation, 'AVAILABILITY', 'Airtable is temporarily unavailable', receipt, retryAfterMs);
    const errorType = readErrorType(response.body);
    if (status === 422 && errorType === 'LIST_RECORDS_ITERATOR_NOT_AVAILABLE') return this.failure('offsetExpired', operation, 'NOT_FOUND', 'Airtable no longer recognizes the list offset', receipt);
    if (status === 404) return this.failure('notFound', operation, 'NOT_FOUND', 'Airtable found no such record or route', receipt);
    if (status === 401) return this.failure('rejected', operation, 'AUTHENTICATION', 'Airtable rejected the personal access token', receipt);
    if (status === 403) return this.failure('rejected', operation, 'AUTHORIZATION', 'Airtable denied access', receipt);
    if (status === 400 || status === 413 || status === 422) return this.failure('rejected', operation, 'VALIDATION', 'Airtable rejected the request as invalid', receipt);
    if (status >= 300 && status < 400) return this.failure('rejected', operation, 'PROTOCOL', 'Airtable redirected the request', receipt);
    return this.failure('rejected', operation, 'PROVIDER_REJECTION', 'Airtable rejected the request', receipt);
  }

  private failure(disposition: ExchangeFailure['disposition'], operation: string, kind: Failure['kind'], message: string, receipt: ReceiptDraft = {}, retryAfterMs = 0): ExchangeFailure {
    return { disposition, failure: { kind, provider: 'airtable', operation, message }, receipt, retryAfterMs };
  }
}

function header(response: ProviderResponse, name: string): string {
  const value = response.headers[name];
  return Array.isArray(value) ? (value[0] ?? '') : (value ?? '');
}

function readErrorType(body: Uint8Array): string {
  if (body.length > 65_536) return '';
  try {
    const decoded: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
    if (!decoded || typeof decoded !== 'object' || !('error' in decoded)) return '';
    const value = decoded.error;
    const candidate = typeof value === 'string' ? value : value && typeof value === 'object' && 'type' in value ? value.type : undefined;
    return typeof candidate === 'string' && /^[A-Z][A-Z0-9_]{1,63}$/.test(candidate) ? candidate : '';
  } catch { return ''; }
}

function waitForSlot(delayMs: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, delayMs);
    signal?.addEventListener('abort', () => { clearTimeout(timer); reject(new Error('cancelled')); }, { once: true });
  });
}
