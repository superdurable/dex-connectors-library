// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListTagsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListTagsInput, ListTagsOutput } from './generated/models.js';

const emptyOutput: ListTagsOutput = { tags: [], total: 0, nextPage: 0 };
type Exchange = { response?: ProviderResponse; error?: 'transport' | 'tooLarge'; retryAfterMs: number };
/** Optional provider sender for deterministic JINGdigital tests. */
export interface JINGdigitalClientOptions { /** Sender; production uses Connector SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
/** Exchange app credentials for an ephemeral token and list ordinary tags. */
export class JINGdigitalClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Validate the documented JINGdigital origin and request limits. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: JINGdigitalClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('JINGdigital configuration and credential source are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://jingopenapi.jingsocial.com');
    if (this.baseUrl !== 'https://jingopenapi.jingsocial.com') throw new TypeError('JINGdigital requires its production origin');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('JINGdigital request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the read-only ordinary-tag Query. */ listTags(): ListTagsOperation { return new ListTagsOperation(this); }
  /** Resolve private application credentials for this attempt. */
  async resolve(call: Call): Promise<{ appId: string; appSecret: string } | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const appId = credentials.appId instanceof SecretString ? credentials.appId.reveal() : '';
      const appSecret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
      if (!validCredential(appId) || !validCredential(appSecret)) return undefined;
      return { appId, appSecret };
    } catch { return undefined; }
  }
  /** Send one bounded POST with no automatic redirects. */
  async request(call: Call, path: string, payload: object, headers: Record<string, string>): Promise<Exchange> {
    const request: ProviderRequest = { url: this.baseUrl + path, method: 'POST', headers: { Accept: 'application/json', 'Content-Type': 'application/json', ...headers }, body: new TextEncoder().encode(JSON.stringify(payload)), timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
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
/** Read one bounded ordinary-tag page without persisting the ephemeral access token. */
export class ListTagsOperation implements Query<ListTagsInput, ListTagsOutput> {
  /** Bind a validated client. */ constructor(private readonly client: JINGdigitalClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListTagsDefinition; }
  /** Classify authorization and tag-list responses. */
  async invoke(call: Call, input: ListTagsInput): Promise<QueryAttempt<ListTagsOutput>> {
    const credentials = await this.client.resolve(call);
    if (!credentials) return branch('defect', 'AUTHENTICATION');
    const page = input.page || 1, pageSize = input.pageSize || 20;
    if (!Number.isSafeInteger(page) || page < 1 || page > 1_000_000 || !Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 100) return branch('defect', 'VALIDATION');
    const auth = await this.client.request(call, '/v1/oauth2/token', { app_id: credentials.appId, app_secret: credentials.appSecret }, {});
    const authResult = classify(auth);
    if (authResult) return authResult;
    const authDocument = decode(auth.response!.body);
    if (!isRecord(authDocument) || !Number.isSafeInteger(authDocument.code)) return branch('invalidResponse', 'PROTOCOL');
    if (authDocument.code !== 0) return providerCode(authDocument.code as number);
    const authData = authDocument.data;
    if (!isRecord(authData) || !validCredential(authData.access_token)) return branch('invalidResponse', 'PROTOCOL');
    const tags = await this.client.request(call, '/v1/tag/select/list', { tag_ids: [], level: 1, page, perPage: pageSize }, { app_id: credentials.appId, access_token: authData.access_token });
    const tagsResult = classify(tags);
    if (tagsResult) return tagsResult;
    const document = decode(tags.response!.body);
    if (!isRecord(document) || !Number.isSafeInteger(document.code)) return branch('invalidResponse', 'PROTOCOL');
    if (document.code !== 0) return providerCode(document.code as number);
    const data = document.data;
    const meta = isRecord(data) ? data._meta : undefined;
    const total = isRecord(meta) ? meta.totalCount ?? 0 : undefined;
    if (!isRecord(data) || !Array.isArray(data.items) || !isRecord(meta) || !Number.isSafeInteger(total) || (total as number) < 0 || meta.currentPage !== page || !Number.isSafeInteger(meta.perPage) || (meta.perPage as number) < 1 || data.items.length > pageSize) return branch('invalidResponse', 'PROTOCOL');
    const selected: NonNullable<ListTagsOutput['tags']> = [];
    for (const item of data.items) {
      const level = isRecord(item) ? item.level ?? 0 : undefined;
      if (!isRecord(item) || !Number.isSafeInteger(item.id) || (item.id as number) <= 0 || typeof item.name !== 'string' || !item.name || Buffer.byteLength(item.name) > 512 || !Number.isSafeInteger(level) || (level as number) < 0 || (level as number) > 10) return branch('invalidResponse', 'PROTOCOL');
      selected.push({ id: item.id as number, name: item.name, level: level as number });
    }
    const nextPage = page * (meta.perPage as number) < (total as number) ? page + 1 : 0;
    if (nextPage && (!selected.length || page === 1_000_000)) return branch('invalidResponse', 'PROTOCOL');
    return newQueryBranch('listed', { tags: selected, total: total as number, nextPage });
  }
}
function classify(exchange: Exchange): QueryAttempt<ListTagsOutput> | undefined {
  if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
  const status = exchange.response.status;
  if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
  if (status !== 200) return branch('providerRejected', status === 401 || status === 403 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION');
  return undefined;
}
function decode(body: Uint8Array): unknown { try { return JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return undefined; } }
function validCredential(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 2048 && /^[!-~]+$/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function providerCode(code: number): QueryAttempt<ListTagsOutput> { if (code === 20000 || code === 40005) return retry('AVAILABILITY'); if (code === 20002) return retry('RATE_LIMIT'); return branch('providerRejected', code === 20001 || code === 40006 ? 'AUTHENTICATION' : 'PROVIDER_REJECTION'); }
function failure(kind: Failure['kind']): Failure { return { provider: 'jingdigital', operation: 'listTags', kind, message: 'JINGdigital tag-list request failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListTagsOutput> { return newQueryBranch(name, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListTagsOutput> { return newQueryRetry(failure(kind), delay); }
