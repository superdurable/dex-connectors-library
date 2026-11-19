// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListOrganizationsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListOrganizationsInput, ListOrganizationsOutput } from './generated/models.js';

/** Optional sender for local provider tests. */
export interface SuxinClientOptions { /** Sender; omitted uses the SDK HTTP transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

type Exchange = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };
/** Request a short-lived token, then list organizations for its derived user ID. */
export class SuxinClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate local settings without contacting Suxin. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SuxinClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Suxin configuration and credentials are required');
    this.baseUrl = config.baseUrl || 'https://pc.suxin365.com';
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    const parsed = new URL(this.baseUrl);
    if (parsed.protocol !== 'https:' || parsed.host !== 'pc.suxin365.com' || parsed.username || parsed.password || parsed.pathname !== '/' || parsed.search || parsed.hash || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Suxin request configuration is invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the organization-list Query. */ listOrganizations(): ListOrganizationsOperation { return new ListOrganizationsOperation(this); }
  /** Resolve credentials for this invocation. */
  async authorize(call: Call): Promise<{ appId: string; apiKey: string } | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const appId = credentials.appId instanceof SecretString ? credentials.appId.reveal() : '';
      const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
      return validCredential(appId) && validCredential(apiKey) ? { appId, apiKey } : undefined;
    } catch { return undefined; }
  }
  /** Send one bounded stage request. */
  async request(call: Call, path: string, method: 'GET' | 'POST', headers: Record<string, string>, body?: Uint8Array): Promise<Exchange> {
    const request: ProviderRequest = { url: this.baseUrl + path, method, headers, ...(body ? { body } : {}), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** List organizations visible to the connected Suxin application. */
export class ListOrganizationsOperation implements Query<ListOrganizationsInput, ListOrganizationsOutput> {
  /** Bind the provider client. */ constructor(private readonly client: SuxinClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListOrganizationsDefinition; }
  /** Classify both provider stages using the Go connector contract. */
  async invoke(call: Call): Promise<QueryAttempt<ListOrganizationsOutput>> {
    const credentials = await this.client.authorize(call);
    if (!credentials) return branch('defect', 'AUTHENTICATION');
    const authBody = new TextEncoder().encode(JSON.stringify(credentials));
    const auth = await this.client.request(call, '/api/v1/auth/token', 'POST', { 'Content-Type': 'application/json', Accept: 'application/json' }, authBody);
    const authFailure = classifyExchange(auth);
    if (authFailure) return authFailure;
    const authWire = parseBody(auth.response!.body);
    if (!isObject(authWire) || typeof authWire.success !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!authWire.success) return providerFailure(authWire.code);
    if (!isObject(authWire.data) || !validCredential(authWire.data.accessToken) || !validIdentifier(authWire.data.userId)) return branch('invalidResponse', 'PROTOCOL');
    const token = authWire.data.accessToken;
    const userId = authWire.data.userId;
    const organizations = await this.client.request(call, `/api/v1/org/list?userId=${encodeURIComponent(userId)}`, 'GET', { Accept: 'application/json', Authorization: `Bearer ${token}` });
    const listFailure = classifyExchange(organizations);
    if (listFailure) return listFailure;
    const listWire = parseBody(organizations.response!.body);
    if (!isObject(listWire) || typeof listWire.success !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!listWire.success) return providerFailure(listWire.code);
    if (!Array.isArray(listWire.data)) return branch('invalidResponse', 'PROTOCOL');
    const output: ListOrganizationsOutput = { organizations: [] };
    for (const item of listWire.data) {
      if (!isObject(item) || !validIdentifier(item.orgId) || typeof item.name !== 'string' || !item.name || item.name.length > 512 || !['COMPANY', 'SELF_EMPLOYED'].includes(String(item.entityType)) || !['VERIFIED', 'UNVERIFIED'].includes(String(item.certifyStatus))) return branch('invalidResponse', 'PROTOCOL');
      ((output.organizations ??= []) as NonNullable<ListOrganizationsOutput['organizations']>).push({ id: item.orgId, name: item.name, entityType: item.entityType as string, certifyStatus: item.certifyStatus as string });
    }
    return newQueryBranch('listed', output);
  }
}

function classifyExchange(exchange: Exchange): QueryAttempt<ListOrganizationsOutput> | undefined {
  if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
  const { status } = exchange.response;
  if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
  if (status !== 200) return branch('providerRejected', status === 401 || status === 403 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
  return undefined;
}
function providerFailure(code: unknown): QueryAttempt<ListOrganizationsOutput> { const value = String(code ?? ''); return value === '5000' ? retry('AVAILABILITY') : branch('providerRejected', value === '4010' ? 'AUTHENTICATION' : 'PROVIDER_REJECTION'); }
function parseBody(body: Uint8Array): unknown { try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; } }
function validCredential(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 2048 && !/[^\x21-\x7e]/.test(value); }
function validIdentifier(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 256 && /^[0-9]+$/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'suxin', operation: 'listOrganizations', kind, message: 'Suxin organization query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListOrganizationsOutput> { return newQueryBranch(name, { organizations: [] }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListOrganizationsOutput> { return newQueryRetry(failure(kind), delay); }
