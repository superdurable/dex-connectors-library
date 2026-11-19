// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCurrentWeatherDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetCurrentWeatherInput, GetCurrentWeatherOutput } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface GeovisearthClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
const emptyWeather: GetCurrentWeatherOutput = { areaCode: '', weather: '', weatherCode: '', temperatureCelsius: 0, relativeHumidityPercent: 0, windDirection: '', windForce: '', surfacePressureHpa: 0, observedAt: '' };

/** Resolve GEOVIS Earth credentials for each weather lookup. */
export class GeovisearthClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: GeovisearthClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('GEOVIS Earth configuration and credentials are required');
    this.baseUrl = config.baseUrl || 'https://api.open.geovisearth.com';
    const origin = new URL(this.baseUrl);
    if (origin.protocol !== 'https:' || origin.hostname !== 'api.open.geovisearth.com' || origin.pathname !== '/' || origin.search || origin.hash || origin.username || origin.password) throw new TypeError('GEOVIS Earth origin is invalid');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('GEOVIS Earth request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the current-weather Query. */ getCurrentWeather(): GetCurrentWeatherOperation { return new GetCurrentWeatherOperation(this); }

  /** Send one bounded weather request without exposing its query-string token. */
  async fetchWeather(call: Call, location: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let token = '';
    try { const resolved = await this.credentials.resolve(call, call.context.cancellationSignal); token = resolved.token instanceof SecretString ? resolved.token.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!token || token.length > 512 || /[^\x21-\x7e]/.test(token)) return { error: 'credential', retryAfterMs: 0 };
    const url = new URL('/v2/cn/area/basic', this.baseUrl);
    url.searchParams.set('location', location);
    url.searchParams.set('token', token);
    const request: ProviderRequest = { url: url.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Get one current-weather observation through GEOVIS Earth. */
export class GetCurrentWeatherOperation implements Query<GetCurrentWeatherInput, GetCurrentWeatherOutput> {
  /** Bind the provider client. */ constructor(private readonly client: GeovisearthClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetCurrentWeatherDefinition; }
  /** Classify one provider response with the Go connector contract. */
  async invoke(call: Call, input: GetCurrentWeatherInput): Promise<QueryAttempt<GetCurrentWeatherOutput>> {
    if (typeof input?.location !== 'string' || !isValidLocation(input.location)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.fetchWeather(call, input.location);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const data = wire as Record<string, unknown>;
    if (!Number.isInteger(data.status)) return branch('invalidResponse', 'PROTOCOL');
    if (data.status !== 0) return branch('providerRejected', 'PROVIDER_REJECTION');
    const observation = object(data.result);
    const date = object(data.date);
    const place = object(data.location);
    if (!observation || !date || !place || typeof observation.wp !== 'string' || !observation.wp || typeof observation.wp_code !== 'string' || !observation.wp_code || !Number.isFinite(observation.tem) || !Number.isInteger(observation.rh) || (observation.rh as number) < 0 || (observation.rh as number) > 100 || typeof observation.wd_desc !== 'string' || !observation.wd_desc || typeof observation.ws_desc !== 'string' || !observation.ws_desc || !Number.isFinite(observation.prs) || (observation.prs as number) <= 0 || typeof date.time !== 'string' || !isValidTimestamp(date.time)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('fetched', { areaCode: typeof place.areaCode === 'string' ? place.areaCode : '', weather: observation.wp, weatherCode: observation.wp_code, temperatureCelsius: observation.tem as number, relativeHumidityPercent: observation.rh as number, windDirection: observation.wd_desc, windForce: observation.ws_desc, surfacePressureHpa: observation.prs as number, observedAt: date.time });
  }
}

function object(value: unknown): Record<string, unknown> | undefined { return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined; }
function isValidLocation(location: string): boolean {
  if (/^WTX_CH[0-9]{9}$/.test(location)) return true;
  const coordinates = location.split(',');
  if (coordinates.length !== 2 || location.trim() !== location) return false;
  const longitude = Number(coordinates[0]); const latitude = Number(coordinates[1]);
  return coordinates.every(coordinate => coordinate?.trim() !== '' && Number.isFinite(Number(coordinate))) && longitude >= -180 && longitude <= 180 && latitude >= -90 && latitude <= 90;
}
function isValidTimestamp(value: string): boolean {
  if (!/^\d{14}$/.test(value)) return false;
  const year = Number(value.slice(0, 4)), month = Number(value.slice(4, 6)), day = Number(value.slice(6, 8)), hour = Number(value.slice(8, 10)), minute = Number(value.slice(10, 12)), second = Number(value.slice(12, 14));
  const timestamp = new Date(Date.UTC(year, month - 1, day, hour, minute, second));
  return timestamp.getUTCFullYear() === year && timestamp.getUTCMonth() + 1 === month && timestamp.getUTCDate() === day && timestamp.getUTCHours() === hour && timestamp.getUTCMinutes() === minute && timestamp.getUTCSeconds() === second;
}
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'geovisearth', operation: 'getCurrentWeather', kind, message: 'GEOVIS Earth weather query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetCurrentWeatherOutput> { return newQueryBranch(name, emptyWeather, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetCurrentWeatherOutput> { return newQueryRetry(failure(kind), delay); }
