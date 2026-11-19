// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetUserDetailDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetUserDetailInput, UserDetail } from './generated/models.js';

const emptyUser: UserDetail = { userId: '', userUuid: '', createdAtMilliseconds: 0, modifiedAtMilliseconds: 0, activated: false, nickname: '' };
type Exchange = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };
type CachedToken = { value: string; expiresAt: number; credentialHash: string };

/** Optional provider sender for isolated Go conformance replay. */
export interface EasemobClientOptions {
  /** Sender; omitted uses the SDK HTTPS transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
}

/** Resolve an App Token and read selected nonsecret IM user metadata. */
export class EasemobClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private cachedToken?: CachedToken;

  /** Validate local settings without contacting Easemob. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: EasemobClientOptions = {}) {
    if (!config || !credentials || typeof config.baseUrl !== 'string') throw new TypeError('Easemob configuration and credentials are required');
    let parsed: URL;
    try { parsed = new URL(config.baseUrl); } catch { throw new TypeError('Easemob REST host is invalid'); }
    if (!['https:', 'http:'].includes(parsed.protocol) || (parsed.protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(parsed.hostname)) || !parsed.hostname || parsed.pathname !== '/' || parsed.username || parsed.password || parsed.search || parsed.hash || config.baseUrl.endsWith('/')) throw new TypeError('Easemob REST host is invalid');
    this.baseUrl = config.baseUrl;
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 65_536;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Easemob request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the user-detail Query. */ getUserDetail(): GetUserDetailOperation { return new GetUserDetailOperation(this); }

  /** Acquire or refresh the short-lived App Token. */
  async appToken(call: Call, credentials: Credentials, organization: string, application: string, forceRefresh: boolean): Promise<{ token?: string; attempt?: QueryAttempt<UserDetail> }> {
    const credentialHash = createHash('sha256').update(credentials.appKey + '\0' + credentials.clientId + '\0' + credentials.clientSecret.reveal()).digest('hex');
    if (!forceRefresh && this.cachedToken?.credentialHash === credentialHash && Date.now() < this.cachedToken.expiresAt) return { token: this.cachedToken.value };
    const body = Buffer.from(JSON.stringify({ grant_type: 'client_credentials', client_id: credentials.clientId, client_secret: credentials.clientSecret.reveal() }), 'utf8');
    const exchange = await this.exchange({ url: this.baseUrl + '/' + organization + '/' + application + '/token', method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
    const classified = classifyExchange(exchange);
    if (classified) return { attempt: classified };
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(exchange.response!.body)); } catch { return { attempt: branch('invalidResponse', 'PROTOCOL') }; }
    if (!isRecord(wire) || typeof wire.access_token !== 'string' || !wire.access_token || /\s/.test(wire.access_token) || !Number.isSafeInteger(wire.expires_in) || (wire.expires_in as number) <= 0 || (wire.expires_in as number) > 365 * 24 * 3600) return { attempt: branch('invalidResponse', 'PROTOCOL') };
    const lifetime = (wire.expires_in as number) * 1000;
    const safetyMargin = Math.min(30_000, lifetime / 10);
    this.cachedToken = { value: wire.access_token, expiresAt: Date.now() + lifetime - safetyMargin, credentialHash };
    return { token: wire.access_token };
  }

  /** Read one user through the bearer App Token. */
  async readUser(call: Call, organization: string, application: string, userId: string, token: string): Promise<Exchange> {
    return this.exchange({ url: this.baseUrl + '/' + organization + '/' + application + '/users/' + userId, method: 'GET', headers: { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: 'Bearer ' + token }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal });
  }

  private async exchange(request: ProviderRequest): Promise<Exchange> {
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Query one registered user's selected account fields. */
export class GetUserDetailOperation implements Query<GetUserDetailInput, UserDetail> {
  /** Bind the provider client. */ constructor(private readonly client: EasemobClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetUserDetailDefinition; }
  /** Classify token and user requests according to the Go connector contract. */
  async invoke(call: Call, input: GetUserDetailInput): Promise<QueryAttempt<UserDetail>> {
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); } catch { return branch('defect', 'AUTHENTICATION'); }
    const appKey = credentials.appKey;
    const parts = typeof appKey === 'string' ? appKey.split('#') : [];
    if (parts.length !== 2 || !parts.every(part => /^[a-z0-9_-]{1,128}$/.test(part)) || typeof credentials.clientId !== 'string' || !credentials.clientId || credentials.clientId.length > 256 || credentials.clientId.trim() !== credentials.clientId || /[\r\n]/.test(credentials.clientId) || !(credentials.clientSecret instanceof SecretString) || !credentials.clientSecret.reveal() || typeof input?.userId !== 'string' || !/^[a-z0-9_.-]{1,64}$/.test(input.userId)) return branch('defect', 'AUTHENTICATION');
    const [organization, application] = parts as [string, string];
    let token = await this.client.appToken(call, credentials, organization, application, false);
    if (token.attempt) return token.attempt;
    let exchange = await this.client.readUser(call, organization, application, input.userId, token.token!);
    if (exchange.response?.status === 401) {
      token = await this.client.appToken(call, credentials, organization, application, true);
      if (token.attempt) return token.attempt;
      exchange = await this.client.readUser(call, organization, application, input.userId, token.token!);
    }
    const classified = classifyExchange(exchange);
    if (classified) return classified;
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(exchange.response!.body)); } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(wire) || wire.count !== 1 || !Array.isArray(wire.entities) || wire.entities.length !== 1 || !isRecord(wire.entities[0])) return branch('invalidResponse', 'PROTOCOL');
    const entity = wire.entities[0];
    if (entity.username !== input.userId || typeof entity.uuid !== 'string' || !entity.uuid || !Number.isSafeInteger(entity.created) || (entity.created as number) <= 0 || !Number.isSafeInteger(entity.modified) || (entity.modified as number) <= 0 || typeof entity.activated !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('found', { userId: entity.username as string, userUuid: entity.uuid, createdAtMilliseconds: entity.created as number, modifiedAtMilliseconds: entity.modified as number, activated: entity.activated, nickname: typeof entity.nickname === 'string' ? entity.nickname : '' }, {});
  }
}

function classifyExchange(exchange: Exchange): QueryAttempt<UserDetail> | undefined {
  if (exchange.error === 'transport' || !exchange.response) return retry('TRANSPORT');
  const status = exchange.response.status;
  if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
  if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
  return undefined;
}
function isRecord(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
function header(headers: Record<string, string | string[] | undefined>, name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'easemob', operation: 'getUserDetail', kind, message: 'Easemob user query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<UserDetail> { return newQueryBranch(name, emptyUser, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<UserDetail> { return newQueryRetry(failure(kind), delay); }
