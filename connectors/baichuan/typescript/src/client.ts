// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListKnowledgeBasesDefinition } from './generated/definitions.js';
import type { Config, Credentials, KnowledgeBase, ListKnowledgeBasesInput, ListKnowledgeBasesOutput } from './generated/models.js';

const emptyOutput: ListKnowledgeBasesOutput = { knowledgeBases: [] };

/** Send one knowledge-base request through a caller-owned transport. */
export type KnowledgeBaseRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional transport dependency for provider tests. */
export interface BaichuanClientOptions {
  /** Request sender; production uses the Connector SDK transport. */
  send?: KnowledgeBaseRequestSender;
}

/** Resolve Baichuan credentials for each knowledge-base query. */
export class BaichuanClient {
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: KnowledgeBaseRequestSender;

  /** Validate configuration without contacting Baichuan. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: BaichuanClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Baichuan configuration and credential source are required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.baichuan-ai.com').replace(/\/$/, '');
    this.timeoutMs = config.timeoutMs ?? 10_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Baichuan timeout and maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the knowledge-base-list Query. */
  listKnowledgeBases(): ListKnowledgeBasesOperation { return new ListKnowledgeBasesOperation(this); }
}

/** Read knowledge bases visible to one Baichuan API key. */
export class ListKnowledgeBasesOperation implements Query<ListKnowledgeBasesInput, ListKnowledgeBasesOutput> {
  /** Bind an authenticated Baichuan client. */
  constructor(private readonly client: BaichuanClient) {}

  /** Return the stable generated Query contract. */
  definition(): QueryDefinition { return ListKnowledgeBasesDefinition; }

  /** Send and classify one knowledge-base-list request. */
  async invoke(call: Call, input: ListKnowledgeBasesInput): Promise<QueryAttempt<ListKnowledgeBasesOutput>> {
    if (input === null || typeof input !== 'object' || Array.isArray(input)) return branch('defect', 'VALIDATION', 'knowledge-base request is invalid');
    let credentials: Credentials;
    try { credentials = await this.client.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid'); }
    const apiKey = credentials.apiKey instanceof SecretString ? credentials.apiKey.reveal() : '';
    if (!isValidAPIKey(apiKey)) return branch('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    let response: ProviderResponse;
    try {
      response = await this.client.send({
        url: `${this.client.baseUrl}/v1/kbs`, method: 'GET',
        headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` },
        timeoutMs: this.client.timeoutMs, maxResponseBytes: this.client.maxResponseBytes,
        addressPolicy: this.client.baseUrl === 'https://api.baichuan-ai.com' ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        const status = classifyStatus(error.status, error.headers ?? {});
        if (status) return status;
        return branch('invalidResponse', 'RESPONSE_TOO_LARGE', 'Baichuan response exceeded the configured limit');
      }
      return retry('AVAILABILITY', 'Baichuan request failed');
    }
    const status = classifyStatus(response.status, response.headers);
    if (status) return status;
    let document: unknown;
    try { document = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(response.body)) as unknown; }
    catch { return branch('invalidResponse', 'PROTOCOL', 'Baichuan returned an invalid knowledge-base list'); }
    if (!isRecord(document) || document.object !== 'list' || !Array.isArray(document.data) || document.data.length > 10_000) return branch('invalidResponse', 'PROTOCOL', 'Baichuan returned an invalid knowledge-base list');
    const knowledgeBases: KnowledgeBase[] = [];
    const seen = new Set<string>();
    for (const item of document.data) {
      if (!isRecord(item) || typeof item.id !== 'string' || !item.id || byteLength(item.id) > 256 || item.object !== 'knowledge-base' || !isOptionalString(item.name) || byteLength(optionalString(item.name)) > 256 || !isOptionalString(item.description) || byteLength(optionalString(item.description)) > 4096 || !isOptionalString(item.status) || !isOptionalInteger(item.created_at) || !isOptionalInteger(item.updated_at) || (optionalInteger(item.created_at) < 0) || (optionalInteger(item.updated_at) < 0) || !(item.file_ids === undefined || item.file_ids === null || Array.isArray(item.file_ids))) return branch('invalidResponse', 'PROTOCOL', 'Baichuan returned an invalid knowledge-base list');
      if (seen.has(item.id)) return branch('invalidResponse', 'PROTOCOL', 'Baichuan returned an invalid knowledge-base list');
      seen.add(item.id);
      const fileIds = item.file_ids === undefined || item.file_ids === null ? null : item.file_ids as unknown[];
      if (fileIds !== null && (fileIds.length > 10_000 || fileIds.some(fileId => typeof fileId !== 'string' || !fileId || byteLength(fileId) > 256))) return branch('invalidResponse', 'PROTOCOL', 'Baichuan returned an invalid knowledge-base list');
      knowledgeBases.push({ id: item.id, name: optionalString(item.name), description: optionalString(item.description), status: optionalString(item.status), fileIds: fileIds as string[] | null, createdAt: optionalInteger(item.created_at), updatedAt: optionalInteger(item.updated_at) });
    }
    return newQueryBranch('listed', { knowledgeBases });
  }
}

function classifyStatus(status: number, headers: ProviderResponse['headers']): QueryAttempt<ListKnowledgeBasesOutput> | undefined {
  const value = headers['retry-after'];
  const delayMs = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '');
  if (status === 429) return retry('RATE_LIMIT', 'Baichuan rate limited the request', delayMs);
  if (status === 408 || status >= 500) return retry('AVAILABILITY', 'Baichuan is temporarily unavailable', delayMs);
  if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION', `Baichuan rejected the request with HTTP ${status}`);
  return undefined;
}
function byteLength(value: string): number { return Buffer.byteLength(value, 'utf8'); }
function optionalString(value: unknown): string { return typeof value === 'string' ? value : ''; }
function optionalInteger(value: unknown): number { return Number.isSafeInteger(value) ? value as number : 0; }
function isOptionalString(value: unknown): boolean { return value === undefined || value === null || typeof value === 'string'; }
function isOptionalInteger(value: unknown): boolean { return value === undefined || value === null || Number.isSafeInteger(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function isValidAPIKey(value: string): boolean { return value.length > 0 && value.length <= 512 && /^[!-~]+$/.test(value); }
function failure(kind: Failure['kind'], message: string): Failure { return { provider: 'baichuan', operation: 'listKnowledgeBases', kind, message }; }
function branch(id: string, kind: Failure['kind'], message: string): QueryAttempt<ListKnowledgeBasesOutput> { return newQueryBranch(id, emptyOutput, {}, failure(kind, message)); }
function retry(kind: Failure['kind'], message: string, delayMs = 0): QueryAttempt<ListKnowledgeBasesOutput> { return newQueryRetry(failure(kind, message), delayMs); }
