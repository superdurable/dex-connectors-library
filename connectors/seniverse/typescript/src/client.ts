// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCurrentWeatherDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetCurrentWeatherInput, GetCurrentWeatherOutput } from './generated/models.js';

const emptyOutput: GetCurrentWeatherOutput = { locationId: '', locationName: '', country: '', weatherText: '', weatherCode: '', temperature: '', temperatureUnit: '', lastUpdate: '' };
const supportedLanguages = new Set(['zh-Hans', 'zh-Hant', 'en', 'ja', 'de', 'fr', 'es', 'pt', 'hi', 'id', 'ru', 'th', 'ar']);

/** Optional request sender for provider tests. */
export interface SeniverseClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve Seniverse credentials for each current-weather lookup. */
export class SeniverseClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly language: string;
  private readonly unit: string;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate weather configuration without contacting Seniverse. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SeniverseClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Seniverse configuration and credentials are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.seniverse.com');
    const origin = new URL(this.baseUrl);
    if (origin.pathname !== '/' || origin.search || origin.hash) throw new TypeError('Seniverse base URL must be an origin');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    this.language = config.language || 'zh-Hans';
    this.unit = config.unit || 'c';
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || !supportedLanguages.has(this.language) || !['c', 'f'].includes(this.unit)) throw new TypeError('Seniverse configuration is invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the current-weather Query. */ getCurrentWeather(): GetCurrentWeatherOperation { return new GetCurrentWeatherOperation(this); }

  /** Send one bounded current-weather request. */
  async fetchWeather(call: Call, location: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number; unit: string }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0, unit: this.unit }; }
    const privateKey = credentials.privateKey instanceof SecretString ? credentials.privateKey.reveal() : '';
    if (!privateKey || privateKey.length > 512 || !/^[!-~]+$/.test(privateKey)) return { error: 'credential', retryAfterMs: 0, unit: this.unit };
    const query = new URLSearchParams({ key: privateKey, location, language: this.language, unit: this.unit });
    let response: ProviderResponse;
    try { response = await this.send({ url: this.baseUrl + '/v3/weather/now.json?' + query.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseUrl.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal }); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0, unit: this.unit };
      return { error: 'transport', retryAfterMs: 0, unit: this.unit };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')), unit: this.unit };
  }
}

/** Read current weather for one Seniverse location. */
export class GetCurrentWeatherOperation implements Query<GetCurrentWeatherInput, GetCurrentWeatherOutput> {
  /** Bind the provider client. */ constructor(private readonly client: SeniverseClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetCurrentWeatherDefinition; }
  /** Classify one current-weather response. */
  async invoke(call: Call, input: GetCurrentWeatherInput): Promise<QueryAttempt<GetCurrentWeatherOutput>> {
    if (typeof input?.location !== 'string' || !input.location || input.location.trim() !== input.location || input.location.length > 256) return branch('defect', 'VALIDATION');
    const exchange = await this.client.fetchWeather(call, input.location);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (status !== 200) {
      const code = isRecord(wire) && typeof wire.status_code === 'string' ? wire.status_code : '';
      if (code === 'AP010014') return retry('RATE_LIMIT', exchange.retryAfterMs);
      return branch('providerRejected', classifyProviderError(code));
    }
    if (!isRecord(wire) || !Array.isArray(wire.results) || wire.results.length !== 1) return branch('invalidResponse', 'PROTOCOL');
    const result = wire.results[0];
    if (!isRecord(result) || !isRecord(result.location) || !isRecord(result.now)) return branch('invalidResponse', 'PROTOCOL');
    const location = result.location;
    const now = result.now;
    if (typeof location.id !== 'string' || !location.id || typeof location.name !== 'string' || !location.name || typeof now.text !== 'string' || !now.text || typeof now.code !== 'string' || !now.code || typeof now.temperature !== 'string' || !now.temperature || !Number.isFinite(Number(now.temperature)) || typeof result.last_update !== 'string' || !Number.isFinite(Date.parse(result.last_update))) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('fetched', { locationId: location.id, locationName: location.name, country: typeof location.country === 'string' ? location.country : '', weatherText: now.text, weatherCode: now.code, temperature: now.temperature, temperatureUnit: exchange.unit, lastUpdate: result.last_update });
  }
}

function classifyProviderError(code: string): Failure['kind'] {
  if (['AP010003', 'AP010004'].includes(code)) return 'AUTHENTICATION';
  if (['AP010002', 'AP010006', 'AP010008', 'AP010009'].includes(code)) return 'AUTHORIZATION';
  if (['AP010012', 'AP010013'].includes(code)) return 'QUOTA_EXHAUSTED';
  if (['AP010001', 'AP010010', 'AP010011', 'AP010017'].includes(code)) return 'VALIDATION';
  return 'PROVIDER_REJECTION';
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'seniverse', operation: 'getCurrentWeather', kind, message: 'Seniverse current-weather query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetCurrentWeatherOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<GetCurrentWeatherOutput> { return newQueryRetry(failure(kind), delayMs); }
