// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { TranslateTextDefinition } from './generated/definitions.js';
import type { Config, Credentials, TranslateTextInput, TranslateTextOutput } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface NiutransClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
const empty: TranslateTextOutput = { translatedText: '' };
/** Resolve NiuTrans credentials for each metered translation query. */
export class NiutransClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate settings without contacting NiuTrans. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: NiutransClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('NiuTrans settings and credentials are required');
    const url = new URL(config.baseUrl || 'https://api.niutrans.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 65_536;
    if (url.protocol !== 'https:' || (url.pathname !== '/' && url.pathname !== '') || url.search || url.hash || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('NiuTrans settings are invalid');
    this.baseUrl = url.origin;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the translation Query. */ translateText(): TranslateTextOperation { return new TranslateTextOperation(this); }
  /** Send one bounded form request. */
  async request(call: Call, input: TranslateTextInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try { const resolved = await this.credentials.resolve(call, call.context.cancellationSignal); apiKey = resolved.apiKey instanceof SecretString ? resolved.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 512 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const form = new URLSearchParams({ from: input.from, to: input.to, apikey: apiKey, src_text: input.text });
    const request: ProviderRequest = { url: `${this.baseUrl}/NiuTransServer/translation`, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' }, body: new TextEncoder().encode(form.toString()), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}
/** Translate bounded text through NiuTrans. */
export class TranslateTextOperation implements Query<TranslateTextInput, TranslateTextOutput> {
  /** Bind the provider client. */ constructor(private readonly client: NiutransClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return TranslateTextDefinition; }
  /** Classify the provider response using the Go contract. */
  async invoke(call: Call, input: TranslateTextInput): Promise<QueryAttempt<TranslateTextOutput>> {
    if (typeof input?.text !== 'string' || !input.text.trim() || [...input.text].length > 1000 || typeof input.from !== 'string' || !/^[A-Za-z][A-Za-z-]{1,11}$/.test(input.from) || typeof input.to !== 'string' || !/^[A-Za-z][A-Za-z-]{1,11}$/.test(input.to) || input.from === input.to) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 || status === 403 ? 'AUTHENTICATION' : status === 400 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const record = wire as Record<string, unknown>;
    if (record.error_code !== undefined && record.error_code !== '') {
      if (record.error_code === '10001') return retry('RATE_LIMIT');
      if (record.error_code === '13008') return retry('AVAILABILITY');
      return branch('providerRejected', 'PROVIDER_REJECTION');
    }
    if (typeof record.tgt_text !== 'string' || !record.tgt_text || new TextEncoder().encode(record.tgt_text).length > 65_536) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('translated', { translatedText: record.tgt_text });
  }
}
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'niutrans', operation: 'translateText', kind, message: 'NiuTrans translation query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<TranslateTextOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<TranslateTextOutput> { return newQueryRetry(failure(kind), delay); }
