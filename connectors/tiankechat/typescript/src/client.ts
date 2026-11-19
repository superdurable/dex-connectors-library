// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, randomBytes } from 'node:crypto';
import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListKnowledgeEntriesDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListKnowledgeEntriesInput, ListKnowledgeEntriesOutput, KnowledgeEntry } from './generated/models.js';

const empty: ListKnowledgeEntriesOutput = { entries: [], total: 0, page: 0, limit: 0 };
/** Inject transport, timestamp, and nonce for deterministic verification. */
export interface TiankeChatClientOptions {
  /** Dispatch requests; production uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Supply signing time; production uses the current clock. */ now?: () => Date;
  /** Supply 16 nonce bytes; production uses cryptographic randomness. */ nonce?: () => Uint8Array;
}
/** Read TiankeChat knowledge metadata without returning question content. */
export class TiankeChatClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  readonly now: () => Date;
  readonly nonce: () => Uint8Array;
  /** Validate provider settings before contacting TiankeChat. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: TiankeChatClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('TiankeChat configuration and credential source are required');
    const url = new URL(config.baseUrl ?? 'https://chat.open-code.cn');
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash || url.pathname !== '/') throw new TypeError('TiankeChat base URL is invalid');
    this.baseUrl = url.origin;
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 << 20) throw new TypeError('TiankeChat request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.nonce = options.nonce ?? (() => randomBytes(16));
  }
  /** Bind the knowledge metadata Query. */ listKnowledgeEntries(): ListKnowledgeEntriesOperation { return new ListKnowledgeEntriesOperation(this); }
}
/** Classify bounded provider responses and retryable outcomes. */
export class ListKnowledgeEntriesOperation implements Query<ListKnowledgeEntriesInput, ListKnowledgeEntriesOutput> {
  /** Bind one validated client. */ constructor(private readonly client: TiankeChatClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListKnowledgeEntriesDefinition; }
  /** Read one knowledge page without exposing entry content. */
  async invoke(call: Call, input: ListKnowledgeEntriesInput): Promise<QueryAttempt<ListKnowledgeEntriesOutput>> {
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); } catch { return branch('defect', 'AUTHENTICATION'); }
    const appId = credentials.appId;
    const secret = credentials.appSecret instanceof SecretString ? credentials.appSecret.reveal() : '';
    if (typeof appId !== 'string' || !/^(ap_|sb_)[!-~]+$/.test(appId) || appId.length > 128 || !secret.startsWith('tks_') || secret.length > 512 || !/^[!-~]+$/.test(secret)) return branch('defect', 'AUTHENTICATION');
    const page = input?.page || 1;
    const limit = input?.limit || 20;
    if (!Number.isSafeInteger(page) || page < 1 || page > 1_000_000 || !Number.isSafeInteger(limit) || limit < 1 || limit > 100) return branch('defect', 'VALIDATION');
    const path = `/api/openapi/knowledge?limit=${limit}&page=${page}`;
    let nonce: string;
    try { const bytes = this.client.nonce(); if (bytes.length !== 16) throw new TypeError('nonce length'); nonce = Buffer.from(bytes).toString('hex'); } catch { return retry('AVAILABILITY'); }
    const timestamp = String(Math.floor(this.client.now().getTime() / 1000));
    const signature = createHmac('sha256', secret).update([timestamp, nonce, 'GET', path, ''].join('\n')).digest('hex');
    const request: ProviderRequest = { url: `${this.client.baseUrl}${path}`, method: 'GET', headers: { Accept: 'application/json', 'X-App-Id': appId, 'X-Timestamp': timestamp, 'X-Nonce': nonce, 'X-Signature': signature }, timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.client.send(request); } catch (error) { return error instanceof BodyTooLargeError && error.status !== undefined ? branch('invalidResponse', 'RESPONSE_TOO_LARGE') : retry('AVAILABILITY'); }
    const header = Object.entries(response.headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    const delay = parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '');
    if (response.status === 429) return retry('RATE_LIMIT', delay);
    if (response.status === 408 || response.status >= 500) return retry('AVAILABILITY', delay);
    if (response.body.length > this.client.maxResponseBytes) return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (response.status !== 200) return branch('providerRejected', response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!isRecord(document) || typeof document.ok !== 'boolean') return branch('invalidResponse', 'PROTOCOL');
    if (!document.ok) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Number.isSafeInteger(document.total) || (document.total as number) < 0 || !Number.isSafeInteger(document.page) || (document.page as number) < 1 || !Number.isSafeInteger(document.limit) || (document.limit as number) < 1 || !Array.isArray(document.items) || document.items.length > limit) return branch('invalidResponse', 'PROTOCOL');
    const entries: KnowledgeEntry[] = [];
    for (const item of document.items) {
      if (!isRecord(item) || !Number.isSafeInteger(item.id) || (item.id as number) <= 0 || typeof item.active !== 'boolean' || !Number.isSafeInteger(item.hit_count) || (item.hit_count as number) < 0 || typeof item.category !== 'string' || item.category.length > 256) return branch('invalidResponse', 'PROTOCOL');
      entries.push({ id: item.id as number, category: item.category, active: item.active, hitCount: item.hit_count as number });
    }
    return newQueryBranch('listed', { entries, total: document.total as number, page: document.page as number, limit: document.limit as number });
  }
}
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function failure(kind: Failure['kind']): Failure { return { provider: 'tiankechat', operation: 'listKnowledgeEntries', kind, message: 'TiankeChat knowledge query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListKnowledgeEntriesOutput> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListKnowledgeEntriesOutput> { return newQueryRetry(failure(kind), delay); }
