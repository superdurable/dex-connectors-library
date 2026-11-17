// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ReadWebPageDefinition } from './generated/definitions.js';
import type { Config, Credentials, ReadWebPageInput, ReadWebPageOutput } from './generated/models.js';

const readerEndpoint = 'https://api.shuyanai.com/v1/reader';
const emptyPage = (): ReadWebPageOutput => ({ title: '', markdown: '' });
/** Provider HTTP sender, injectable for deterministic tests. */
export type DataEyesRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Client dependencies for tests and approved transports. */
export interface DataEyesClientOptions {
  /** SDK transport when omitted. */ send?: DataEyesRequestSender;
  /** Clock used to interpret HTTP Retry-After dates. */ now?: () => Date;
  /** Test server endpoint; production uses the fixed DataEyes Reader URL. */ readerEndpoint?: string;
}
/** Authenticated DataEyes public web-page reader client. */
export class DataEyesClient {
  private readonly endpoint: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: DataEyesRequestSender;
  private readonly now: () => Date;
  /** Validate settings without contacting the provider; credentials resolve for each call. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: DataEyesClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('DataEyes configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 20_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20))
      throw new TypeError('DataEyes request limits are invalid');
    this.endpoint = options.readerEndpoint ?? readerEndpoint;
    const parsed = new URL(this.endpoint);
    if ((parsed.protocol !== 'https:' && !(parsed.protocol === 'http:' && ['127.0.0.1', 'localhost'].includes(parsed.hostname))) || parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== '/v1/reader')
      throw new TypeError('DataEyes reader endpoint is invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the readWebPage Query. */ readWebPage(): ReadWebPageOperation { return new ReadWebPageOperation(this); }
  /** Resolve the private reading Key and send one bounded request. */
  async read(call: Call, pageURL: string): Promise<{ response: ProviderResponse } | { failure: Failure; retry?: true; retryAfterMs?: number; branch?: string }> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return { branch: 'defect', failure: failure('AUTHENTICATION', 'DataEyes reading Key is unavailable or invalid') }; }
    if (!validAPIKey(apiKey)) return { branch: 'defect', failure: failure('AUTHENTICATION', 'DataEyes reading Key is unavailable or invalid') };
    const request: ProviderRequest = {
      url: this.endpoint, method: 'POST',
      headers: { Authorization: `Bearer ${apiKey}`, 'Content-Type': 'application/json', Accept: 'application/json' },
      body: new TextEncoder().encode(JSON.stringify({ url: pageURL, format: 'markdown' })),
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.endpoint.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 408 || error.status > 500) return statusOutcome(error.status, error.headers ?? {}, this.now());
        return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'DataEyes response exceeded the configured limit') };
      }
      return { retry: true, failure: failure('AVAILABILITY', 'DataEyes reader request failed') };
    }
    if (response.status === 429 || response.status === 408 || response.status > 500) return statusOutcome(response.status, response.headers, this.now());
    if (response.body.length > this.maxResponseBytes) return { branch: 'invalidResponse', failure: failure('RESPONSE_TOO_LARGE', 'DataEyes response exceeded the configured limit') };
    return { response };
  }
}
/** Read one public HTTPS page as bounded Markdown. */
export class ReadWebPageOperation implements Query<ReadWebPageInput, ReadWebPageOutput> {
  /** Bind the provider client. */ constructor(private readonly client: DataEyesClient) {}
  /** Generated Query definition. */ definition(): QueryDefinition { return ReadWebPageDefinition; }
  /** Validate the page URL, call DataEyes, and classify the result. */
  async invoke(call: Call, input: ReadWebPageInput): Promise<QueryAttempt<ReadWebPageOutput>> {
    if (!publicPageURL(input?.url)) return terminal('defect', 'VALIDATION', 'DataEyes URL must be a public HTTPS page without query or fragment');
    const exchange = await this.client.read(call, input.url);
    if ('failure' in exchange) return exchange.retry ? newQueryRetry(exchange.failure, exchange.retryAfterMs ?? 0) : newQueryBranch(exchange.branch!, emptyPage(), {}, exchange.failure);
    const response = exchange.response;
    const retryAfterMs = responseRetryAfter(response, new Date());
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { wire = undefined; }
    const fields = wire && typeof wire === 'object' && !Array.isArray(wire) ? wire as Record<string, unknown> : undefined;
    const code = fields?.code;
    const hasCode = typeof code === 'number' && Number.isSafeInteger(code);
    if (response.status === 500) {
      if (hasCode && definitePageOutcomeCode(code)) return terminal('providerRejected', 'PROVIDER_REJECTION', 'DataEyes could not read this page');
      return newQueryRetry(failure('AVAILABILITY', 'DataEyes reader is temporarily unavailable'), retryAfterMs);
    }
    if (hasCode && code !== 0) {
      if ([32000, 32001, 32003, 32004, 40001, 40003].includes(code)) return terminal('providerRejected', 'PROVIDER_REJECTION', 'DataEyes could not read this page');
      if (code === 32002 || code === 50001) return newQueryRetry(failure('AVAILABILITY', 'DataEyes reader is temporarily unavailable'), retryAfterMs);
      return terminal('providerRejected', 'PROVIDER_REJECTION', `DataEyes rejected reading with code ${code}`);
    }
    if (response.status !== 200) return terminal('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION', `DataEyes rejected reading with HTTP ${response.status}`);
    const data = fields?.data;
    if (!hasCode || code !== 0 || !data || typeof data !== 'object' || Array.isArray(data)) return terminal('invalidResponse', 'PROTOCOL', 'DataEyes returned invalid page data');
    const page = data as Record<string, unknown>;
    const metadata = page.metadata;
    if (metadata != null && (typeof metadata !== 'object' || Array.isArray(metadata))) return terminal('invalidResponse', 'PROTOCOL', 'DataEyes returned invalid page data');
    const title = metadata && typeof metadata === 'object' ? (metadata as Record<string, unknown>).title : undefined;
    if (title != null && typeof title !== 'string') return terminal('invalidResponse', 'PROTOCOL', 'DataEyes returned invalid page data');
    const markdown = page.markdown;
    if (typeof markdown !== 'string' || Buffer.byteLength(markdown) > 100_000 || (typeof title === 'string' && Buffer.byteLength(title) > 1000)) return terminal('invalidResponse', 'PROTOCOL', 'DataEyes returned invalid page data');
    return newQueryBranch('found', { title: title ?? '', markdown });
  }
}
function publicPageURL(value: unknown): value is string {
  if (typeof value !== 'string' || !value || Buffer.byteLength(value) > 2048 || value.trim() !== value || !value.startsWith('https://') || value.includes('?') || value.includes('#')) return false;
  const authority = value.slice(8).split('/')[0] ?? '';
  if (authority.includes('@') || authority.includes('[')) return false;
  const [hostname, port, ...more] = authority.split(':');
  if (more.length || (port && port !== '443') || !hostname || !hostname.includes('.') || !/^[A-Za-z0-9.-]+$/.test(hostname) || isIP(hostname)) return false;
  try { const parsed = new URL(value); return parsed.protocol === 'https:' && !parsed.username && !parsed.password && !parsed.search && !parsed.hash && parsed.hostname.toLowerCase() !== 'localhost'; }
  catch { return false; }
}
function validAPIKey(value: string): boolean { return value.length > 0 && value.length <= 1024 && /^[!-~]+$/.test(value); }
function definitePageOutcomeCode(code: number): boolean { return [32000, 32001, 32003, 32004].includes(code); }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'dataeyes', operation: 'readWebPage', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<ReadWebPageOutput> { return newQueryBranch(branch, emptyPage(), {}, failure(kind, message)); }
function responseRetryAfter(response: ProviderResponse, now: Date): number { const entry = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1]; return parseRetryAfter(Array.isArray(entry) ? entry[0] ?? '' : entry ?? '', now); }
function statusOutcome(status: number, headers: ProviderResponse['headers'], now: Date): { retry: true; failure: Failure; retryAfterMs: number } { const entry = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1]; return { retry: true, retryAfterMs: parseRetryAfter(Array.isArray(entry) ? entry[0] ?? '' : entry ?? '', now), failure: status === 429 ? failure('RATE_LIMIT', 'DataEyes rate limited the read') : failure('AVAILABILITY', 'DataEyes reader is temporarily unavailable') }; }
