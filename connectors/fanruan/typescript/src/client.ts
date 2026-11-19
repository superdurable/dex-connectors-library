// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListAppsPageDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListAppsPageInput, ListAppsPageOutput } from './generated/models.js';

const appsEndpoint = 'https://api.jiandaoyun.com/api/v5/app/list';

/** Optional provider sender for local tests. */
export interface FanruanClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** List apps authorized by a Jiandaoyun API key. */
export class FanruanClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting Jiandaoyun. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: FanruanClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Jiandaoyun configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Jiandaoyun request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the application page Query. */ listAppsPage(): ListAppsPageOperation { return new ListAppsPageOperation(this); }

  /** Send one bounded app page request. */
  async list(call: Call, skip: number, limit: number): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 1024 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: appsEndpoint, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: `Bearer ${apiKey}` }, body: Buffer.from(JSON.stringify({ limit, skip })), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read one Jiandaoyun application page. */
export class ListAppsPageOperation implements Query<ListAppsPageInput, ListAppsPageOutput> {
  /** Bind the provider client. */ constructor(private readonly client: FanruanClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListAppsPageDefinition; }
  /** Classify one page using the Go connector contract. */
  async invoke(call: Call, input: ListAppsPageInput): Promise<QueryAttempt<ListAppsPageOutput>> {
    const skip = input?.skip;
    const selectedLimit = input?.limit;
    if (!Number.isSafeInteger(skip) || !Number.isSafeInteger(selectedLimit) || skip < 0 || skip > Number.MAX_SAFE_INTEGER - 100 || selectedLimit < 0 || selectedLimit > 100) return branch('defect', 'VALIDATION');
    const limit = selectedLimit || 100;
    const exchange = await this.client.list(call, skip, limit);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { wire = undefined; }
    if (status === 400) return isObject(wire) && wire.code === 8303 ? retry('RATE_LIMIT', exchange.retryAfterMs) : branch('providerRejected', 'PROVIDER_REJECTION');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (!isObject(wire) || !Array.isArray(wire.apps) || wire.apps.some(app => !isObject(app) || typeof app.app_id !== 'string' || !app.app_id || typeof app.name !== 'string' || !app.name)) return branch('invalidResponse', 'PROTOCOL');
    const apps = wire.apps.map(app => ({ id: app.app_id as string, name: app.name as string }));
    return newQueryBranch('listed', { apps, nextSkip: apps.length === limit ? skip + limit : 0 });
  }
}

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'fanruan', operation: 'listAppsPage', kind, message: 'Jiandaoyun application query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListAppsPageOutput> { return newQueryBranch(name, { apps: [], nextSkip: 0 }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListAppsPageOutput> { return newQueryRetry(failure(kind), delay); }
