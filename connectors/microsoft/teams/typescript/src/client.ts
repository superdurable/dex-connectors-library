// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  ReauthorizationRequiredError, resolveCredential, resolveCredentialAfterRejection,
  type Call, type CredentialProvider, type CredentialRefreshDriver, type Failure, type FailureKind,
  type ReceiptDraft,
} from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import {
  BodyTooLargeError, MaxErrorBodyBytes, ProviderTransportError, isHeaderSafeCredential,
  parseRetryAfter, readErrorTokens, sendProviderRequest, validateBaseURL,
  type ProviderHTTPClient, type ProviderResponse,
} from '@superdurable/connectors-sdk/providerhttp';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials } from './generated/models.js';
import { queryEncode, safeToken } from './message.js';
import { ListThreadRepliesOperation, PostChannelMessageOperation, PostChatMessageOperation, PostThreadReplyOperation } from './operations.js';

const encoder = new TextEncoder();
const requestTimeoutMs = 20_000;

/** One bounded Graph exchange, including dispatch and response-body evidence. */
export interface Exchange {
  /** HTTP status, or zero when no status was received. */ status: number;
  /** Safe provider headers. */ headers: Record<string, string | string[] | undefined>;
  /** Complete bounded body, absent after a read failure or secret reflection. */ body: Uint8Array;
  /** True when the body exceeded its configured limit. */ bodyTooLarge?: boolean;
  /** True when a response body failed after status was known. */ bodyReadFailed?: boolean;
  /** True when the response body reflected the bearer token. */ reflectsSecret?: boolean;
  /** Sanitized transport failure. */ error?: Error;
  /** False only with positive evidence that Graph received no request. */ dispatched: boolean;
}
/** Transport, credential, and clock dependencies for Graph operations. */
export interface TeamsClientOptions {
  /** Caller-owned HTTP transport; the default uses the Connector SDK provider transport. */ transport?: ProviderHTTPClient;
  /** Observation clock for receipts and Retry-After. */ now?: () => Date;
  /** Caller-owned OAuth token fetch, used only by the generated manifest driver. */ tokenFetch?: typeof fetch;
}

/** Authenticated Microsoft Teams Graph client for one Dex connection. */
export class TeamsClient {
  /** Validated Graph base endpoint. */ readonly endpoint: string;
  /** Positive successful response byte limit. */ readonly maxResponseBytes: number;
  /** Positive combined content and subject byte limit. */ readonly maxMessageBytes: number;
  /** Call-time credential provider. */ readonly credentials: CredentialProvider<Credentials>;
  /** Generated manifest OAuth refresh driver. */ readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  /** Provider transport. */ readonly transport: ProviderHTTPClient;
  /** Observation clock. */ readonly now: () => Date;
  readonly #operationDeadlines = new WeakMap<Call, number>();

  /** Validate settings and bind renewable credentials. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: TeamsClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Microsoft Teams configuration and credentials are required');
    this.endpoint = validateBaseURL(config.endpoint || 'https://graph.microsoft.com/v1.0');
    this.maxResponseBytes = config.maxResponseBytes === undefined || config.maxResponseBytes === 0
      ? 4_194_304 : config.maxResponseBytes;
    this.maxMessageBytes = config.maxMessageBytes === undefined || config.maxMessageBytes === 0
      ? 81_920 : config.maxMessageBytes;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1 || !Number.isSafeInteger(this.maxMessageBytes) || this.maxMessageBytes < 1) throw new RangeError('Microsoft Teams response and message limits must be positive');
    this.credentials = credentials;
    this.transport = options.transport ?? { send: sendProviderRequest };
    this.now = options.now ?? (() => new Date());
    this.refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'Microsoft', auth: Authorization, credentialFields: CredentialFields,
      now: this.now, ...(options.tokenFetch ? { fetch: options.tokenFetch } : {}),
    });
  }

  /** Return the listThreadReplies Query. */
  listThreadReplies(): ListThreadRepliesOperation { return new ListThreadRepliesOperation(this); }
  /** Return the postChannelMessage Mutation. */
  postChannelMessage(): PostChannelMessageOperation { return new PostChannelMessageOperation(this); }
  /** Return the postThreadReply Mutation. */
  postThreadReply(): PostThreadReplyOperation { return new PostThreadReplyOperation(this); }
  /** Return the postChatMessage Mutation. */
  postChatMessage(): PostChatMessageOperation { return new PostChatMessageOperation(this); }

  /** Resolve a renewable credential and check its bearer header form. */
  async resolveCredentials(call: Call): Promise<Credentials> {
    const deadline = Date.now() + 25_000;
    this.#operationDeadlines.set(call, deadline);
    const signal = AbortSignal.any([call.context.cancellationSignal, AbortSignal.timeout(25_000)]);
    const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, signal);
    if (!isHeaderSafeCredential(credentials.accessToken.reveal())) throw new ReauthorizationRequiredError();
    return credentials;
  }

  /** Send one Graph request and refresh one rejected read credential when available. */
  async exchange(
    call: Call, credentials: Credentials, method: string, path: string,
    options: { payload?: Uint8Array; absoluteURL?: string; query?: Record<string, string>; retryRejected?: boolean; signal?: AbortSignal } = {},
  ): Promise<{ exchange: Exchange; credentials: Credentials }> {
    const target = options.absoluteURL || this.endpoint + path + (options.query ? `?${queryEncode(options.query)}` : '');
    const deadline = this.#operationDeadlines.get(call) ?? Date.now() + 25_000;
    for (let attempt = 0; attempt < 2; attempt++) {
      const remaining = Math.max(1, Math.min(requestTimeoutMs, deadline - Date.now()));
      const signal = AbortSignal.any([options.signal ?? call.context.cancellationSignal, AbortSignal.timeout(remaining)]);
      const bearer = credentials.accessToken.reveal();
      const headers: Record<string, string> = {
        Authorization: `Bearer ${bearer}`, Accept: 'application/json', 'client-request-id': call.id,
      };
      if (options.payload !== undefined) headers['Content-Type'] = 'application/json';
      let observedStatus = 0;
      let observedHeaders: Record<string, string | string[] | undefined> = {};
      try {
        const response: ProviderResponse = await this.transport.send({
          url: target, method, headers, maxResponseBytes: this.maxResponseBytes,
          maxErrorResponseBytes: MaxErrorBodyBytes,
          addressPolicy: 'unrestricted', timeoutMs: remaining,
          ...(options.payload !== undefined ? { body: options.payload } : {}),
          signal,
          onResponseChunk: (_chunk, status, responseHeaders) => {
            observedStatus = status;
            observedHeaders = responseHeaders as Record<string, string | string[] | undefined>;
          },
        });
        if (response.status === 401 && options.retryRejected !== false && attempt === 0 && 'resolveAfterRejection' in this.credentials) {
          try {
            const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, signal);
            if (isHeaderSafeCredential(replacement.accessToken.reveal())) { credentials = replacement; continue; }
          } catch { /* The original 401 remains a conclusive provider refusal. */ }
        }
        const secret = encoder.encode(bearer);
        const reflectsSecret = secret.length > 0 && Buffer.from(response.body).includes(Buffer.from(secret));
        return { exchange: {
          status: response.status, headers: response.headers, body: reflectsSecret ? new Uint8Array() : response.body,
          reflectsSecret, dispatched: true,
        }, credentials };
      } catch (error) {
        if (error instanceof BodyTooLargeError) return { exchange: {
          status: error.status ?? observedStatus, headers: error.headers ?? observedHeaders, body: new Uint8Array(),
          bodyTooLarge: true, dispatched: true,
        }, credentials };
        const isReadFailure = observedStatus >= 200 && observedStatus < 300;
        const isNotSent = error instanceof ProviderTransportError && error.isProvablyNotSent;
        return { exchange: {
          status: observedStatus, headers: observedHeaders, body: new Uint8Array(),
          ...(isReadFailure ? { bodyReadFailed: true } : {}),
          ...(isReadFailure ? {} : { error: new ProviderTransportError(isNotSent) }),
          dispatched: isReadFailure || !isNotSent,
        }, credentials };
      }
    }
    throw new TypeError('Microsoft Teams rejected credential retry was exhausted');
  }

  /** Build the Go-compatible provider receipt. */
  receipt(call: Call, exchange: Exchange, objectID = ''): ReceiptDraft {
    const requestID = safeToken(headerValue(exchange.headers, 'request-id'));
    return {
      callId: call.id, idempotencyKey: call.idempotencyKey ?? '', provider: 'microsoft-teams',
      observedAt: this.now().toISOString().replace(/\.000Z$/, 'Z'),
      ...(objectID ? { providerObjectId: objectID } : {}),
      ...(requestID ? { providerRequestId: requestID } : {}),
    };
  }
  /** Build a safe Graph failure. */
  failure(operation: string, kind: FailureKind, message: string): Failure {
    return { kind, provider: 'microsoft-teams', operation, message };
  }
  /** Parse Retry-After through the Connector SDK policy. */
  retryAfter(exchange: Exchange): number { return parseRetryAfter(headerValue(exchange.headers, 'retry-after'), this.now()); }
  /** Read Graph's machine-readable error code only. */
  errorCodeSuffix(exchange: Exchange): string {
    const tokens = readErrorTokens(exchange.body, ['/error/code']);
    return tokens.length ? ` ${tokens[0]}` : '';
  }
}
/** Read an HTTP header independent of the transport's casing. */
export function headerValue(headers: Record<string, string | string[] | undefined>, name: string): string {
  const selected = Object.entries(headers).find(([key]) => key.toLowerCase() === name.toLowerCase())?.[1];
  return typeof selected === 'string' ? selected : Array.isArray(selected) ? selected[0] ?? '' : '';
}
