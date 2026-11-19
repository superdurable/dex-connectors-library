// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCurrentWeatherDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetCurrentWeatherInput, GetCurrentWeatherOutput } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface JuheClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve Juhe credentials for each weather query. */
export class JuheClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting Juhe. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: JuheClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Juhe configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://apis.juhe.cn');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 524_288;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Juhe request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the city weather Query. */ getCurrentWeather(): GetCurrentWeatherOperation { return new GetCurrentWeatherOperation(this); }

  /** Send one bounded city weather request. */
  async getWeather(call: Call, city: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let key = '';
    try { const credential = await this.credentials.resolve(call, call.context.cancellationSignal); key = credential.apiKey instanceof SecretString ? credential.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!key || key.length > 512 || /[^\x21-\x7e]/.test(key)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = {
      url: this.baseURL + '/simpleWeather/query', method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json' },
      body: new TextEncoder().encode(new URLSearchParams({ city, key }).toString()),
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal,
    };
    try {
      const response = await this.send(request);
      return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
  }
}

/** Read current conditions for one supported city. */
export class GetCurrentWeatherOperation implements Query<GetCurrentWeatherInput, GetCurrentWeatherOutput> {
  /** Bind the provider client. */ constructor(private readonly client: JuheClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetCurrentWeatherDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: GetCurrentWeatherInput): Promise<QueryAttempt<GetCurrentWeatherOutput>> {
    if (!input || typeof input.city !== 'string' || !input.city.trim() || input.city.trim() !== input.city || new TextEncoder().encode(input.city).length > 128) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getWeather(call, input.city);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const data = wire as { error_code?: unknown; result?: unknown };
    if (!Number.isInteger(data.error_code)) return branch('invalidResponse', 'PROTOCOL');
    if (data.error_code !== 0) return classifyError(data.error_code as number);
    if (!data.result || typeof data.result !== 'object' || Array.isArray(data.result)) return branch('invalidResponse', 'PROTOCOL');
    const result = data.result as { city?: unknown; realtime?: unknown };
    if (!result.realtime || typeof result.realtime !== 'object' || Array.isArray(result.realtime)) return branch('invalidResponse', 'PROTOCOL');
    const real = result.realtime as Record<string, unknown>;
    if (typeof result.city !== 'string' || !result.city || typeof real.info !== 'string' || !real.info || typeof real.wid !== 'string' || !real.wid) return branch('invalidResponse', 'PROTOCOL');
    for (const key of ['temperature', 'humidity', 'direct', 'power', 'aqi']) if (real[key] != null && typeof real[key] !== 'string') return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('fetched', { city: result.city, weatherText: real.info, weatherCode: real.wid, temperatureCelsius: (real.temperature ?? '') as string, humidityPercent: (real.humidity ?? '') as string, windDirection: (real.direct ?? '') as string, windPower: (real.power ?? '') as string, airQualityIndex: (real.aqi ?? '') as string });
  }
}

function classifyError(code: number): QueryAttempt<GetCurrentWeatherOutput> {
  if ([207303, 10014, 10020].includes(code)) return retry('AVAILABILITY');
  if (code === 10011) return retry('RATE_LIMIT');
  if ([10001, 10003, 10009].includes(code)) return branch('providerRejected', 'AUTHENTICATION');
  if ([10002, 10005, 10007, 10008].includes(code)) return branch('providerRejected', 'AUTHORIZATION');
  if ([10012, 10013].includes(code)) return branch('providerRejected', 'QUOTA_EXHAUSTED');
  if ([207301, 207302].includes(code)) return branch('providerRejected', 'VALIDATION');
  return branch('providerRejected', 'PROVIDER_REJECTION');
}
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'juhe', operation: 'getCurrentWeather', kind, message: 'Juhe current weather query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetCurrentWeatherOutput> { return newQueryBranch(name, { city: '', weatherText: '', weatherCode: '', temperatureCelsius: '', humidityPercent: '', windDirection: '', windPower: '', airQualityIndex: '' }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetCurrentWeatherOutput> { return newQueryRetry(failure(kind), delay); }
