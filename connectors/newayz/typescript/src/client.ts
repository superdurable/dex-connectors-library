// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCurrentWeatherDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetCurrentWeatherInput, GetCurrentWeatherOutput } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface NewayzClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
const empty: GetCurrentWeatherOutput = { province: '', city: '', cityCode: '', weather: '', temperatureCelsius: '', windDirection: '', windPower: '', humidityPercent: '', reportTime: '' };
/** Resolve WAYZ credentials for each current-weather query. */
export class NewayzClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate settings without contacting WAYZ. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: NewayzClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('WAYZ settings and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('WAYZ limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the weather Query. */ getCurrentWeather(): GetCurrentWeatherOperation { return new GetCurrentWeatherOperation(this); }
  /** Send one bounded request without exposing the query-string credential. */
  async request(call: Call, cityCode: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let accessKey = '';
    try { const resolved = await this.credentials.resolve(call, call.context.cancellationSignal); accessKey = resolved.accessKey instanceof SecretString ? resolved.accessKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!accessKey || accessKey.length > 1024 || /[^\x21-\x7e]/.test(accessKey)) return { error: 'credential', retryAfterMs: 0 };
    const url = new URL('https://api.newayz.com/location/scenario/v1/weather');
    url.searchParams.set('access_key', accessKey); url.searchParams.set('city', cityCode); url.searchParams.set('extensions', 'base');
    const request: ProviderRequest = { url: url.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: parseRetryAfter(header(error.headers ?? {}, 'retry-after')) };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}
/** Query one live weather observation. */
export class GetCurrentWeatherOperation implements Query<GetCurrentWeatherInput, GetCurrentWeatherOutput> {
  /** Bind the provider client. */ constructor(private readonly client: NewayzClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetCurrentWeatherDefinition; }
  /** Classify the provider response using the Go contract. */
  async invoke(call: Call, input: GetCurrentWeatherInput): Promise<QueryAttempt<GetCurrentWeatherOutput>> {
    const cityCode = input?.cityCode;
    if (typeof cityCode !== 'string' || !/^\d{6}$/.test(cityCode)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, cityCode);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let data: Record<string, unknown> | undefined;
    try { data = fieldsByName(JSON.parse(new TextDecoder().decode(body))); } catch { return branch('invalidResponse','PROTOCOL'); }
    if (!data) return branch('invalidResponse','PROTOCOL');
    if (typeof data.status !== 'string' || !data.status || (data.infocode != null && typeof data.infocode !== 'string') || (data.lives != null && !Array.isArray(data.lives))) return branch('invalidResponse','PROTOCOL');
    const observations = (data.lives ?? []) as unknown[];
    let live: Record<string, unknown> = {};
    for (const observation of observations) {
      const fields = observation == null ? {} : fieldsByName(observation);
      if (!fields || ['province','city','adcode','weather','temperature','winddirection','windpower','humidity','reporttime'].some(name => fields[name] != null && typeof fields[name] !== 'string')) return branch('invalidResponse','PROTOCOL');
      live = fields;
    }
    if (data.status !== '1' || data.infocode !== '10000') return branch('providerRejected','PROVIDER_REJECTION');
    if (observations.length !== 1 || live.adcode !== cityCode || !live.city || !live.weather || !live.temperature) return branch('invalidResponse','PROTOCOL');
    const string = (name: string) => String(live[name] ?? '');
    return newQueryBranch('fetched', { province: string('province'), city: string('city'), cityCode, weather: string('weather'), temperatureCelsius: string('temperature'), windDirection: string('winddirection'), windPower: string('windpower'), humidityPercent: string('humidity'), reportTime: string('reporttime') });
  }
}
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'newayz', operation: 'getCurrentWeather', kind, message: 'WAYZ current-weather query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetCurrentWeatherOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetCurrentWeatherOutput> { return newQueryRetry(failure(kind), delay); }

function fieldsByName(value: unknown): Record<string, unknown> | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined;
  return Object.fromEntries(Object.entries(value).map(([name, field]) => [name.toLowerCase(), field]));
}
