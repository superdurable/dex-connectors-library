// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetChargeStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetChargeStatusInput, GetChargeStatusOutput } from './generated/models.js';

/** Optional sender for provider tests. */
export interface PingPPClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Read PingPP provider data with bounded HTTP responses. */
export class PingPPClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate connection settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: PingPPClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('PingPP configuration and credentials are required');

    this.baseURL = validateBaseURL(config.baseUrl || 'https://api.pingxx.com').replace(/\/$/, '');
    if (new URL(this.baseURL).pathname !== '/') throw new TypeError('baseUrl must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs || 10000;
    this.maxResponseBytes = config.maxResponseBytes || 262144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('invalid request limits');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the getChargeStatus Query. */
  getChargeStatus(): GetChargeStatusOperation { return new GetChargeStatusOperation(this); }
  /** Resolve credentials and send one provider request. */
  async request(call: Call, input: GetChargeStatusInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let key = '';
    try { const fields = await this.credentials.resolve(call, call.context.cancellationSignal); key = fields.apiKey instanceof SecretString ? fields.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!key || key.length > 512 || key.split('').some(character => character.charCodeAt(0) <= 32 || character.charCodeAt(0) === 127)) return { error: 'credential', retryAfterMs: 0 };
    const url = `${this.baseURL}/v1/charges/${input.chargeId}`;
    const method = 'GET'; const headers = { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${key}:`).toString('base64')}` }; const body = undefined;
    const providerRequest: ProviderRequest = { url, method, headers, body, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(providerRequest); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Classify PingPP provider replies as Query branches or retries. */
export class GetChargeStatusOperation implements Query<GetChargeStatusInput, GetChargeStatusOutput> {
  /** Bind one validated provider client. */ constructor(private readonly client: PingPPClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetChargeStatusDefinition; }
  /** Execute one read-only provider Query. */
  async invoke(call: Call, input: GetChargeStatusInput): Promise<QueryAttempt<GetChargeStatusOutput>> {
    const branch = (name: string, kind: Failure['kind']): QueryAttempt<GetChargeStatusOutput> => newQueryBranch(name, { chargeId: "", paid: false, refunded: false, reversed: false, liveMode: false }, {}, failure(kind));
    const retry = (kind: Failure['kind'], delay = 0): QueryAttempt<GetChargeStatusOutput> => newQueryRetry(failure(kind), delay);
    const bad = (): QueryAttempt<GetChargeStatusOutput> => branch('invalidResponse', 'PROTOCOL');
    if (typeof input?.chargeId !== 'string' || !/^ch_[A-Za-z0-9_-]{1,125}$/.test(input.chargeId)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response; const retryAfterMs = exchange.retryAfterMs;
    if (status === 429 || status === 403) return retry('RATE_LIMIT', retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', retryAfterMs);
    if (status === 404) return branch('notFound', 'PROVIDER_REJECTION');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 400 ? 'VALIDATION' : 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return bad(); }
    if (!isRecord(wire) || wire.id !== input.chargeId || wire.object !== 'charge' || typeof wire.paid !== 'boolean' || typeof wire.refunded !== 'boolean' || typeof wire.reversed !== 'boolean' || typeof wire.livemode !== 'boolean') return bad();
    return newQueryBranch('fetched', { chargeId: wire.id, paid: wire.paid, refunded: wire.refunded, reversed: wire.reversed, liveMode: wire.livemode });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'pingpp', operation: 'getChargeStatus', kind, message: 'PingPP getChargeStatus failed' }; }
