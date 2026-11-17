// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderStalledError, ProviderTransportError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GeocodeAddressDefinition } from './generated/definitions.js';
import type { Config, Credentials, GeocodeAddressInput, GeocodeAddressOutput, GeocodedLocation } from './generated/models.js';

const emptyOutput: GeocodeAddressOutput = { locations: [] };
/** Send one Amap request through a caller-owned transport. */
export type AmapRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for provider tests and approved proxies. */
export interface AmapClientOptions {
  /** Request sender; production uses the Connector SDK bounded HTTP transport. */
  send?: AmapRequestSender;
}
/** Resolve Amap credentials for each geocoding request. */
export class AmapClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: AmapRequestSender;
  /** Validate settings without contacting Amap. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: AmapClientOptions = {}) {
    if (!credentials) throw new TypeError('Amap credential source is required');
    this.baseUrl = config.baseUrl ?? 'https://restapi.amap.com';
    const parsed = new URL(this.baseUrl);
    if (parsed.protocol !== 'https:' || parsed.username || parsed.password || parsed.pathname !== '/' || parsed.search || parsed.hash) throw new TypeError('Amap base URL must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('Amap configuration is invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the geocoding Query to this client. */
  geocodeAddress(): GeocodeAddressOperation { return new GeocodeAddressOperation(this); }
}
/** Convert a structured Chinese address into candidate coordinates. */
export class GeocodeAddressOperation implements Query<GeocodeAddressInput, GeocodeAddressOutput> {
  constructor(readonly client: AmapClient) { if (!client) throw new TypeError('Amap client is required'); }
  /** Return the generated Query identity and retry policy. */
  definition(): QueryDefinition { return GeocodeAddressDefinition; }
  /** Make one provider request and classify the observed outcome. */
  async invoke(call: Call, input: GeocodeAddressInput): Promise<QueryAttempt<GeocodeAddressOutput>> {
    if (typeof input?.address !== 'string' || !input.address.trim() || input.address.length > 512 || (input.city !== undefined && (typeof input.city !== 'string' || input.city.length > 128))) return branch('defect', 'VALIDATION', 'address or city is invalid');
    let key: string;
    try {
      const credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal);
      key = credentials.apiKey.reveal();
      if (!isValidAPIKey(key)) throw new TypeError('invalid Amap key');
    } catch { return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid'); }
    const parameters = new URLSearchParams();
    parameters.set('address', input.address);
    if (input.city) parameters.set('city', input.city);
    parameters.set('key', key);
    parameters.set('output', 'JSON');
    const request: ProviderRequest = {
      url: `${this.client.baseUrl}/v3/geocode/geo?${parameters}`, method: 'GET', headers: { Accept: 'application/json' },
      maxResponseBytes: this.client.maxResponseBytes, maxErrorResponseBytes: 4096, timeoutMs: this.client.timeoutMs,
      addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.client.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError || error instanceof ProviderTransportError || error instanceof ProviderStalledError) {
        const status = error.status;
        const delay = retryAfter(error.headers);
        if (status === 429) return retry('RATE_LIMIT', 'Amap rate limited the request', delay);
        if (status === 408 || (status !== undefined && status >= 500)) return retry('AVAILABILITY', 'Amap is temporarily unavailable', delay);
        if (status !== undefined && status !== 200) return httpRejection(status);
        if (error instanceof BodyTooLargeError) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Amap response exceeded the configured limit');
        return retry('AVAILABILITY', 'Amap response was interrupted');
      }
      return retry('AVAILABILITY', 'Amap request failed');
    }
    const delay = retryAfter(response.headers);
    if (response.status === 429) return retry('RATE_LIMIT', 'Amap rate limited the request', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'Amap is temporarily unavailable', delay);
    if (response.status !== 200) return httpRejection(response.status);
    let envelope: unknown;
    try { envelope = JSON.parse(Buffer.from(response.body).toString('utf8')) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Amap returned an invalid response'); }
    if (!isRecord(envelope) || (envelope.status !== '0' && envelope.status !== '1') || typeof envelope.infocode !== 'string' || !envelope.infocode) return branch('invalidResponse', 'PROTOCOL', 'Amap returned an invalid response');
    if (envelope.infocode === '10004' || envelope.infocode === '10014') return retry('RATE_LIMIT', 'Amap rate limited the request', delay);
    if (envelope.status === '0') return branch('providerRejected', classifyFailure(envelope.infocode), 'Amap rejected the geocoding request');
    if (envelope.infocode !== '10000' || !Array.isArray(envelope.geocodes) || !/^(0|[1-9]\d*)$/.test(String(envelope.count)) || Number(envelope.count) !== envelope.geocodes.length || envelope.geocodes.length > 100) return branch('invalidResponse', 'PROTOCOL', 'Amap returned an invalid response');
    const locations: GeocodedLocation[] = [];
    for (const raw of envelope.geocodes) {
      const location = decodeLocation(raw);
      if (!location) return branch('invalidResponse', 'PROTOCOL', 'Amap returned an invalid response');
      locations.push(location);
    }
    return newQueryBranch('geocoded', { locations });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidAPIKey(key: unknown): key is string { return typeof key === 'string' && key.length > 0 && key.length <= 256 && /^[!-~]+$/.test(key); }
function stringOrEmpty(value: unknown): string { return typeof value === 'string' ? value : ''; }
function decodeLocation(value: unknown): GeocodedLocation | undefined {
  if (!isRecord(value) || typeof value.location !== 'string' || typeof value.formatted_address !== 'string' || typeof value.adcode !== 'string' || typeof value.level !== 'string') return undefined;
  const coordinates = value.location.split(',');
  if (coordinates.length !== 2 || !coordinates.every(part => part !== '' && Number.isFinite(Number(part)))) return undefined;
  const longitude = Number(coordinates[0]); const latitude = Number(coordinates[1]);
  if (longitude < -180 || longitude > 180 || latitude < -90 || latitude > 90) return undefined;
  return { formattedAddress: value.formatted_address, province: stringOrEmpty(value.province), city: stringOrEmpty(value.city), district: stringOrEmpty(value.district), adcode: value.adcode, longitude, latitude, level: value.level };
}
function retryAfter(headers: ProviderResponse['headers'] | undefined): number { const value = headers?.['retry-after']; return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? ''); }
function classifyFailure(code: string): Failure['kind'] {
  if (code === '10001' || code === '10013') return 'AUTHENTICATION';
  if (['10002', '10005', '10007', '10009', '10012'].includes(code)) return 'AUTHORIZATION';
  if (code === '10003' || code === '10010') return 'QUOTA_EXHAUSTED';
  if (code === '20000' || code === '20001') return 'VALIDATION';
  return 'PROVIDER_REJECTION';
}
function httpRejection(status: number): QueryAttempt<GeocodeAddressOutput> { return branch('providerRejected', 'PROVIDER_REJECTION', `Amap rejected the request with HTTP ${status}`); }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<GeocodeAddressOutput> { return newQueryBranch(id, emptyOutput, {}, { provider: 'amap', operation: 'geocodeAddress', kind, message }); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<GeocodeAddressOutput> { return newQueryRetry({ provider: 'amap', operation: 'geocodeAddress', kind, message }, delay); }
