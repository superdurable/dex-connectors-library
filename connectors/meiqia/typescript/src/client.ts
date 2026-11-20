// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetConversationDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetConversationInput, GetConversationOutput } from './generated/models.js';

const endpoint = 'https://api.meiqia.com/v1/conversations/';
const emptyOutput: GetConversationOutput = { conversationID: 0, startedAt: '', endedAt: '', mainChannel: '' };
/** Optional HTTP sender for deterministic provider tests. */
export interface MeiqiaClientOptions { /** Sender; defaults to the Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Read the conversation metadata with private credentials resolved per call. */
export class MeiqiaClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate local settings without contacting Meiqia. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: MeiqiaClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Meiqia configuration and credential source are required');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Meiqia request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only conversation Query. */ getConversation(): GetConversationOperation { return new GetConversationOperation(this); }
  /** Dispatch one bounded provider request. */
  async request(call: Call, input: GetConversationInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const apiKey = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    if (!/^[!-~]+$/.test(apiKey) || apiKey.length > 1024) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = { url: `${endpoint}${input.conversationID}`, method: 'GET', headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
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
/** Classify Meiqia conversation results without retaining secrets or metadata. */
export class GetConversationOperation implements Query<GetConversationInput, GetConversationOutput> {
  /** Bind one validated client. */ constructor(private readonly client: MeiqiaClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return GetConversationDefinition; }
  /** Read and validate the conversation metadata. */
  async invoke(call: Call, _input: GetConversationInput): Promise<QueryAttempt<GetConversationOutput>> {
    if (!Number.isSafeInteger(_input.conversationID) || _input.conversationID <= 0) return branch('defect', 'VALIDATION');
    const exchange = await this.client.request(call, _input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status === 404) return branch('noData', 'NOT_FOUND');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || document.conv_id !== _input.conversationID || typeof document.conv_start_tm !== 'string' || !document.conv_start_tm || document.conv_start_tm.length > 64) return branch('invalidResponse', 'PROTOCOL');
    const endedAt = document.conv_end_tm ?? '';
    const mainChannel = document.main_channel ?? '';
    if (typeof endedAt !== 'string' || endedAt.length > 64 || typeof mainChannel !== 'string' || mainChannel.length > 64) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { conversationID: _input.conversationID, startedAt: document.conv_start_tm, endedAt, mainChannel });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'meiqia', operation: 'getConversation', kind, message: 'Meiqia provider request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetConversationOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetConversationOutput> { return newQueryRetry(failure(kind), delay); }
