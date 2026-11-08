// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type MutationDefinition, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { DownloadObjectDefinition, ListObjectsDefinition, UploadObjectDefinition } from './generated/definitions.js';
import type { DownloadedObject, DownloadObjectInput, ListObjectsInput, Object as StorageObject, ObjectsPage, UploadObjectInput } from './generated/models.js';
import type { CloudStorageClient, StorageExchange } from './client.js';

const bucketPattern = /^[a-z0-9][a-z0-9._-]{1,220}[a-z0-9]$/;
const generationPattern = /^[1-9][0-9]{0,19}$/;
const emptyObject = (): StorageObject => ({ name: '', generation: '' });
const emptyPage = (): ObjectsPage => ({ objects: [], nextPageToken: '' });
const emptyDownload = (): DownloadedObject => ({ contentBase64: '', contentType: '' });

/** List one bounded page of object identities. */
export class ListObjectsOperation implements Query<ListObjectsInput, ObjectsPage> {
  /** Bind the Cloud Storage client. */ constructor(private readonly client: CloudStorageClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return ListObjectsDefinition; }
  /** Read one object page. */
  async invoke(call: Call, input: ListObjectsInput): Promise<QueryAttempt<ObjectsPage>> {
    const empty = emptyPage();
    if (!input || !validBucket(input.bucket) || !Number.isSafeInteger(input.pageSize) || input.pageSize < 0 || input.pageSize > 1000 ||
        !validText(input.prefix, 1024) || !validText(input.pageToken, 4096))
      return queryBranch('listObjects', 'defect', empty, 'VALIDATION', 'object page input is invalid');
    const pageSize = input.pageSize || 100;
    const query: Record<string, readonly string[]> = { maxResults: [String(pageSize)] };
    if (input.prefix) query.prefix = [input.prefix];
    if (input.pageToken) query.pageToken = [input.pageToken];
    const exchange = await this.client.request(call, `/storage/v1/b/${encodeURIComponent(input.bucket)}/o?${encodeQueryValues(query)}`);
    const failure = classifyRead('listObjects', exchange, empty);
    if (failure) return failure;
    const document = parseJSON(exchange.body);
    if (!isObject(document)) return queryBranch('listObjects', 'invalidResponse', empty, 'PROTOCOL', 'Google returned an invalid object page');
    const items = document.items ?? [];
    const nextPageToken = document.nextPageToken ?? '';
    if (!Array.isArray(items) || items.length > pageSize || !validText(nextPageToken, 4096) ||
        items.some(item => !isObject(item) || !validObjectName(item.name) || !validGeneration(item.generation)))
      return queryBranch('listObjects', 'invalidResponse', empty, 'PROTOCOL', 'Google returned an invalid object page');
    const objects: StorageObject[] = items.map(item => ({ name: item.name as string, generation: item.generation as string }));
    return newQueryBranch('listed', { objects, nextPageToken });
  }
}

/** Download one bounded object body. */
export class DownloadObjectOperation implements Query<DownloadObjectInput, DownloadedObject> {
  /** Bind the Cloud Storage client. */ constructor(private readonly client: CloudStorageClient) {}
  /** Return the generated Query contract. */ definition(): QueryDefinition { return DownloadObjectDefinition; }
  /** Read the selected object generation. */
  async invoke(call: Call, input: DownloadObjectInput): Promise<QueryAttempt<DownloadedObject>> {
    const empty = emptyDownload();
    if (!input || !validBucket(input.bucket) || !validObjectName(input.name) || (input.generation !== '' && !validGeneration(input.generation)))
      return queryBranch('downloadObject', 'defect', empty, 'VALIDATION', 'object identity is invalid');
    const query: Record<string, readonly string[]> = { alt: ['media'] };
    if (input.generation) query.generation = [input.generation];
    const path = `/storage/v1/b/${encodeURIComponent(input.bucket)}/o/${encodeURIComponent(input.name)}?${encodeQueryValues(query)}`;
    const exchange = await this.client.request(call, path, { media: true });
    if (exchange.status === 404 && !exchange.error)
      return queryBranch('downloadObject', 'notFound', empty, 'PROVIDER_REJECTION', 'object was not found');
    const failure = classifyRead('downloadObject', exchange, empty);
    if (failure) return failure;
    return newQueryBranch('downloaded', { contentBase64: Buffer.from(exchange.body).toString('base64'), contentType: exchange.contentType });
  }
}

/** Upload one object only when no live object has the same name. */
export class UploadObjectOperation implements Mutation<UploadObjectInput, StorageObject> {
  /** Bind the Cloud Storage client. */ constructor(private readonly client: CloudStorageClient) {}
  /** Return the generated Mutation contract. */ definition(): MutationDefinition { return UploadObjectDefinition; }
  /** Record stable Dex call identity; the generation precondition prevents duplicate writes. */ idempotencyKey(callId: string): string { return callId; }
  /** Send a conditional upload and compare an existing object after 412. */
  async invoke(call: Call, input: UploadObjectInput): Promise<MutationAttempt<StorageObject>> {
    const empty = emptyObject();
    if (!input || !validBucket(input.bucket) || !validObjectName(input.name) || !validContentType(input.contentType) ||
        typeof input.contentBase64 !== 'string' || Buffer.byteLength(input.contentBase64) > Math.ceil(this.client.maxUploadBytes / 3) * 4 + 4)
      return mutationBranch('defect', empty, 'VALIDATION', 'object upload input is invalid');
    const content = Buffer.from(input.contentBase64, 'base64');
    if (content.length > this.client.maxUploadBytes || content.toString('base64') !== input.contentBase64)
      return mutationBranch('defect', empty, 'VALIDATION', 'object content is invalid or too large');
    const credentials = await this.client.resolveToken(call);
    if (credentials.error === 'credential') return mutationBranch('defect', empty, 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    if (credentials.error) return mutationRetry('TRANSPORT', 'connection credentials could not be refreshed');
    const query = encodeQueryValues({ ifGenerationMatch: ['0'], name: [input.name], uploadType: ['media'] });
    const path = `/upload/storage/v1/b/${encodeURIComponent(input.bucket)}/o?${query}`;
    const exchange = await this.client.request(call, path, { method: 'POST', body: content, contentType: input.contentType, token: credentials.token });
    if (exchange.status === 412 && !exchange.error) return this.confirmExistingObject(call, input, content);
    if (exchange.status === 429) return mutationRetry('RATE_LIMIT', 'Google rate limited the upload', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return mutationRetry('AVAILABILITY', 'Google Cloud Storage upload is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.error) return mutationRetry('TRANSPORT', 'Google Cloud Storage upload failed');
    if (exchange.status !== 200 && exchange.status !== 201)
      return mutationBranch('providerRejected', empty, 'PROVIDER_REJECTION', `Google rejected HTTP ${exchange.status}`);
    const object = projectObject(parseJSON(exchange.body), input.name);
    if (!object) return mutationRetry('PROTOCOL', 'Google returned invalid upload metadata; checking object on retry');
    return newMutationBranch('uploaded', object);
  }

  private async confirmExistingObject(call: Call, input: UploadObjectInput, content: Uint8Array): Promise<MutationAttempt<StorageObject>> {
    const empty = emptyObject();
    const path = `/storage/v1/b/${encodeURIComponent(input.bucket)}/o/${encodeURIComponent(input.name)}`;
    const metadata = await this.client.request(call, path);
    if (metadata.error || metadata.status === 429 || metadata.status >= 500)
      return mutationRetry('AVAILABILITY', 'matching object metadata could not be confirmed', metadata.retryAfterMs);
    if (metadata.status !== 200) return mutationRetry('AVAILABILITY', 'matching object changed during confirmation');
    const object = projectObject(parseJSON(metadata.body), input.name);
    if (!object) return mutationRetry('PROTOCOL', 'Google returned invalid object metadata');
    const query = encodeQueryValues({ alt: ['media'], generation: [object.generation] });
    const media = await this.client.request(call, `${path}?${query}`, { media: true });
    if (media.status === 404) return mutationRetry('AVAILABILITY', 'object changed during upload confirmation');
    if (media.error === 'tooLarge') return mutationBranch('objectExists', empty, 'PROVIDER_REJECTION', 'an object with different content already exists');
    if (media.error || media.status === 429 || media.status >= 500)
      return mutationRetry('AVAILABILITY', 'existing object could not be confirmed', media.retryAfterMs);
    if (media.status !== 200) return mutationBranch('providerRejected', empty, 'PROVIDER_REJECTION', 'Google denied object confirmation');
    if (!Buffer.from(media.body).equals(Buffer.from(content)))
      return mutationBranch('objectExists', empty, 'PROVIDER_REJECTION', 'an object with different content already exists');
    return newMutationBranch('uploaded', object);
  }
}

function classifyRead<Output>(operation: string, exchange: StorageExchange, empty: Output): QueryAttempt<Output> | undefined {
  if (exchange.error === 'credential') return queryBranch(operation, 'defect', empty, 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
  if (exchange.error === 'redirect') return queryBranch(operation, 'invalidResponse', empty, 'PROTOCOL', 'Google returned an unsafe download redirect');
  if (exchange.error === 'request' || exchange.error === 'refresh') return queryRetry(operation, 'TRANSPORT', 'Google Cloud Storage request failed');
  if (exchange.status === 429) return queryRetry(operation, 'RATE_LIMIT', 'Google rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return queryRetry(operation, 'AVAILABILITY', 'Google Cloud Storage is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status !== 200) return queryBranch(operation, 'providerRejected', empty, 'PROVIDER_REJECTION', `Google rejected HTTP ${exchange.status}`);
  if (exchange.error === 'tooLarge') return queryBranch(operation, 'invalidResponse', empty, 'RESPONSE_TOO_LARGE', 'Google response exceeded the configured limit');
  return undefined;
}
function projectObject(value: unknown, name: string): StorageObject | undefined {
  if (!isObject(value) || value.name !== name || !validGeneration(value.generation)) return undefined;
  return { name, generation: value.generation };
}
function validBucket(value: unknown): value is string { return typeof value === 'string' && bucketPattern.test(value); }
function validGeneration(value: unknown): value is string { return typeof value === 'string' && generationPattern.test(value); }
function validText(value: unknown, maxBytes: number): value is string { return typeof value === 'string' && Buffer.byteLength(value) <= maxBytes && !/[\r\n]/.test(value); }
function validObjectName(value: unknown): value is string { return validText(value, 1024) && value !== ''; }
function validContentType(value: unknown): value is string { return typeof value === 'string' && value.length > 0 && value.length <= 256 && !/[\r\n]/.test(value) && /^[^\s/;]+\/[^\s/;]+(?:\s*;.*)?$/.test(value); }
function isObject(value: unknown): value is Record<string, unknown> { return typeof value === 'object' && value !== null && !Array.isArray(value); }
function parseJSON(body: Uint8Array): unknown { try { return JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { return undefined; } }
function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'google-cloud-storage', operation, message }; }
function queryBranch<Output>(operation: string, branch: string, empty: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> { return newQueryBranch(branch, empty, {}, failure(operation, kind, message)); }
function queryRetry<Output>(operation: string, kind: Failure['kind'], message: string, delay = 0): QueryAttempt<Output> { return newQueryRetry(failure(operation, kind, message), delay); }
function mutationBranch(branch: string, empty: StorageObject, kind: Failure['kind'], message: string): MutationAttempt<StorageObject> { return newMutationBranch(branch, empty, {}, failure('uploadObject', kind, message)); }
function mutationRetry(kind: Failure['kind'], message: string, delay = 0): MutationAttempt<StorageObject> { return newMutationRetry(failure('uploadObject', kind, message), delay); }
function encodeQueryValues(values: Record<string, readonly string[]>): string {
  const escape = (value: string): string => encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%20/g, '+');
  const fields: string[] = [];
  for (const key of Object.keys(values).sort()) for (const value of values[key] ?? []) fields.push(`${escape(key)}=${escape(value)}`);
  return fields.join('&');
}
