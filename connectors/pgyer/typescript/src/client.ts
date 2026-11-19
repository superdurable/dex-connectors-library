// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListUploadedAppsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListUploadedAppsInput, UploadedAppPage } from './generated/models.js';

/** Optional sender for provider tests. */
export interface PgyerClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Read Pgyer provider data with bounded HTTP responses. */
export class PgyerClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate connection settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: PgyerClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Pgyer configuration and credentials are required');

    this.baseURL = validateBaseURL(config.baseUrl || 'https://www.pgyer.com').replace(/\/$/, '');
    if (new URL(this.baseURL).pathname !== '/') throw new TypeError('baseUrl must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs || 15000;
    this.maxResponseBytes = config.maxResponseBytes || 1048576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('invalid request limits');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the listUploadedApps Query. */
  listUploadedApps(): ListUploadedAppsOperation { return new ListUploadedAppsOperation(this); }
  /** Resolve credentials and send one provider request. */
  async request(call: Call, input: ListUploadedAppsInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let key = '';
    try { const fields = await this.credentials.resolve(call, call.context.cancellationSignal); key = fields.apiKey instanceof SecretString ? fields.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!key || key.length > 512 || key.split('').some(character => character.charCodeAt(0) <= 32 || character.charCodeAt(0) === 127)) return { error: 'credential', retryAfterMs: 0 };
    const url = `${this.baseURL}/apiv2/app/listMy`;
    const method = 'POST'; const headers = { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' }; const form = new URLSearchParams({ _api_key: key }); if (input.page > 0) form.set('page', String(input.page)); const body = new TextEncoder().encode(form.toString());
    const providerRequest: ProviderRequest = { url, method, headers, body, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(providerRequest); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Classify Pgyer provider replies as Query branches or retries. */
export class ListUploadedAppsOperation implements Query<ListUploadedAppsInput, UploadedAppPage> {
  /** Bind one validated provider client. */ constructor(private readonly client: PgyerClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListUploadedAppsDefinition; }
  /** Execute one read-only provider Query. */
  async invoke(call: Call, input: ListUploadedAppsInput): Promise<QueryAttempt<UploadedAppPage>> {
    const branch = (name: string, kind: Failure['kind']): QueryAttempt<UploadedAppPage> => newQueryBranch(name, { apps: [] }, {}, failure(kind));
    const retry = (kind: Failure['kind'], delay = 0): QueryAttempt<UploadedAppPage> => newQueryRetry(failure(kind), delay);
    const bad = (): QueryAttempt<UploadedAppPage> => branch('invalidResponse', 'PROTOCOL');
    if (!Number.isInteger(input?.page) || input.page < 0 || input.page > 10000) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response; const retryAfterMs = exchange.retryAfterMs;
    if (status === 429) return retry('RATE_LIMIT', retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return bad(); }
    if (!isRecord(wire) || !Number.isInteger(wire.code)) return bad();
    const code = wire.code as number;
    if (code === 1098) return retry('RATE_LIMIT', retryAfterMs);
    if (code === 1003 || code === 1188) return retry('AVAILABILITY', retryAfterMs);
    if (code !== 0) return branch('providerRejected', [1001,1002,1055].includes(code) ? 'AUTHENTICATION' : [1122,1124].includes(code) ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (!Array.isArray(wire.data) || wire.data.length > 100) return bad();
    const apps = [] as NonNullable<UploadedAppPage['apps']>;
    for (const item of wire.data) {
      if (!isRecord(item) || !item.appKey || !item.buildKey || !item.buildName || (item.buildType !== 1 && item.buildType !== 2)) return bad();
      apps.push({ appKey: String(item.appKey), buildKey: String(item.buildKey), name: String(item.buildName), buildType: item.buildType as number, version: String(item.buildVersion ?? ''), createdAt: String(item.buildCreated ?? '') });
    }
    return newQueryBranch('listed', { apps });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'pgyer', operation: 'listUploadedApps', kind, message: 'Pgyer listUploadedApps failed' }; }
