// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListTeamsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListTeamsInput, ListTeamsOutput } from './generated/models.js';

/** Optional sender for provider tests. */
export interface OnesClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Read Ones provider data with bounded HTTP responses. */
export class OnesClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate connection settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: OnesClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Ones configuration and credentials are required');
    if (!config.baseUrl) throw new TypeError('ONES tenant origin is required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://invalid.example').replace(/\/$/, '');
    if (new URL(this.baseURL).pathname !== '/') throw new TypeError('baseUrl must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs || 10000;
    this.maxResponseBytes = config.maxResponseBytes || 2097152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('invalid request limits');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the listTeams Query. */
  listTeams(): ListTeamsOperation { return new ListTeamsOperation(this); }
  /** Resolve credentials and send one provider request. */
  async request(call: Call): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let key = '';
    try { const fields = await this.credentials.resolve(call, call.context.cancellationSignal); key = fields.apiKey instanceof SecretString ? fields.apiKey.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!key || key.length > 512 || key.split('').some(character => character.charCodeAt(0) <= 32 || character.charCodeAt(0) >= 127)) return { error: 'credential', retryAfterMs: 0 };
    const url = `${this.baseURL}/openapi/v2/account/teams`;
    const method = 'GET'; const headers = { Accept: 'application/json', Authorization: `Bearer ${key}` }; const body = undefined;
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

/** Classify Ones provider replies as Query branches or retries. */
export class ListTeamsOperation implements Query<ListTeamsInput, ListTeamsOutput> {
  /** Bind one validated provider client. */ constructor(private readonly client: OnesClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListTeamsDefinition; }
  /** Execute one read-only provider Query. */
  async invoke(call: Call, input: ListTeamsInput): Promise<QueryAttempt<ListTeamsOutput>> {
    void input;
    const branch = (name: string, kind: Failure['kind']): QueryAttempt<ListTeamsOutput> => newQueryBranch(name, { teams: [] }, {}, failure(kind));
    const retry = (kind: Failure['kind'], delay = 0): QueryAttempt<ListTeamsOutput> => newQueryRetry(failure(kind), delay);
    const bad = (): QueryAttempt<ListTeamsOutput> => branch('invalidResponse', 'PROTOCOL');

    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response; const retryAfterMs = exchange.retryAfterMs;
    if (status === 429) return retry('RATE_LIMIT', retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return bad(); }
    if (!isRecord(wire)) return bad();
    if (wire.result === 'FAIL') return branch('providerRejected', wire.errorCode === 'Unauthorized' ? 'AUTHENTICATION' : wire.errorCode === 'Forbidden' ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    const data = isRecord(wire.data) ? wire.data : null;
    if (wire.result !== 'SUCCESS' || !data || !Array.isArray(data.teams) || data.teams.length > 10000) return bad();
    const teams = [] as NonNullable<ListTeamsOutput['teams']>;
    for (const item of data.teams) {
      if (!isRecord(item) || !validName(item.id, 256) || item.id.trim() !== item.id || !validName(item.name, 512) || !Number.isInteger(item.status)) return bad();
      teams.push({ id: item.id as string, name: item.name as string, status: item.status as number });
    }
    return newQueryBranch('listed', { teams });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function validName(value: unknown, maximum: number): value is string { return typeof value === 'string' && !!value && Buffer.byteLength(value) <= maximum; }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'ones', operation: 'listTeams', kind, message: 'Ones listTeams failed' }; }
