// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type CallID, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { parseRetryAfter, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { CreateFolderBranch, CreateFolderDefinition, GetFileBranch, GetFileDefinition, ReadFileTextBranch, ReadFileTextDefinition, SearchFilesBranch, SearchFilesDefinition, UploadFileBranch, UploadFileDefinition } from './generated/definitions.js';
import type { CreateFolderInput, CreateFolderOutput, Credentials, DriveItem, FileSummary, GetFileInput, ReadFileTextInput, ReadFileTextOutput, SearchFilesInput, SearchFilesOutput, UploadFileInput, UploadFileOutput } from './generated/models.js';
import { decodeItem, decodeJSON, errorCodes, failure, header, itemFields, odataQuery, pathEscape, retryableStatus, searchFields, statusFailureKind, textFields, validateIDs, validateName, type Exchange, type OneDriveClient } from './client.js';
import { computeQuickXorHash } from './quick_xor_hash.js';

const zeroItem = (): DriveItem => ({ id: '', name: '', isFolder: false, sizeBytes: 0 });
const zeroSearch = (): SearchFilesOutput => ({ files: [] });
const zeroText = (): ReadFileTextOutput => ({ itemId: '', name: '', sizeBytes: 0, byteCount: 0 });
const zeroUpload = (): UploadFileOutput => ({ item: zeroItem() });
const zeroFolder = (): CreateFolderOutput => ({ folder: zeroItem() });
const mediaTypePattern = /^[a-z0-9!#$%&'*+.^_`|~-]+\/[a-z0-9!#$%&'*+.^_`|~-]+$/;
const textMIMETypes = new Set(['application/json', 'application/ld+json', 'application/xml', 'application/yaml', 'application/x-yaml', 'application/x-ndjson', 'application/javascript', 'application/sql', 'application/x-sh', 'application/toml']);
const textExtensions = new Set(['.txt','.md','.markdown','.csv','.tsv','.json','.ndjson','.xml','.yaml','.yml','.toml','.ini','.cfg','.conf','.log','.sql','.html','.htm','.css','.js','.ts','.py','.go','.sh']);

function queryFailure<T>(client: OneDriveClient, operation: string, exchange: Exchange, zero: T, notFound: string): QueryAttempt<T> | undefined {
  const result = client.failedRead(operation, exchange, { notFound, providerRejected: 'providerRejected', invalidResponse: 'invalidResponse', defect: 'defect' });
  return !result ? undefined : result.branch ? newQueryBranch(result.branch, zero, result.receipt, result.failure) : newQueryRetry(result.failure, result.retryAfterMs);
}
function mutationReadFailure<T>(client: OneDriveClient, operation: string, exchange: Exchange, zero: T): MutationAttempt<T> | undefined {
  const result = client.failedRead(operation, exchange, { notFound: '', providerRejected: 'providerRejected', invalidResponse: 'invalidResponse', defect: 'defect' });
  return !result ? undefined : result.branch ? newMutationBranch(result.branch, zero, result.receipt, result.failure) : newMutationRetry(result.failure, result.retryAfterMs);
}
function resultItem(exchange: Exchange): DriveItem { return decodeItem(decodeJSON(exchange.body)); }
function summary(item: DriveItem): FileSummary { return { id: item.id, name: item.name, isFolder: item.isFolder, sizeBytes: item.sizeBytes, ...(item.driveId ? { driveId: item.driveId } : {}), ...(item.parentFolderId ? { parentFolderId: item.parentFolderId } : {}), ...(item.isPackage ? { isPackage: true } : {}), ...(item.mimeType ? { mimeType: item.mimeType } : {}), ...(item.lastModifiedDateTime ? { lastModifiedDateTime: item.lastModifiedDateTime } : {}), ...(item.webUrl ? { webUrl: item.webUrl } : {}) }; }
function matchesMIME(item: DriveItem, mimeType: string): boolean { return !mimeType || !item.isFolder && (item.mimeType ?? '').toLowerCase() === mimeType.toLowerCase(); }
function matchesItem(item: DriveItem, input: SearchFilesInput): boolean {
  if (input.name) { const name = item.name.toLowerCase(), wanted = input.name.toLowerCase(); if (input.nameMatch === 'contains' ? !name.includes(wanted) : name !== wanted) return false; }
  return matchesMIME(item, input.mimeType ?? '');
}
function retryDelay(headers: ProviderResponse['headers'], client: OneDriveClient): number { return parseRetryAfter(header(headers, 'retry-after'), client.now()); }

/** One bounded Graph search page. */
export class SearchFilesOperation implements Query<SearchFilesInput, SearchFilesOutput> {
  /** Bind the client. */ constructor(private readonly client: OneDriveClient) { if (!client) throw new TypeError('OneDrive client is required'); }
  /** Return stable manifest metadata. */ definition() { return SearchFilesDefinition; }
  /** Read by path, children, or indexed drive-wide search. */
  async invoke(call: Call, input: SearchFilesInput): Promise<QueryAttempt<SearchFilesOutput>> {
    const operation = 'searchFiles', zero = zeroSearch();
    const driveID = input.driveId ?? '', parentID = input.parentFolderId ?? '', name = input.name ?? '', mimeType = input.mimeType ?? '';
    const pageSize = input.pageSize || this.client.searchPageSize;
    try {
      validateIDs(driveID, parentID);
      if (input.nameMatch && !['exact','contains'].includes(input.nameMatch)) throw new TypeError('nameMatch must be exact or contains');
      if (input.nameMatch && !name) throw new TypeError('nameMatch requires a name');
      if (name) {
        if (!name.trim()) throw new TypeError('name cannot be only whitespace');
        if (Buffer.byteLength(name) > 255) throw new TypeError('name must be valid UTF-8 of at most 255 bytes');
        // eslint-disable-next-line no-control-regex
        if (/[/\\*<>?:|#%"\x00-\x1f\x7f]/.test(name)) throw new TypeError('name cannot contain / \\ * < > ? : | # % or "');
      }
      if (mimeType && !mediaTypePattern.test(mimeType)) throw new TypeError('mimeType must be one lowercase type/subtype without parameters');
      if (input.pageToken && (Buffer.byteLength(input.pageToken) > 8192 || /[ \r\n\t]/.test(input.pageToken) || !this.client.validNextLink(input.pageToken))) throw new TypeError('pageToken must be a Microsoft Graph next-page link from an earlier searchFiles result');
      if (!Number.isInteger(pageSize) || pageSize < 1 || pageSize > 200) throw new TypeError('pageSize must be from 1 to 200');
      if (!parentID && !name) throw new TypeError('name is required when parentFolderId is blank, because drive search needs search text');
    } catch (error) { return newQueryBranch(SearchFilesBranch.Defect, zero, zeroReceipt(), failure(operation, 'VALIDATION', (error as Error).message)); }
    const resolved = await this.client.resolveCredentials(call, operation, driveID);
    if (!resolved.credentials) return newQueryBranch(SearchFilesBranch.Defect, zero, zeroReceipt(), resolved.failure);
    const credentials = resolved.credentials;
    const isPath = Boolean(parentID && name && input.nameMatch !== 'contains' && !input.pageToken);
    const isIndexed = !parentID;
    if (isPath) {
      const exchange = await this.client.send(call, credentials, 'GET', this.client.childURL(driveID, parentID, name) + '?' + odataQuery(['$select', searchFields]));
      const failed = queryFailure(this.client, operation, exchange, zero, SearchFilesBranch.NotFound);
      if (failed) {
        if (failed.kind === 'branch' && failed.branch === SearchFilesBranch.NotFound) return newQueryBranch(SearchFilesBranch.NotFound, zero, failed.receipt, failure(operation, 'NOT_FOUND', 'no item matched the search'));
        return failed;
      }
      const received = this.client.receipt(exchange);
      let item: DriveItem;
      try { item = resultItem(exchange); if (item.name.toLowerCase() !== name.toLowerCase()) throw new TypeError('name mismatch'); }
      catch { return newQueryBranch(SearchFilesBranch.InvalidResponse, zero, received, failure(operation, 'PROTOCOL', 'provider returned an invalid item response')); }
      const files = matchesMIME(item, mimeType) ? [summary(item)] : [];
      const output = { files };
      const itemReceipt = this.client.receipt(exchange, item.id);
      return files.length ? newQueryBranch(SearchFilesBranch.Found, output, itemReceipt) : newQueryBranch(SearchFilesBranch.NotFound, output, itemReceipt, failure(operation, 'NOT_FOUND', 'no item matched the search'));
    }
    const query = '?' + odataQuery(['$select', searchFields], ['$top', String(pageSize)]);
    const target = input.pageToken || (isIndexed ? this.client.driveURL(driveID) + "/root/search(q='" + pathEscape(name.replaceAll("'", "''")) + "')" + query : this.client.itemURL(driveID, parentID) + '/children' + query);
    const exchange = await this.client.send(call, credentials, 'GET', target);
    const failed = queryFailure(this.client, operation, exchange, zero, SearchFilesBranch.ProviderRejected);
    if (failed) return failed;
    const received = this.client.receipt(exchange);
    let output: SearchFilesOutput;
    try {
      const page = decodeJSON(exchange.body);
      if (!page || typeof page !== 'object' || Array.isArray(page)) throw new TypeError('invalid collection');
      const record = page as Record<string, unknown>;
      if (record.value != null && !Array.isArray(record.value)) throw new TypeError('invalid collection');
      const values = record.value as unknown[] | undefined ?? [];
      if (values.length > 200) throw new TypeError('too many items');
      const nextLink = record['@odata.nextLink'] ?? '';
      if (typeof nextLink !== 'string' || nextLink && (Buffer.byteLength(nextLink) > 8192 || !this.client.validNextLink(nextLink))) throw new TypeError('invalid next link');
      const files = values.map(decodeItem).filter(item => matchesItem(item, input)).map(summary);
      output = { files, ...(nextLink ? { nextPageToken: nextLink } : {}), ...(isIndexed ? { isIndexedSearch: true } : {}) };
    } catch { return newQueryBranch(SearchFilesBranch.InvalidResponse, zero, received, failure(operation, 'PROTOCOL', 'provider returned an invalid search response')); }
    return !output.files?.length && !output.nextPageToken ? newQueryBranch(SearchFilesBranch.NotFound, output, received, failure(operation, 'NOT_FOUND', 'no item matched the search')) : newQueryBranch(SearchFilesBranch.Found, output, received);
  }
}

/** One drive-item metadata read. */
export class GetFileOperation implements Query<GetFileInput, DriveItem> {
  /** Bind the client. */ constructor(private readonly client: OneDriveClient) { if (!client) throw new TypeError('OneDrive client is required'); }
  /** Return stable manifest metadata. */ definition() { return GetFileDefinition; }
  /** Read one Graph item with strict metadata decoding. */
  async invoke(call: Call, input: GetFileInput): Promise<QueryAttempt<DriveItem>> {
    const operation = 'getFile', zero = zeroItem(), driveID = input.driveId ?? '';
    try { validateIDs(driveID, input.itemId); if (!input.itemId) throw new TypeError('itemId'); }
    catch { return newQueryBranch(GetFileBranch.Defect, zero, zeroReceipt(), failure(operation, 'VALIDATION', 'itemId must be a Microsoft Graph item ID or root, and driveId a drive ID or blank')); }
    const resolved = await this.client.resolveCredentials(call, operation, driveID);
    if (!resolved.credentials) return newQueryBranch(GetFileBranch.Defect, zero, zeroReceipt(), resolved.failure);
    const exchange = await this.client.send(call, resolved.credentials, 'GET', this.client.itemURL(driveID, input.itemId) + '?' + odataQuery(['$select', itemFields]));
    const failed = queryFailure(this.client, operation, exchange, zero, GetFileBranch.NotFound);
    if (failed) return failed;
    try { const item = resultItem(exchange); return newQueryBranch(GetFileBranch.Found, item, this.client.receipt(exchange, item.id)); }
    catch { return newQueryBranch(GetFileBranch.InvalidResponse, zero, this.client.receipt(exchange, input.itemId), failure(operation, 'PROTOCOL', 'provider returned an invalid item response')); }
  }
}

function unsupportedReason(item: DriveItem): string {
  if (item.isFolder) return 'item is a folder';
  if (item.isPackage) return 'item is a package, such as a OneNote notebook';
  const mimeType = (item.mimeType ?? '').split(';', 1)[0]!.trim().toLowerCase();
  if (mimeType.startsWith('text/') || textMIMETypes.has(mimeType) || mimeType.endsWith('+json') || mimeType.endsWith('+xml')) return '';
  const name = item.name.toLowerCase(), extension = name.includes('.') ? '.' + name.split('.').pop()! : '';
  if ((!item.mimeType || item.mimeType === 'application/octet-stream') && textExtensions.has(extension)) return '';
  return 'file type is not text';
}

/** Read bounded UTF-8 content from Graph or one preauthenticated redirect. */
export class ReadFileTextOperation implements Query<ReadFileTextInput, ReadFileTextOutput> {
  /** Bind the client. */ constructor(private readonly client: OneDriveClient) { if (!client) throw new TypeError('OneDrive client is required'); }
  /** Return stable manifest metadata. */ definition() { return ReadFileTextDefinition; }
  /** Fetch metadata before content to avoid downloading unsupported or oversized files. */
  async invoke(call: Call, input: ReadFileTextInput): Promise<QueryAttempt<ReadFileTextOutput>> {
    const operation = 'readFileText', zero = zeroText(), driveID = input.driveId ?? '';
    try { validateIDs(driveID, input.itemId); if (!input.itemId || input.itemId === 'root') throw new TypeError('itemId'); }
    catch { return newQueryBranch(ReadFileTextBranch.Defect, zero, zeroReceipt(), failure(operation, 'VALIDATION', 'itemId must be a Microsoft Graph file item ID, and driveId a drive ID or blank')); }
    const resolved = await this.client.resolveCredentials(call, operation, driveID);
    if (!resolved.credentials) return newQueryBranch(ReadFileTextBranch.Defect, zero, zeroReceipt(), resolved.failure);
    let credentials = resolved.credentials;
    const itemURL = this.client.itemURL(driveID, input.itemId);
    const metadata = await this.client.send(call, credentials, 'GET', itemURL + '?' + odataQuery(['$select', textFields]));
    credentials = metadata.credentials ?? credentials;
    const failed = queryFailure(this.client, operation, metadata, zero, ReadFileTextBranch.NotFound);
    if (failed) return failed;
    let item: DriveItem;
    try { item = resultItem(metadata); }
    catch { return newQueryBranch(ReadFileTextBranch.InvalidResponse, zero, this.client.receipt(metadata, input.itemId), failure(operation, 'PROTOCOL', 'provider returned an invalid item response')); }
    const output: ReadFileTextOutput = { itemId: item.id, name: item.name, sizeBytes: item.sizeBytes, byteCount: 0, ...(item.mimeType ? { mimeType: item.mimeType } : {}) };
    const reason = unsupportedReason(item);
    if (reason) return newQueryBranch(ReadFileTextBranch.UnsupportedContent, output, this.client.receipt(metadata, input.itemId), failure(operation, 'VALIDATION', reason));
    if (item.sizeBytes > this.client.maxTextBytes) return newQueryBranch(ReadFileTextBranch.TooLarge, output, this.client.receipt(metadata, input.itemId), failure(operation, 'RESPONSE_TOO_LARGE', 'file size exceeds the configured text limit'));
    let content = await this.client.send(call, credentials, 'GET', itemURL + '/content', { limit: this.client.maxTextBytes, raw: true });
    const first = this.classifyContent(output, content, false);
    if (first) return first;
    if (redirectStatus(content.status)) {
      const location = header(content.headers, 'location');
      if (!this.validDownloadURL(location)) return newQueryBranch(ReadFileTextBranch.InvalidResponse, output, this.client.receipt(content, item.id), failure(operation, 'PROTOCOL', 'provider returned an unusable download redirect'));
      const downloaded = await this.client.send(call, credentials, 'GET', location, { limit: this.client.maxTextBytes, raw: true, preauthenticated: true });
      const second = this.classifyContent(output, downloaded, true, content);
      if (second) return second;
      if (redirectStatus(downloaded.status)) return newQueryBranch(ReadFileTextBranch.InvalidResponse, output, this.client.receipt(content, item.id), failure(operation, 'PROTOCOL', 'file download redirected more than once'));
      content = { ...downloaded, headers: content.headers };
    }
    let text: string;
    try { text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(content.body); if (text.includes('\0')) throw new TypeError('NUL'); }
    catch { return newQueryBranch(ReadFileTextBranch.UnsupportedContent, output, this.client.receipt(content, item.id), failure(operation, 'VALIDATION', 'file content is not valid UTF-8 text')); }
    return newQueryBranch(ReadFileTextBranch.Read, { ...output, ...(text ? { text } : {}), byteCount: content.body.length }, this.client.receipt(content, item.id));
  }
  private validDownloadURL(location: string): boolean {
    try { const parsed = new URL(location); if (!parsed.hostname || parsed.username || parsed.password || parsed.hash) return false; if (parsed.protocol === 'https:') return true; return parsed.protocol === 'http:' && this.client.origin.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(parsed.hostname); }
    catch { return false; }
  }
  private classifyContent(output: ReadFileTextOutput, exchange: Exchange, download: boolean, graphResponse?: Exchange): QueryAttempt<ReadFileTextOutput> | undefined {
    const operation = 'readFileText', receipt = this.client.receipt(graphResponse ?? exchange, output.itemId);
    if (exchange.error === 'tooLarge') return newQueryBranch(ReadFileTextBranch.TooLarge, output, receipt, failure(operation, 'RESPONSE_TOO_LARGE', 'file content exceeds the configured text limit'));
    if (exchange.error === 'defect') return newQueryBranch(ReadFileTextBranch.Defect, output, receipt, failure(operation, 'LOCAL_DEFECT', 'provider request could not be built'));
    if (exchange.error) return newQueryRetry(failure(operation, 'TRANSPORT', 'file download failed'));
    if (exchange.status >= 200 && exchange.status < 300 || redirectStatus(exchange.status)) return undefined;
    if (download && [401,403].includes(exchange.status)) return newQueryRetry(failure(operation, 'AUTHORIZATION', 'file download link expired'));
    return queryFailure(this.client, operation, exchange, output, ReadFileTextBranch.NotFound);
  }
}
function redirectStatus(status: number): boolean { return [301,302,303,307,308].includes(status); }

/** Write bytes at a deterministic parent-and-name path. */
export class UploadFileOperation implements Mutation<UploadFileInput, UploadFileOutput> {
  /** Bind the client. */ constructor(private readonly client: OneDriveClient) { if (!client) throw new TypeError('OneDrive client is required'); }
  /** Return stable manifest metadata. */ definition() { return UploadFileDefinition; }
  /** Use the connector call ID as the receipt's stable deduplication key. */ idempotencyKey(callID: CallID, _input: UploadFileInput): string { void _input; return callID; }
  /** PUT file bytes and reconcile a conflict by content hash. */
  async invoke(call: Call, input: UploadFileInput): Promise<MutationAttempt<UploadFileOutput>> {
    const operation = 'uploadFile', zero = zeroUpload(), driveID = input.driveId ?? '', parentID = input.parentFolderId ?? '';
    let content: Uint8Array;
    try {
      validateIDs(driveID, parentID); validateName(input.name, false);
      if (!['fail','replace'].includes(input.conflictBehavior)) throw new TypeError('conflictBehavior must be fail or replace');
      if (!mediaTypePattern.test(input.mimeType)) throw new TypeError('mimeType must be one lowercase type/subtype without parameters');
      if (input.textContent && input.byteContent) throw new TypeError('set textContent or byteContent, not both');
      if (input.byteContent && (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(input.byteContent))) throw new TypeError('invalid base64 byteContent');
      content = input.textContent ? Buffer.from(input.textContent, 'utf8') : Buffer.from(input.byteContent ?? '', 'base64');
      if (content.length > this.client.maxUploadBytes) throw new TypeError(`content exceeds the configured upload limit of ${this.client.maxUploadBytes} bytes`);
    } catch (error) { return newMutationBranch(UploadFileBranch.Defect, zero, zeroReceipt(), failure(operation, 'VALIDATION', (error as Error).message)); }
    const resolved = await this.client.resolveCredentials(call, operation, driveID);
    if (!resolved.credentials) return newMutationBranch(UploadFileBranch.Defect, zero, zeroReceipt(), resolved.failure);
    let credentials = resolved.credentials;
    const target = this.client.childURL(driveID, parentID, input.name) + ':/content?' + odataQuery(['@microsoft.graph.conflictBehavior', input.conflictBehavior]);
    const exchange = await this.client.send(call, credentials, 'PUT', target, { body: content, contentType: input.mimeType });
    credentials = exchange.credentials ?? credentials;
    const received = this.client.receipt(exchange);
    if (exchange.error === 'defect') return newMutationBranch(UploadFileBranch.Defect, zero, received, failure(operation, 'LOCAL_DEFECT', 'provider request could not be built'));
    if (exchange.error) return newMutationRetry(failure(operation, 'TRANSPORT', 'provider upload outcome is unknown'));
    const codes = errorCodes(exchange.body);
    if (exchange.status >= 200 && exchange.status < 300) {
      try { const item = resultItem(exchange); if (item.isFolder) throw new TypeError('folder'); return newMutationBranch(UploadFileBranch.Uploaded, { item }, this.client.receipt(exchange, item.id)); }
      catch { return newMutationRetry(failure(operation, 'PROTOCOL', 'provider returned an invalid upload response')); }
    }
    if (exchange.status === 409 || exchange.status === 423) return this.readBack(call, credentials, input, content, codes);
    if (retryableStatus(exchange.status, codes)) return newMutationRetry(failure(operation, statusFailureKind(exchange.status, codes), 'provider temporarily rejected the upload'), retryDelay(exchange.headers, this.client));
    return newMutationBranch(UploadFileBranch.ProviderRejected, zero, received, failure(operation, statusFailureKind(exchange.status, codes), 'provider rejected the upload'));
  }
  private async readBack(call: Call, credentials: Credentials, input: UploadFileInput, content: Uint8Array, codes: readonly string[]): Promise<MutationAttempt<UploadFileOutput>> {
    const operation = 'uploadFile', zero = zeroUpload(), driveID = input.driveId ?? '';
    const target = this.client.childURL(driveID, input.parentFolderId ?? '', input.name) + '?' + odataQuery(['$select', itemFields]);
    const exchange = await this.client.send(call, credentials, 'GET', target);
    const failed = mutationReadFailure(this.client, operation, exchange, zero);
    if (failed) return exchange.status === 404 && failed.kind === 'retry' ? newMutationRetry(failure(operation, 'CONFLICT', 'conflicting item is not readable yet')) : failed;
    const received = this.client.receipt(exchange);
    let item: DriveItem;
    try { item = resultItem(exchange); }
    catch { return newMutationBranch(UploadFileBranch.InvalidResponse, zero, received, failure(operation, 'PROTOCOL', 'provider returned an invalid item response')); }
    const itemReceipt = this.client.receipt(exchange, item.id);
    if (!item.isFolder && !item.isPackage && !item.quickXorHash) return newMutationRetry(failure(operation, 'AVAILABILITY', "provider has not reported the existing file's content hash yet"));
    const identical = !item.isFolder && !item.isPackage && item.name.toLowerCase() === input.name.toLowerCase() && item.sizeBytes === content.length && item.quickXorHash === computeQuickXorHash(content);
    if (identical) return newMutationBranch(UploadFileBranch.Uploaded, { item, isExistingFileIdentical: true }, itemReceipt);
    if (input.conflictBehavior === 'fail' && codes.includes('nameAlreadyExists')) return newMutationBranch(UploadFileBranch.AlreadyExists, { item }, itemReceipt, failure(operation, 'CONFLICT', 'an item with this name already exists'));
    return newMutationBranch(UploadFileBranch.ProviderRejected, zero, itemReceipt, failure(operation, 'CONFLICT', 'provider rejected the upload because the item is locked or conflicts'));
  }
}

/** Create one folder or accept the same existing folder. */
export class CreateFolderOperation implements Mutation<CreateFolderInput, CreateFolderOutput> {
  /** Bind the client. */ constructor(private readonly client: OneDriveClient) { if (!client) throw new TypeError('OneDrive client is required'); }
  /** Return stable manifest metadata. */ definition() { return CreateFolderDefinition; }
  /** Use the connector call ID as the receipt's stable deduplication key. */ idempotencyKey(callID: CallID, _input: CreateFolderInput): string { void _input; return callID; }
  /** POST one folder and classify a same-name item through read-back. */
  async invoke(call: Call, input: CreateFolderInput): Promise<MutationAttempt<CreateFolderOutput>> {
    const operation = 'createFolder', zero = zeroFolder(), driveID = input.driveId ?? '', parentID = input.parentFolderId ?? '';
    try { validateIDs(driveID, parentID); validateName(input.name, true); }
    catch (error) { return newMutationBranch(CreateFolderBranch.Defect, zero, zeroReceipt(), failure(operation, 'VALIDATION', (error as Error).message)); }
    const resolved = await this.client.resolveCredentials(call, operation, driveID);
    if (!resolved.credentials) return newMutationBranch(CreateFolderBranch.Defect, zero, zeroReceipt(), resolved.failure);
    let credentials = resolved.credentials;
    const body = Buffer.from(JSON.stringify({ '@microsoft.graph.conflictBehavior': 'fail', folder: {}, name: input.name }), 'utf8');
    const target = this.client.itemURL(driveID, parentID) + '/children?' + odataQuery(['@microsoft.graph.conflictBehavior', 'fail']);
    const exchange = await this.client.send(call, credentials, 'POST', target, { body, contentType: 'application/json' });
    credentials = exchange.credentials ?? credentials;
    const received = this.client.receipt(exchange);
    if (exchange.error === 'defect') return newMutationBranch(CreateFolderBranch.Defect, zero, received, failure(operation, 'LOCAL_DEFECT', 'provider request could not be built'));
    if (exchange.error) return newMutationRetry(failure(operation, 'TRANSPORT', 'provider folder outcome is unknown'));
    const codes = errorCodes(exchange.body);
    if (exchange.status >= 200 && exchange.status < 300) {
      try { const item = resultItem(exchange); if (!item.isFolder) throw new TypeError('not folder'); return newMutationBranch(CreateFolderBranch.Created, { folder: item }, this.client.receipt(exchange, item.id)); }
      catch { return newMutationRetry(failure(operation, 'PROTOCOL', 'provider returned an invalid folder response')); }
    }
    if (exchange.status === 409 && codes.includes('nameAlreadyExists')) return this.readBack(call, credentials, input);
    if (retryableStatus(exchange.status, codes)) return newMutationRetry(failure(operation, statusFailureKind(exchange.status, codes), 'provider temporarily rejected the folder creation'), retryDelay(exchange.headers, this.client));
    return newMutationBranch(CreateFolderBranch.ProviderRejected, zero, received, failure(operation, statusFailureKind(exchange.status, codes), 'provider rejected the folder creation'));
  }
  private async readBack(call: Call, credentials: Credentials, input: CreateFolderInput): Promise<MutationAttempt<CreateFolderOutput>> {
    const operation = 'createFolder', zero = zeroFolder(), driveID = input.driveId ?? '';
    const target = this.client.childURL(driveID, input.parentFolderId ?? '', input.name) + '?' + odataQuery(['$select', itemFields]);
    const exchange = await this.client.send(call, credentials, 'GET', target);
    const failed = mutationReadFailure(this.client, operation, exchange, zero);
    if (failed) return exchange.status === 404 && failed.kind === 'retry' ? newMutationRetry(failure(operation, 'CONFLICT', 'conflicting item is not readable yet')) : failed;
    const received = this.client.receipt(exchange);
    let item: DriveItem;
    try { item = resultItem(exchange); }
    catch { return newMutationBranch(CreateFolderBranch.InvalidResponse, zero, received, failure(operation, 'PROTOCOL', 'provider returned an invalid item response')); }
    const itemReceipt = this.client.receipt(exchange, item.id);
    return item.isFolder ? newMutationBranch(CreateFolderBranch.Created, { folder: item, isExistingFolder: true }, itemReceipt) : newMutationBranch(CreateFolderBranch.NameConflict, { folder: item }, itemReceipt, failure(operation, 'CONFLICT', 'a file already has this name'));
  }
}
