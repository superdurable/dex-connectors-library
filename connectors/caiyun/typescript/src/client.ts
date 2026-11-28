// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, randomBytes } from 'node:crypto';
import { isIP } from 'node:net';
import { newQueryBranch, newQueryRetry, SecretString, zeroReceipt, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCurrentWeatherDefinition } from './generated/definitions.js';
import type { Config, CredentialSource, GetCurrentWeatherInput, GetCurrentWeatherOutput } from './generated/models.js';

/** Dependencies for approved transports and deterministic signed requests. */
export interface CaiyunClientOptions {
  /** SDK transport when omitted. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Signing clock; the system clock is used by default. */ now?: () => Date;
  /** Sixteen cryptographically random signing bytes; randomBytes is used by default. */ nonce?: () => Uint8Array;
}

/** Caiyun Weather API v2.6 realtime client. */
export class CaiyunClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly nonce: () => Uint8Array;

  /** Validate the weather origin and limits without contacting Caiyun. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: CaiyunClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Caiyun configuration and credential source are required');
    const url = new URL(config.baseUrl ?? 'https://api.caiyunapp.com');
    const hostname = url.hostname.toLowerCase();
    const isLoopback = hostname === 'localhost' || (isIP(hostname) === 4 && hostname.startsWith('127.')) || hostname === '[::1]';
    if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && isLoopback)) || url.username || url.password || url.search || url.hash || url.pathname !== '/')
      throw new TypeError('Caiyun API URL must be an HTTPS origin or loopback test origin');
    this.baseURL = url.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0)
      throw new TypeError('Caiyun request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.nonce = options.nonce ?? (() => randomBytes(16));
  }

  /** Bind the getCurrentWeather Query. */ getCurrentWeather(): GetCurrentWeatherOperation { return new GetCurrentWeatherOperation(this); }

  /** Resolve credentials and send one signed realtime weather request. */
  async requestWeather(call: Call, input: GetCurrentWeatherInput): Promise<{ response?: ProviderResponse; observation?: GetCurrentWeatherOutput; tooLarge?: boolean; credentialFailed?: boolean; transportFailed?: boolean; nonceFailed?: boolean }> {
    let appKey: string;
    let appSecret: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      appKey = credentials.appKey instanceof SecretString ? credentials.appKey.reveal() : '';
      appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    } catch { return { credentialFailed: true }; }
    if (!validAppKey(appKey) || !validAppSecret(appSecret)) return { credentialFailed: true };
    let nonceBytes: Uint8Array;
    try { nonceBytes = this.nonce(); }
    catch { return { nonceFailed: true }; }
    if (nonceBytes.length !== 16) return { nonceFailed: true };
    const nonce = Buffer.from(nonceBytes).toString('hex');
    const timestamp = Math.floor(this.now().getTime() / 1000).toString();
    const path = `/v2.6/${appKey}/${fixedDecimal(input.longitude)},${fixedDecimal(input.latitude)}/realtime`;
    const canonical = ['GET', path, '', appKey, nonce, timestamp].join(':');
    const signature = createHmac('sha256', appSecret).update(canonical).digest('base64').replaceAll('+', '-').replaceAll('/', '_');
    const request: ProviderRequest = {
      url: this.baseURL + path, method: 'GET',
      headers: { Accept: 'application/json', 'x-cy-nonce': nonce, 'x-cy-timestamp': timestamp, 'x-cy-signature': signature },
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.baseURL.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal,
    };
    try {
      const response = await this.send(request);
      if (response.status !== 200) return { response };
      if (response.body.length > this.maxResponseBytes) return { response, tooLarge: true };
      const observation = decodeObservation(response.body, appKey, appSecret);
      return observation ? { response, observation } : { response };
    }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined)
        return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, tooLarge: true };
      return { transportFailed: true };
    }
  }

  /** Interpret Retry-After with the signing clock. */
  retryDelay(response: ProviderResponse): number {
    const value = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '', this.now()) || 0;
  }
}

/** Read one realtime weather observation. */
export class GetCurrentWeatherOperation implements Query<GetCurrentWeatherInput, GetCurrentWeatherOutput> {
  /** Bind the client. */ constructor(private readonly client: CaiyunClient) {}
  /** Return the generated Query definition. */ definition() { return GetCurrentWeatherDefinition; }
  /** Classify provider, retry, and malformed weather outcomes. */
  async invoke(call: Call, input: GetCurrentWeatherInput): Promise<QueryAttempt<GetCurrentWeatherOutput>> {
    if (!inRange(input.longitude, -180, 180) || !inRange(input.latitude, -90, 90))
      return terminal('defect', 'VALIDATION', 'longitude or latitude is invalid');
    const result = await this.client.requestWeather(call, input);
    if (result.credentialFailed) return terminal('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    if (result.nonceFailed) return newQueryRetry(failure('AVAILABILITY', 'Caiyun request nonce could not be generated'));
    if (result.transportFailed || !result.response) return newQueryRetry(failure('AVAILABILITY', 'Caiyun request failed; a prior attempt may consume another plan call'));
    const response = result.response;
    const retryAfter = this.client.retryDelay(response);
    if (response.status === 408 || response.status >= 500) return newQueryRetry(failure('AVAILABILITY', 'Caiyun is temporarily unavailable'), retryAfter);
    if (response.status === 429) return newQueryRetry(failure('RATE_LIMIT', 'Caiyun rate or quota limit reached'), retryAfter);
    if (response.status !== 200) return terminal('providerRejected', classifyHTTP(response.status), 'Caiyun rejected the weather request');
    if (result.tooLarge) return terminal('invalidResponse', 'RESPONSE_TOO_LARGE', 'Caiyun response exceeded the configured limit');
    if (!result.observation) return terminal('invalidResponse', 'PROTOCOL', 'Caiyun returned invalid realtime weather data');
    return newQueryBranch('fetched', result.observation, zeroReceipt());
  }
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function decodeObservation(body: Uint8Array, appKey: string, appSecret: string): GetCurrentWeatherOutput | undefined {
  let document: unknown;
  try {
    document = JSON.parse(new TextDecoder('utf-8', { ignoreBOM: true }).decode(body), (_name: string, value: unknown) => {
      if (!isObject(value)) return value;
      return Object.fromEntries(Object.entries(value).map(([name, field]) => [name.toLowerCase(), field]));
    });
  } catch { return undefined; }
  if (!isObject(document) || document.status !== 'ok' || !isObject(document.result)) return undefined;
  const observation = document.result.realtime;
  if (!isObject(observation) || observation.status !== 'ok' || !isObject(observation.wind)) return undefined;
  const { skycon, temperature, humidity, wind } = observation;
  if (!inRange(temperature, -150, 150) || !inRange(humidity, 0, 1) || !inRange(wind.speed, 0, 200)
      || typeof skycon !== 'string' || !skycon || skycon.includes(appKey) || skycon.includes(appSecret)) return undefined;
  return { temperatureCelsius: temperature, relativeHumidity: humidity, skyCondition: skycon, windSpeedMetersPerSecond: wind.speed };
}

function inRange(value: unknown, min: number, max: number): value is number { return typeof value === 'number' && Number.isFinite(value) && value >= min && value <= max; }
function validAppKey(value: string): boolean { return value.length > 0 && value.length <= 256 && /^[A-Za-z0-9_-]+$/.test(value); }
function validAppSecret(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[!-~]+$/.test(value); }
function classifyHTTP(status: number): Failure['kind'] { return [400, 401].includes(status) ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION'; }
function failure(kind: Failure['kind'], message: string): Failure { return { kind, provider: 'caiyun', operation: 'getCurrentWeather', message }; }
function terminal(branch: string, kind: Failure['kind'], message: string): QueryAttempt<GetCurrentWeatherOutput> {
  return newQueryBranch(branch, { temperatureCelsius: 0, relativeHumidity: 0, skyCondition: '', windSpeedMetersPerSecond: 0 }, zeroReceipt(), failure(kind, message));
}
function fixedDecimal(value: number): string {
  if (Object.is(value, -0)) return '-0';
  const text = value.toString();
  if (!/[eE]/.test(text)) return text;
  const [mantissa, exponentText] = text.split(/[eE]/);
  const exponent = Number(exponentText);
  const isNegative = mantissa?.startsWith('-') ?? false;
  const digits = (isNegative ? mantissa!.slice(1) : mantissa!).replace('.', '');
  const decimalPosition = (isNegative ? mantissa!.slice(1) : mantissa!).split('.')[0]!.length + exponent;
  const expanded = decimalPosition <= 0 ? `0.${'0'.repeat(-decimalPosition)}${digits}` :
    decimalPosition >= digits.length ? digits + '0'.repeat(decimalPosition - digits.length) :
      `${digits.slice(0, decimalPosition)}.${digits.slice(decimalPosition)}`;
  return (isNegative ? '-' : '') + expanded;
}
