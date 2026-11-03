// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, SecretString, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { AddressRefusal, BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials } from './generated/models.js';
import { CreateConversationOperation, FindContactByEmailOperation, GetConversationOperation, ReplyToConversationOperation, SearchConversationsOperation, UpdateConversationOperation } from './operations.js';

/** Sender used for local provider stand-ins. */
export type ReamazeRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport and clock dependencies. */
export interface ReamazeClientOptions {
  /** Base URL for a local Re:amaze-compatible stand-in. */ apiBaseURL?: string;
  /** Provider HTTP sender. */ send?: ReamazeRequestSender;
  /** Receipt and retry clock. */ now?: () => Date;
}
/** Result of a credential lookup before any provider request. */
export type CredentialResult = { kind: 'resolved'; email: string; token: string } | { kind: 'terminal' | 'transient' };
/** Classified provider exchange. */
export type ReamazeExchange =
  | { kind: 'success'; response: ProviderResponse; requestId: string }
  | { kind: 'notFound' | 'rateLimited' | 'notSent' | 'unavailable' | 'rejected' | 'invalid' | 'defect'; failure: Failure; retryAfterMs: number; status: number; requestId: string };

/** Authenticated Re:amaze API v1 client. */
export class ReamazeClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: ReamazeRequestSender;
  private readonly now: () => Date;

  /** Validate configuration without contacting Re:amaze. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: ReamazeClientOptions = {}) {
    if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(config.brand)) throw new TypeError('invalid Re:amaze brand');
    if (!credentials) throw new TypeError('Re:amaze credential provider is required');
    this.endpoint = validateBaseURL(options.apiBaseURL ?? `https://${config.brand}.reamaze.io/api/v1`);
    this.maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('invalid Re:amaze maxResponseBytes');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind a search Query. */ searchConversations() { return new SearchConversationsOperation(this); }
  /** Bind a conversation read Query. */ getConversation() { return new GetConversationOperation(this); }
  /** Bind a create Mutation. */ createConversation() { return new CreateConversationOperation(this); }
  /** Bind an update Mutation. */ updateConversation() { return new UpdateConversationOperation(this); }
  /** Bind a reply Mutation. */ replyToConversation() { return new ReplyToConversationOperation(this); }
  /** Bind a contact search Query. */ findContactByEmail() { return new FindContactByEmailOperation(this); }

  /** Make a secret-free receipt after a provider attempt. */
  receipt(call: Call, objectId = '', requestId = ''): ReceiptDraft {
    return { callId: call.id, provider: 'reamaze', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}),
      ...(requestId ? { providerRequestId: requestId } : {}) };
  }

  /** Resolve fresh credentials for every provider request. */
  async resolveCredentials(call: Call): Promise<CredentialResult> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiToken instanceof SecretString) || !isBareEmail(credentials.email)) return { kind: 'terminal' };
      const token = credentials.apiToken.reveal();
      return isHeaderSafeCredential(token) ? { kind: 'resolved', email: credentials.email, token } : { kind: 'terminal' };
    } catch (error) {
      return { kind: error instanceof ReauthorizationRequiredError || error instanceof ObjectNotFoundError || error instanceof ConnectionNotSelectedError || error instanceof CredentialBrokerRejectedError ? 'terminal' : 'transient' };
    }
  }

  /** Send one request and classify its response by status without exposing response text. */
  async exchange(call: Call, operation: string, credentials: Extract<CredentialResult, { kind: 'resolved' }>, method: string, path: string, query?: URLSearchParams, payload?: unknown, signal?: AbortSignal): Promise<ReamazeExchange> {
    let body: Uint8Array | undefined;
    try { if (payload !== undefined) body = Buffer.from(JSON.stringify(payload)); }
    catch { return this.failure('defect', operation, 'LOCAL_DEFECT', 'Re:amaze request could not be built'); }
    const url = this.endpoint + path + (query && query.size ? `?${query.toString().replace(/\*/g, '%2A').replace(/%7E/g, '~')}` : '');
    const headers: Record<string, string> = { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${credentials.email}:${credentials.token}`).toString('base64')}` };
    if (body) headers['Content-Type'] = 'application/json';
    let response: ProviderResponse;
    try {
      response = await this.send({ url, method, headers, ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes,
        addressPolicy: new URL(this.endpoint).protocol === 'https:' && !['localhost', '127.0.0.1', '[::1]'].includes(new URL(this.endpoint).hostname) ? 'publicHTTPS' : 'privateNetworkHTTP',
        timeoutMs: 12_000, signal: signal ?? call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.headers) {
          if (error.status >= 200 && error.status < 300) return this.failure('invalid', operation, 'RESPONSE_TOO_LARGE', 'Re:amaze response exceeds the configured maxResponseBytes limit', 0, error.status, safeRequestId(error.headers));
          response = { status: error.status, headers: error.headers, body: Buffer.alloc(0) };
        } else return this.failure('unavailable', operation, 'TRANSPORT', 'Re:amaze response could not be read');
      } else if (error instanceof AddressRefusal || error instanceof ProviderTransportError && error.isProvablyNotSent) {
        return this.failure('notSent', operation, 'TRANSPORT', 'Re:amaze could not be reached; no request was sent');
      } else return this.failure('unavailable', operation, 'TRANSPORT', 'Re:amaze request failed before a response arrived');
    }
    const requestId = safeRequestId(response.headers);
    const status = response.status;
    if (status >= 200 && status < 300) {
      if (Buffer.from(response.body).includes(Buffer.from(credentials.token))) return this.failure('invalid', operation, 'PROTOCOL', 'Re:amaze response reflected the connection credential', 0, status, requestId);
      return { kind: 'success', response, requestId };
    }
    let retryAfterMs = parseRetryAfter(header(response, 'retry-after'), this.now());
    if (status === 429) { if (!retryAfterMs) retryAfterMs = 60_000; return this.failure('rateLimited', operation, 'RATE_LIMIT', 'Re:amaze rate limited the request (HTTP 429)', retryAfterMs, status, requestId); }
    if (status === 408 || status >= 500) return this.failure('unavailable', operation, 'AVAILABILITY', `Re:amaze could not complete the request (HTTP ${status})`, retryAfterMs, status, requestId);
    if (status === 404) return this.failure('notFound', operation, 'NOT_FOUND', 'Re:amaze found no such resource in the brand (HTTP 404)', 0, status, requestId);
    if (status >= 300 && status < 400) return this.failure('rejected', operation, 'PROTOCOL', `Re:amaze redirected the request (HTTP ${status}); check that brand is the brand's reamaze.io subdomain`, 0, status, requestId);
    const failureKind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 409 ? 'CONFLICT' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION';
    return this.failure('rejected', operation, failureKind, `Re:amaze rejected the request (HTTP ${status})`, 0, status, requestId);
  }

  private failure(kind: Exclude<ReamazeExchange['kind'], 'success'>, operation: string, failureKind: Failure['kind'], message: string, retryAfterMs = 0, status = 0, requestId = ''): ReamazeExchange {
    return { kind, failure: { kind: failureKind, provider: 'reamaze', operation, message }, retryAfterMs, status, requestId };
  }
}

function header(response: ProviderResponse, name: string): string {
  const value = response.headers[name];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
function safeRequestId(headers: ProviderResponse['headers']): string {
  const value = headers['x-request-id'];
  const requestId = Array.isArray(value) ? value[0] ?? '' : value ?? '';
  return /^[A-Za-z0-9._:-]{1,128}$/.test(requestId) ? requestId : '';
}
/** Validate one bare address for the Basic user name and operation filters. */
export function isBareEmail(value: string): boolean {
  if (!value || /[ "()<>,;:\\]/.test(value) || value.includes('[') || value.includes(']')) return false;
  if (value.includes('..') || [...value].some(character => character.charCodeAt(0) <= 32 || character.charCodeAt(0) === 127)) return false;
  const parts = value.split('@');
  if (parts.length !== 2) return false;
  const [localPart, domain] = parts;
  return !!localPart && !!domain && !localPart.startsWith('.') && !localPart.endsWith('.') && !domain.startsWith('.') && !domain.endsWith('.');
}
