// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetTokenPlanRemainsDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetTokenPlanRemainsInput, GetTokenPlanRemainsOutput } from './generated/models.js';


const emptyOutput: GetTokenPlanRemainsOutput = { modelGroups: [] };
/** Optional HTTP sender for deterministic provider tests. */
export interface MiniMaxClientOptions { /** Sender; defaults to the Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read the Token Plan usage with private credentials resolved per call. */
export class MiniMaxClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate local settings without contacting MiniMax. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MiniMaxClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('MiniMax configuration and credential source are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://www.minimaxi.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('MiniMax request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only Token Plan Query. */ getTokenPlanRemains(): GetTokenPlanRemainsOperation { return new GetTokenPlanRemainsOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!/^[!-~]+$/.test(apiKey) || apiKey.length > 512) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${this.baseUrl}/v1/token_plan/remains`, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseUrl === 'https://www.minimaxi.com' ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterValue = Object.entries(response.headers).find(([key]) => key.toLowerCase() === 'retry-after')?.[1];
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(Array.isArray(retryAfterValue) ? retryAfterValue[0] ?? '' : retryAfterValue ?? '') };
  }
}
/** Classify MiniMax Token Plan results without retaining secrets or metadata. */
export class GetTokenPlanRemainsOperation implements Query<GetTokenPlanRemainsInput, GetTokenPlanRemainsOutput> {
  /** Bind one validated client. */ constructor(private readonly client: MiniMaxClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetTokenPlanRemainsDefinition; }
  /** Read and validate the Token Plan usage. */
  async invoke(call: Call, _input: GetTokenPlanRemainsInput): Promise<QueryAttempt<GetTokenPlanRemainsOutput>> {
    void _input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || !isRecord(document.base_resp) || !Number.isInteger(document.base_resp.status_code)) return branch('invalidResponse', 'PROTOCOL');
    const statusCode = document.base_resp.status_code as number;
    if (statusCode === 1002 || statusCode === 1039) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (statusCode !== 0) return branch('providerRejected', statusCode === 1004 ? 'AUTHENTICATION' : statusCode === 1008 ? 'QUOTA_EXHAUSTED' : 'PROVIDER_REJECTION');
    if (!Array.isArray(document.model_remains) || document.model_remains.length > 1000) return branch('invalidResponse', 'PROTOCOL');
    const modelGroups: NonNullable<GetTokenPlanRemainsOutput['modelGroups']> = [];
    const seen = new Set<string>();
    for (const group of document.model_remains) {
      if (!isRecord(group) || typeof group.model_name !== 'string' || !group.model_name || group.model_name.length > 256 || !isNonnegativeInteger(group.end_time) || !isNonnegativeInteger(group.weekly_end_time) || seen.has(group.model_name)) return branch('invalidResponse', 'PROTOCOL');
      seen.add(group.model_name);
      const intervalPercent = group.current_interval_remaining_percent;
      const weeklyPercent = group.current_weekly_remaining_percent;
      const intervalStatus = group.current_interval_status;
      const weeklyStatus = group.current_weekly_status;
      if (!isPercent(intervalPercent) || !isPercent(weeklyPercent) || !isOptionalInteger(intervalStatus) || !isOptionalInteger(weeklyStatus)) return branch('invalidResponse', 'PROTOCOL');
      modelGroups.push({ modelName: group.model_name, intervalResetAtMillis: group.end_time, weeklyResetAtMillis: group.weekly_end_time, ...(intervalPercent !== undefined && intervalPercent !== null ? { intervalRemainingPercent: intervalPercent } : {}), ...(weeklyPercent !== undefined && weeklyPercent !== null ? { weeklyRemainingPercent: weeklyPercent } : {}), ...(intervalStatus !== undefined && intervalStatus !== null ? { intervalStatus } : {}), ...(weeklyStatus !== undefined && weeklyStatus !== null ? { weeklyStatus } : {}) });
    }
    return newQueryBranch('fetched', { modelGroups });
  }
}
function isNonnegativeInteger(value: unknown): value is number { return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0; }
function isOptionalInteger(value: unknown): value is number | null | undefined { return value == null || (typeof value === 'number' && Number.isSafeInteger(value)); }
function isPercent(value: unknown): value is number | null | undefined { return value == null || (typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 1000); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'minimax', operation: 'getTokenPlanRemains', kind, message: 'MiniMax provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetTokenPlanRemainsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetTokenPlanRemainsOutput> { return newQueryRetry(failure(kind), delay); }
