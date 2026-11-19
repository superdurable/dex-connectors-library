// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetAccountStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetAccountStatusInput, GetAccountStatusOutput } from './generated/models.js';

const endpoint = 'https://api.istero.com/resource/v1/isas/mine/information';
const emptyOutput: GetAccountStatusOutput = { isid: 0, todayUsed: 0, todayRemaining: 0 };

/** Optional provider sender for deterministic ISAS tests. */
export interface IsTeroClientOptions {
  /** Sender; production uses the Connector SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Resolve current ISAS credentials and read daily quota counters. */
export class IsTeroClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate request limits without contacting ISAS. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: IsTeroClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('ISAS configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('ISAS request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the read-only account-status Query. */ getAccountStatus(): GetAccountStatusOperation { return new GetAccountStatusOperation(this); }

  /** Send one authenticated account request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; isid: string; retryAfterMs: number }> {
    let apiKey = '';
    let isid = '';
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
      isid = credentials.isid;
    } catch { return { error: 'credential', isid, retryAfterMs: 0 }; }
    if (!apiKey || apiKey.length > 512 || !/^[!-~]+$/.test(apiKey) || !/^[1-9][0-9]{0,17}$/.test(isid)) return { error: 'credential', isid, retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${endpoint}?isid=${encodeURIComponent(isid)}`, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', isid, retryAfterMs: 0 };
      return { error: 'transport', isid, retryAfterMs: 0 };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, isid, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '') };
  }
}

/** Query free ISAS daily call counters without storing credentials in Flow state. */
export class GetAccountStatusOperation implements Query<GetAccountStatusInput, GetAccountStatusOutput> {
  /** Bind a validated ISAS client. */ constructor(private readonly client: IsTeroClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetAccountStatusDefinition; }
  /** Classify the provider's account response. */
  async invoke(call: Call): Promise<QueryAttempt<GetAccountStatusOutput>> {
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || !Number.isInteger(document.code)) return branch('invalidResponse', 'PROTOCOL');
    if (document.code === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (document.code === 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (document.code === 403) return branch('providerRejected', 'AUTHENTICATION');
    if (document.code === 502) return branch('providerRejected', 'AUTHORIZATION');
    if (document.code !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    const data = isRecord(document.data) ? document.data : undefined;
    if (status !== 200 || !data || !Number.isSafeInteger(data.isid) || String(data.isid) !== exchange.isid || !Number.isSafeInteger(data.today_used) || (data.today_used as number) < 0 || !Number.isSafeInteger(data.today_remaining) || (data.today_remaining as number) < 0) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('retrieved', { isid: data.isid as number, todayUsed: data.today_used as number, todayRemaining: data.today_remaining as number });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'istero', operation: 'getAccountStatus', kind, message: 'ISAS account status request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetAccountStatusOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetAccountStatusOutput> { return newQueryRetry(failure(kind), delay); }
