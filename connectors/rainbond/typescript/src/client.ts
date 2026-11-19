// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListTeamsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListTeamsInput, ListTeamsOutput } from './generated/models.js';

const emptyOutput: ListTeamsOutput = { teams: [], total: 0 };

/** Optional request sender for provider tests. */
export interface RainbondClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve a Rainbond console token for each team-list attempt. */
export class RainbondClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate console origin and request limits without making a provider request. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: RainbondClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Rainbond configuration and credentials are required');
    this.baseUrl = validateBaseURL(config.baseUrl);
    const origin = new URL(this.baseUrl);
    if (origin.protocol !== 'https:' || origin.pathname !== '/' || origin.search || origin.hash) throw new TypeError('Rainbond console must be an HTTPS origin');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Rainbond request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the team-list Query. */ listTeams(): ListTeamsOperation { return new ListTeamsOperation(this); }

  /** Send one bounded team page request. */
  async fetchTeams(call: Call, page: number, pageSize: number): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!apiKey || apiKey.length > 512 || !/^[!-~]+$/.test(apiKey)) return { error: 'credential', retryAfterMs: 0 };
    const query = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
    let response: ProviderResponse;
    try { response = await this.send({ url: this.baseUrl + '/openapi/v1/teams?' + query.toString(), method: 'GET', headers: { Accept: 'application/json', Authorization: apiKey }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal }); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** List one page of accessible Rainbond teams. */
export class ListTeamsOperation implements Query<ListTeamsInput, ListTeamsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: RainbondClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListTeamsDefinition; }
  /** Classify one team-list response. */
  async invoke(call: Call, input: ListTeamsInput): Promise<QueryAttempt<ListTeamsOutput>> {
    let page = input?.page ?? 0;
    let pageSize = input?.pageSize ?? 0;
    if (page === 0) page = 1;
    if (pageSize === 0) pageSize = 20;
    if (!Number.isInteger(page) || page < 1 || page > 1_000_000 || !Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100) return branch('defect', 'VALIDATION');
    const exchange = await this.client.fetchTeams(call, page, pageSize);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || !Number.isInteger(wire.total) || (wire.total as number) < 0 || !Array.isArray(wire.tenants) || wire.tenants.length > pageSize) return branch('invalidResponse', 'PROTOCOL');
    const teams: NonNullable<ListTeamsOutput['teams']> = [];
    for (const tenant of wire.tenants) {
      if (!isRecord(tenant) || typeof tenant.tenant_id !== 'string' || !tenant.tenant_id || tenant.tenant_id.length > 256 || tenant.tenant_id.trim() !== tenant.tenant_id || typeof tenant.tenant_name !== 'string' || !tenant.tenant_name || tenant.tenant_name.length > 512 || (tenant.is_active !== undefined && typeof tenant.is_active !== 'boolean')) return branch('invalidResponse', 'PROTOCOL');
      teams.push({ id: tenant.tenant_id, name: tenant.tenant_name, active: tenant.is_active === true });
    }
    return newQueryBranch('listed', { teams, total: wire.total as number });
  }
}

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'rainbond', operation: 'listTeams', kind, message: 'Rainbond team-list query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListTeamsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delayMs = 0): QueryAttempt<ListTeamsOutput> { return newQueryRetry(failure(kind), delayMs); }
