// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry,
  type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt,
} from '@superdurable/connectors-sdk';
import {
  ContinueFolderDefinition, DownloadFileDefinition, GetMetadataDefinition, ListFolderDefinition, UploadFileDefinition,
} from './generated/definitions.js';
import {
  ContinueFolderInputSchema, DownloadFileInputSchema, GetMetadataInputSchema, ListFolderInputSchema,
  UploadFileInputSchema,
  type ContinueFolderInput, type DownloadedFile, type DownloadFileInput, type EntryMetadata,
  type FolderPage, type GetMetadataInput, type ListFolderInput, type UploadedFile, type UploadFileInput,
} from './models.js';
import {
  emptyDownloadedFile, emptyEntryMetadata, emptyFolderPage, emptyUploadedFile,
} from './generated/mock.js';
import type { DropboxClient, DropboxExchange } from './client.js';

function failure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'dropbox', operation, message };
}
function queryFailure<T>(operation: string, branch: string, kind: Failure['kind'], message: string, empty: T): QueryAttempt<T> {
  return newQueryBranch(branch, empty, {}, failure(operation, kind, message));
}
function queryRetry<T>(operation: string, kind: Failure['kind'], message: string, delay = 0): QueryAttempt<T> {
  return newQueryRetry(failure(operation, kind, message), delay);
}
function uploadFailure(branch: string, kind: Failure['kind'], message: string): MutationAttempt<UploadedFile> {
  return newMutationBranch(branch, emptyUploadedFile, {}, failure('uploadFile', kind, message));
}
function uploadRetry(kind: Failure['kind'], message: string, delay = 0): MutationAttempt<UploadedFile> {
  return newMutationRetry(failure('uploadFile', kind, message), delay);
}
function validPath(path: string, allowRoot: boolean): boolean {
  if (!path) return allowRoot;
  if (Buffer.byteLength(path, 'utf8') > 4096 || Buffer.from(path, 'utf8').toString('utf8') !== path) return false;
  for (const character of path) if (character.codePointAt(0)! < 32 || character.codePointAt(0) === 127) return false;
  return ['/','id:','ns:','rev:'].some(prefix => path.startsWith(prefix));
}
function requiredString(object: Record<string, unknown>, key: string): string {
  const value = object[key];
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new TypeError('Dropbox field has an invalid type');
  return value;
}
function objectBody(body: Uint8Array): Record<string, unknown> {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('Dropbox object is invalid');
  return value as Record<string, unknown>;
}
function decodeEntry(value: unknown, upload = false): EntryMetadata {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('Dropbox entry is invalid');
  const record = value as Record<string, unknown>;
  const kind = upload ? 'file' : requiredString(record, '.tag');
  const entry = { id: requiredString(record, 'id'), name: requiredString(record, 'name'),
    pathDisplay: requiredString(record, 'path_display'), kind };
  if (!['file', 'folder'].includes(kind) || !entry.id || !entry.name || !entry.pathDisplay) throw new TypeError('Dropbox entry identity is incomplete');
  return entry;
}
function decodeFolderPage(body: Uint8Array): FolderPage {
  const record = objectBody(body);
  if (!Array.isArray(record.entries) || typeof record.has_more !== 'boolean') throw new TypeError('Dropbox folder page is invalid');
  const cursor = requiredString(record, 'cursor');
  if (!cursor) throw new TypeError('Dropbox folder cursor is missing');
  return { entries: record.entries.map(item => decodeEntry(item)), cursor, hasMore: record.has_more };
}
function errorSummary(body: Uint8Array): string {
  try { return requiredString(objectBody(body), 'error_summary'); }
  catch { return ''; }
}
function queryDecision<T>(operation: string, exchange: DropboxExchange, empty: T, notFound = '', cursorExpired = ''): QueryAttempt<T> | undefined {
  if (exchange.error === 'credential') return queryFailure(operation, 'defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid', empty);
  if (exchange.error === 'refresh') return queryRetry(operation, 'AVAILABILITY', 'Dropbox credentials could not be refreshed');
  if (exchange.error === 'request') return queryRetry(operation, 'TRANSPORT', 'Dropbox request failed');
  if (exchange.status === 429) return queryRetry(operation, 'RATE_LIMIT', 'Dropbox rate limited the request', exchange.retryAfterMs);
  if (exchange.status === 408 || exchange.status >= 500) return queryRetry(operation, 'AVAILABILITY', 'Dropbox is temporarily unavailable', exchange.retryAfterMs);
  if (exchange.status === 409) {
    const summary = errorSummary(exchange.body);
    if (notFound && summary.startsWith('path/not_found')) return queryFailure(operation, notFound, 'NOT_FOUND', 'Dropbox did not find the resource', empty);
    if (cursorExpired && summary.startsWith('reset/')) return queryFailure(operation, cursorExpired, 'PROVIDER_REJECTION', 'Dropbox folder cursor expired', empty);
  }
  if (exchange.status !== 200) return queryFailure(operation, 'providerRejected', 'PROVIDER_REJECTION', `Dropbox rejected HTTP ${exchange.status}`, empty);
  if (exchange.error === 'tooLarge') return queryFailure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'Dropbox response exceeded the configured limit', empty);
  if (exchange.error) return queryRetry(operation, 'TRANSPORT', 'Dropbox response was interrupted');
  return undefined;
}

/** One bounded Dropbox folder-page Query. */
export class ListFolderOperation implements Query<ListFolderInput, FolderPage> {
  /** Bind the provider client. */ constructor(private readonly client: DropboxClient) { if (!client) throw new TypeError('Dropbox client is required'); }
  /** Return stable manifest metadata. */ definition() { return ListFolderDefinition; }
  /** List one folder page. */
  async invoke(call: Call, input: ListFolderInput): Promise<QueryAttempt<FolderPage>> {
    input = ListFolderInputSchema.parse(input);
    const path = input.path ?? '';
    if (!validPath(path, true) || !Number.isInteger(input.limit) || input.limit < 1 || input.limit > 2000)
      return queryFailure('listFolder', 'defect', 'VALIDATION', 'folder path or page limit is invalid', emptyFolderPage);
    const exchange = await this.client.postJSON(call, '/2/files/list_folder', { path, limit: input.limit });
    const decision = queryDecision('listFolder', exchange, emptyFolderPage, 'notFound');
    if (decision) return decision;
    try { return newQueryBranch('listed', decodeFolderPage(exchange.body)); }
    catch { return queryFailure('listFolder', 'invalidResponse', 'PROTOCOL', 'Dropbox returned invalid JSON', emptyFolderPage); }
  }
}

/** One Dropbox entry-metadata Query. */
export class GetMetadataOperation implements Query<GetMetadataInput, EntryMetadata> {
  /** Bind the provider client. */ constructor(private readonly client: DropboxClient) { if (!client) throw new TypeError('Dropbox client is required'); }
  /** Return stable manifest metadata. */ definition() { return GetMetadataDefinition; }
  /** Read one file or folder identity. */
  async invoke(call: Call, input: GetMetadataInput): Promise<QueryAttempt<EntryMetadata>> {
    input = GetMetadataInputSchema.parse(input);
    if (!validPath(input.path, false)) return queryFailure('getMetadata', 'defect', 'VALIDATION', 'file or folder path is invalid', emptyEntryMetadata);
    const exchange = await this.client.postJSON(call, '/2/files/get_metadata', { path: input.path });
    const decision = queryDecision('getMetadata', exchange, emptyEntryMetadata, 'notFound');
    if (decision) return decision;
    try { return newQueryBranch('found', decodeEntry(objectBody(exchange.body))); }
    catch { return queryFailure('getMetadata', 'invalidResponse', 'PROTOCOL', 'Dropbox returned invalid JSON', emptyEntryMetadata); }
  }
}

/** One Dropbox cursor-continuation Query. */
export class ContinueFolderOperation implements Query<ContinueFolderInput, FolderPage> {
  /** Bind the provider client. */ constructor(private readonly client: DropboxClient) { if (!client) throw new TypeError('Dropbox client is required'); }
  /** Return stable manifest metadata. */ definition() { return ContinueFolderDefinition; }
  /** Read the next folder page or change set. */
  async invoke(call: Call, input: ContinueFolderInput): Promise<QueryAttempt<FolderPage>> {
    input = ContinueFolderInputSchema.parse(input);
    const cursor = input.cursor;
    if (!cursor || Buffer.byteLength(cursor) > 4096 || Buffer.from(cursor).toString('utf8') !== cursor || /[\r\n]/.test(cursor) || cursor.includes('\0'))
      return queryFailure('continueFolder', 'defect', 'VALIDATION', 'folder cursor is invalid', emptyFolderPage);
    const exchange = await this.client.postJSON(call, '/2/files/list_folder/continue', { cursor });
    const decision = queryDecision('continueFolder', exchange, emptyFolderPage, '', 'cursorExpired');
    if (decision) return decision;
    try { return newQueryBranch('listed', decodeFolderPage(exchange.body)); }
    catch { return queryFailure('continueFolder', 'invalidResponse', 'PROTOCOL', 'Dropbox returned invalid JSON', emptyFolderPage); }
  }
}

/** One bounded Dropbox content Query. */
export class DownloadFileOperation implements Query<DownloadFileInput, DownloadedFile> {
  /** Bind the provider client. */ constructor(private readonly client: DropboxClient) { if (!client) throw new TypeError('Dropbox client is required'); }
  /** Return stable manifest metadata. */ definition() { return DownloadFileDefinition; }
  /** Return one file identity and Base64 content. */
  async invoke(call: Call, input: DownloadFileInput): Promise<QueryAttempt<DownloadedFile>> {
    input = DownloadFileInputSchema.parse(input);
    if (!validPath(input.path, false)) return queryFailure('downloadFile', 'defect', 'VALIDATION', 'file path is invalid', emptyDownloadedFile);
    const exchange = await this.client.download(call, input.path);
    if (exchange.error === 'credential') return queryFailure('downloadFile', 'defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid', emptyDownloadedFile);
    if (exchange.error === 'refresh') return queryRetry('downloadFile', 'AVAILABILITY', 'Dropbox credentials could not be refreshed');
    if (exchange.error === 'request') return queryRetry('downloadFile', 'TRANSPORT', 'Dropbox download request failed');
    if (exchange.status === 429) return queryRetry('downloadFile', 'RATE_LIMIT', 'Dropbox rate limited the download', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return queryRetry('downloadFile', 'AVAILABILITY', 'Dropbox download is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status === 409 && errorSummary(exchange.body).startsWith('path/not_found'))
      return queryFailure('downloadFile', 'notFound', 'NOT_FOUND', 'Dropbox did not find the file', emptyDownloadedFile);
    if (exchange.status !== 200) return queryFailure('downloadFile', 'providerRejected', 'PROVIDER_REJECTION', `Dropbox rejected HTTP ${exchange.status}`, emptyDownloadedFile);
    if (exchange.error === 'tooLarge') return queryFailure('downloadFile', 'tooLarge', 'RESPONSE_TOO_LARGE', 'Dropbox file exceeds the configured download limit', emptyDownloadedFile);
    if (exchange.error) return queryRetry('downloadFile', 'TRANSPORT', 'Dropbox download response was interrupted');
    try {
      const file = decodeEntry(JSON.parse(exchange.metadata));
      if (file.kind !== 'file') throw new TypeError('Dropbox metadata is not a file');
      return newQueryBranch('downloaded', { file, contentBase64: Buffer.from(exchange.body).toString('base64'), contentBytes: exchange.body.length });
    } catch { return queryFailure('downloadFile', 'invalidResponse', 'PROTOCOL', 'Dropbox returned invalid file metadata', emptyDownloadedFile); }
  }
}

/** One bounded Dropbox replacement Mutation. */
export class UploadFileOperation implements Mutation<UploadFileInput, UploadedFile> {
  /** Bind the provider client. */ constructor(private readonly client: DropboxClient) { if (!client) throw new TypeError('Dropbox client is required'); }
  /** Return stable manifest metadata. */ definition() { return UploadFileDefinition; }
  /** Record stable call identity; Dropbox uses absolute overwrite rather than a request key. */ idempotencyKey(callID: string, input: UploadFileInput): string { void input; return callID; }
  /** Overwrite the same path with the same bytes on every retry. */
  async invoke(call: Call, input: UploadFileInput): Promise<MutationAttempt<UploadedFile>> {
    input = UploadFileInputSchema.parse(input);
    if (!input.path.startsWith('/') || !validPath(input.path, false)) return uploadFailure('defect', 'VALIDATION', 'upload path must be absolute');
    if (Math.floor(input.contentBase64.length / 4) * 3 > this.client.maxUploadBytes) return uploadFailure('defect', 'VALIDATION', 'upload exceeds the configured byte limit');
    const content = Buffer.from(input.contentBase64, 'base64');
    if (content.toString('base64') !== input.contentBase64) return uploadFailure('defect', 'VALIDATION', 'upload content must be bounded Base64');
    if (content.length > this.client.maxUploadBytes) return uploadFailure('defect', 'VALIDATION', 'upload content must be bounded Base64');
    const exchange = await this.client.upload(call, input.path, content);
    if (exchange.error === 'credential') return uploadFailure('defect', 'AUTHENTICATION', 'connection credentials are unavailable or invalid');
    if (exchange.error === 'refresh') return uploadRetry('AVAILABILITY', 'Dropbox credentials could not be refreshed');
    if (exchange.error === 'request') return uploadRetry('TRANSPORT', 'Dropbox upload request failed');
    if (exchange.status === 429) return uploadRetry('RATE_LIMIT', 'Dropbox rate limited the upload', exchange.retryAfterMs);
    if (exchange.status === 408 || exchange.status >= 500) return uploadRetry('AVAILABILITY', 'Dropbox upload is temporarily unavailable', exchange.retryAfterMs);
    if (exchange.status !== 200) return uploadFailure('providerRejected', 'PROVIDER_REJECTION', `Dropbox rejected HTTP ${exchange.status}`);
    if (exchange.error === 'tooLarge') return uploadFailure('invalidResponse', 'RESPONSE_TOO_LARGE', 'Dropbox upload metadata exceeded the configured limit');
    if (exchange.error) return uploadRetry('TRANSPORT', 'Dropbox upload response was interrupted');
    try { return newMutationBranch('uploaded', { file: decodeEntry(objectBody(exchange.body), true) }); }
    catch { return uploadFailure('invalidResponse', 'PROTOCOL', 'Dropbox returned invalid upload metadata'); }
  }
}
