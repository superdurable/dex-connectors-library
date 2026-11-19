// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListFolderFilesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListFolderFilesInput, ListFolderFilesOutput } from './generated/models.js';

type Exchange = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };
type TokenOutcome = { token: string } | { kind: Failure['kind']; retry: boolean; invalidResponse: boolean; delay: number };

/** Optional provider sender for local tests. */
export interface KingsoftOfficeClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read WPS 365 file metadata with a cached application token. */
export class KingsoftOfficeClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly pageSize: number;
  private readonly requestSigningEnabled: boolean;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private cachedToken = '';
  private tokenFingerprint = '';
  private tokenExpiry = 0;

  /** Validate WPS connection settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: KingsoftOfficeClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('WPS configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://openapi.wps.cn');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    this.pageSize = config.pageSize || 100;
    this.requestSigningEnabled = config.requestSigningEnabled ?? false;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || !Number.isSafeInteger(this.pageSize) || this.pageSize < 1 || this.pageSize > 500) throw new TypeError('WPS request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the folder file list Query. */ listFolderFiles(): ListFolderFilesOperation { return new ListFolderFilesOperation(this); }

  /** Resolve application credentials for one call. */
  async getCredentials(call: Call): Promise<{ appID: string; appKey: string } | undefined> {
    try {
      const credential = await this.credentials.resolve(call, call.context.cancellationSignal);
      const appKey = credential.appKey instanceof SecretString ? credential.appKey.reveal() : '';
      if (!validResourceID(credential.appId) || !appKey || appKey.length > 512 || /[^\x21-\x7e]/.test(appKey)) return undefined;
      return { appID: credential.appId, appKey };
    } catch { return undefined; }
  }

  /** Exchange an application key for a token, reusing a valid cached token. */
  async accessToken(call: Call, appID: string, appKey: string): Promise<TokenOutcome> {
    const fingerprint = createHash('sha256').update(`${appID}:${appKey}`).digest('hex');
    if (this.cachedToken && this.tokenFingerprint === fingerprint && Date.now() + 300_000 < this.tokenExpiry) return { token: this.cachedToken };
    const form = new URLSearchParams({ grant_type: 'client_credentials', client_id: appID, client_secret: appKey });
    const exchange = await this.request(call, `${this.baseURL}/oauth2/token`, 'POST', { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' }, Buffer.from(form.toString()));
    if (exchange.error === 'transport' || !exchange.response) return { kind: 'AVAILABILITY', retry: true, invalidResponse: false, delay: 0 };
    const { status, body } = exchange.response;
    if (status === 429) return { kind: 'RATE_LIMIT', retry: true, invalidResponse: false, delay: exchange.retryAfterMs };
    if (status === 408 || status >= 500) return { kind: 'AVAILABILITY', retry: true, invalidResponse: false, delay: exchange.retryAfterMs };
    if (status !== 200) return { kind: classifyHTTP(status), retry: false, invalidResponse: false, delay: 0 };
    if (exchange.error === 'tooLarge') return { kind: 'RESPONSE_TOO_LARGE', retry: false, invalidResponse: true, delay: 0 };
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return { kind: 'PROTOCOL', retry: false, invalidResponse: true, delay: 0 }; }
    if (!isObject(wire)) return { kind: 'PROTOCOL', retry: false, invalidResponse: true, delay: 0 };
    if (wire.code !== undefined && wire.code !== 0) return { kind: 'AUTHENTICATION', retry: false, invalidResponse: false, delay: 0 };
    if (typeof wire.access_token !== 'string' || !wire.access_token || /\s/.test(wire.access_token) || !Number.isInteger(wire.expires_in) || (wire.expires_in as number) < 360 || (wire.expires_in as number) > 86400 || typeof wire.token_type !== 'string' || wire.token_type.toLowerCase() !== 'bearer') return { kind: 'PROTOCOL', retry: false, invalidResponse: true, delay: 0 };
    this.tokenFingerprint = fingerprint;
    this.cachedToken = wire.access_token;
    this.tokenExpiry = Date.now() + (wire.expires_in as number) * 1000;
    return { token: this.cachedToken };
  }

  /** Fetch one file metadata page. */
  async list(call: Call, input: ListFolderFilesInput, token: string, appID: string, appKey: string): Promise<Exchange> {
    const path = `/v7/drives/${encodeURIComponent(input.driveId)}/files/${encodeURIComponent(input.parentId)}/children`;
    const query = new URLSearchParams({ page_size: String(this.pageSize) });
    if (input.pageToken) query.set('page_token', input.pageToken);
    const url = `${this.baseURL}${path}?${query}`;
    const headers: Record<string, string> = { Accept: 'application/json', Authorization: `Bearer ${token}` };
    if (this.requestSigningEnabled) {
      const date = new Date().toUTCString();
      headers['Content-Type'] = 'application/json';
      headers['X-Kso-Date'] = date;
      const signature = createHmac('sha256', appKey).update(`KSO-1GET${path}?${query}application/json${date}`).digest('hex');
      headers['X-Kso-Authorization'] = `KSO-1 ${appID}:${signature}`;
    }
    return this.request(call, url, 'GET', headers);
  }

  /** Send one bounded WPS request. */
  private async request(call: Call, url: string, method: 'GET' | 'POST', headers: Record<string, string>, body?: Uint8Array): Promise<Exchange> {
    const request: ProviderRequest = { url, method, headers, body, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }

  /** Return configured file-page capacity. */ pageCapacity(): number { return this.pageSize; }
}

/** Read one WPS drive folder page. */
export class ListFolderFilesOperation implements Query<ListFolderFilesInput, ListFolderFilesOutput> {
  /** Bind the provider client. */ constructor(private readonly client: KingsoftOfficeClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListFolderFilesDefinition; }
  /** Classify token and file-list responses using the Go connector contract. */
  async invoke(call: Call, input: ListFolderFilesInput): Promise<QueryAttempt<ListFolderFilesOutput>> {
    if (!validResourceID(input?.driveId) || !validResourceID(input?.parentId) || typeof input.pageToken !== 'string' || input.pageToken.length > 2048 || /[^\x21-\x7e]/.test(input.pageToken)) return branch('defect', 'VALIDATION');
    const credentials = await this.client.getCredentials(call);
    if (!credentials) return branch('defect', 'AUTHENTICATION');
    const tokenOutcome = await this.client.accessToken(call, credentials.appID, credentials.appKey);
    if (!('token' in tokenOutcome)) return tokenOutcome.retry ? retry(tokenOutcome.kind, tokenOutcome.delay) : branch(tokenOutcome.invalidResponse ? 'invalidResponse' : 'providerRejected', tokenOutcome.kind);
    const exchange = await this.client.list(call, input, tokenOutcome.token, credentials.appID, credentials.appKey);
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', classifyHTTP(status));
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code === 429000001) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (wire.code !== 0) return branch('providerRejected', classifyCode(wire.code as number));
    const data = wire.data;
    if (!isObject(data) || !Array.isArray(data.items) || data.items.length > this.client.pageCapacity() || (data.next_page_token !== undefined && (typeof data.next_page_token !== 'string' || data.next_page_token.length > 2048))) return branch('invalidResponse', 'PROTOCOL');
    const items: ListFolderFilesOutput['items'] = [];
    for (const item of data.items) {
      if (!isObject(item) || typeof item.id !== 'string' || !item.id || typeof item.name !== 'string' || !item.name || typeof item.type !== 'string' || !item.type || !Number.isSafeInteger(item.size) || (item.size as number) < 0) return branch('invalidResponse', 'PROTOCOL');
      items.push({ id: item.id, name: item.name, type: item.type, sizeBytes: item.size as number });
    }
    return newQueryBranch('listed', { items, nextPageToken: (data.next_page_token as string | undefined) ?? '' });
  }
}

function validResourceID(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 256 && !/[^\x21-\x7e]|[/?#]/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function classifyHTTP(status: number): Failure['kind'] { return status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 404 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION'; }
function classifyCode(code: number): Failure['kind'] { return code === 40100011 || code === 40100008 ? 'AUTHENTICATION' : code === 403000001 || code === 40300010 ? 'AUTHORIZATION' : code === 400000001 ? 'QUOTA_EXHAUSTED' : 'PROVIDER_REJECTION'; }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'kingsoftoffice', operation: 'listFolderFiles', kind, message: 'WPS folder query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListFolderFilesOutput> { return newQueryBranch(name, { items: [], nextPageToken: '' }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListFolderFilesOutput> { return newQueryRetry(failure(kind), delay); }
