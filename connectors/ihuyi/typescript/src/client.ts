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
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 65_536;
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
    const address = publicIPv4(input?.ipAddress);
    if (!address) return branch('defect', 'VALIDATION');
    const exchange = await this.client.fetchLocation(call, address);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: Record<string, unknown> | undefined;
    try { wire = fieldsByName(JSON.parse(new TextDecoder().decode(body), readProviderNumber)); }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire) return branch('invalidResponse', 'PROTOCOL');
    const code = wire.code;
    if (typeof code !== 'number' || !Number.isInteger(code)) return branch('invalidResponse', 'PROTOCOL');
    const data = wire.result;
    wire = data == null ? {} : fieldsByName(data);
    if (!wire) return branch('invalidResponse', 'PROTOCOL');
    const values = wire;
    if (['country', 'porovince', 'city', 'district', 'operator'].some(field => values[field] != null && typeof values[field] !== 'string')) return branch('invalidResponse', 'PROTOCOL');

    if (code !== 2) return branch('providerRejected', [405, 4050, 40501, 40502].includes(code) ? 'AUTHENTICATION' : [400, 4052].includes(code) ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (typeof values.porovince !== 'string' || !values.porovince.trim() || typeof values.city !== 'string' || !values.city.trim()) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { country: String(values.country ?? ''), city: values.city, province: values.porovince, district: String(values.district ?? ''), operator: String(values.operator ?? '') });
  }
}
function publicIPv4(value: unknown): string | undefined {
  if (typeof value !== 'string' || value.includes('%')) return undefined;
  const version = isIP(value);
  let address = value;
  if (version === 6) {
    const mapped = /^\[::ffff:([a-f0-9]+):([a-f0-9]+)\]$/.exec(new URL('http://[' + value + ']').hostname);
    if (!mapped) return undefined;
    const high = parseInt(mapped[1]!, 16);
    const low = parseInt(mapped[2]!, 16);
    address = [high >>> 8, high & 255, low >>> 8, low & 255].join('.');
  } else if (version !== 4) return undefined;
  const parts = address.split('.');
  const first = Number(parts[0]);
  const second = Number(parts[1]);
  return !(parts.every(part => part === '0')) && first !== 10 && first !== 127 && !(first >= 224 && first <= 239) && !(first === 169 && second === 254) && !(first === 172 && second >= 16 && second <= 31) && !(first === 192 && second === 168) ? address : undefined;
}
function validCredential(value: string): boolean { return !!value && value.length <= 1024 && value.trim() === value && [...value].every(character => character.charCodeAt(0) > 32 && character.charCodeAt(0) < 127); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'ihuyi', operation: 'lookupIPLocation', kind, message: 'Ihuyi IP location query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<LookupIPLocationOutput> { return newQueryBranch(name, emptyLocation, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<LookupIPLocationOutput> { return newQueryRetry(failure(kind), delay); }

function fieldsByName(value: unknown): Record<string, unknown> | undefined {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return undefined;
  return Object.fromEntries(Object.entries(value).map(([name, field]) => [name.toLowerCase(), field]));
}

// Go decodes the result code into int64 before classifying the provider response.
function readProviderNumber(key: string, value: unknown, context?: { source?: string }): unknown {
  if (key.toLowerCase() !== 'code' || typeof value !== 'number') return value;
  const source = context?.source;
  if (source === undefined) return Number.isSafeInteger(value) ? value : null;
  if (!/^-?\d+$/.test(source) || source.length > 20) return null;
  const integer = BigInt(source);
  return integer >= -(1n << 63n) && integer < (1n << 63n) ? value : null;
}
