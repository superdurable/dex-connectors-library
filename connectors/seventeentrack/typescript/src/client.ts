// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListTrackedShipmentsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListTrackedShipmentsInput, TrackedShipmentPage, TrackedShipment } from './generated/models.js';

const emptyPage: TrackedShipmentPage = { pageNumber: 0, total: 0, shipments: [] };
const requestPath = '/track/v2.4/gettracklist';

/** Optional transport dependency for provider tests. */
export interface SeventeenTrackClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Read existing 17TRACK shipments with bounded provider responses. */
export class SeventeenTrackClient {
  readonly baseURL: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate settings and bind the rotating credential source. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: SeventeenTrackClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('17TRACK configuration and credential source are required');
    this.baseURL = validateBaseURL(config.baseUrl ?? 'https://api.17track.net').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('17TRACK configuration is invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the tracking-list Query. */
  listTrackedShipments(): ListTrackedShipmentsOperation { return new ListTrackedShipmentsOperation(this); }
}

/** Read one page of already registered tracking statuses. */
export class ListTrackedShipmentsOperation implements Query<ListTrackedShipmentsInput, TrackedShipmentPage> {
  /** Bind an authenticated 17TRACK client. */
  constructor(private readonly client: SeventeenTrackClient) {}
  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return ListTrackedShipmentsDefinition; }
  /** Request and classify one tracking-list page. */
  async invoke(call: Call, input: ListTrackedShipmentsInput): Promise<QueryAttempt<TrackedShipmentPage>> {
    if (!input || !Number.isSafeInteger(input.pageNumber) || input.pageNumber < 0 || input.pageNumber > 100_000) return branch('defect', 'VALIDATION', '17TRACK page number must be between 1 and 100000');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', '17TRACK API key is unavailable or invalid'); }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!apiKey || apiKey.length > 2048 || !/^[!-~]+$/.test(apiKey)) return branch('defect', 'AUTHENTICATION', '17TRACK API key is unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({ url: this.client.baseURL + requestPath, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json', '17token': apiKey }, body: new TextEncoder().encode(JSON.stringify({ page_no: input.pageNumber || 1 })), timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', '17TRACK response exceeded the configured limit');
      if (error instanceof BodyTooLargeError && error.status !== undefined) return classifyStatus(error.status, error.headers ?? {});
      return retry('AVAILABILITY', '17TRACK tracking-list request failed');
    }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return classifyStatus(response.status, response.headers);
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', '17TRACK returned an invalid tracking-list response'); }
    if (response.status !== 200) return classifyStatus(response.status, response.headers);
    if (!isRecord(document) || !Number.isSafeInteger(document.code)) return branch('invalidResponse', 'PROTOCOL', '17TRACK returned an invalid tracking-list response');
    if (document.code !== 0) return branch('providerRejected', 'PROVIDER_REJECTION', '17TRACK rejected tracking-list query');
    if (!isRecord(document.data)) return branch('invalidResponse', 'PROTOCOL', '17TRACK returned an invalid tracking-list response');
    if (Array.isArray(document.data.errors) && document.data.errors.length > 0) return branch('providerRejected', 'PROVIDER_REJECTION', '17TRACK rejected tracking-list query');
    const page = document.page;
    const accepted = document.data.accepted;
    if (!isRecord(page) || !Number.isSafeInteger(page.page_no) || (page.page_no as number) < 0 || !Number.isSafeInteger(page.data_total) || (page.data_total as number) < 0 || !Number.isSafeInteger(page.page_size) || (page.page_size as number) > 40 || !Array.isArray(accepted) || accepted.length > 40) return branch('invalidResponse', 'PROTOCOL', '17TRACK returned an invalid tracking-list response');
    const shipments: TrackedShipment[] = [];
    for (const item of accepted) {
      if (!isRecord(item) || typeof item.number !== 'string' || !item.number || !Number.isSafeInteger(item.carrier) || (item.carrier as number) <= 0) return branch('invalidResponse', 'PROTOCOL', '17TRACK returned an invalid tracking-list response');
      shipments.push({ number: item.number, carrier: item.carrier as number, packageStatus: typeof item.package_status === 'string' ? item.package_status : '', trackingStatus: typeof item.tracking_status === 'string' ? item.tracking_status : '' });
    }
    return newQueryBranch('listed', { pageNumber: page.page_no as number, total: page.data_total as number, shipments });
  }
}

function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<TrackedShipmentPage> {
  const retryAfterHeader = headers['retry-after'];
  const retryAfterMs = parseRetryAfter(Array.isArray(retryAfterHeader) ? retryAfterHeader[0] ?? '' : retryAfterHeader ?? '');
  if (status === 429) return retry('RATE_LIMIT', '17TRACK rate limited tracking-list query', retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', '17TRACK tracking API is temporarily unavailable', retryAfterMs);
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : [400, 404, 412].includes(status) ? 'VALIDATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind, `17TRACK rejected tracking-list query with HTTP ${status}`);
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'seventeentrack', operation: 'listTrackedShipments', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<TrackedShipmentPage> { return newQueryBranch(id, emptyPage, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<TrackedShipmentPage> { return newQueryRetry(failure(kind, message), delayMs); }
