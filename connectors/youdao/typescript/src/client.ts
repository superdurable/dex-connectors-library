// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { TranslateTextDefinition } from './generated/definitions.js';
import type { Config, Credentials, TranslateTextInput, TextTranslation } from './generated/models.js';

const empty: TextTranslation = { translation: [], sourceLanguage: '', targetLanguage: '' };
/** Optional transport, clock, and salt source for deterministic verification. */
export interface YoudaoClientOptions {
  /** Send one provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply the current time for the v3 signature. */ now?: () => Date;
  /** Supply 16 cryptographically random salt bytes. */ salt?: () => Uint8Array;
}
/** Translate bounded text through Youdao's signed HTTPS API. */
export class YoudaoClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly salt: () => Uint8Array;
  /** Validate endpoint and bounds without contacting Youdao. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: YoudaoClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Youdao configuration and credential source are required');
    this.baseUrl = (config.baseUrl || 'https://openapi.youdao.com').replace(/\/$/, '');
    const endpoint = new URL(this.baseUrl);
    if (endpoint.protocol !== 'https:' || endpoint.pathname !== '/' || endpoint.search || endpoint.hash || endpoint.username || endpoint.password) throw new TypeError('Youdao endpoint must be an HTTPS root');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Youdao request bounds are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.salt = options.salt ?? (() => randomBytes(16));
  }
  /** Bind the translation Query. */ translateText(): TranslateTextOperation { return new TranslateTextOperation(this); }
  /** Sign and send one bounded text translation. */
  async request(call: Call, input: TranslateTextInput): Promise<QueryAttempt<TextTranslation>> {
    const characters = typeof input?.text === 'string' ? [...input.text] : [];
    if (!input || typeof input.text !== 'string' || !input.text.trim() || characters.length > 5000 || !validLanguage(input.sourceLanguage) || !validLanguage(input.targetLanguage) || input.targetLanguage === 'auto') return branch('defect', 'VALIDATION');
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const appKey = credentials.appKey;
    const secret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (typeof appKey !== 'string' || !appKey || appKey.length > 256 || !/^[!-~]+$/.test(appKey) || !secret) return branch('defect', 'AUTHENTICATION');
    let saltBytes: Uint8Array;
    try { saltBytes = this.salt(); }
    catch { return retry('TRANSPORT'); }
    if (saltBytes.length !== 16) return retry('TRANSPORT');
    const salt = Buffer.from(saltBytes).toString('hex');
    const curtime = String(Math.floor(this.now().getTime() / 1000));
    const signatureText = characters.length > 20 ? `${characters.slice(0, 10).join('')}${characters.length}${characters.slice(-10).join('')}` : input.text;
    const sign = createHash('sha256').update(`${appKey}${signatureText}${salt}${curtime}${secret}`).digest('hex');
    const form = new URLSearchParams({ q: input.text, from: input.sourceLanguage, to: input.targetLanguage, appKey, salt, curtime, signType: 'v3', sign });
    const request: ProviderRequest = { url: `${this.baseUrl}/api`, method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json' }, body: Buffer.from(form.toString()), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      return retry('TRANSPORT');
    }
    const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', delay);
    if (response.body.length > this.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    let data: unknown;
    try { data = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(data) || typeof data.errorCode !== 'string' || !data.errorCode) return branch('invalidResponse', 'PROTOCOL');
    if (data.errorCode === '0') {
      if (!Array.isArray(data.translation) || data.translation.length < 1 || data.translation.length > 10 || data.translation.some(value => typeof value !== 'string' || !value || Buffer.byteLength(value, 'utf8') > 65_536)) return branch('invalidResponse', 'PROTOCOL');
      return newQueryBranch('translated', { translation: data.translation as string[], sourceLanguage: input.sourceLanguage, targetLanguage: input.targetLanguage });
    }
    if (data.errorCode === '411' || data.errorCode === '412') return retry('RATE_LIMIT', delay);
    if (['302', '303', '304'].includes(data.errorCode)) return retry('AVAILABILITY', delay);
    return branch('providerRejected', ['108', '111', '202', '203', '206', '401'].includes(data.errorCode) ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
  }
}
/** Translate text to the requested target language. */
export class TranslateTextOperation implements Query<TranslateTextInput, TextTranslation> {
  /** Bind one validated client. */ constructor(private readonly client: YoudaoClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return TranslateTextDefinition; }
  /** Invoke one safe provider read. */ invoke(call: Call, input: TranslateTextInput): Promise<QueryAttempt<TextTranslation>> { return this.client.request(call, input); }
}
function validLanguage(value: string): boolean { return typeof value === 'string' && /^[A-Za-z-]{1,24}$/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'youdao', operation: 'translateText', kind, message: 'Youdao translation failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<TextTranslation> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<TextTranslation> { return newQueryRetry(failure(kind), delay); }
