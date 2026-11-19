// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListUsersDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListUsersInput, ListUsersOutput } from './generated/models.js';

const emptyUsers = (): ListUsersOutput => ({ users: [], hasMore: false, nextMarker: '' });

/** Inject transport and clock for deterministic tests. */
export interface KingsoftCloudClientOptions {
  /** Provider sender; omitted uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** UTC timestamp source for signatures. */ now?: () => Date;
}

/** List Kingsoft Cloud IAM identities with per-call signed credentials. */
export class KingsoftCloudClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly region: string;
  private readonly service: string;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate IAM connection settings. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: KingsoftCloudClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Kingsoft Cloud configuration and credentials are required');
    this.baseURL = validateBaseURL(config.baseUrl || 'https://iam.api.ksyun.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    this.region = config.region || 'cn-beijing-6';
    this.service = config.service || 'iam';
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || !this.region || !this.service) throw new TypeError('Kingsoft Cloud request settings are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the listUsers Query. */ listUsers(): ListUsersOperation { return new ListUsersOperation(this); }

  /** Sign and send one bounded IAM page request. */
  async get(call: Call, input: ListUsersInput): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let accessKeyID: string;
    let secretAccessKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      accessKeyID = credentials.accessKeyId instanceof SecretString ? credentials.accessKeyId.reveal() : '';
      secretAccessKey = credentials.secretAccessKey instanceof SecretString ? credentials.secretAccessKey.reveal() : '';
    } catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!validCredential(accessKeyID) || !validCredential(secretAccessKey)) return { error: 'credential', retryAfterMs: 0 };
    const query = new URLSearchParams({ Accesskey: accessKeyID, Action: 'ListUsers', Service: this.service, Region: this.region, Version: '2015-11-01', Timestamp: this.now().toISOString().replace('.000Z', 'Z'), SignatureVersion: '1.0', SignatureMethod: 'HMAC-SHA256', MaxItems: String(input.maxItems || 100) });
    if (input.marker) query.set('Marker', input.marker);
    query.set('Signature', createHmac('sha256', secretAccessKey).update(canonicalQuery(query)).digest('hex'));
    const request: ProviderRequest = { url: `${this.baseURL}/?${canonicalQuery(query)}`, method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: this.baseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (response.body.length > this.maxResponseBytes) return { response, error: 'tooLarge', retryAfterMs };
    return { response, retryAfterMs };
  }
}

/** Return one bounded page of IAM users. */
export class ListUsersOperation implements Query<ListUsersInput, ListUsersOutput> {
  /** Bind the client. */ constructor(private readonly client: KingsoftCloudClient) {}
  /** Return the generated operation definition. */ definition(): QueryDefinition { return ListUsersDefinition; }
  /** Classify provider outcomes according to the Go connector. */
  async invoke(call: Call, input: ListUsersInput): Promise<QueryAttempt<ListUsersOutput>> {
    if (!input || (input.maxItems !== undefined && (!Number.isInteger(input.maxItems) || input.maxItems < 0 || input.maxItems > 100)) || (input.marker !== undefined && (input.marker.length > 320 || input.marker.trim() !== input.marker))) return branch('defect', 'VALIDATION', 'IAM page size or marker is invalid');
    const exchange = await this.client.get(call, input);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION', 'IAM credentials are unavailable or invalid');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY', 'IAM request failed');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', 'IAM rate limited the request', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', 'IAM is temporarily unavailable', exchange.retryAfterMs);
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION', `IAM rejected the request with HTTP ${status}`);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'IAM response exceeded the configured limit');
    let raw: unknown;
    try { raw = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL', 'IAM returned an invalid response'); }
    if (!isObject(raw) || !isObject(raw.ListUserResult) || !isObject(raw.ListUserResult.Users) || !Array.isArray(raw.ListUserResult.Users.member) || typeof raw.ListUserResult.IsTruncated !== 'boolean' || (raw.ListUserResult.Marker !== undefined && typeof raw.ListUserResult.Marker !== 'string')) return branch('invalidResponse', 'PROTOCOL', 'IAM returned an invalid response');
    const result = raw.ListUserResult;
    const members = (result.Users as Record<string, unknown>).member as unknown[];
    if (members.length > 100 || (result.IsTruncated && !result.Marker)) return branch('invalidResponse', 'PROTOCOL', 'IAM returned an invalid response');
    const users: ListUsersOutput['users'] = [];
    for (const member of members) {
      if (!isObject(member) || typeof member.UserId !== 'string' || !member.UserId || typeof member.UserName !== 'string' || !member.UserName) return branch('invalidResponse', 'PROTOCOL', 'IAM returned an invalid response');
      users.push({ userId: member.UserId, userName: member.UserName });
    }
    return newQueryBranch('listed', { users, hasMore: result.IsTruncated as boolean, nextMarker: result.IsTruncated ? result.Marker as string : '' });
  }
}

function canonicalQuery(query: URLSearchParams): string { return [...query.entries()].sort(([left], [right]) => left.localeCompare(right, 'en')).map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(value)}`).join('&'); }
function validCredential(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[\x21-\x7e]+$/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'kingsoftcloud', operation: 'listUsers', kind, message }; }
function branch(name: string, kind: Failure['kind'], message: string): QueryAttempt<ListUsersOutput> { return newQueryBranch(name, emptyUsers(), {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delay = 0): QueryAttempt<ListUsersOutput> { return newQueryRetry(failure(kind, message), delay); }
