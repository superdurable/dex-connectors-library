// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { CredentialBrokerUnavailableError } from '@superdurable/connectors-sdk/projectconfig';
import type { Config, Credentials } from './generated/models.js';
import { CreatePageOperation, GetPageOperation, QueryDatabaseOperation, SearchOperation, UpdatePagePropertiesOperation } from './operations.js';

const defaultEndpoint = 'https://api.notion.com';
const notionVersion = '2026-03-11';

/** Injectable bounded provider sender. */
export type NotionRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** One provider exchange, including positive evidence about request dispatch. */
export type NotionExchange = { response: ProviderResponse; receipt: ReceiptDraft } | { failure: Failure; disposition: 'retry' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect' | 'uncertain' | 'committed'; retryAfterMs?: number; receipt: ReceiptDraft; providerId?: string; isProvablyNotSent?: boolean };
/** A classified exchange before any successful response. */
export type NotionExchangeFailure = Extract<NotionExchange, { failure: Failure }>;

/** Replace the network transport in deterministic provider tests. */
export interface NotionClientOptions {
  /** Bounded provider request sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock for receipt timestamps and Retry-After. */ now?: () => Date;
}

/** Notion API client with call-time credential resolution. */
export class NotionClient {
  readonly endpoint: string;
  readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate the endpoint and required credential source without contacting Notion. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: NotionClientOptions = {}) {
    if (!credentials) throw new TypeError('Notion credential source is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.maxResponseBytes = config.maxResponseBytes || 4_194_304;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Notion maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind title search. */ search(): SearchOperation { return new SearchOperation(this); }
  /** Bind database row query. */ queryDatabase(): QueryDatabaseOperation { return new QueryDatabaseOperation(this); }
  /** Bind page read. */ getPage(): GetPageOperation { return new GetPageOperation(this); }
  /** Bind single-dispatch page creation. */ createPage(): CreatePageOperation { return new CreatePageOperation(this); }
  /** Bind absolute page property update. */ updatePageProperties(): UpdatePagePropertiesOperation { return new UpdatePagePropertiesOperation(this); }

  /** Build a secret-safe receipt. */
  receipt(call: Call, response?: ProviderResponse, providerObjectId = ''): ReceiptDraft {
    const requestId = response?.headers['x-notion-request-id'];
    const providerRequestId = Array.isArray(requestId) ? requestId[0] : requestId;
    return { callId: call.id, provider: 'notion', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(providerObjectId ? { providerObjectId } : {}),
      ...(providerRequestId && /^[!-~]{1,128}$/.test(providerRequestId) ? { providerRequestId } : {}),
    };
  }

  /** Resolve a credential before a single-dispatch write is claimed. */
  async resolveToken(call: Call, operation: string, signal?: AbortSignal): Promise<{ token: string } | { outcome: NotionExchangeFailure }> {
    const emptyReceipt = this.receipt(call);
    try {
      const credentials = await this.credentials.resolve(call, signal ?? call.context.cancellationSignal);
      const token = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
      if (!isHeaderSafeCredential(token)) throw new TypeError('invalid Notion token');
      return { token };
    } catch (error) {
      if (error instanceof ReauthorizationRequiredError) return { outcome: { disposition: 'defect', failure: failure(operation, 'AUTHENTICATION', 'Notion connection must be authorized again'), receipt: emptyReceipt, isProvablyNotSent: true } };
      if (error instanceof CredentialBrokerUnavailableError || error instanceof DOMException && error.name === 'AbortError') return { outcome: { disposition: 'retry', failure: failure(operation, 'AUTHENTICATION', 'Notion credential broker is temporarily unavailable'), receipt: emptyReceipt, isProvablyNotSent: true } };
      return { outcome: { disposition: 'defect', failure: failure(operation, 'AUTHENTICATION', 'Notion connection credentials are unavailable'), receipt: emptyReceipt, isProvablyNotSent: true } };
    }
  }

  /** Send one bounded API request and classify its transport and HTTP outcome. */
  async exchange(call: Call, operation: string, subject: string, method: string, path: string, body?: unknown, mode: 'read' | 'repeatableWrite' | 'unkeyedWrite' = 'read', signal?: AbortSignal, resolvedToken?: string, providerObjectId = ''): Promise<NotionExchange> {
    const emptyReceipt = this.receipt(call, undefined, providerObjectId);
    const credentials = resolvedToken ? { token: resolvedToken } : await this.resolveToken(call, operation, signal);
    if ('outcome' in credentials) return { ...credentials.outcome, receipt: emptyReceipt };
    const token = credentials.token;
    let encoded: Uint8Array | undefined;
    try { if (body !== undefined) encoded = Buffer.from(JSON.stringify(body)); }
    catch { return { disposition: 'defect', failure: failure(operation, 'LOCAL_DEFECT', 'Notion request could not be built'), receipt: emptyReceipt }; }
    const request: ProviderRequest = {
      url: `${this.endpoint}/v1${path}`, method,
      headers: { Authorization: `Bearer ${token}`, 'Notion-Version': notionVersion, Accept: 'application/json', ...(encoded ? { 'Content-Type': 'application/json' } : {}) },
      ...(encoded ? { body: encoded } : {}),
      maxResponseBytes: Math.max(this.maxResponseBytes, 16_384), timeoutMs: mode === 'read' ? 20_000 : 60_000,
      addressPolicy: this.endpoint === defaultEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP', signal: signal ?? call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array(this.maxResponseBytes + 1) };
      } else {
        const isProvablyNotSent = error instanceof ProviderTransportError && error.isProvablyNotSent;
        const disposition = mode === 'unkeyedWrite' && !isProvablyNotSent ? 'uncertain' : 'retry';
        return { disposition, failure: failure(operation, 'TRANSPORT', `Notion ${subject} is temporarily unavailable`), receipt: emptyReceipt, isProvablyNotSent };
      }
    }
    const receipt = this.receipt(call, response, providerObjectId);
    const status = response.status;
    const isBlocked = status === 429 && readToken(response.body, 'additional_data', 'rate_limit_reason') === 'public_api_request_blocked';
    if (isBlocked) return { disposition: 'providerRejected', failure: failure(operation, 'PROVIDER_REJECTION', 'Notion blocked API access'), receipt };
    if (status === 429 || status === 529 || status === 409 || (mode !== 'unkeyedWrite' && (status === 408 || status >= 500))) {
      const retryAfter = response.headers['retry-after'];
      const retryAfterMs = parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '', this.now());
      return { disposition: 'retry', failure: failure(operation, status === 429 || status === 529 ? 'RATE_LIMIT' : status === 409 ? 'CONFLICT' : 'AVAILABILITY', `Notion ${subject} is temporarily unavailable`), retryAfterMs, receipt, isProvablyNotSent: mode === 'unkeyedWrite' };
    }
    if (status === 503 && mode === 'unkeyedWrite') {
      let providerId = '';
      const candidate = readToken(response.body, 'additional_data', 'committed_resource_id');
      if (/^[0-9a-f]{32}$|^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(candidate)) {
        const compact = candidate.replaceAll('-', '').toLowerCase();
        providerId = `${compact.slice(0, 8)}-${compact.slice(8, 12)}-${compact.slice(12, 16)}-${compact.slice(16, 20)}-${compact.slice(20)}`;
      }
      if (providerId) return { disposition: 'committed', failure: failure(operation, 'AVAILABILITY', 'Notion saved the page'), providerId, receipt };
    }
    if (status === 404) return { disposition: 'notFound', failure: failure(operation, 'NOT_FOUND', `Notion ${subject} was not found`), receipt };
    if (status >= 400 && status < 500 && status !== 408) return { disposition: 'providerRejected', failure: failure(operation, status === 400 ? 'VALIDATION' : status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION', `Notion rejected ${subject} with HTTP ${status}`), receipt };
    if (status < 200 || status >= 300) return { disposition: mode === 'unkeyedWrite' ? 'uncertain' : 'invalidResponse', failure: failure(operation, status >= 500 ? 'AVAILABILITY' : 'PROTOCOL', `Notion returned HTTP ${status}`), receipt };
    if (response.body.length > this.maxResponseBytes) return { disposition: mode === 'unkeyedWrite' ? 'uncertain' : 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'Notion response exceeds the configured limit'), receipt };
    if (Buffer.from(response.body).includes(Buffer.from(token))) return { disposition: mode === 'unkeyedWrite' ? 'uncertain' : 'invalidResponse', failure: failure(operation, 'PROTOCOL', 'Notion response contains credential material'), receipt };
    return { response, receipt };
  }
}

/** Build a failure without provider message text or credentials. */
export function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'notion', operation, kind, message }; }

function readToken(body: Uint8Array, parent: string, key: string): string {
  try { const data: unknown = JSON.parse(Buffer.from(body).toString('utf8')); if (typeof data !== 'object' || data === null) return ''; const value = (data as Record<string, unknown>)[parent]; if (typeof value === 'object' && value !== null) { const token = (value as Record<string, unknown>)[key]; return typeof token === 'string' && /^[!-~]{1,128}$/.test(token) ? token : ''; } return ''; } catch { return ''; }
}
