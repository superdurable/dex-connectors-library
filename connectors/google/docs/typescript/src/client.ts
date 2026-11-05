// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { resolveCredential, resolveCredentialAfterRejection, type Call, type CredentialRefreshDriver, type Failure, type FailureKind, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, readErrorTokens, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, Credentials, CredentialSource } from './generated/models.js';
import { GetDocumentTextOperation, CreateDocumentOperation, ReplaceDocumentTextOperation, AppendTextOperation } from './operations.js';

/** Replaceable bounded transport for an in-process provider stand-in. */
export type DocsTransport = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Constructor-injected transport, clock, and manifest refresh driver. */
export interface DocsClientOptions {
  /** Provider transport; defaults to the SDK's policy-checked HTTP sender. */
  transport?: DocsTransport;
  /** Clock used for receipts and Retry-After dates. */
  now?: () => Date;
  /** Optional refresh driver override for tests. */
  refreshDriver?: CredentialRefreshDriver<Credentials>;
}

/** A request failed before a response was available. */
export class DocsRequestError extends Error {
  /** Whether a provider write provably never reached the provider. */
  readonly beforeSend: boolean;
  /** Whether the accepted response exceeded the configured limit. */
  readonly tooLarge: boolean;
  /** HTTP status if a response was received. */
  readonly status: number;
  /** Response headers if a response was received. */
  readonly headers: ProviderResponse['headers'];
  /** Capture only safe transport facts for branch classification. */
  constructor(beforeSend: boolean, tooLarge = false, status = 0, headers: ProviderResponse['headers'] = {}) {
    super('provider request failed'); this.beforeSend = beforeSend; this.tooLarge = tooLarge; this.status = status; this.headers = headers;
  }
}

/** Validated Google Docs and Drive client bound to host-managed credentials. */
export class DocsClient {
  /** Documents API root including /v1/documents. */ readonly documentsBaseURL: string;
  /** Drive files API root. */ readonly driveBaseURL: string;
  /** Drive upload API root. */ readonly driveUploadBaseURL: string;
  /** Maximum accepted successful response size. */ readonly maxResponseBytes: number;
  /** Maximum UTF-8 text size. */ readonly maxTextBytes: number;
  readonly #credentials: CredentialSource;
  readonly #refreshDriver: CredentialRefreshDriver<Credentials>;
  readonly #transport: DocsTransport;
  readonly #now: () => Date;

  /** Apply manifest defaults and bind required host dependencies. */
  constructor(config: Config, credentials: CredentialSource, options: DocsClientOptions = {}) {
    if (!credentials) throw new TypeError('credential provider is required');
    const docsEndpoint = validateBaseURL(config.docsEndpoint || 'https://docs.googleapis.com');
    const driveEndpoint = validateBaseURL(config.driveEndpoint || 'https://www.googleapis.com');
    const maxResponseBytes = config.maxResponseBytes ?? 8_388_608;
    const maxTextBytes = config.maxTextBytes ?? 1_048_576;
    if (!Number.isSafeInteger(maxResponseBytes) || maxResponseBytes < 1) throw new TypeError('Google Docs response limit must be positive');
    if (!Number.isSafeInteger(maxTextBytes) || maxTextBytes < 1 || maxTextBytes > 4_194_304) throw new TypeError('Google Docs text limit must be from 1 to 4194304 bytes');
    this.documentsBaseURL = `${docsEndpoint}/v1/documents`;
    this.driveBaseURL = `${driveEndpoint}/drive/v3`;
    this.driveUploadBaseURL = `${driveEndpoint}/upload/drive/v3`;
    this.maxResponseBytes = maxResponseBytes;
    this.maxTextBytes = maxTextBytes;
    this.#credentials = credentials;
    this.#now = options.now ?? (() => new Date());
    this.#transport = options.transport ?? sendProviderRequest;
    this.#refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({
      providerName: 'google-docs', auth: Authorization, credentialFields: CredentialFields, now: this.#now,
    });
  }

  /** Bind the getDocumentText operation. */ getDocumentText(): GetDocumentTextOperation { return new GetDocumentTextOperation(this); }
  /** Bind the createDocument operation. */ createDocument(): CreateDocumentOperation { return new CreateDocumentOperation(this); }
  /** Bind the replaceDocumentText operation. */ replaceDocumentText(): ReplaceDocumentTextOperation { return new ReplaceDocumentTextOperation(this); }
  /** Bind the appendText operation. */ appendText(): AppendTextOperation { return new AppendTextOperation(this); }

  /** Resolve the current access token, including a host-coordinated refresh. */
  async resolveCredentials(call: Call): Promise<Credentials> {
    try {
      const credentials = await resolveCredential(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal);
      if (!isHeaderSafeCredential(credentials.accessToken?.reveal() ?? '') || !['google-oauth', 'workspace-domain-delegation', ''].includes(credentials.authMethodId ?? '')) throw new TypeError('invalid credential');
      return credentials;
    } catch { throw new TypeError('connection credentials are unavailable'); }
  }

  /** Ask the host to refresh after one rejected access token. */
  async refreshCredentialsAfterRejection(call: Call): Promise<Credentials> {
    const credentials = await resolveCredentialAfterRejection(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal);
    if (!isHeaderSafeCredential(credentials.accessToken?.reveal() ?? '')) throw new TypeError('connection credentials are unavailable');
    return credentials;
  }

  /** Send one request; an idempotent request may refresh once after a 401. */
  async request(call: Call, credentials: Credentials, method: string, url: string, body?: Uint8Array, contentType?: string, singleDispatch = false, signal?: AbortSignal): Promise<ProviderResponse> {
    let current = credentials;
    for (let attempt = 0; attempt < 2; attempt++) {
      const token = current.accessToken?.reveal() ?? '';
      if (!isHeaderSafeCredential(token)) throw new DocsRequestError(true);
      const request: ProviderRequest = {
        url, method, headers: { Authorization: `Bearer ${token}`, ...(contentType ? { 'Content-Type': contentType } : {}) },
        ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes,
        maxErrorResponseBytes: Math.min(Number.MAX_SAFE_INTEGER, Math.max(this.maxResponseBytes, 65_536) + 1),
        addressPolicy: 'unrestricted', timeoutMs: 25_000,
        ...(signal ? { signal } : { signal: call.context.cancellationSignal }),
      };
      let response: ProviderResponse;
      try { response = await this.#transport(request); }
      catch (error) {
        if (error instanceof BodyTooLargeError) {
          if (error.status && (error.status < 200 || error.status >= 300)) response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
          else throw new DocsRequestError(false, true, error.status ?? 0, error.headers ?? {});
        } else {
          const isBeforeSend = !!error && typeof error === 'object' && ('beforeSend' in error && error.beforeSend === true || 'isProvablyNotSent' in error && error.isProvablyNotSent === true);
          throw new DocsRequestError(isBeforeSend);
        }
      }
      if (response.status >= 200 && response.status < 300 && response.body.byteLength > this.maxResponseBytes) {
        throw new DocsRequestError(false, true, response.status, response.headers);
      }
      if ((response.status < 200 || response.status >= 300) && response.body.byteLength > Math.max(this.maxResponseBytes, 65_536)) {
        response = { ...response, body: new Uint8Array() };
      }
      if (response.status !== 401 || attempt !== 0 || singleDispatch || !('resolveAfterRejection' in this.#credentials)) return response;
      try {
        current = await resolveCredentialAfterRejection(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal);
        if (!isHeaderSafeCredential(current.accessToken?.reveal() ?? '')) return response;
      } catch { return response; }
    }
    throw new DocsRequestError(true);
  }

  /** Return a receipt with only provider-safe identifiers. */
  receipt(call: Call, response: ProviderResponse | undefined, objectID = ''): ReceiptDraft {
    return { callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'google-docs', providerObjectId: objectID, providerRequestId: header(response?.headers ?? {}, 'x-goog-request-id') || header(response?.headers ?? {}, 'x-request-id'), observedAt: this.#now().toISOString() };
  }

  /** Return the observation clock for Retry-After dates. */ now(): Date { return this.#now(); }
}

/** Read a case-insensitive single HTTP header. */
export function header(headers: ProviderResponse['headers'], name: string): string {
  const value = headers[name] ?? headers[name.toLowerCase()] ?? headers[name.toUpperCase()];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}

/** Construct a credential-safe connector failure. */
export function docsFailure(operation: string, kind: FailureKind, message: string): Failure {
  return { provider: 'google-docs', operation, kind, message };
}

/** Classify a Google error status and machine-readable reason. */
export function statusFailureKind(status: number, tokens: string[]): FailureKind {
  if (status === 401) return 'AUTHENTICATION';
  if (isRateLimit(status, tokens)) return 'RATE_LIMIT';
  if (status === 403) return 'AUTHORIZATION';
  if (status === 404) return 'NOT_FOUND';
  if (status === 409) return 'CONFLICT';
  if (status >= 500 || status === 408) return 'AVAILABILITY';
  return 'PROVIDER_REJECTION';
}

/** Return Google's documented rate limit statuses. */
export function isRateLimit(status: number, tokens: string[]): boolean {
  return status === 429 || status === 403 && tokens.some(token => ['rateLimitExceeded', 'userRateLimitExceeded', 'RATE_LIMIT_EXCEEDED'].includes(token));
}

/** Return whether a request can safely be retried after this status. */
export function isRetryable(status: number, tokens: string[]): boolean {
  return status >= 500 || status === 408 || isRateLimit(status, tokens);
}

/** Extract Google error reasons without reflecting response text. */
export function googleTokens(response: ProviderResponse): string[] {
  return readErrorTokens(response.body, ['/error/errors/0/reason', '/error/status', '/error/details/0/reason']);
}

/** Parse Retry-After using the SDK's Go-compatible parser. */
export function retryAfterMs(response: ProviderResponse, now: Date): number {
  return parseRetryAfter(header(response.headers, 'retry-after'), now);
}

/** Match Go url.Values.Encode, including its escaping of tilde. */
export function goQuery(values: Record<string, string>): string {
  return Object.keys(values).sort().map(key => `${goQueryEscape(key)}=${goQueryEscape(values[key]!)}`).join('&');
}

function goQueryEscape(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*~]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%20/g, '+');
}

/** Match Go url.PathEscape's literal path character set. */
export function goPathEscape(value: string): string {
  let escaped = '';
  for (const byte of Buffer.from(value, 'utf8')) {
    const character = String.fromCharCode(byte);
    escaped += /[A-Za-z0-9._~$&+=:@-]/.test(character) ? character : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
  }
  return escaped;
}
