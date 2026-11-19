// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, randomUUID } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListObjectMetadataDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListObjectMetadataInput, ListObjectMetadataOutput } from './generated/models.js';

/** Test seams for transport, request IDs, and the token clock. */
export interface FxiaokeClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; traceId?: () => string; now?: () => Date; }
const emptyObjects: ListObjectMetadataOutput = { objects: [] };
type Exchange = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };
type CredentialValues = { appId: string; appSecret: string; permanentCode: string };

/** Cache short-lived FXiaoke access tokens in Worker memory. */
export class FxiaokeClient {
  private readonly baseUrl: string;
  private readonly employeeId: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly traceId: () => string;
  private readonly now: () => Date;
  private token?: { key: string; value: string; enterpriseAccount: string; validUntil: number };
  /** Validate cloud host and employee context before any provider request. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: FxiaokeClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('FXiaoke configuration and credentials are required');
    const cloudHost = config.cloudHost || 'open.fxiaoke.com';
    if (!validCloudHost(cloudHost) || !validIdentifier(config.employeeId)) throw new TypeError('FXiaoke cloud host or employee ID is invalid');
    this.baseUrl = 'https://' + cloudHost;
    this.employeeId = config.employeeId;
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('FXiaoke request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.traceId = options.traceId ?? randomUUID;
    this.now = options.now ?? (() => new Date());
  }
  /** Bind the read-only CRM object metadata Query. */ listObjectMetadata(): ListObjectMetadataOperation { return new ListObjectMetadataOperation(this); }
  /** Resolve current app credentials for one attempt. */
  async resolveCredentials(call: Call): Promise<CredentialValues | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const appId = credentials.appId;
      const appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
      const permanentCode = credentials.permanentCode instanceof SecretString ? credentials.permanentCode.reveal() : '';
      return validIdentifier(appId) && validSecret(appSecret) && validSecret(permanentCode) ? { appId, appSecret, permanentCode } : undefined;
    } catch { return undefined; }
  }
  /** Exchange app credentials for one short-lived access token. */
  async tokenFor(call: Call, credentials: CredentialValues): Promise<{ value?: string; enterpriseAccount?: string; attempt?: QueryAttempt<ListObjectMetadataOutput> }> {
    const key = createHash('sha256').update(`${credentials.appId}\0${credentials.appSecret}\0${credentials.permanentCode}`).digest('hex');
    if (this.token?.key === key && this.now().getTime() < this.token.validUntil) return { value: this.token.value, enterpriseAccount: this.token.enterpriseAccount };
    const traceId = this.traceId();
    if (!validTraceId(traceId)) return { attempt: retry('AVAILABILITY') };
    const payload = JSON.stringify({ appId: credentials.appId, appSecret: credentials.appSecret, permanentCode: credentials.permanentCode, grantType: 'app_secret' });
    const exchange = await this.exchange(this.request(call, `/oauth2.0/token?thirdTraceId=${traceId}`, {}, payload));
    const classified = classifyExchange(exchange);
    if (classified) return { attempt: classified };
    const wire = decode(exchange.response?.body);
    if (!isObject(wire) || !Number.isInteger(wire.errorCode)) return { attempt: branch('invalidResponse', 'PROTOCOL') };
    if (wire.errorCode !== 0) return { attempt: transientCode(wire.errorCode as number) ? retry('AVAILABILITY') : branch('providerRejected', 'AUTHENTICATION') };
    if (!validSecret(wire.accessToken) || !validIdentifier(wire.ea) || !Number.isSafeInteger(wire.expiresIn) || (wire.expiresIn as number) <= 0 || (wire.expiresIn as number) > 7200) return { attempt: branch('invalidResponse', 'PROTOCOL') };
    const validitySeconds = Math.max(1, Math.min((wire.expiresIn as number) - 60, 6000));
    this.token = { key, value: wire.accessToken as string, enterpriseAccount: wire.ea as string, validUntil: this.now().getTime() + validitySeconds * 1000 };
    return { value: this.token.value, enterpriseAccount: this.token.enterpriseAccount };
  }
  /** Invalidate one token after provider-expiration response. */ clearToken(): void { this.token = undefined; }
  /** Read visible CRM object definitions. */
  async list(call: Call, token: string, enterpriseAccount: string): Promise<Exchange> {
    const traceId = this.traceId();
    if (!validTraceId(traceId)) return { error: 'transport', retryAfterMs: 0 };
    return this.exchange(this.request(call, `/cgi/crm/v2/object/list?thirdTraceId=${traceId}`, { Authorization: 'Bearer ' + token, 'x-fs-ea': enterpriseAccount, 'x-fs-userid': this.employeeId }, '{}'));
  }
  private request(call: Call, path: string, extraHeaders: Record<string, string>, body: string): ProviderRequest {
    return { url: this.baseUrl + path, method: 'POST', headers: { ...extraHeaders, 'Content-Type': 'application/json', Accept: 'application/json' }, body: new TextEncoder().encode(body), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
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

/** Classify FXiaoke token and CRM object responses with the Go Query contract. */
export class ListObjectMetadataOperation implements Query<ListObjectMetadataInput, ListObjectMetadataOutput> {
  /** Bind the provider client. */ constructor(private readonly client: FxiaokeClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListObjectMetadataDefinition; }
  /** Read visible CRM object definitions for the configured employee. */
  async invoke(call: Call): Promise<QueryAttempt<ListObjectMetadataOutput>> {
    const credentials = await this.client.resolveCredentials(call);
    if (!credentials) return branch('defect', 'AUTHENTICATION');
    const token = await this.client.tokenFor(call, credentials);
    if (token.attempt) return token.attempt;
    const exchange = await this.client.list(call, token.value ?? '', token.enterpriseAccount ?? '');
    const classified = classifyExchange(exchange);
    if (classified) return classified;
    const wire = decode(exchange.response?.body);
    if (!isObject(wire)) return branch('invalidResponse', 'PROTOCOL');
    if (Number.isInteger(wire.errorCode) && wire.errorCode !== 0) {
      if (wire.errorCode === 20016) { this.client.clearToken(); return retry('AUTHENTICATION'); }
      return transientCode(wire.errorCode as number) ? retry('AVAILABILITY') : branch('providerRejected', 'PROVIDER_REJECTION');
    }
    if (!isObject(wire.data) || !Array.isArray(wire.data.objects)) return branch('invalidResponse', 'PROTOCOL');
    const objects: ListObjectMetadataOutput['objects'] = [];
    for (const item of wire.data.objects) {
      if (!isObject(item) || typeof item.describeApiName !== 'string' || !item.describeApiName || typeof item.describeDisplayName !== 'string' || !item.describeDisplayName || typeof item.isActive !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
      objects.push({ apiName: item.describeApiName, displayName: item.describeDisplayName, active: item.isActive });
    }
    return newQueryBranch('listed', { objects });
  }
}
function validCloudHost(host: string): boolean { return host === 'open.fxiaoke.com' || (host.length <= 128 && /^open-[a-z0-9.-]+\.(fxiaoke|sharecrm)\.com$/.test(host) && !host.includes('..') && !host.includes('-.') && !host.includes('.-')); }
function validIdentifier(value: unknown): value is string { return typeof value === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(value); }
function validSecret(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 1024 && value.trim() === value && [...value].every(character => character.charCodeAt(0) > 32 && character.charCodeAt(0) < 127); }
function validTraceId(value: string): boolean { return /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value); }
function transientCode(code: number): boolean { return [-1, -2, 14001, 30004, 50009].includes(code); }
function classifyExchange(exchange: Exchange): QueryAttempt<ListObjectMetadataOutput> | undefined {
  if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
  const status = exchange.response.status;
  if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
  if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
  if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
  return undefined;
}
function decode(body: Uint8Array | undefined): unknown { try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; } }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'fxiaoke', operation: 'listObjectMetadata', kind, message: 'FXiaoke object metadata query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListObjectMetadataOutput> { return newQueryBranch(name, emptyObjects, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListObjectMetadataOutput> { return newQueryRetry(failure(kind), delay); }
