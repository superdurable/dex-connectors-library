// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCurrentWeatherDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetCurrentWeatherInput, GetCurrentWeatherOutput } from './generated/models.js';

const emptyOutput: GetCurrentWeatherOutput = { conditionText: '', conditionCode: '', temperatureValue: 0, temperatureUnit: '', validTime: '', attributions: [] };

/** Optional request sender for provider tests. */
export interface QWeatherClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve QWeather API Host and Key for each current-weather lookup. */
export class QWeatherClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate request limits without contacting QWeather. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: QWeatherClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('QWeather configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('QWeather request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the current-weather Query. */ getCurrentWeather(): GetCurrentWeatherOperation { return new GetCurrentWeatherOperation(this); }

  /** Send one bounded weather request to the account-specific API Host. */
  async fetchWeather(call: Call, input: GetCurrentWeatherInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number; apiHost: string; apiKey: string }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0, apiHost: '', apiKey: '' }; }
    const apiHost = credentials.apiHost instanceof SecretString ? credentials.apiHost.reveal() : '';
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!validAPIHost(apiHost) || !apiKey || apiKey.length > 1024 || !/^[!-~]+$/.test(apiKey)) return { error: 'credential', retryAfterMs: 0, apiHost: '', apiKey: '' };
    const path = `/weather/v1/current/${input.latitude.toFixed(6)}/${input.longitude.toFixed(6)}`;
    let response: ProviderResponse;
    try { response = await this.send({ url: `https://${apiHost}${path}`, method: 'GET', headers: { 'X-QW-Api-Key': apiKey, Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0, apiHost, apiKey };
      return { error: 'transport', retryAfterMs: 0, apiHost, apiKey };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')), apiHost, apiKey };
  }
}

/** Read current weather at decimal latitude and longitude. */
export class GetCurrentWeatherOperation implements Query<GetCurrentWeatherInput, GetCurrentWeatherOutput> {
  /** Bind the provider client. */ constructor(private readonly client: QWeatherClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetCurrentWeatherDefinition; }
  /** Classify one weather response. */
  async invoke(call: Call, input: GetCurrentWeatherInput): Promise<QueryAttempt<GetCurrentWeatherOutput>> {
    if (!input || !Number.isFinite(input.latitude) || input.latitude < -90 || input.latitude > 90 || !Number.isFinite(input.longitude) || input.longitude < -180 || input.longitude > 180) return branch('defect', 'VALIDATION');
    const exchange = await this.client.fetchWeather(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status === 429) return retry(isMonthlyLimit(body) ? 'QUOTA_EXHAUSTED' : 'RATE_LIMIT', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !isRecord(wire.condition) || !isRecord(wire.temperature) || !isRecord(wire.metadata)) return branch('invalidResponse', 'PROTOCOL');
    const condition = wire.condition;
    const temperature = wire.temperature;
    const attributions = wire.metadata.attributions;
    if (typeof condition.text !== 'string' || !condition.text || condition.text.includes(exchange.apiKey) || condition.text.includes(exchange.apiHost) || typeof condition.code !== 'string' || !condition.code || typeof temperature.value !== 'number' || !Number.isFinite(temperature.value) || typeof temperature.unit !== 'string' || !temperature.unit || typeof wire.validTime !== 'string' || !Number.isFinite(Date.parse(wire.validTime)) || !Array.isArray(attributions) || attributions.length < 1 || attributions.length > 16 || attributions.some(value => typeof value !== 'string' || !value || value.length > 500 || value.includes(exchange.apiKey) || value.includes(exchange.apiHost))) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { conditionText: condition.text, conditionCode: condition.code, temperatureValue: temperature.value, temperatureUnit: temperature.unit, validTime: wire.validTime, attributions: attributions as string[] });
  }
}

function validAPIHost(host: string): boolean { return host.length > 0 && host.length <= 253 && host !== 'qweatherapi.com' && host.endsWith('.qweatherapi.com') && host.split('.').every(label => label.length > 0 && label.length <= 63 && /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label)); }
function isMonthlyLimit(body: Uint8Array): boolean { try { const wire = JSON.parse(new TextDecoder().decode(body)) as unknown; return isRecord(wire) && isRecord(wire.error) && typeof wire.error.type === 'string' && wire.error.type.toLowerCase().includes('over-monthly-limit'); } catch { return false; } }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'qweather', operation: 'getCurrentWeather', kind, message: 'QWeather current-weather query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetCurrentWeatherOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<GetCurrentWeatherOutput> { return newQueryRetry(failure(kind), delayMs); }
