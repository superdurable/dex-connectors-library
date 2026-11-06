// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { resolveCredential, resolveCredentialAfterRejection, type Call, type CredentialRefreshDriver, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, readErrorTokens, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { OAuthAuthMethodID } from './models.js';

/** Injectable HTTP sender used by local replay tests. */
export type ExcelTransport = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Graph result, including status when the body was unreadable. */
export interface GraphResult { status: number; headers: ProviderResponse['headers']; body: Uint8Array; tooLarge: boolean; transportFailure: boolean; credentials: Credentials; }
/** One classified read failure. */
export interface ReadFailure { branch?: string; failure: Failure; retryAfterMs: number; receipt: ReceiptDraft; }

const encoder = new TextEncoder();
const requestIDPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const graphErrorClasses: Record<string, 'retry' | 'notFound' | 'tooLarge' | 'rejected'> = {
  accessconflict: 'retry', gatewaytimeoutuncategorized: 'retry', serviceunavailableuncategorized: 'retry',
  toomanyrequestsuncategorized: 'retry', transientfailure: 'retry', activitylimitreached: 'retry',
  notfounduncategorized: 'notFound', itemnotfound: 'notFound', payloadtoolargeuncategorized: 'tooLarge', rangeexceedslimit: 'tooLarge',
  accessdenied: 'rejected', badrequestuncategorized: 'rejected', conflictuncategorized: 'rejected', filteredrangeconflict: 'rejected',
  forbiddenuncategorized: 'rejected', generalexception: 'rejected', insertdeleteconflict: 'rejected', internalservererroruncategorized: 'rejected',
  invalidargument: 'rejected', invalidreference: 'rejected', itemalreadyexists: 'rejected', methodnotalloweduncategorized: 'rejected',
  nonblankcelloffsheet: 'rejected', notimplementeduncategorized: 'rejected', requestaborted: 'rejected', unauthorizeduncategorized: 'rejected',
  unsupportedoperation: 'rejected', unsupportedworkbook: 'rejected',
};

/** Provider-safe failure with the stable Excel identity. */
export function excelFailure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'microsoft-excel', operation, message };
}

/** Path-escape one Graph identifier exactly as Go url.PathEscape. */
export function pathEscape(value: string): string {
  return [...encoder.encode(value)].map(byte =>
    byte >= 65 && byte <= 90 || byte >= 97 && byte <= 122 || byte >= 48 && byte <= 57 || '-_.~$&+=:@'.includes(String.fromCharCode(byte))
      ? String.fromCharCode(byte) : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`).join('');
}

/** Complete Graph workbook URL for a Drive item. */
export function workbookPath(driveId: string, workbookId: string): string {
  return `/drives/${pathEscape(driveId)}/items/${pathEscape(workbookId)}/workbook`;
}
/** Complete Graph table URL. */
export function tablePath(driveId: string, workbookId: string, table: string): string {
  return `${workbookPath(driveId, workbookId)}/tables/${pathEscape(table)}`;
}
/** Complete Graph range URL. */
export function rangePath(driveId: string, workbookId: string, worksheet: string, address: string): string {
  return `${workbookPath(driveId, workbookId)}/worksheets/${pathEscape(worksheet)}/range(address='${address}')`;
}

/** Decode a Graph JSON response without silently rounding unsafe integers. */
export function graphJSON(body: Uint8Array): unknown {
  const raw = new TextDecoder().decode(body);
  const parse = JSON.parse as (text: string, reviver: (key: string, value: unknown, context?: { source?: string }) => unknown) => unknown;
  return parse(raw, (key, value, context) => {
    if (['rowCount', 'columnCount', 'index'].includes(key) && typeof value === 'number' &&
      (!context?.source || !/^-?(?:0|[1-9][0-9]*)$/.test(context.source)))
      throw new TypeError(`Graph field ${key} must be an integer JSON literal`);
    if (typeof value === 'number' && (!Number.isFinite(value) || !Number.isSafeInteger(value) && Number.isInteger(value))) {
      throw new TypeError(`Graph field ${key || '<root>'} cannot be represented exactly`);
    }
    return value;
  });
}

/** Graph request transport with the Go connector's response and resend bounds. */
export class GraphClient {
  readonly maxResponseBytes: number;
  readonly maxCells: number;
  readonly #credentials: CredentialSource;
  readonly #refreshDriver: CredentialRefreshDriver<Credentials>;
  readonly #transport: ExcelTransport;
  readonly #endpoint: string;
  readonly #now: () => Date;

  /** Validate limits and inject the host credential source. */
  constructor(config: Config, credentials: CredentialSource, refreshDriver: CredentialRefreshDriver<Credentials>, transport: ExcelTransport = sendProviderRequest, endpoint = 'https://graph.microsoft.com/v1.0', now: () => Date = () => new Date()) {
    this.maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    this.maxCells = config.maxCells ?? 10_000;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1 || !Number.isSafeInteger(this.maxCells) || this.maxCells < 1 || this.maxCells > 100_000) throw new TypeError('Microsoft Excel limits are invalid');
    if (!credentials || !refreshDriver || !transport || !now) throw new TypeError('Microsoft Excel dependencies are required');
    if (endpoint !== 'https://graph.microsoft.com/v1.0' && !/^http:\/\/(?:localhost|127\.0\.0\.1):\d+\/v1\.0$/.test(endpoint)) throw new TypeError('Microsoft Graph endpoint is invalid');
    this.#credentials = credentials; this.#refreshDriver = refreshDriver; this.#transport = transport; this.#endpoint = endpoint; this.#now = now;
  }

  /** Resolve current credentials outside the Flow state. */
  async credentials(call: Call): Promise<Credentials> {
    try {
      const credentials = await resolveCredential(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal);
      if (credentials.authMethodId !== OAuthAuthMethodID || !isHeaderSafeCredential(credentials.accessToken.reveal())) throw new TypeError();
      return credentials;
    } catch { throw new TypeError('connection credentials are unavailable'); }
  }

  /** Send one Graph request and at most one forced refresh after a 401. */
  async request(call: Call, credentials: Credentials, method: string, path: string, body?: unknown): Promise<GraphResult> {
    let current = credentials;
    for (let attempt = 0; attempt < 2; attempt++) {
      let status = 0;
      let headers: ProviderResponse['headers'] = {};
      const encoded = body === undefined ? undefined : encoder.encode(JSON.stringify(body));
      const request: ProviderRequest = { url: `${this.#endpoint}${path}`, method,
        headers: { Authorization: `Bearer ${current.accessToken.reveal()}`, Accept: 'application/json', ...(encoded ? { 'Content-Type': 'application/json' } : {}) },
        ...(encoded ? { body: encoded } : {}), maxResponseBytes: Math.max(this.maxResponseBytes, 65_536),
        addressPolicy: this.#endpoint.startsWith('http://') ? 'privateNetworkHTTP' : 'publicHTTPS', timeoutMs: 20_000,
        signal: call.context.cancellationSignal,
        onResponseChunk: (_chunk, responseStatus, responseHeaders) => { status = responseStatus; headers = responseHeaders; },
      };
      let result: GraphResult;
      try {
        const response = await this.#transport(request);
        result = { ...response, tooLarge: response.body.length > this.maxResponseBytes && response.status >= 200 && response.status < 300, transportFailure: false, credentials: current };
      } catch (error) {
        if (error instanceof BodyTooLargeError) {
          result = { status: error.status ?? status, headers: error.headers ?? headers, body: new Uint8Array(), tooLarge: (error.status ?? status) >= 200 && (error.status ?? status) < 300, transportFailure: false, credentials: current };
        } else result = { status, headers, body: new Uint8Array(), tooLarge: false, transportFailure: true, credentials: current };
      }
      if (result.status !== 401 || attempt !== 0 || !('resolveAfterRejection' in this.#credentials)) return result;
      try {
        current = await resolveCredentialAfterRejection(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal);
        if (current.authMethodId !== OAuthAuthMethodID || !isHeaderSafeCredential(current.accessToken.reveal())) return result;
      } catch { return result; }
    }
    throw new TypeError('Microsoft Graph request could not be sent');
  }

  /** Receipt for a completed Graph response. */
  receipt(call: Call, result: GraphResult): ReceiptDraft {
    const requestID = header(result.headers, 'request-id');
    return { callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'microsoft-excel', providerRequestId: requestIDPattern.test(requestID) ? requestID : '', observedAt: this.#now().toISOString() };
  }

  /** Parse the provider delay against the client clock. */
  retryAfter(headers: ProviderResponse['headers']): number {
    return parseRetryAfter(header(headers, 'retry-after'), this.#now());
  }

  /** Classify a failed read, keeping provider errors from an oversized body. */
  readFailure(call: Call, operation: string, result: GraphResult, branches: { NotFound: string; TooLarge?: string; ProviderRejected: string; InvalidResponse: string }): ReadFailure | undefined {
    const receipt = this.receipt(call, result);
    if (result.transportFailure) return { failure: excelFailure(operation, 'TRANSPORT', 'provider is unavailable'), retryAfterMs: 0, receipt };
    if (result.status >= 200 && result.status < 300) return result.tooLarge ? { branch: branches.TooLarge ?? branches.InvalidResponse, failure: excelFailure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds the configured response limit'), retryAfterMs: 0, receipt } : undefined;
    const tokens = errorTokens(result.body);
    const kind = statusFailureKind(result.status, tokens);
    switch (classifyGraphError(result.status, tokens)) {
      case 'retry': return { failure: excelFailure(operation, kind, 'provider temporarily rejected the request'), retryAfterMs: this.retryAfter(result.headers), receipt };
      case 'notFound': return { branch: branches.NotFound, failure: excelFailure(operation, 'NOT_FOUND', 'workbook resource was not found or is not visible to the connection'), retryAfterMs: 0, receipt };
      case 'tooLarge': return { branch: branches.TooLarge ?? branches.InvalidResponse, failure: excelFailure(operation, 'RESPONSE_TOO_LARGE', 'Excel reported that the range exceeds its limit'), retryAfterMs: 0, receipt };
      default: return { branch: branches.ProviderRejected, failure: excelFailure(operation, kind, 'provider rejected the request'), retryAfterMs: 0, receipt };
    }
  }
}

/** Read an HTTP response header case insensitively. */
export function header(headers: ProviderResponse['headers'], name: string): string {
  const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(value) ? value[0] ?? '' : typeof value === 'string' ? value : '';
}
/** Read Graph error codes in documented inner-first order. */
export function errorTokens(body: Uint8Array): string[] {
  return readErrorTokens(body, ['/error/innerError/code', '/error/innererror/code', '/error/code']);
}
/** Classify the decisive Graph code before its HTTP status. */
export function classifyGraphError(status: number, tokens: string[]): 'retry' | 'notFound' | 'tooLarge' | 'rejected' {
  for (const token of tokens) if (graphErrorClasses[token.toLowerCase()]) return graphErrorClasses[token.toLowerCase()]!;
  if ([429, 408, 500, 502, 503, 504].includes(status)) return 'retry';
  if (status === 404) return 'notFound';
  if (status === 413) return 'tooLarge';
  return 'rejected';
}
/** Map Graph HTTP status and code to a Dex failure kind. */
export function statusFailureKind(status: number, tokens: string[]): Failure['kind'] {
  if (status === 401) return 'AUTHENTICATION';
  if (status === 429 || tokens.some(token => ['toomanyrequestsuncategorized', 'activitylimitreached'].includes(token.toLowerCase()))) return 'RATE_LIMIT';
  if (status === 403) return 'AUTHORIZATION';
  if (status === 404) return 'NOT_FOUND';
  if (status === 409) return 'CONFLICT';
  if (status >= 500 || status === 408) return 'AVAILABILITY';
  return 'PROVIDER_REJECTION';
}
