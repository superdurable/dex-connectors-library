// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCurrentWeatherDefinition } from './generated/definitions.js';
import type { Config, Credentials, CurrentWeather, GetCurrentWeatherInput } from './generated/models.js';

const emptyWeather: CurrentWeather = { city: '', condition: '', temperatureCelsius: 0, humidityPercent: 0, updatedAtBeijing: '' };

/** Send one weather request through a caller-owned transport. */
export type WeatherRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional transport dependency for provider tests. */
export interface APISpaceClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: WeatherRequestSender;
}

/** Resolve APISpace credentials for each weather query. */
export class APISpaceClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: WeatherRequestSender;

  /** Validate configuration without contacting APISpace. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: APISpaceClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('APISpace configuration and credential source are required');
    const baseUrl = validateBaseURL(config.baseUrl || 'https://eolink.o.apispace.com');
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('APISpace timeout and response limit must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the current-weather Query. */
  getCurrentWeather(): GetCurrentWeatherOperation { return new GetCurrentWeatherOperation(this); }
}

/** Read current weather for one APISpace city area code. */
export class GetCurrentWeatherOperation implements Query<GetCurrentWeatherInput, CurrentWeather> {
  /** Bind an authenticated APISpace client. */
  constructor(private readonly client: APISpaceClient) {}

  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return GetCurrentWeatherDefinition; }

  /** Send and classify one current-weather request. */
  async invoke(call: Call, input: GetCurrentWeatherInput): Promise<QueryAttempt<CurrentWeather>> {
    if (!/^[A-Za-z0-9]{1,32}$/.test(input.areaCode ?? '')) return branch('defect', 'VALIDATION', 'APISpace area code must be 1 to 32 ASCII letters or digits');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'APISpace API token is unavailable or invalid'); }
    const token = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
    if (!isValidAPIToken(token)) return branch('defect', 'AUTHENTICATION', 'APISpace API token is unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({
        url: `${this.client.baseUrl}/456456/weather/v001/now?areacode=${encodeURIComponent(input.areaCode)}`,
        method: 'GET',
        headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'Authorization-Type': 'apikey', 'X-APISpace-Token': token },
        timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: this.client.baseUrl === 'https://eolink.o.apispace.com' ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        if (error.status === 429 || error.status === 413 || error.status === 408 || error.status >= 500) {
          const early = classifyEarlyStatus(error.status, error.headers ?? {});
          if (early) return early;
        }
        return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'APISpace response exceeded the configured limit');
      }
      return retry('AVAILABILITY', 'APISpace weather request failed');
    }
    const early = classifyEarlyStatus(response.status, response.headers);
    if (early) return early;
    if (response.status !== 200) {
      const kind: Failure['kind'] = response.status === 401 ? 'AUTHENTICATION' : response.status === 403 || response.status === 416 ? 'AUTHORIZATION' : response.status === 400 || response.status === 404 ? 'VALIDATION' : 'PROVIDER_REJECTION';
      return branch('providerRejected', kind, `APISpace rejected weather query with HTTP ${response.status}`);
    }
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'APISpace returned an invalid weather response'); }
    if (!isRecord(document) || !Number.isInteger(document.status)) return branch('invalidResponse', 'PROTOCOL', 'APISpace returned an invalid weather response');
    if (document.status !== 0) return branch('providerRejected', 'PROVIDER_REJECTION', `APISpace rejected weather query with status ${document.status}`);
    const result = document.result;
    if (!isRecord(result) || !isRecord(result.location) || !isRecord(result.realtime)) return branch('invalidResponse', 'PROTOCOL', 'APISpace returned an invalid weather response');
    const location = result.location;
    const realtime = result.realtime;
    if (typeof location.name !== 'string' || !location.name || typeof realtime.text !== 'string' || !realtime.text || typeof realtime.temp !== 'number' || !Number.isFinite(realtime.temp) || !Number.isInteger(realtime.rh) || (realtime.rh as number) < 0 || (realtime.rh as number) > 100 || typeof result.last_update !== 'string' || !result.last_update) return branch('invalidResponse', 'PROTOCOL', 'APISpace returned an invalid weather response');
    return newQueryBranch('found', { city: location.name, condition: realtime.text, temperatureCelsius: realtime.temp, humidityPercent: realtime.rh as number, updatedAtBeijing: result.last_update });
  }
}

function classifyEarlyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<CurrentWeather> | undefined {
  const retryAfterHeader = headers['retry-after'];
  const retryAfterMs = parseRetryAfter(Array.isArray(retryAfterHeader) ? retryAfterHeader[0] ?? '' : retryAfterHeader ?? '');
  if (status === 429 || status === 413) return retry('RATE_LIMIT', 'APISpace rate limited weather requests', retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'APISpace weather is temporarily unavailable', retryAfterMs);
  return undefined;
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidAPIToken(value: string): boolean { return value.length > 0 && value.length <= 2048 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'apispace', operation: 'getCurrentWeather', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<CurrentWeather> { return newQueryBranch(id, emptyWeather, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<CurrentWeather> { return newQueryRetry(failure(kind, message), delayMs); }
