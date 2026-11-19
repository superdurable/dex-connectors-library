// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetShipmentStatusCountsDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetShipmentStatusCountsInput, ShipmentStatusCounts } from './generated/models.js';

const statusPath = '/v3/trackings/status';
const emptyCounts: ShipmentStatusCounts = { pending: 0, notFound: 0, inTransit: 0, pickup: 0, delivered: 0, expired: 0, undelivered: 0, exception: 0 };

/** Optional provider sender for local tests. */
export interface FiftyonetrackingClientOptions {
  /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read aggregate parcel statuses from 51Tracking. */
export class FiftyonetrackingClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local limits without contacting 51Tracking. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: FiftyonetrackingClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('51Tracking configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('51Tracking request limits are invalid');
    let parsedUrl: URL;
    try { parsedUrl = new URL(config.baseUrl || 'https://api.51tracking.com'); } catch { throw new TypeError('51Tracking baseUrl must be an HTTPS origin'); }
    if (parsedUrl.protocol !== 'https:' || !parsedUrl.hostname || parsedUrl.username || parsedUrl.password || parsedUrl.search || parsedUrl.hash || (parsedUrl.pathname !== '/' && parsedUrl.pathname !== '') || parsedUrl.hostname === 'localhost') throw new TypeError('51Tracking baseUrl must be an HTTPS origin');
    this.baseUrl = parsedUrl.origin;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the parcel-status Query. */ getShipmentStatusCounts(): GetShipmentStatusCountsOperation { return new GetShipmentStatusCountsOperation(this); }

  /** Send one bounded status-count request. */
  async list(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiKey = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 2048 || /[^\x21-\x7e]/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: this.baseUrl + statusPath, method: 'GET', headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'Tracking-Api-Key': apiKey }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read the current 51Tracking parcel-status counts. */
export class GetShipmentStatusCountsOperation implements Query<GetShipmentStatusCountsInput, ShipmentStatusCounts> {
  /** Bind the provider client. */ constructor(private readonly client: FiftyonetrackingClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetShipmentStatusCountsDefinition; }
  /** Classify one status-count request using the Go connector contract. */
  async invoke(call: Call): Promise<QueryAttempt<ShipmentStatusCounts>> {
    const exchange = await this.client.list(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : [400, 404, 412].includes(status) ? 'VALIDATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { wire = undefined; }
    if (!isObject(wire) || typeof wire.code !== 'number' || !Number.isSafeInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    const data = wire.data;
    if (!isObject(data)) return branch('invalidResponse', 'PROTOCOL');
    const fields = ['pending', 'notfound', 'transit', 'pickup', 'delivered', 'expired', 'undelivered', 'exception'];
    if (fields.some(field => typeof data[field] !== 'number' || !Number.isSafeInteger(data[field]) || (data[field] as number) < 0)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('counted', { pending: data.pending as number, notFound: data.notfound as number, inTransit: data.transit as number, pickup: data.pickup as number, delivered: data.delivered as number, expired: data.expired as number, undelivered: data.undelivered as number, exception: data.exception as number });
  }
}

function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'fiftyonetracking', operation: 'getShipmentStatusCounts', kind, message: '51Tracking status-count query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ShipmentStatusCounts> { return newQueryBranch(name, { ...emptyCounts }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ShipmentStatusCounts> { return newQueryRetry(failure(kind), delay); }
