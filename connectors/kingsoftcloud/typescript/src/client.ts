// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListUsersDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListUsersInput, ListUsersOutput } from './generated/models.js';

type Exchange = { response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number };

/** Optional sender and clock for signed IAM request tests. */
export interface KingsoftCloudClientOptions {
  /** Sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock; omitted uses wall time. */ now?: () => Date;
}

/** Read bounded Kingsoft Cloud IAM user identities. */
export class KingsoftCloudClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly region: string;
  private readonly service: string;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate IAM endpoint and request limits. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: KingsoftCloudClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Kingsoft Cloud configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://iam.api.ksyun.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    this.region = config.region || 'cn-beijing-6';
    this.service = config.service || 'iam';
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || !this.region || !this.service) throw new TypeError('Kingsoft Cloud IAM settings are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the IAM user-list Query. */ listUsers(): ListUsersOperation { return new ListUsersOperation(this); }

  /** Sign and send one IAM page request. */
  async list(call: Call, input: ListUsersInput): Promise<Exchange> {
    let accessKeyID = '', secretAccessKey = '';
    try {
      const fields = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessKeyID = fields.accessKeyId instanceof SecretString ? fields.accessKeyId.reveal() : '';
      secretAccessKey = fields.secretAccessKey instanceof SecretString ? fields.secretAccessKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validCredential(accessKeyID) || !validCredential(secretAccessKey)) return { error: 'credential', retryAfterMs: 0 };
    const query: Record<string, string> = { Accesskey: accessKeyID, Action: 'ListUsers', Service: this.service, Region: this.region, Version: '2015-11-01', Timestamp: this.now().toISOString().replace('.000Z', 'Z'), SignatureVersion: '1.0', SignatureMethod: 'HMAC-SHA256', MaxItems: String(input.maxItems || 100) };
    if (input.marker) query.Marker = input.marker;
    query.Signature = createHmac('sha256', secretAccessKey).update(canonicalQuery(query)).digest('hex');
    const request: ProviderRequest = { url: `${this.baseURL}/?${canonicalQuery(query)}`, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
  }
}

/** Read one page of Kingsoft Cloud IAM users. */
export class ListUsersOperation implements Query<ListUsersInput, ListUsersOutput> {
  /** Bind a validated IAM client. */ constructor(private readonly client: KingsoftCloudClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListUsersDefinition; }
  /** Classify response, transport, and pagination outcomes. */
  async invoke(call: Call, input: ListUsersInput): Promise<QueryAttempt<ListUsersOutput>> {
    const maxItems = input?.maxItems ?? 0;
    const marker = input?.marker ?? '';
    if (!Number.isInteger(maxItems) || maxItems < 0 || maxItems > 100 || typeof marker !== 'string' || marker.length > 320 || marker.trim() !== marker) return branch('defect', 'VALIDATION');
    const exchange = await this.client.list(call, { marker, maxItems });
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isObject(wire) || !isObject(wire.ListUserResult) || !isObject(wire.ListUserResult.Users)) return branch('invalidResponse', 'PROTOCOL');
    const result = wire.ListUserResult;
    const userEnvelope = result.Users as Record<string, unknown>;
    const users = Array.isArray(userEnvelope.member) ? userEnvelope.member : [];
    if (users.length > 100 || typeof result.IsTruncated !== 'boolean' || result.IsTruncated && (typeof result.Marker !== 'string' || !result.Marker)) return branch('invalidResponse', 'PROTOCOL');
    const identities: ListUsersOutput['users'] = [];
    for (const user of users) {
      if (!isObject(user) || typeof user.UserId !== 'string' || !user.UserId || typeof user.UserName !== 'string' || !user.UserName) return branch('invalidResponse', 'PROTOCOL');
      identities.push({ userId: user.UserId, userName: user.UserName });
    }
    return newQueryBranch('listed', { users: identities, hasMore: result.IsTruncated, nextMarker: result.IsTruncated ? result.Marker as string : '' });
  }
}

function canonicalQuery(query: Record<string, string>): string { return Object.keys(query).sort().map(key => `${encodeRFC3986(key)}=${encodeRFC3986(query[key] ?? '')}`).join('&'); }
function encodeRFC3986(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`); }
function validCredential(value: unknown): value is string { return typeof value === 'string' && !!value && value.length <= 512 && /^[\x21-\x7e]+$/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'kingsoftcloud', operation: 'listUsers', kind, message: 'Kingsoft Cloud IAM user list failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListUsersOutput> { return newQueryBranch(name, { users: [], hasMore: false, nextMarker: '' }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListUsersOutput> { return newQueryRetry(failure(kind), delay); }
