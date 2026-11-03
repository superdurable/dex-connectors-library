// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { XMLParser, XMLValidator } from 'fast-xml-parser';
import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, type Call, type Failure, type FailureKind, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { GetObjectTextDefinition, HeadObjectDefinition, ListObjectsDefinition, PutObjectDefinition } from './generated/definitions.js';
import { GetObjectTextInputSchema, HeadObjectInputSchema, ListObjectsInputSchema, PutObjectInputSchema, type GetObjectTextInput, type HeadObjectInput, type ListObjectsInput, type ListObjectsOutput, type ObjectMetadata, type ObjectSummary, type ObjectText, type PutObjectInput, type StoredObject } from './generated/models.js';
import { containsCredential, header, retryAfter, safeHeader, validateKey, type S3Client, type S3Response, type S3Session } from './client.js';

const parser = new XMLParser({ ignoreAttributes: true, parseTagValue: false, processEntities: true });
const zeroList: ListObjectsOutput = { bucket: '', isTruncated: false };
const zeroMetadata: ObjectMetadata = { bucket: '', key: '', sizeBytes: 0 };
const zeroText: ObjectText = { bucket: '', key: '', sizeBytes: 0 };
const zeroStored: StoredObject = { bucket: '', key: '', sizeBytes: 0 };

/** List one bounded ListObjectsV2 page. */
export class ListObjectsOperation implements Query<ListObjectsInput, ListObjectsOutput> {
  /** Bind one client. */ constructor(private readonly client: S3Client) {}
  /** Return the manifest operation identity. */ definition() { return ListObjectsDefinition; }
  /** Send and classify one bucket listing. */
  async invoke(call: Call, input: ListObjectsInput): Promise<QueryAttempt<ListObjectsOutput>> {
    const operation = 'listObjects';
    if (!ListObjectsInputSchema.safeParse(input).success) return defectQuery(operation, zeroList, 'invalid list input');
    let bucket: string;
    try {
      bucket = this.client.bucket(input.bucket);
      for (const field of ['prefix', 'delimiter', 'startAfter'] as const) validateKey(input[field] ?? '', field, true);
      if (input.continuationToken && (Buffer.byteLength(input.continuationToken) > 4096 || !/^[!-~]+$/.test(input.continuationToken))) throw new TypeError('continuationToken must be printable');
    } catch (error) { return defectQuery(operation, zeroList, errorMessage(error)); }
    const maxKeys = input.maxKeys || this.client.listPageSize;
    if (!Number.isSafeInteger(maxKeys) || maxKeys < 1 || maxKeys > 1000) return defectQuery(operation, zeroList, 'maxKeys must be from 1 to 1000');
    const session = await this.client.startSession(call);
    if (!session) return credentialDefectQuery(operation, zeroList);
    const query: Record<string, string> = { 'list-type': '2', 'encoding-type': 'url', 'max-keys': String(maxKeys) };
    if (input.prefix) query.prefix = input.prefix;
    if (input.delimiter) query.delimiter = input.delimiter;
    if (input.startAfter) query['start-after'] = input.startAfter;
    if (input.continuationToken) query['continuation-token'] = input.continuationToken;
    let response: S3Response;
    try { response = await this.client.exchange(call, session, { method: 'GET', bucket, query }); }
    catch { return newQueryRetry(failure(operation, 'TRANSPORT', 'S3 could not be reached or did not finish the list')); }
    const receipt = this.client.receipt(call, response, bucket);
    if (response.status < 200 || response.status >= 300) return queryFailure(operation, 'list', response, session, receipt, { bucket, isTruncated: false }, false, this.client.currentTime());
    if (response.isTooLarge) return newQueryBranch('invalidResponse', { bucket, isTruncated: false }, receipt, failure(operation, 'RESPONSE_TOO_LARGE', 'S3 list page exceeds maxResponseBytes'));
    const contents = Buffer.from(response.body).toString('utf8');
    if (containsCredential(contents, session)) return newQueryBranch('invalidResponse', { bucket, isTruncated: false }, receipt, failure(operation, 'PROTOCOL', 'S3 list page contains credentials'));
    let output: ListObjectsOutput;
    try { output = decodeListPage(contents, bucket, input.prefix, input.delimiter); }
    catch { return newQueryBranch('invalidResponse', { bucket, isTruncated: false }, receipt, failure(operation, 'PROTOCOL', 'S3 returned an invalid list page')); }
    if (!output.objects?.length && !output.commonPrefixes?.length && !output.isTruncated) return newQueryBranch('notFound', output, receipt, failure(operation, 'NOT_FOUND', 'no object matched the prefix'));
    return newQueryBranch('found', output, receipt);
  }
}

/** Read one object's metadata without its body. */
export class HeadObjectOperation implements Query<HeadObjectInput, ObjectMetadata> {
  /** Bind one client. */ constructor(private readonly client: S3Client) {}
  /** Return the manifest operation identity. */ definition() { return HeadObjectDefinition; }
  /** Send and classify one HEAD request. */
  async invoke(call: Call, input: HeadObjectInput): Promise<QueryAttempt<ObjectMetadata>> {
    const operation = 'headObject';
    if (!HeadObjectInputSchema.safeParse(input).success) return defectQuery(operation, zeroMetadata, 'invalid object input');
    let bucket: string;
    try { bucket = this.client.bucket(input.bucket); validateKey(input.key); }
    catch (error) { return defectQuery(operation, zeroMetadata, errorMessage(error)); }
    const session = await this.client.startSession(call);
    if (!session) return credentialDefectQuery(operation, zeroMetadata);
    const identity = { bucket, key: input.key, sizeBytes: 0 };
    let response: S3Response;
    try { response = await this.client.exchange(call, session, { method: 'HEAD', bucket, key: input.key, maxBytes: 65_536 }); }
    catch { return newQueryRetry(failure(operation, 'TRANSPORT', 'S3 could not be reached or did not finish the object metadata read')); }
    const receipt = this.client.receipt(call, response, bucket, input.key);
    if (response.status < 200 || response.status >= 300) return queryFailure(operation, 'object', response, session, receipt, identity, true, this.client.currentTime());
    const metadata = decodeMetadata(response, session, bucket, input.key);
    if (!metadata) return newQueryBranch('invalidResponse', identity, receipt, failure(operation, 'PROTOCOL', 'S3 returned object metadata without a valid size or modification time'));
    return newQueryBranch('found', metadata, receipt);
  }
}

/** Read bounded UTF-8 text from one object. */
export class GetObjectTextOperation implements Query<GetObjectTextInput, ObjectText> {
  /** Bind one client. */ constructor(private readonly client: S3Client) {}
  /** Return the manifest operation identity. */ definition() { return GetObjectTextDefinition; }
  /** Send and classify one object GET. */
  async invoke(call: Call, input: GetObjectTextInput): Promise<QueryAttempt<ObjectText>> {
    const operation = 'getObjectText';
    if (!GetObjectTextInputSchema.safeParse(input).success) return defectQuery(operation, zeroText, 'invalid object input');
    let bucket: string;
    try { bucket = this.client.bucket(input.bucket); validateKey(input.key); }
    catch (error) { return defectQuery(operation, zeroText, errorMessage(error)); }
    const session = await this.client.startSession(call);
    if (!session) return credentialDefectQuery(operation, zeroText);
    const identity: ObjectText = { bucket, key: input.key, sizeBytes: 0 };
    let response: S3Response;
    try { response = await this.client.exchange(call, session, { method: 'GET', bucket, key: input.key, maxBytes: Math.max(this.client.maxTextBytes, 65_536) }); }
    catch { return newQueryRetry(failure(operation, 'TRANSPORT', 'S3 could not be reached or did not finish the object read')); }
    const receipt = this.client.receipt(call, response, bucket, input.key);
    if (response.status < 200 || response.status >= 300) return queryFailure(operation, 'object', response, session, receipt, identity, true, this.client.currentTime());
    const metadata = decodeMetadata(response, session, bucket, input.key);
    const output: ObjectText = { bucket, key: input.key, sizeBytes: metadata?.sizeBytes ?? parseLength(header(response, 'content-length')) ?? 0 };
    const contentType = safeHeader(response, 'content-type', session);
    const etag = safeHeader(response, 'etag', session);
    if (contentType) output.contentType = contentType;
    if (etag) output.eTag = etag;
    if (metadata?.versionId) output.versionId = metadata.versionId;
    if (metadata?.lastModified) output.lastModified = metadata.lastModified;
    const encoding = header(response, 'content-encoding').trim();
    const mediaType = contentType.split(';', 1)[0]?.trim().toLowerCase() ?? '';
    const allowed = mediaType.startsWith('text/') || ['application/json', 'application/ld+json', 'application/xml', 'application/yaml', 'application/x-yaml', 'application/x-ndjson', 'application/javascript', 'application/sql', 'application/csv', 'application/toml'].includes(mediaType) || ['+json', '+xml', '+yaml'].some(suffix => mediaType.endsWith(suffix));
    if ((encoding && encoding.toLowerCase() !== 'identity') || (!allowed && mediaType !== '' && mediaType !== 'application/octet-stream' && mediaType !== 'binary/octet-stream')) return newQueryBranch('unsupportedContent', output, receipt, failure(operation, 'VALIDATION', 'object content is not UTF-8 text'));
    if (output.sizeBytes > this.client.maxTextBytes || response.isTooLarge) return newQueryBranch('tooLarge', output, receipt, failure(operation, 'RESPONSE_TOO_LARGE', 'object size exceeds maxTextBytes'));
    if (metadata?.sizeBytes !== undefined && metadata.sizeBytes !== response.body.length) return newQueryBranch('invalidResponse', output, receipt, failure(operation, 'PROTOCOL', 'S3 returned content whose length differs from Content-Length'));
    let text: string;
    try { text = new TextDecoder('utf-8', { fatal: true }).decode(response.body); }
    catch { return newQueryBranch('unsupportedContent', { ...output, sizeBytes: response.body.length }, receipt, failure(operation, 'VALIDATION', 'object content is not valid UTF-8 text')); }
    output.sizeBytes = response.body.length;
    if (text.includes('\0')) return newQueryBranch('unsupportedContent', output, receipt, failure(operation, 'VALIDATION', 'object content is not valid UTF-8 text'));
    if (containsCredential(text, session)) return newQueryBranch('invalidResponse', output, receipt, failure(operation, 'PROTOCOL', 'object content contains credentials'));
    output.text = text;
    return newQueryBranch('read', output, receipt);
  }
}

/** Replace or create one object with repeat-safe bytes. */
export class PutObjectOperation implements Mutation<PutObjectInput, StoredObject> {
  /** Bind one client. */ constructor(private readonly client: S3Client) {}
  /** Return the manifest operation identity. */ definition() { return PutObjectDefinition; }
  /** Use the stable Dex Call ID as the object marker. */ idempotencyKey(callId: string): string { return callId; }
  /** Send a signed PUT and reconcile create-only precondition failures. */
  async invoke(call: Call, input: PutObjectInput): Promise<MutationAttempt<StoredObject>> {
    const operation = 'putObject';
    if (!PutObjectInputSchema.safeParse(input).success) return defectMutation(zeroStored, 'invalid object input');
    let bucket: string;
    let content: Buffer;
    try {
      bucket = this.client.bucket(input.bucket); validateKey(input.key);
      if (input.textContent && input.byteContent) throw new TypeError('set at most one of textContent and byteContent');
      if (input.byteContent && (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(input.byteContent))) throw new TypeError('byteContent must be base64');
      content = input.textContent ? Buffer.from(input.textContent, 'utf8') : Buffer.from(input.byteContent ?? '', 'base64');
      if (content.length > this.client.maxUploadBytes) throw new TypeError('content exceeds maxUploadBytes');
      validatePutHeaders(input);
      if (!/^[!-~]+$/.test(call.id) || call.id.length > 128) throw new TypeError('idempotency key is missing or not header-safe');
    } catch (error) { return defectMutation(zeroStored, errorMessage(error)); }
    const session = await this.client.startSession(call);
    if (!session) return newMutationBranch('defect', zeroStored, {}, failure(operation, 'AUTHENTICATION', 'S3 connection credentials are unavailable'));
    const requested: StoredObject = { bucket, key: input.key, sizeBytes: content.length, contentType: input.contentType };
    const headers: Record<string, string> = { 'content-type': input.contentType, 'content-md5': createHash('md5').update(content).digest('base64'), 'x-amz-meta-dex-idempotency-key': call.id };
    for (const [name, value] of Object.entries(input.metadata ?? {})) headers[`x-amz-meta-${name}`] = value;
    if (input.isCreateOnly) headers['if-none-match'] = '*';
    let response: S3Response;
    try { response = await this.client.exchange(call, session, { method: 'PUT', bucket, key: input.key, headers, body: content, maxBytes: 65_536 }); }
    catch { return newMutationRetry(failure(operation, 'TRANSPORT', 'S3 could not be reached or did not finish the object write')); }
    const receipt = this.client.receipt(call, response, bucket, input.key);
    if (response.status >= 200 && response.status < 300) {
      const etag = safeHeader(response, 'etag', session);
      const versionId = safeHeader(response, 'x-amz-version-id', session);
      return newMutationBranch('stored', { ...requested, ...(etag ? { eTag: etag } : {}), ...(versionId ? { versionId } : {}) }, receipt);
    }
    if (input.isCreateOnly && response.status === 412) return this.readExisting(call, session, requested);
    const classified = classify(operation, 'object write', response, session, false, this.client.currentTime());
    if (classified.retry) return newMutationRetry(classified.failure, classified.retryAfterMs);
    return newMutationBranch('providerRejected', requested, receipt, classified.failure);
  }

  private async readExisting(call: Call, session: S3Session, requested: StoredObject): Promise<MutationAttempt<StoredObject>> {
    let response: S3Response;
    try { response = await this.client.exchange(call, session, { method: 'HEAD', bucket: requested.bucket, key: requested.key, maxBytes: 65_536 }); }
    catch { return newMutationRetry(failure('putObject', 'TRANSPORT', 'S3 could not be reached or did not finish the object metadata read')); }
    const receipt = this.client.receipt(call, response, requested.bucket, requested.key);
    if (response.status === 404) return newMutationRetry(failure('putObject', 'CONFLICT', 'the object was removed before it could be read'));
    if (response.status < 200 || response.status >= 300) {
      const classified = classify('putObject', 'object', response, session, true, this.client.currentTime());
      if (classified.retry) return newMutationRetry(classified.failure, classified.retryAfterMs);
      if (classified.failure.kind === 'AUTHORIZATION') return newMutationBranch('providerRejected', requested, receipt, classified.failure);
      return newMutationBranch('providerRejected', requested, receipt, classified.failure);
    }
    const metadata = decodeMetadata(response, session, requested.bucket, requested.key);
    if (!metadata) return newMutationBranch('providerRejected', requested, receipt, failure('putObject', 'PROTOCOL', 'S3 returned invalid metadata for the existing object'));
    const stored: StoredObject = { bucket: requested.bucket, key: requested.key, sizeBytes: metadata.sizeBytes, contentType: metadata.contentType ?? '' };
    if (metadata.eTag) stored.eTag = metadata.eTag;
    if (metadata.versionId) stored.versionId = metadata.versionId;
    if (metadata.lastModified) stored.lastModified = metadata.lastModified;
    if (metadata.metadata?.['dex-idempotency-key'] === call.id) return newMutationBranch('stored', { ...stored, isFromEarlierAttempt: true }, receipt);
    return newMutationBranch('alreadyExists', stored, receipt, failure('putObject', 'CONFLICT', 'another object already exists'));
  }
}

function failure(operation: string, kind: FailureKind, message: string): Failure { return { provider: 'amazon-s3', operation, kind, message }; }
function errorMessage(error: unknown): string { return error instanceof Error ? error.message : 'S3 input is invalid'; }
function defectQuery<T>(operation: string, value: T, message: string): QueryAttempt<T> { return newQueryBranch('defect', value, {}, failure(operation, 'VALIDATION', message)); }
function credentialDefectQuery<T>(operation: string, value: T): QueryAttempt<T> { return newQueryBranch('defect', value, {}, failure(operation, 'AUTHENTICATION', 'S3 connection credentials are unavailable')); }
function defectMutation(value: StoredObject, message: string): MutationAttempt<StoredObject> { return newMutationBranch('defect', value, {}, failure('putObject', 'VALIDATION', message)); }

function classify(operation: string, subject: string, response: S3Response, session: S3Session, isObject: boolean, now: Date): { retry: boolean; notFound: boolean; failure: Failure; retryAfterMs: number } {
  const code = response.status === 404 && response.body.length === 0 ? '' : errorCode(response.body, session);
  const status = response.status;
  const delay = retryAfter(response, now);
  let kind: FailureKind = 'PROVIDER_REJECTION';
  let retry = false;
  let notFound = false;
  if (status === 429 || code === 'SlowDown') { kind = 'RATE_LIMIT'; retry = true; }
  else if (status === 408 || code === 'RequestTimeout') { kind = 'TRANSPORT'; retry = true; }
  else if (code === 'ConditionalRequestConflict' || code === 'OperationAborted') { kind = 'CONFLICT'; retry = true; }
  else if (status === 307 || (status >= 500 && status !== 501)) { kind = 'AVAILABILITY'; retry = true; }
  else if (status === 404 && isObject && code !== 'NoSuchBucket') { kind = 'NOT_FOUND'; notFound = true; }
  else if (status === 404) kind = 'NOT_FOUND';
  else if (code === 'RequestTimeTooSkewed' || ['InvalidAccessKeyId', 'SignatureDoesNotMatch', 'ExpiredToken', 'InvalidToken', 'TokenRefreshRequired', 'InvalidSecurity'].includes(code) || status === 401) kind = 'AUTHENTICATION';
  else if (status === 403) kind = 'AUTHORIZATION';
  else if (status === 412 || status === 409) kind = 'CONFLICT';
  return { retry, notFound, failure: failure(operation, kind, `S3 rejected the ${subject} with HTTP ${status}${code ? ` ${code}` : ''}`), retryAfterMs: delay };
}

function queryFailure<T>(operation: string, subject: string, response: S3Response, session: S3Session, receipt: ReturnType<S3Client['receipt']>, value: T, isObject: boolean, now: Date): QueryAttempt<T> {
  const classified = classify(operation, subject, response, session, isObject, now);
  if (classified.retry) return newQueryRetry(classified.failure, classified.retryAfterMs);
  return newQueryBranch(classified.notFound ? 'notFound' : 'providerRejected', value, receipt, classified.failure);
}

function errorCode(body: Uint8Array, session: S3Session): string {
  const text = Buffer.from(body).toString('utf8');
  if (text.length > 65_536 || XMLValidator.validate(text) !== true) return '';
  const parsed = parser.parse(text) as { Error?: { Code?: unknown } };
  const code = parsed.Error?.Code;
  return typeof code === 'string' && /^[A-Za-z0-9.]{1,64}$/.test(code) && !containsCredential(code, session, true) ? code : '';
}

function parseLength(value: string): number | undefined { return /^\d+$/.test(value) && Number.isSafeInteger(Number(value)) ? Number(value) : undefined; }
function parseDate(value: string): string | undefined { const date = new Date(value); return value && !Number.isNaN(date.getTime()) ? date.toISOString().replace('.000Z', 'Z') : undefined; }

function decodeMetadata(response: S3Response, session: S3Session, bucket: string, key: string): ObjectMetadata | undefined {
  const size = parseLength(header(response, 'content-length'));
  const lastModified = parseDate(header(response, 'last-modified'));
  if (size === undefined || !lastModified) return undefined;
  const output: ObjectMetadata = { bucket, key, sizeBytes: size, lastModified };
  for (const [field, name] of Object.entries({ eTag: 'etag', contentType: 'content-type', contentEncoding: 'content-encoding', contentLanguage: 'content-language', contentDisposition: 'content-disposition', cacheControl: 'cache-control', versionId: 'x-amz-version-id', storageClass: 'x-amz-storage-class', serverSideEncryption: 'x-amz-server-side-encryption' })) {
    const value = safeHeader(response, name, session);
    if (value) (output as unknown as Record<string, unknown>)[field] = value;
  }
  const metadata: Record<string, string> = {};
  for (const name of Object.keys(response.headers)) if (name.toLowerCase().startsWith('x-amz-meta-')) {
    const value = safeHeader(response, name, session);
    if (value) metadata[name.slice('x-amz-meta-'.length).toLowerCase()] = value;
  }
  if (Object.keys(metadata).length) output.metadata = metadata;
  const missing = parseLength(header(response, 'x-amz-missing-meta'));
  if (missing && missing > 0) output.missingMetadataCount = missing;
  return output;
}

function decodeListPage(xml: string, bucket: string, prefix?: string, delimiter?: string): ListObjectsOutput {
  if (XMLValidator.validate(xml) !== true) throw new TypeError('invalid XML');
  const document = parser.parse(xml) as { ListBucketResult?: Record<string, unknown> };
  const page = document.ListBucketResult;
  if (!page) throw new TypeError('missing ListBucketResult');
  const isTruncated = page.IsTruncated === 'true';
  const nextContinuationToken = typeof page.NextContinuationToken === 'string' ? page.NextContinuationToken : '';
  if (isTruncated && !nextContinuationToken) throw new TypeError('missing continuation token');
  if (nextContinuationToken && (Buffer.byteLength(nextContinuationToken) > 4096 || !/^[!-~]+$/.test(nextContinuationToken))) throw new TypeError('invalid continuation token');
  const isEncoded = page.EncodingType === 'url';
  const decodeKey = (value: unknown): string => {
    if (typeof value !== 'string' || !value) throw new TypeError('missing key');
    const decoded = isEncoded ? decodeURIComponent(value.replace(/\+/g, ' ')) : value;
    if (!decoded) throw new TypeError('missing key');
    return decoded;
  };
  const objects: ObjectSummary[] = [];
  for (const item of asArray(page.Contents)) {
    const record = asRecord(item);
    const size = parseLength(String(record.Size ?? ''));
    if (size === undefined) throw new TypeError('invalid object size');
    const summary: ObjectSummary = { key: decodeKey(record.Key), sizeBytes: size };
    if (typeof record.ETag === 'string' && record.ETag) summary.eTag = record.ETag;
    if (typeof record.StorageClass === 'string' && record.StorageClass) summary.storageClass = record.StorageClass;
    if (typeof record.LastModified === 'string' && record.LastModified) {
      const date = parseDate(record.LastModified);
      if (!date) throw new TypeError('invalid object date');
      summary.lastModified = date;
    }
    objects.push(summary);
  }
  const commonPrefixes = asArray(page.CommonPrefixes).map(item => decodeKey(asRecord(item).Prefix));
  return { bucket, ...(prefix ? { prefix } : {}), ...(delimiter ? { delimiter } : {}), ...(objects.length ? { objects } : {}), ...(commonPrefixes.length ? { commonPrefixes } : {}), isTruncated, ...(nextContinuationToken ? { nextContinuationToken } : {}) };
}

function asArray(value: unknown): unknown[] { return value === undefined ? [] : Array.isArray(value) ? value : [value]; }
function asRecord(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid XML element'); return value as Record<string, unknown>; }

function validatePutHeaders(input: PutObjectInput): void {
  if (!input.contentType || Buffer.byteLength(input.contentType) > 256 || /[^\x20-\x7E]/.test(input.contentType)) throw new TypeError('contentType must be a printable media type');
  let metadataBytes = 0;
  for (const [name, value] of Object.entries(input.metadata ?? {})) {
    if (!/^[a-z0-9][a-z0-9._-]{0,127}$/.test(name) || name === 'dex-idempotency-key' || /[^\x20-\x7E]/.test(value)) throw new TypeError('user metadata is invalid');
    metadataBytes += Buffer.byteLength(name) + Buffer.byteLength(value);
  }
  if (metadataBytes > 2048) throw new TypeError('user metadata exceeds 2048 bytes');
}
