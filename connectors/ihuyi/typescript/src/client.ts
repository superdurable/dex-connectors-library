// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { isIP } from 'node:net';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { LookupIPLocationDefinition } from './generated/definitions.js';
import type { Config, Credentials, LookupIPLocationInput, LookupIPLocationOutput } from './generated/models.js';

/** Test seams for transport and time-bound signatures. */
export interface IhuyiClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; now?: () => Date; }
const emptyLocation: LookupIPLocationOutput = { country: '', province: '', city: '', district: '', operator: '' };

/** Resolve Ihuyi credentials for each public IPv4 lookup. */
export class IhuyiClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  /** Validate local request limits without contacting Ihuyi. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: IhuyiClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Ihuyi configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('Ihuyi limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the read-only IP location Query. */ lookupIPLocation(): LookupIPLocationOperation { return new LookupIPLocationOperation(this); }
  /** Send one signed location request. */
  async fetchLocation(call: Call, ipAddress: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let apiId = '', apiKey = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiId = credentials.apiId;
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validCredential(apiId) || !validCredential(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const timestamp = String(Math.floor(this.now().getTime() / 1000));
    const password = createHash('md5').update(apiId + apiKey + timestamp).digest('hex');
    const form = new URLSearchParams({ account: apiId, password, ip: ipAddress, time: timestamp });
    const request: ProviderRequest = { url: 'https://api.ihuyi.com/appdev/ip/Submit.json', method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json' }, body: new TextEncoder().encode(form.toString()), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Classify signed Ihuyi IP responses with the Go Query contract. */
export class LookupIPLocationOperation implements Query<LookupIPLocationInput, LookupIPLocationOutput> {
  /** Bind the provider client. */ constructor(private readonly client: IhuyiClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return LookupIPLocationDefinition; }
  /** Invoke one public IPv4 lookup. */
  async invoke(call: Call, input: LookupIPLocationInput): Promise<QueryAttempt<LookupIPLocationOutput>> {
    if (!isPublicIPv4(input?.ipAddress)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.fetchLocation(call, input.ipAddress);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire) || !Number.isInteger(wire.code)) return branch('invalidResponse', 'PROTOCOL');
    if (wire.code !== 2) return branch('providerRejected', [405, 4050, 40501, 40502].includes(wire.code as number) ? 'AUTHENTICATION' : [400, 4052].includes(wire.code as number) ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (!isObject(wire.result) || typeof wire.result.porovince !== 'string' || !wire.result.porovince.trim() || typeof wire.result.city !== 'string' || !wire.result.city.trim()) return branch('invalidResponse', 'PROTOCOL');
    const result = wire.result;
    return newQueryBranch('found', { country: typeof result.country === 'string' ? result.country : '', province: result.porovince as string, city: result.city as string, district: typeof result.district === 'string' ? result.district : '', operator: typeof result.operator === 'string' ? result.operator : '' });
  }
}
function isPublicIPv4(value: unknown): value is string {
  if (typeof value !== 'string' || isIP(value) !== 4) return false;
  const octets = value.split('.').map(Number);
  const first = octets[0] ?? 0, second = octets[1] ?? 0;
  return first !== 0 && first !== 10 && first !== 127 && first < 224 && !(first === 169 && second === 254) && !(first === 172 && second >= 16 && second <= 31) && !(first === 192 && second === 168);
}
function validCredential(value: string): boolean { return !!value && value.length <= 1024 && value.trim() === value && [...value].every(character => character.charCodeAt(0) > 32 && character.charCodeAt(0) < 127); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'ihuyi', operation: 'lookupIPLocation', kind, message: 'Ihuyi IP location query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(name, emptyLocation, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind), delay); }
