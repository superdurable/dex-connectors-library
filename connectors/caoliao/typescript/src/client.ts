// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListQRCodesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListQRCodesInput, ListQRCodesOutput, QRCodeSummary } from './generated/models.js';

/** Optional HTTP transport for local provider tests. */
export interface CaoliaoClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve Caoliao credentials for each QR-code listing query. */
export class CaoliaoClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting Caoliao. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: CaoliaoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Caoliao configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://open.cli.im');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Caoliao request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the QR-code listing Query. */ listQRCodes(): ListQRCodesOperation { return new ListQRCodesOperation(this); }

  /** Send one bounded QR-code page request. */
  async getQRCodes(call: Call, input: ListQRCodesInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try { const credential = await this.credentials.resolve(call, call.context.cancellationSignal); apiKey = credential.apiKey instanceof SecretString ? credential.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const payload: { page_size?: number; page_token?: string } = {};
    if (input.pageSize) payload.page_size = input.pageSize;
    if (input.pageToken) payload.page_token = input.pageToken;
    const request: ProviderRequest = { url: this.baseURL + '/api/v2/rpc/qrcodes/list', method: 'POST', headers: { Accept: 'application/json', Authorization: 'Bearer ' + apiKey, 'Content-Type': 'application/json' }, body: new TextEncoder().encode(JSON.stringify(payload)), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read one organization QR-code page without scanned records. */
export class ListQRCodesOperation implements Query<ListQRCodesInput, ListQRCodesOutput> {
  /** Bind the provider client. */ constructor(private readonly client: CaoliaoClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListQRCodesDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: ListQRCodesInput): Promise<QueryAttempt<ListQRCodesOutput>> {
    if (!input || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION');
    const pageSize = input.pageSize ?? 0;
    const pageToken = input.pageToken ?? '';
    if (!Number.isInteger(pageSize) || pageSize < 0 || pageSize > 100 || typeof pageToken !== 'string' || pageToken.length > 4096 || !safeToken(pageToken)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getQRCodes(call, { pageSize, pageToken });
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 || status === 403 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const response = wire as { code?: unknown; error_code?: unknown; data?: unknown };
    if (typeof response.code !== 'number' || !Number.isInteger(response.code)) return branch('invalidResponse', 'PROTOCOL');
    if (response.code !== 0) {
      if (response.code === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
      if (response.code === 502 || response.error_code === 100300) return retry('AVAILABILITY', exchange.retryAfterMs);
      return branch('providerRejected', response.code === 401 || response.error_code === 100315 || response.error_code === 100316 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
    }
    if (!response.data || typeof response.data !== 'object' || Array.isArray(response.data)) return branch('invalidResponse', 'PROTOCOL');
    const data = response.data as { list?: unknown; next_page_token?: unknown; total?: unknown };
    if (!Array.isArray(data.list) || data.list.length > 100 || typeof data.total !== 'number' || !Number.isInteger(data.total) || data.total < 0) return branch('invalidResponse', 'PROTOCOL');
    const nextPageToken = data.next_page_token ?? '';
    if (typeof nextPageToken !== 'string' || nextPageToken.length > 4096 || !safeToken(nextPageToken)) return branch('invalidResponse', 'PROTOCOL');
    const qrCodes: QRCodeSummary[] = [];
    for (const entry of data.list) {
      if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return branch('invalidResponse', 'PROTOCOL');
      const code = entry as { name?: unknown; coding?: unknown };
      if (typeof code.name !== 'string' || !code.name || Buffer.byteLength(code.name) > 1024 || typeof code.coding !== 'string' || !code.coding || code.coding.length > 256 || !safeToken(code.coding)) return branch('invalidResponse', 'PROTOCOL');
      qrCodes.push({ name: code.name, coding: code.coding });
    }
    return newQueryBranch('listed', { qrCodes, nextPageToken, total: data.total });
  }
}

function safeToken(value: string): boolean { return !/[^\x21-\x7e]/.test(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'caoliao', operation: 'listQRCodes', kind, message: 'Caoliao QR-code listing failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListQRCodesOutput> { return newQueryBranch(name, { qrCodes: [], nextPageToken: '', total: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListQRCodesOutput> { return newQueryRetry(failure(kind), delay); }
