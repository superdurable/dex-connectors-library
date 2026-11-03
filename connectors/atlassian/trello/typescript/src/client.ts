// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { AddressRefusal, BodyTooLargeError, MaxErrorBodyBytes, ProviderTransportError, parseRetryAfter, readErrorTokens, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { AddCommentOperation, CreateCardOperation, GetCardOperation, ListCardsOperation, UpdateCardOperation } from './operations.js';

const defaultEndpoint = 'https://api.trello.com/1';
const defaultMaxResponseBytes = 4_194_304;
const requestTimeoutMs = 20_000;
const requestIdPattern = /^[A-Za-z0-9-]{1,64}$/;
const quotedCredentialPattern = /^[!-~]+$/;

/** Injectable Trello transport for provider tests. */
export type TrelloRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Client dependencies supplied at construction. */
export interface TrelloClientOptions {
  /** Transport; omitted in production. */ send?: TrelloRequestSender;
  /** Receipt clock. */ now?: () => Date;
}

/** A bounded Trello response or a classified transport failure. */
export type TrelloExchange =
  | { response: ProviderResponse; receipt: ReceiptDraft }
  | { error: Error; receipt: ReceiptDraft; status?: number; headers?: ProviderResponse['headers']; isProvablyNotSent: boolean };

/** Trello echoed private credential material in a response body. */
export class TrelloCredentialReflectionError extends Error {
  /** Prevent secret-bearing response content from entering the error. */
  constructor() { super('Trello response contains credential material'); }
}

/** Authenticated Trello REST client for one Dex connection. */
export class TrelloClient {
  /** Validated Trello API base URL. */
  readonly endpoint: string;
  /** Maximum successful response size in bytes. */
  readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: TrelloRequestSender;
  private readonly now: () => Date;

  /** Validate configuration without contacting Trello. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: TrelloClientOptions = {}) {
    if (!credentials) throw new TypeError('Trello credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Trello maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind a card page query. */ listCards(): ListCardsOperation { return new ListCardsOperation(this); }
  /** Bind a card detail query. */ getCard(): GetCardOperation { return new GetCardOperation(this); }
  /** Bind a card create mutation. */ createCard(): CreateCardOperation { return new CreateCardOperation(this); }
  /** Bind an absolute card update mutation. */ updateCard(): UpdateCardOperation { return new UpdateCardOperation(this); }
  /** Bind a comment create mutation. */ addComment(): AddCommentOperation { return new AddCommentOperation(this); }

  /** Record safe provider correlation without credentials or response content. */
  receipt(call: Call, objectId = '', headers?: ProviderResponse['headers']): ReceiptDraft {
    const rawRequestId = headers?.['atl-request-id'];
    const requestId = Array.isArray(rawRequestId) ? rawRequestId[0] : rawRequestId;
    return {
      callId: call.id, provider: 'trello', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}),
      ...(requestId && requestIdPattern.test(requestId) ? { providerRequestId: requestId } : {}),
    };
  }

  /** Resolve private credentials and perform one bounded provider exchange. */
  async exchange(call: Call, operation: string, method: string, path: string, query?: URLSearchParams, payload?: unknown, signal?: AbortSignal): Promise<TrelloExchange> {
    void operation;
    let apiKey: string;
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, signal ?? call.context.cancellationSignal);
      if (!(credentials.apiKey instanceof SecretString) || !(credentials.token instanceof SecretString)) throw new TypeError('invalid Trello credentials');
      apiKey = credentials.apiKey.reveal(); token = credentials.token.reveal();
      if (![apiKey, token].every(value => quotedCredentialPattern.test(value) && !/["\\,]/.test(value))) throw new TypeError('invalid Trello credentials');
    } catch {
      return { error: new TypeError('Trello connection credentials are unavailable'), receipt: {}, isProvablyNotSent: true };
    }
    let body: Uint8Array | undefined;
    try { if (payload !== undefined) body = Buffer.from(JSON.stringify(payload), 'utf8'); }
    catch { return { error: new TypeError('Trello request could not be built'), receipt: {}, isProvablyNotSent: true }; }
    const target = this.endpoint + path + (query && query.size ? `?${query.toString()}` : '');
    const request: ProviderRequest = {
      url: target, method,
      headers: { Accept: 'application/json', Authorization: `OAuth oauth_consumer_key="${apiKey}", oauth_token="${token}"`, ...(body ? { 'Content-Type': 'application/json' } : {}) },
      ...(body ? { body } : {}), maxResponseBytes: Math.max(this.maxResponseBytes, MaxErrorBodyBytes),
      addressPolicy: this.endpoint === defaultEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP',
      timeoutMs: requestTimeoutMs, signal: signal ?? call.context.cancellationSignal,
    };
    try {
      const response = await this.send(request);
      const receipt = this.receipt(call, '', response.headers);
      if (response.status >= 200 && response.status < 300 && response.body.length > this.maxResponseBytes) {
        return { error: new BodyTooLargeError(response.status, response.headers), receipt, status: response.status, headers: response.headers, isProvablyNotSent: false };
      }
      if (Buffer.from(response.body).includes(Buffer.from(apiKey)) || Buffer.from(response.body).includes(Buffer.from(token))) {
        return { error: new TrelloCredentialReflectionError(), receipt, status: response.status, headers: response.headers, isProvablyNotSent: false };
      }
      return { response, receipt };
    } catch (error) {
      const transportError = error instanceof Error ? error : new Error('Trello transport failed');
      const status = error instanceof BodyTooLargeError ? error.status : undefined;
      const headers = error instanceof BodyTooLargeError ? error.headers : undefined;
      return {
        error: transportError, receipt: this.receipt(call, '', headers),
        ...(status !== undefined ? { status } : {}), ...(headers ? { headers } : {}),
        isProvablyNotSent: (error instanceof ProviderTransportError && error.isProvablyNotSent) || error instanceof AddressRefusal,
      };
    }
  }
}

/** Return a secret-safe connector failure. */
export function trelloFailure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { provider: 'trello', operation, kind, message };
}

/** Classify Trello's status without reading unsafe error text. */
export function trelloStatusKind(status: number): Failure['kind'] {
  if (status === 400) return 'VALIDATION';
  if (status === 401) return 'AUTHENTICATION';
  if (status === 403) return 'AUTHORIZATION';
  if (status === 404) return 'NOT_FOUND';
  if (status === 409) return 'CONFLICT';
  if (status === 429) return 'RATE_LIMIT';
  return 'PROVIDER_REJECTION';
}

/** Honor Retry-After, using one Trello window for bare 429. */
export function trelloRetryDelay(status: number, headers?: ProviderResponse['headers']): number {
  const value = headers?.['retry-after'];
  const first = Array.isArray(value) ? value[0] : value;
  const delay = parseRetryAfter(first ?? '');
  return status === 429 && delay === 0 ? 10_000 : delay;
}

/** Name a validated machine error code without exposing Trello's free-form text. */
export function trelloRateLimitMessage(subject: string, body?: Uint8Array): string {
  const token = body && body.length <= MaxErrorBodyBytes ? readErrorTokens(body, ['/error'])[0] : undefined;
  return `Trello rate limited the ${subject}${token ? ` [${token}]` : ''}`;
}
