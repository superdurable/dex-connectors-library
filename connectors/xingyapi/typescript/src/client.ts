// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetInstanceOnlineStatusDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetInstanceOnlineStatusInput, GetInstanceOnlineStatusOutput } from './generated/models.js';

const endpoint = 'https://api.xingyapi.com/api/personal/getInfo';
const emptyOutput: GetInstanceOnlineStatusOutput = { isOnline: false, accountID: '' };

/** Optional provider transport for deterministic tests. */
export interface XingyAPIClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve the API Key for each instance status Query. */
export class XingyAPIClient {
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate request limits without contacting the provider. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: XingyAPIClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('XingyAPI configuration and credentials are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('XingyAPI request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the instance status Query. */
  getInstanceOnlineStatus(): GetInstanceOnlineStatusOperation { return new GetInstanceOnlineStatusOperation(this); }
}

/** Read one managed Enterprise WeChat instance's login status. */
export class GetInstanceOnlineStatusOperation implements Query<GetInstanceOnlineStatusInput, GetInstanceOnlineStatusOutput> {
  /** Bind a validated provider client. */ constructor(private readonly client: XingyAPIClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetInstanceOnlineStatusDefinition; }
  /** Classify one provider attempt. */
  async invoke(call: Call, input: GetInstanceOnlineStatusInput): Promise<QueryAttempt<GetInstanceOnlineStatusOutput>> {
    if (!input || typeof input.instanceGUID !== 'string' || !/^inst_[A-Za-z0-9_-]{1,128}$/.test(input.instanceGUID)) return branch('defect', 'VALIDATION');
    let apiKey = '';
    try { const fields = await this.client.credentials.resolve(call, call.context.cancellationSignal); apiKey = fields.apiKey instanceof SecretString ? fields.apiKey.reveal() : ''; }
    catch { return branch('defect', 'AUTHENTICATION'); }
    if (!apiKey || apiKey.length > 1024 || !/^[!-~]+$/.test(apiKey)) return branch('defect', 'AUTHENTICATION');
    let response: ProviderResponse; let tooLarge = false;
    try {
      response = await this.client.send({ url: endpoint, method: 'POST', headers: { 'X-Nebula-Key': apiKey, 'Content-Type': 'application/json', Accept: 'application/json' }, body: new TextEncoder().encode(JSON.stringify({ instance_guid: input.instanceGUID })), timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    } catch (error) {
      if (!(error instanceof BodyTooLargeError) || error.status === undefined) return retry('AVAILABILITY');
      response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }; tooLarge = true;
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    const retryAfterMs = parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', retryAfterMs);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', retryAfterMs);
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (tooLarge || response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let raw = ''; let document: unknown;
    try { raw = new TextDecoder('utf-8', { fatal: true }).decode(response.body); document = JSON.parse(raw) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.ok !== 'boolean' || !Number.isInteger(document.code)) return branch('invalidResponse', 'PROTOCOL');
    if (document.code === 50001 && !document.ok && document.message === 'no account is currently logged in on this device') return newQueryBranch('checked', emptyOutput);
    if (document.code === 50000) return retry('AVAILABILITY', retryAfterMs);
    if (document.code !== 0) return branch('providerRejected', document.code === 401 ? 'AUTHENTICATION' : document.code === 40301 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (!document.ok || !isRecord(document.data)) return branch('invalidResponse', 'PROTOCOL');
    const match = /"uin"\s*:\s*(?:"([0-9]{1,20})"|([0-9]{1,20}))(?![0-9])/.exec(raw);
    const accountID = match?.[1] ?? match?.[2] ?? '';
    if (!accountID || !/^[0-9]{1,20}$/.test(accountID)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('checked', { isOnline: accountID !== '0', accountID: accountID === '0' ? '' : accountID });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'xingyapi', operation: 'getInstanceOnlineStatus', kind, message: 'XingyAPI instance status failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetInstanceOnlineStatusOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetInstanceOnlineStatusOutput> { return newQueryRetry(failure(kind), delay); }
