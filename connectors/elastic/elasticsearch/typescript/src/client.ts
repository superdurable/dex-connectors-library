// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry,
  type Call, type CredentialProvider, type Failure, type Mutation, type MutationAttempt,
  type Query, type QueryAttempt,
} from '@superdurable/connectors-sdk';
import {
  BodyTooLargeError, ProviderTransportError, parseRetryAfter, sendProviderRequest, validateBaseURL,
  type ProviderRequest, type ProviderResponse,
} from '@superdurable/connectors-sdk/providerhttp';
import { DeleteDocumentDefinition, IndexDocumentDefinition, SearchDocumentsDefinition } from './generated/definitions.js';
import type {
  Config, Credentials, DeleteDocumentInput, DeletedDocument, IndexDocumentInput,
  IndexedDocument, SearchDocumentsInput, SearchHit, SearchPage,
} from './generated/models.js';

const indexPattern = /^[a-z0-9.][a-z0-9._-]*$/;
const emptyIndexed: IndexedDocument = { index: '', id: '' };
const emptyDeleted: DeletedDocument = { index: '', id: '', wasDeleted: false };
const emptyPage: SearchPage = { total: 0, relation: '', hits: [] };

/** Injectable HTTP sender and clock for provider replay. */
export interface ElasticsearchClientOptions {
  /** Provider sender; omitted uses the SDK's redirect-safe transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Clock for Retry-After dates. */
  now?: () => Date;
}

/** One bounded provider outcome without credential-bearing request data. */
export interface ElasticsearchExchange {
  /** HTTP status, or zero without a response. */ status: number;
  /** Complete bounded response body. */ body: Uint8Array;
  /** Retry-After delay in milliseconds. */ retryAfterMs: number;
  /** Safe transport or response classification. */ error?: 'request' | 'tooLarge';
}

/** Elasticsearch REST client with call-time Basic credentials. */
export class ElasticsearchClient {
  /** Validated cluster URL without a trailing slash. */ readonly clusterUrl: string;
  /** Maximum complete document request size in bytes. */ readonly maxDocumentBytes: number;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Validate cluster settings without contacting Elasticsearch. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: ElasticsearchClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Elasticsearch configuration and credentials are required');
    this.clusterUrl = validateBaseURL(config.clusterUrl);
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    this.maxDocumentBytes = config.maxDocumentBytes ?? 1_048_576;
    if (![this.timeoutMs, this.maxResponseBytes, this.maxDocumentBytes].every(value =>
      Number.isSafeInteger(value) && value > 0))
      throw new TypeError('Elasticsearch request limits must be positive integers');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the absolute replacement Mutation. */ indexDocument(): IndexDocumentOperation {
    return new IndexDocumentOperation(this);
  }
  /** Bind the stable-ID deletion Mutation. */ deleteDocument(): DeleteDocumentOperation {
    return new DeleteDocumentOperation(this);
  }
  /** Bind the bounded search Query. */ searchDocuments(): SearchDocumentsOperation {
    return new SearchDocumentsOperation(this);
  }

  /** Resolve Basic authorization without returning credentials in failures. */
  async authorization(call: Call): Promise<string | undefined> {
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      const username = credentials.username;
      const password = credentials.password.reveal();
      if (!username || /[:\r\n\0]/.test(username) || !password || /[\r\n\0]/.test(password)) return undefined;
      return `Basic ${Buffer.from(`${username}:${password}`).toString('base64')}`;
    } catch { return undefined; }
  }

  /** Send one bounded request without automatic redirects. */
  async exchange(call: Call, method: 'GET' | 'PUT' | 'DELETE', url: string,
    headers: Record<string, string>, body?: Uint8Array): Promise<ElasticsearchExchange> {
    const request: ProviderRequest = {
      url, method, headers, maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
      addressPolicy: url.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
      ...(body ? { body } : {}),
    };
    try {
      const response = await this.send(request);
      return { status: response.status, body: response.body,
        retryAfterMs: this.retryAfter(response.headers) };
    } catch (error) {
      if (error instanceof BodyTooLargeError)
        return { status: error.status ?? 0, body: new Uint8Array(),
          retryAfterMs: this.retryAfter(error.headers ?? {}), error: 'tooLarge' };
      if (error instanceof ProviderTransportError)
        return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error: 'request' };
      return { status: 0, body: new Uint8Array(), retryAfterMs: 0, error: 'request' };
    }
  }

  private retryAfter(headers: ProviderResponse['headers']): number {
    const value = Object.entries(headers).find(([name]) => name.toLowerCase() === 'retry-after')?.[1];
    return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '', this.now());
  }
}

/** Fully replace one document under a stable caller-selected ID. */
export class IndexDocumentOperation implements Mutation<IndexDocumentInput, IndexedDocument> {
  /** Bind the provider client. */ constructor(private readonly client: ElasticsearchClient) {}
  /** Return the generated Mutation contract. */ definition() { return IndexDocumentDefinition; }
  /** Use stable Dex call identity; absolute PUT is safe to retry. */
  idempotencyKey(callId: string): string { return callId; }

  /** Retry unconfirmed responses with the same absolute document source. */
  async invoke(call: Call, input: IndexDocumentInput): Promise<MutationAttempt<IndexedDocument>> {
    if (!input || !validIndex(input.index) || !validDocumentID(input.documentId) || !isRecord(input.source))
      return indexFailure('defect', 'VALIDATION');
    let body: Uint8Array;
    try { body = Buffer.from(JSON.stringify(input.source)); }
    catch { return indexFailure('defect', 'VALIDATION'); }
    if (body.byteLength > this.client.maxDocumentBytes) return indexFailure('defect', 'VALIDATION');
    const authorization = await this.client.authorization(call);
    if (!authorization) return indexFailure('defect', 'AUTHENTICATION');
    const exchange = await this.client.exchange(call, 'PUT', documentURL(this.client.clusterUrl, input.index, input.documentId),
      { Accept: 'application/json', 'Content-Type': 'application/json', Authorization: authorization }, body);
    const retry = documentRetry<IndexedDocument>(exchange, 'indexDocument');
    if (retry) return retry;
    if (exchange.status === 200 || exchange.status === 201) {
      const result = parseJSON(exchange.body);
      if (!isRecord(result) || result._index !== input.index || result._id !== input.documentId ||
        (result.result !== 'created' && result.result !== 'updated')) return indexRetry('PROTOCOL');
      return newMutationBranch('indexed', { index: input.index, id: input.documentId });
    }
    return indexFailure('providerRejected', rejectionKind(exchange.status));
  }
}

/** Ensure one stable document ID is absent. */
export class DeleteDocumentOperation implements Mutation<DeleteDocumentInput, DeletedDocument> {
  /** Bind the provider client. */ constructor(private readonly client: ElasticsearchClient) {}
  /** Return the generated Mutation contract. */ definition() { return DeleteDocumentDefinition; }
  /** Use stable Dex call identity; repeating DELETE is safe. */
  idempotencyKey(callId: string): string { return callId; }

  /** Treat both deleted and already absent as success. */
  async invoke(call: Call, input: DeleteDocumentInput): Promise<MutationAttempt<DeletedDocument>> {
    if (!input || !validIndex(input.index) || !validDocumentID(input.documentId))
      return deleteFailure('defect', 'VALIDATION');
    const authorization = await this.client.authorization(call);
    if (!authorization) return deleteFailure('defect', 'AUTHENTICATION');
    const exchange = await this.client.exchange(call, 'DELETE', documentURL(this.client.clusterUrl, input.index, input.documentId),
      { Accept: 'application/json', Authorization: authorization });
    const retry = documentRetry<DeletedDocument>(exchange, 'deleteDocument');
    if (retry) return retry;
    if (exchange.status === 200 || exchange.status === 404) {
      const result = parseJSON(exchange.body);
      if (!isRecord(result)) return deleteRetry('PROTOCOL');
      const error = result.error;
      if (exchange.status === 404 && isRecord(error) && error.type === 'index_not_found_exception')
        return deleteFailure('indexNotFound', 'NOT_FOUND');
      if (result._index !== input.index || result._id !== input.documentId ||
        (result.result !== 'deleted' && result.result !== 'not_found')) return deleteRetry('PROTOCOL');
      return newMutationBranch('absent', {
        index: input.index, id: input.documentId, wasDeleted: result.result === 'deleted',
      });
    }
    return deleteFailure('providerRejected', rejectionKind(exchange.status));
  }
}

/** Search one Elasticsearch index with bounded query-string pagination. */
export class SearchDocumentsOperation implements Query<SearchDocumentsInput, SearchPage> {
  /** Bind the provider client. */ constructor(private readonly client: ElasticsearchClient) {}
  /** Return the generated Query contract. */ definition() { return SearchDocumentsDefinition; }

  /** Reject partial results and retry timed-out or shard-failed searches. */
  async invoke(call: Call, input: SearchDocumentsInput): Promise<QueryAttempt<SearchPage>> {
    const size = input?.size || 10;
    const from = input?.from ?? 0;
    if (!input || !validIndex(input.index) || typeof input.query !== 'string' || !input.query.trim() ||
      Buffer.byteLength(input.query) > 4096 || !Number.isSafeInteger(size) || size < 1 || size > 100 ||
      !Number.isSafeInteger(from) || from < 0 || from + size > 10_000)
      return searchFailure('defect', 'VALIDATION');
    const authorization = await this.client.authorization(call);
    if (!authorization) return searchFailure('defect', 'AUTHENTICATION');
    const query = new URLSearchParams({ q: input.query, size: String(size), from: String(from),
      allow_partial_search_results: 'false' });
    const exchange = await this.client.exchange(call, 'GET',
      `${this.client.clusterUrl}/${input.index}/_search?${query}`,
      { Accept: 'application/json', Authorization: authorization });
    if (exchange.error === 'request') return searchRetry('AVAILABILITY');
    if (exchange.status === 429) return searchRetry('RATE_LIMIT', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status === 409 || exchange.status >= 500)
      return searchRetry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.status === 404) return searchFailure('indexNotFound', 'NOT_FOUND');
    if (exchange.status !== 200) return searchFailure('providerRejected', rejectionKind(exchange.status));
    if (exchange.error === 'tooLarge') return searchFailure('invalidResponse', 'RESPONSE_TOO_LARGE');
    const body = parseJSON(exchange.body);
    if (!isRecord(body) || typeof body.timed_out !== 'boolean' || !isRecord(body._shards))
      return searchFailure('invalidResponse', 'PROTOCOL');
    if (body.timed_out || (typeof body._shards.failed === 'number' && body._shards.failed > 0))
      return searchRetry('AVAILABILITY');
    const hitsRoot = body.hits;
    const total = isRecord(hitsRoot) ? hitsRoot.total : undefined;
    const hits = isRecord(hitsRoot) ? hitsRoot.hits : undefined;
    if (!isRecord(total) || !Number.isSafeInteger(total.value) || (total.value as number) < 0 ||
      (total.relation !== 'eq' && total.relation !== 'gte') || !Array.isArray(hits))
      return searchFailure('invalidResponse', 'PROTOCOL');
    const parsedHits: SearchHit[] = [];
    for (const hit of hits) {
      if (!isRecord(hit) || typeof hit._index !== 'string' || !hit._index ||
        typeof hit._id !== 'string' || !hit._id || !Object.hasOwn(hit, '_source') ||
        (hit._score !== undefined && hit._score !== null && typeof hit._score !== 'number'))
        return searchFailure('invalidResponse', 'PROTOCOL');
      parsedHits.push({ index: hit._index, id: hit._id, source: hit._source,
        ...(typeof hit._score === 'number' ? { score: hit._score } : {}) });
    }
    return newQueryBranch('found', { total: total.value as number, relation: total.relation, hits: parsedHits });
  }
}

function validIndex(index: unknown): index is string {
  return typeof index === 'string' && index.length <= 255 && indexPattern.test(index) && index !== '.' && index !== '..';
}
function validDocumentID(id: unknown): id is string {
  return typeof id === 'string' && !!id && Buffer.byteLength(id) <= 512 && !/[\r\n\0]/.test(id);
}
function documentURL(clusterUrl: string, index: string, documentId: string): string {
  return `${clusterUrl}/${index}/_doc/${encodeURIComponent(documentId)}`;
}
function parseJSON(body: Uint8Array): unknown {
  try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; }
  catch { return undefined; }
}
function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function rejectionKind(status: number): Failure['kind'] {
  return status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION';
}
function failure(operation: string, kind: Failure['kind']): Failure {
  return { kind, provider: 'elasticsearch', operation, message: 'Elasticsearch provider operation failed' };
}
function documentRetry<T>(exchange: ElasticsearchExchange, operation: string): MutationAttempt<T> | undefined {
  if (exchange.status === 429) return newMutationRetry(failure(operation, 'RATE_LIMIT'), exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status === 409 || exchange.status >= 500)
    return newMutationRetry(failure(operation, 'AVAILABILITY'), exchange.retryAfterMs);
  if (exchange.error === 'tooLarge') return newMutationRetry(failure(operation, 'RESPONSE_TOO_LARGE'));
  if (exchange.error) return newMutationRetry(failure(operation, 'TRANSPORT'));
  return undefined;
}
function indexFailure(branch: string, kind: Failure['kind']): MutationAttempt<IndexedDocument> {
  return newMutationBranch(branch, emptyIndexed, {}, failure('indexDocument', kind));
}
function indexRetry(kind: Failure['kind']): MutationAttempt<IndexedDocument> {
  return newMutationRetry(failure('indexDocument', kind));
}
function deleteFailure(branch: string, kind: Failure['kind']): MutationAttempt<DeletedDocument> {
  return newMutationBranch(branch, emptyDeleted, {}, failure('deleteDocument', kind));
}
function deleteRetry(kind: Failure['kind']): MutationAttempt<DeletedDocument> {
  return newMutationRetry(failure('deleteDocument', kind));
}
function searchFailure(branch: string, kind: Failure['kind']): QueryAttempt<SearchPage> {
  return newQueryBranch(branch, emptyPage, {}, failure('searchDocuments', kind));
}
function searchRetry(kind: Failure['kind'], after = 0): QueryAttempt<SearchPage> {
  return newQueryRetry(failure('searchDocuments', kind), after);
}
