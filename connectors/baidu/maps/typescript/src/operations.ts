// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { parseRetryAfter } from '@superdurable/connectors-sdk/providerhttp';
import { newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { GeocodeAddressDefinition } from './generated/definitions.js';
import type { GeocodeAddressInput, GeocodeAddressOutput } from './generated/models.js';
import type { BaiduMapsClient, GeocodeResponse } from './client.js';

const emptyOutput: GeocodeAddressOutput = { longitude: 0, latitude: 0, precise: false, confidence: 0, comprehension: 0, level: '' };
const encoder = new TextEncoder();
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'baidu-maps', operation: 'geocodeAddress', message }; }
function branch(name: string, kind: Failure['kind'], message: string): QueryAttempt<GeocodeAddressOutput> {
  return newQueryBranch(name, emptyOutput, zeroReceipt(), failure(kind, message));
}
function retry(kind: Failure['kind'], message: string, retryAfter = 0): QueryAttempt<GeocodeAddressOutput> {
  return newQueryRetry(failure(kind, message), retryAfter);
}
function providerKind(status: number): Failure['kind'] {
  if (status === 2) return 'VALIDATION';
  if (status === 3 || status === 102 || status === 240 || (status >= 200 && status <= 299)) return 'AUTHORIZATION';
  if (status === 4 || (status >= 300 && status <= 399)) return 'QUOTA_EXHAUSTED';
  if (status === 5 || status === 101) return 'AUTHENTICATION';
  return 'PROVIDER_REJECTION';
}
function decode(body: Uint8Array): { status: number; output: GeocodeAddressOutput } {
  const payload: unknown = JSON.parse(new TextDecoder().decode(body));
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) throw new TypeError('invalid Baidu Maps envelope');
  const envelope = payload as Record<string, unknown>;
  if (!Number.isInteger(envelope.status)) throw new TypeError('invalid Baidu Maps status');
  const status = envelope.status as number;
  if (status !== 0) return { status, output: emptyOutput };
  const result = envelope.result;
  if (!result || typeof result !== 'object' || Array.isArray(result)) throw new TypeError('invalid Baidu Maps result');
  const fields = result as Record<string, unknown>;
  const location = fields.location;
  if (!location || typeof location !== 'object' || Array.isArray(location)) throw new TypeError('invalid Baidu Maps location');
  const point = location as Record<string, unknown>;
  const longitude = point.lng, latitude = point.lat, precise = fields.precise, confidence = fields.confidence, comprehension = fields.comprehension;
  if (typeof longitude !== 'number' || !Number.isFinite(longitude) || longitude < -180 || longitude > 180 ||
      typeof latitude !== 'number' || !Number.isFinite(latitude) || latitude < -90 || latitude > 90 ||
      (precise !== 0 && precise !== 1) || !Number.isInteger(confidence) || (confidence as number) < 0 || (confidence as number) > 100 ||
      !Number.isInteger(comprehension) || (comprehension as number) < 0 || (comprehension as number) > 100 ||
      (fields.level !== undefined && fields.level !== null && typeof fields.level !== 'string')) throw new TypeError('invalid Baidu Maps values');
  return { status, output: { longitude, latitude, precise: precise === 1, confidence: confidence as number, comprehension: comprehension as number, level: typeof fields.level === 'string' ? fields.level : '' } };
}

/** Baidu Maps address geocoding query. */
export class GeocodeAddressOperation implements Query<GeocodeAddressInput, GeocodeAddressOutput> {
  /** Bind the provider client. */ constructor(private readonly client: BaiduMapsClient) {}
  /** Return the generated operation definition. */ definition() { return GeocodeAddressDefinition; }
  /** Geocode one address and classify the provider outcome. */
  async invoke(call: Call, input: GeocodeAddressInput): Promise<QueryAttempt<GeocodeAddressOutput>> {
    if (typeof input.address !== 'string' || !input.address.trim() || encoder.encode(input.address).length > 128 ||
        (input.city !== undefined && (typeof input.city !== 'string' || encoder.encode(input.city).length > 128)))
      return branch('defect', 'VALIDATION', 'address or city is invalid');
    const accessKey = await this.client.resolveAccessKey(call);
    if (!accessKey) return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    let response: GeocodeResponse;
    try { response = await this.client.requestGeocode(call, accessKey, input); }
    catch { return retry('AVAILABILITY', 'Baidu Maps request failed'); }
    const retryAfterHeader = response.headers['retry-after'];
    const retryAfter = parseRetryAfter(Array.isArray(retryAfterHeader) ? retryAfterHeader[0] ?? '' : retryAfterHeader ?? '') || 0;
    if (response.status === 429) return retry('RATE_LIMIT', 'Baidu Maps rate limited the request', retryAfter);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', 'Baidu Maps is temporarily unavailable', retryAfter);
    if (response.status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `Baidu Maps rejected the request with HTTP ${response.status}`);
    if (response.isTooLarge) return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Baidu Maps response exceeded the configured limit');
    try {
      const decoded = decode(response.body);
      if (decoded.status === 1) return retry('AVAILABILITY', 'Baidu Maps reported a server error', retryAfter);
      if (decoded.status !== 0) return branch('providerRejected', providerKind(decoded.status), 'Baidu Maps rejected the geocoding request');
      return newQueryBranch('geocoded', decoded.output, zeroReceipt());
    } catch { return branch('invalidResponse', 'PROTOCOL', 'Baidu Maps returned an invalid response'); }
  }
}
