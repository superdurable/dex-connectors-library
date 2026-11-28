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
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: parseRetryAfter(header(error.headers ?? {}, 'retry-after')) };
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
    if (!input || typeof input.city !== 'string' || !input.city || /^\p{White_Space}|\p{White_Space}$/u.test(input.city) || new TextEncoder().encode(input.city).length > 128) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getWeather(call, input.city);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    let data: Record<string, unknown> | undefined;
    try { data = fieldsByName(JSON.parse(new TextDecoder().decode(body), readProviderNumber)); } catch { return branch('invalidResponse','PROTOCOL'); }
    if (!data) return branch('invalidResponse','PROTOCOL');
    if (typeof data.error_code !== 'number' || !Number.isInteger(data.error_code)) return branch('invalidResponse','PROTOCOL');
    const result = data.result == null ? {} : fieldsByName(data.result);
    if (!result || (result.city != null && typeof result.city !== 'string')) return branch('invalidResponse','PROTOCOL');
    const real = result.realtime == null ? {} : fieldsByName(result.realtime);
    if (!real || ['info','wid','temperature','humidity','direct','power','aqi'].some(name => real[name] != null && typeof real[name] !== 'string')) return branch('invalidResponse','PROTOCOL');
    if (data.error_code !== 0) return classifyError(data.error_code);
    if (!result.city || typeof result.city !== 'string' || typeof real.info !== 'string' || !real.info || typeof real.wid !== 'string' || !real.wid) return branch('invalidResponse','PROTOCOL');
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

function fieldsByName(value: unknown): Record<string, unknown> | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined;
  return Object.fromEntries(Object.entries(value).map(([name, field]) => [name.toLowerCase(), field]));
}

// Go decodes the result code into int64 before classifying the provider response.
function readProviderNumber(key: string, value: unknown, context?: { source?: string }): unknown {
  if (key.toLowerCase() !== 'error_code' || typeof value !== 'number') return value;
  const source = context?.source;
  if (source === undefined) return Number.isSafeInteger(value) ? value : null;
  if (!/^-?\d+$/.test(source) || source.length > 20) return null;
  const integer = BigInt(source);
  return integer >= -(1n << 63n) && integer < (1n << 63n) ? value : null;
}
