// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListTicketStatusesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListTicketStatusesInput, ListTicketStatusesOutput } from './generated/models.js';

const empty: ListTicketStatusesOutput = { statuses: [] };
/** Optional provider transport, clock, and nonce source for deterministic verification. */
export interface UdeskClientOptions {
  /** Send one provider request. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply the current time for the v2 signature. */ now?: () => Date;
  /** Supply 16 cryptographically random nonce bytes. */ nonce?: () => Uint8Array;
}
/** Read account-defined ticket statuses through signed Udesk Open API requests. */
export class UdeskClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly nonce: () => Uint8Array;
  /** Validate account origin and request bounds without contacting Udesk. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: UdeskClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Udesk configuration and credential source are required');
    this.baseUrl = (config.baseUrl || '').replace(/\/$/, '');
    const endpoint = new URL(this.baseUrl);
    const host = endpoint.hostname.toLowerCase();
    const isLocal = host === 'localhost' || host === '127.0.0.1' || host === '::1';
    const subdomain = host.endsWith('.udesk.cn') ? host.slice(0, -'.udesk.cn'.length) : '';
    if (endpoint.username || endpoint.password || endpoint.search || endpoint.hash || endpoint.pathname !== '/' || !(isLocal && ['http:', 'https:'].includes(endpoint.protocol) || endpoint.protocol === 'https:' && !endpoint.port && /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(subdomain))) throw new TypeError('Udesk account origin is invalid');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Udesk request bounds are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.nonce = options.nonce ?? (() => randomBytes(16));
  }
  /** Bind the ticket-status Query. */ listTicketStatuses(): ListTicketStatusesOperation { return new ListTicketStatusesOperation(this); }
  /** Sign and send one status-list request. */
  async request(call: Call, input: ListTicketStatusesInput): Promise<QueryAttempt<ListTicketStatusesOutput>> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const email = credentials.adminEmail instanceof SecretString ? credentials.adminEmail.reveal() : '';
    const token = credentials.apiToken instanceof SecretString ? credentials.apiToken.reveal() : '';
    if (!validEmail(email) || !/^[!-~]{1,512}$/.test(token)) return branch('defect', 'AUTHENTICATION');
    let nonceBytes: Uint8Array;
    try { nonceBytes = this.nonce(); }
    catch { return retry('AVAILABILITY'); }
    if (nonceBytes.length !== 16) return retry('AVAILABILITY');
    const nonce = Buffer.from(nonceBytes).toString('hex');
    const timestamp = String(Math.floor(this.now().getTime() / 1000));
    const sign = createHash('sha256').update(`${email}&${token}&${timestamp}&${nonce}&v2`).digest('hex');
    const query = new URLSearchParams({ email, timestamp, nonce, sign_version: 'v2', sign });
    if (input.enabledOnly !== undefined && input.enabledOnly !== null) query.set('enable', String(input.enabledOnly));
    const request: ProviderRequest = { url: `${this.baseUrl}/open_api_v1/tickets/status?${query}`, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status === 200) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
      return retry('AVAILABILITY');
    }
    const retryAfter = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', delay);
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    if (response.body.length > this.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let data: unknown;
    try { data = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(data) || typeof data.code !== 'number' || !Number.isInteger(data.code)) return branch('invalidResponse', 'PROTOCOL');
    if (data.code !== 1000) return branch('providerRejected', data.code === 2059 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
    if (!Array.isArray(data.status)) return branch('invalidResponse', 'PROTOCOL');
    const statuses: ListTicketStatusesOutput['statuses'] = [];
    for (const item of data.status) {
      if (!isRecord(item) || typeof item.status_id !== 'number' || !Number.isSafeInteger(item.status_id) || item.status_id <= 0 || typeof item.name !== 'string' || !item.name || typeof item.en_name !== 'string' || !item.en_name || typeof item.enable !== 'boolean' || item.color !== undefined && typeof item.color !== 'string') return branch('invalidResponse', 'PROTOCOL');
      statuses.push({ statusId: item.status_id, name: item.name, englishName: item.en_name, enabled: item.enable, color: typeof item.color === 'string' ? item.color : '' });
    }
    return newQueryBranch('listed', { statuses });
  }
}
/** Read account ticket statuses with an optional enabled filter. */
export class ListTicketStatusesOperation implements Query<ListTicketStatusesInput, ListTicketStatusesOutput> {
  /** Bind one validated client. */ constructor(private readonly client: UdeskClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListTicketStatusesDefinition; }
  /** Invoke one safe signed provider read. */ invoke(call: Call, input: ListTicketStatusesInput): Promise<QueryAttempt<ListTicketStatusesOutput>> { return this.client.request(call, input); }
}
function validEmail(email: string): boolean { if (!email || email.length > 320 || email.trim() !== email || (email.match(/@/g) ?? []).length !== 1 || /[\s&=?#]/.test(email)) return false; const [local, domain] = email.split('@'); return Boolean(local && domain?.includes('.')); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'udesk', operation: 'listTicketStatuses', kind, message: 'Udesk ticket status request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListTicketStatusesOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListTicketStatusesOutput> { return newQueryRetry(failure(kind), delay); }
