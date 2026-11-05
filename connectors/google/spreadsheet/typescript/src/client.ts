// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, ProviderTransportError, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { ReauthorizationRequiredError, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { GetValuesOperation, FindRowOperation, UpsertRowOperation } from './operations.js';

const defaultEndpoint = 'https://sheets.googleapis.com/v4';
const defaultResponseBytes = 4_194_304;
const defaultRows = 10_000;

/** Caller-owned transport and clock for deterministic provider tests. */
export interface SpreadsheetClientOptions {
  /** HTTP sender; omit for the SDK's bounded transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock for receipts. */
  now?: () => Date;
  /** Token endpoint transport for OAuth agreement tests. */
  refreshFetch?: typeof fetch;
}

/** One response or transport classification, preserving status on oversized replies. */
export interface SpreadsheetExchange {
  /** Provider HTTP status; zero when no response arrived. */ status: number;
  /** Provider response headers. */ headers: ProviderResponse['headers'];
  /** Bounded body. */ body: Uint8Array;
  /** Exact safe request identity, when given. */ requestId: string;
  /** Response exceeded the configured bound. */ isTooLarge: boolean;
  /** Request was provably not sent. */ isProvablyNotSent: boolean;
  /** Transport or local error. */ error?: unknown;
}

/** Validated Google Sheets client with host-owned refreshing credentials. */
export class SpreadsheetClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  readonly maxRows: number;
  private readonly credentials: CredentialSource;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver: ManifestCredentialRefreshDriver<Credentials>;

  /** Bind one named connection without contacting Google. */
  constructor(config: Config, credentials: CredentialSource, options: SpreadsheetClientOptions = {}) {
    if (!credentials) throw new TypeError('Google Sheets credential provider is required');
    const endpoint = config.endpoint || defaultEndpoint;
    let parsed: URL;
    try { parsed = new URL(endpoint); } catch { throw new TypeError('Google Sheets endpoint must be absolute'); }
    const rawAuthority = /^https?:\/\/([^/?#]*)/.exec(endpoint)?.[1] ?? '';
    const rawHostname = rawAuthority.replace(/^.*@/, '').replace(/:[0-9]+$/, '');
    if (!parsed.hostname || (parsed.protocol !== 'https:' && !['localhost', '127.0.0.1'].includes(rawHostname))) throw new TypeError('Google Sheets endpoint must use HTTPS');
    this.endpoint = endpoint.replace(/\/+$/, '');
    this.maxResponseBytes = config.maxResponseBytes || defaultResponseBytes;
    this.maxRows = config.maxRows || defaultRows;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1 || !Number.isSafeInteger(this.maxRows) || this.maxRows < 2) throw new TypeError('Google Sheets response and row limits must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Google', auth: Authorization, credentialFields: CredentialFields, ...(options.refreshFetch ? { fetch: options.refreshFetch } : {}) });
  }

  /** Bind the bounded range query. */ getValues(): GetValuesOperation { return new GetValuesOperation(this); }
  /** Bind the stable row query. */ findRow(): FindRowOperation { return new FindRowOperation(this); }
  /** Bind the idempotent row upsert. */ upsertRow(): UpsertRowOperation { return new UpsertRowOperation(this); }

  /** Resolve a credential through generated OAuth refresh semantics. */
  async resolve(call: Call): Promise<{ credential?: Credentials; failure?: Failure; reauthorization: boolean }> {
    try {
      const credential = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      if (!credential.accessToken.reveal()) throw new ReauthorizationRequiredError();
      return { credential, reauthorization: false };
    } catch (error) {
      const reauthorization = error instanceof ReauthorizationRequiredError;
      return { failure: this.failure(call.operation.operationId, reauthorization ? 'AUTHENTICATION' : 'AVAILABILITY', reauthorization ? 'connection credentials are unavailable; reconnect the connection' : 'credential resolution is temporarily unavailable'), reauthorization };
    }
  }

  /** Refresh once after a rejected access token, retaining the generated provider's persistence contract. */
  async refreshAfterRejection(call: Call, rejected: Credentials, allowUnchanged = false): Promise<{ credential?: Credentials; reauthorization: boolean }> {
    if (!('resolveAfterRejection' in this.credentials)) return { reauthorization: true };
    try {
      const credential = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      if (!credential.accessToken.reveal() || (!allowUnchanged && credential.accessToken.reveal() === rejected.accessToken.reveal())) return { reauthorization: true };
      return { credential, reauthorization: false };
    } catch (error) { return { reauthorization: error instanceof ReauthorizationRequiredError }; }
  }

  /** Make one request, refreshing once on 401 for reads. */
  async request(call: Call, credential: Credentials, method: string, spreadsheetId: string, suffix: string, query: Record<string, string>, body?: unknown, write = false, signal?: AbortSignal): Promise<{ exchange: SpreadsheetExchange; credential: Credentials }> {
    const url = `${this.endpoint}/spreadsheets/${goPathEscape(spreadsheetId)}${suffix}?${new URLSearchParams(Object.entries(query).sort(([a], [b]) => a.localeCompare(b))).toString()}`;
    const payload = body === undefined ? undefined : Buffer.from(JSON.stringify(body));
    for (let attempt = 0; attempt < (write ? 1 : 2); attempt++) {
      const headers: Record<string, string> = { Authorization: `Bearer ${credential.accessToken.reveal()}` };
      if (payload) headers['Content-Type'] = 'application/json';
      if (write) headers['X-Idempotency-Key'] = call.idempotencyKey ?? '';
      const request: ProviderRequest = {
        url, method, headers, ...(payload ? { body: payload } : {}),
        maxResponseBytes: this.maxResponseBytes,
        addressPolicy: 'unrestricted', timeoutMs: 25_000,
        signal: signal ?? call.context.cancellationSignal,
      };
      let exchange: SpreadsheetExchange;
      try {
        const response = await this.send(request);
        exchange = { status: response.status, headers: response.headers, body: response.body, requestId: header(response.headers, 'x-goog-request-id') || header(response.headers, 'x-request-id'), isTooLarge: false, isProvablyNotSent: false };
      } catch (error) {
        if (error instanceof BodyTooLargeError) {
          const headers = error.headers ?? {};
          exchange = { status: error.status ?? 0, headers, body: new Uint8Array(), requestId: header(headers, 'x-goog-request-id') || header(headers, 'x-request-id'), isTooLarge: true, isProvablyNotSent: false, error };
        } else {
          exchange = { status: 0, headers: {}, body: new Uint8Array(), requestId: '', isTooLarge: false, isProvablyNotSent: error instanceof ProviderTransportError && error.isProvablyNotSent, error };
        }
      }
      if (write || exchange.status !== 401 || attempt !== 0 || !('resolveAfterRejection' in this.credentials)) return { exchange, credential };
      const replacement = await this.refreshAfterRejection(call, credential, true);
      if (!replacement.credential) return { exchange, credential };
      credential = replacement.credential;
    }
    throw new TypeError('Google Sheets authenticated request retry was exhausted');
  }

  /** Decode a bounded Sheets values response and retain Go's scalar formatting. */
  decodeValues(exchange: SpreadsheetExchange, operation: string): { value?: { range: string; majorDimension: string; values: string[][] }; failure?: Failure } {
    let data: unknown;
    try { data = JSON.parse(Buffer.from(exchange.body).toString('utf8')); } catch { return { failure: this.failure(operation, 'PROTOCOL', 'provider response is invalid') }; }
    if (!data || typeof data !== 'object' || Array.isArray(data)) return { failure: this.failure(operation, 'PROTOCOL', 'provider response is invalid') };
    const fields = data as Record<string, unknown>;
    if (fields.range != null && typeof fields.range !== 'string' || fields.majorDimension != null && typeof fields.majorDimension !== 'string' || fields.values != null && (!Array.isArray(fields.values) || fields.values.some(row => row !== null && !Array.isArray(row)))) return { failure: this.failure(operation, 'PROTOCOL', 'provider response is invalid') };
    const rows = (fields.values ?? []) as Array<Array<unknown> | null>;
    if (rows.length > this.maxRows) return { failure: this.failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds configured row limit') };
    return { value: { range: fields.range as string ?? '', majorDimension: fields.majorDimension as string ?? '', values: rows.map(row => (row ?? []).map(goFormatValue)) } };
  }

  /** Return provider-safe request correlation. */
  receipt(call: Call, exchange?: SpreadsheetExchange, objectId = ''): ReceiptDraft {
    return { callId: call.id, provider: 'google-sheets', ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), ...(objectId ? { providerObjectId: objectId } : {}), ...(exchange?.requestId ? { providerRequestId: exchange.requestId } : {}), observedAt: this.now().toISOString() };
  }

  /** Construct a provider-safe Go-compatible failure. */
  failure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'google-sheets', operation, kind, message }; }
}

/** Escape a path component with Go url.PathEscape's safe set. */
export function goPathEscape(value: string): string { return Array.from(Buffer.from(value)).map(byte => /[A-Za-z0-9._~$&+=:@-]/.test(String.fromCharCode(byte)) ? String.fromCharCode(byte) : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`).join(''); }

function header(headers: ProviderResponse['headers'], name: string): string { const value = headers[name]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function goFormatValue(value: unknown): string {
  if (value === null) return '<nil>';
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  if (Array.isArray(value)) return `[${value.map(goFormatValue).join(' ')}]`;
  if (typeof value === 'object') return `map[${Object.entries(value).sort(([a], [b]) => a.localeCompare(b)).map(([key, field]) => `${key}:${goFormatValue(field)}`).join(' ')}]`;
  return '<nil>';
}
