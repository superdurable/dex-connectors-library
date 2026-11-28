// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListTagsDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListTagsInput, TagsResult, TagMetadata } from './generated/models.js';

const emptyOutput: TagsResult = { total: 0, tags: [] };
/** Sender used for Weiban token and tag-list requests. */
export type WeibanRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport dependency for deterministic provider tests. */
export interface WeibanClientOptions { /** Production uses the SDK sender. */ send?: WeibanRequestSender; }
/** Read Weiban tag metadata using credentials resolved per call. */
export class WeibanClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: WeibanRequestSender;
  /** Validate the provider origin and bounded request settings. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: WeibanClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Weiban configuration and credentials are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://open.weibanzhushou.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Weiban request limits must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the first-page tag Query. */ listTags(): ListTagsOperation { return new ListTagsOperation(this); }
}
/** Fetch up to ten authorized tags without customer records. */
export class ListTagsOperation implements Query<ListTagsInput, TagsResult> {
  /** Bind the Query client. */ constructor(private readonly client: WeibanClient) {}
  /** Stable manifest definition. */ definition(): QueryDefinition { return ListTagsDefinition; }
  /** Fetch an access token then the first tag page. */
  async invoke(call: Call, input: ListTagsInput): Promise<QueryAttempt<TagsResult>> {
    void input;
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION'); }
    const corpId = credentials.corpId;
    const secret = credentials.secret instanceof SecretString ? credentials.secret.reveal() : '';
    if (!validCredential(corpId) || !validCredential(secret)) return branch('defect', 'AUTHENTICATION');
    const tokenBody = new TextEncoder().encode(JSON.stringify({ corp_id: corpId, secret }));
    const tokenResponse = await this.request(call, { url: this.client.baseUrl + '/open-api/access_token/get', method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body: tokenBody });
    if ('attempt' in tokenResponse) return tokenResponse.attempt;
    let tokenDocument: unknown;
    try { tokenDocument = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(tokenResponse.response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(tokenDocument) || !Number.isSafeInteger(tokenDocument.errcode)) return branch('invalidResponse', 'PROTOCOL');
    if (tokenDocument.errcode !== 0) return isBusy(tokenDocument.errcode as number) ? retry('AVAILABILITY') : branch('providerRejected', 'AUTHENTICATION');
    if (!validCredential(tokenDocument.access_token) || !Number.isSafeInteger(tokenDocument.expires_in) || (tokenDocument.expires_in as number) <= 0) return branch('invalidResponse', 'PROTOCOL');
    const query = new URLSearchParams({ access_token: tokenDocument.access_token, limit: '10', offset: '0' });
    const tagsResponse = await this.request(call, { url: this.client.baseUrl + '/open-api/tag/list?' + query.toString(), method: 'GET', headers: { Accept: 'application/json' } });
    if ('attempt' in tagsResponse) return tagsResponse.attempt;
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(tagsResponse.response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || !Number.isSafeInteger(document.errcode)) return branch('invalidResponse', 'PROTOCOL');
    if (document.errcode !== 0) return isBusy(document.errcode as number) ? retry('AVAILABILITY') : branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Number.isSafeInteger(document.total) || (document.total as number) < 0 || !Array.isArray(document.tag_list) || document.tag_list.length > 100 || document.tag_list.length > (document.total as number)) return branch('invalidResponse', 'PROTOCOL');
    const tags: TagMetadata[] = [];
    for (const item of document.tag_list as unknown[]) {
      if (tags.length === 10) break;
      if (!isRecord(item) || typeof item.tag_id !== 'string' || !item.tag_id || Buffer.byteLength(item.tag_id) > 128 || (item.tag_name != null && (typeof item.tag_name !== 'string' || Buffer.byteLength(item.tag_name) > 512)) || (item.tag_group != null && (typeof item.tag_group !== 'string' || Buffer.byteLength(item.tag_group) > 512))) return branch('invalidResponse', 'PROTOCOL');
      tags.push({ id: item.tag_id, name: typeof item.tag_name === 'string' ? item.tag_name : '', group: typeof item.tag_group === 'string' ? item.tag_group : '' });
    }
    return newQueryBranch('listed', { total: document.total as number, tags });
  }
  private async request(call: Call, fields: Pick<ProviderRequest, 'url' | 'method' | 'headers' | 'body'>): Promise<{ response: ProviderResponse } | { attempt: QueryAttempt<TagsResult> }> {
    let response: ProviderResponse;
    let oversized = false;
    try { response = await this.client.send({ ...fields, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
      addressPolicy: this.client.baseUrl.startsWith('https:') ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal }); }
    catch (error) {
      if (!(error instanceof BodyTooLargeError) || error.status === undefined) return { attempt: retry('AVAILABILITY') };
      response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
      oversized = true;
    }
    if (response.status === 429 || response.status === 408 || response.status >= 500) return { attempt: classifyStatus(response.status, response.headers) };
    if (oversized || response.body.length > this.client.maxResponseBytes) return { attempt: branch('invalidResponse', 'RESPONSE_TOO_LARGE') };
    if (response.status !== 200) return { attempt: classifyStatus(response.status, response.headers) };
    return { response };
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function validCredential(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && Buffer.byteLength(value) <= 2048 && value.trim() === value; }
function isBusy(code: number): boolean { return [500, 10051, 10052, 10053].includes(code); }
function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<TagsResult> {
  const value = headers['retry-after']; const delay = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', delay);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', delay);
  const kind: Failure['kind'] = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : [400, 404, 412].includes(status) ? 'VALIDATION' : 'PROVIDER_REJECTION';
  return branch('providerRejected', kind);
}
function failure(kind: Failure['kind']): Failure { return { provider: 'weiban', operation: 'listTags', kind, message: 'Weiban tag request failed' }; }
function branch(id: string, kind: Failure['kind']): QueryAttempt<TagsResult> { return newQueryBranch(id, emptyOutput, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<TagsResult> { return newQueryRetry(failure(kind), delay); }
