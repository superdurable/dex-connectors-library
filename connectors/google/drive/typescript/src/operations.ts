// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { readErrorTokens } from '@superdurable/connectors-sdk/providerhttp';
import { CheckpointError, InsufficientTimeError, newFence, type Fence } from '@superdurable/connectors-sdk/singledispatch';
import { GetFileDefinition, ReadFileTextDefinition, SearchFilesDefinition, UploadFileDefinition } from './generated/definitions.js';
import type { File, GetFileInput, ReadFileTextInput, ReadFileTextOutput, SearchFilesInput, SearchFilesOutput, UploadFileInput, UploadFileOutput, Credentials } from './generated/models.js';
import { convertFile, failure, fileSummary, goPathEscape, isDriveId, isPlainMediaType, isRateLimited, jsonObject, quoteDriveQuery, statusFailureKind, type DriveClient, type DriveResponse, type ReadOutcome } from './client.js';

const fullFileFields = 'id,name,mimeType,description,parents,driveId,createdTime,modifiedTime,size,md5Checksum,version,webViewLink,trashed,owners(displayName,emailAddress),lastModifyingUser(displayName,emailAddress),shortcutDetails(targetId,targetMimeType)';
const emptyFile = (): File => ({ id: '', name: '', mimeType: '', isTrashed: false });
const emptySearch = (): SearchFilesOutput => ({ files: [] });
const emptyRead = (): ReadFileTextOutput => ({ fileId: '', name: '', mimeType: '', byteCount: 0 });
const emptyUpload = (): UploadFileOutput => ({ file: emptyFile() });
const driveTextTypes = new Set(['application/json', 'application/ld+json', 'application/xml', 'application/yaml', 'application/x-yaml', 'application/x-ndjson', 'application/javascript', 'application/sql']);
const workspaceExports: Record<string, string> = { 'application/vnd.google-apps.document': 'text/plain', 'application/vnd.google-apps.spreadsheet': 'text/csv', 'application/vnd.google-apps.presentation': 'text/plain' };

function queryOutcome<T>(outcome: ReadOutcome, empty: T): QueryAttempt<T> {
  return outcome.branch ? newQueryBranch(outcome.branch, empty, outcome.receipt, outcome.failure) : newQueryRetry(outcome.failure, outcome.retryAfterMs);
}
function defect<T>(operation: string, value: T, kind: Failure['kind'], message: string): QueryAttempt<T> {
  return newQueryBranch('defect', value, zeroReceipt(), failure(kind, operation, message));
}

/** Search one bounded page of visible Drive files. */
export class SearchFilesOperation implements Query<SearchFilesInput, SearchFilesOutput> {
  constructor(readonly client: DriveClient) { if (!client) throw new TypeError('Drive client is required'); }
  /** Return the manifest definition. */ definition() { return SearchFilesDefinition; }
  /** Build Drive's filtered query and classify one response. */
  async invoke(call: Call, input: SearchFilesInput): Promise<QueryAttempt<SearchFilesOutput>> {
    const pageSize = input.pageSize || this.client.searchPageSize;
    if (input.name && !input.name.trim()) return defect('searchFiles', emptySearch(), 'VALIDATION', 'name cannot be only whitespace');
    if (input.nameMatch && !['exact', 'contains'].includes(input.nameMatch)) return defect('searchFiles', emptySearch(), 'VALIDATION', 'nameMatch must be exact or contains');
    if (input.nameMatch && !input.name) return defect('searchFiles', emptySearch(), 'VALIDATION', 'nameMatch requires a name');
    if (input.mimeType && !isPlainMediaType(input.mimeType)) return defect('searchFiles', emptySearch(), 'VALIDATION', 'mimeType must be one lowercase type/subtype without parameters');
    if (input.parentFolderId && !isDriveId(input.parentFolderId)) return defect('searchFiles', emptySearch(), 'VALIDATION', 'parentFolderId must be a Drive folder ID or root');
    if ((input.pageToken && (Buffer.byteLength(input.pageToken) > 4096 || /[ \r\n\t]/.test(input.pageToken)))) return defect('searchFiles', emptySearch(), 'VALIDATION', 'pageToken is not a Drive page token');
    if (!Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 100) return defect('searchFiles', emptySearch(), 'VALIDATION', 'pageSize must be from 1 to 100');
    let credentials: Credentials;
    try { credentials = await this.client.resolve(call); } catch { return defect('searchFiles', emptySearch(), 'AUTHENTICATION', 'connection credentials are unavailable'); }
    const terms: string[] = [];
    if (input.name) terms.push(`name ${input.nameMatch === 'contains' ? 'contains' : '='} ${quoteDriveQuery(input.name)}`);
    if (input.mimeType) terms.push(`mimeType = ${quoteDriveQuery(input.mimeType)}`);
    if (input.parentFolderId) terms.push(`${quoteDriveQuery(input.parentFolderId)} in parents`);
    terms.push('trashed = false');
    const target = this.client.filesListURL({ q: terms.join(' and '), pageSize: String(pageSize), orderBy: 'modifiedTime desc,name', fields: 'nextPageToken,incompleteSearch,files(id,name,mimeType,parents,modifiedTime,webViewLink)', spaces: 'drive', ...(input.pageToken ? { pageToken: input.pageToken } : {}) });
    const { result, outcome } = await this.client.read(call, credentials, target, this.client.maxResponseBytes, 'providerRejected', 'invalidResponse', 'providerRejected', 'defect', 'searchFiles');
    if (outcome) return queryOutcome(outcome, emptySearch());
    const receipt = this.client.receipt(call, result!.requestId);
    try {
      const document = jsonObject(result!.response.body);
      if (document.files != null && (!Array.isArray(document.files) || document.files.length > pageSize)) throw new TypeError('invalid files');
      if (document.nextPageToken != null && typeof document.nextPageToken !== 'string') throw new TypeError('invalid cursor');
      if (document.incompleteSearch != null && typeof document.incompleteSearch !== 'boolean') throw new TypeError('invalid flag');
      const files = (document.files ?? []).map(resource => fileSummary(convertFile(resource)));
      const output: SearchFilesOutput = { files, ...(document.nextPageToken ? { nextPageToken: document.nextPageToken as string } : {}), ...(document.incompleteSearch ? { isIncompleteSearch: true } : {}) };
      if (!files.length && !output.nextPageToken) return newQueryBranch('notFound', output, receipt, failure('NOT_FOUND', 'searchFiles', 'no file matched the search'));
      return newQueryBranch('found', output, receipt);
    } catch { return newQueryBranch('invalidResponse', emptySearch(), receipt, failure('PROTOCOL', 'searchFiles', 'provider returned an invalid search response')); }
  }
}

/** Read one Drive metadata record. */
export class GetFileOperation implements Query<GetFileInput, File> {
  constructor(readonly client: DriveClient) { if (!client) throw new TypeError('Drive client is required'); }
  /** Return the manifest definition. */ definition() { return GetFileDefinition; }
  /** Classify one bounded files.get response. */
  async invoke(call: Call, input: GetFileInput): Promise<QueryAttempt<File>> {
    if (!isDriveId(input.fileId)) return defect('getFile', emptyFile(), 'VALIDATION', 'fileId must be a Drive file ID');
    let credentials: Credentials;
    try { credentials = await this.client.resolve(call); } catch { return defect('getFile', emptyFile(), 'AUTHENTICATION', 'connection credentials are unavailable'); }
    const { result, outcome } = await this.client.read(call, credentials, this.client.fileMetadataURL(input.fileId, fullFileFields), this.client.maxResponseBytes, 'notFound', 'invalidResponse', 'providerRejected', 'defect', 'getFile');
    if (outcome) return queryOutcome(outcome, emptyFile());
    const receipt = this.client.receipt(call, result!.requestId, input.fileId);
    try {
      const file = convertFile(jsonObject(result!.response.body));
      if (file.id !== input.fileId && input.fileId !== 'root') throw new TypeError('wrong ID');
      return newQueryBranch('found', file, receipt);
    } catch { return newQueryBranch('invalidResponse', emptyFile(), receipt, failure('PROTOCOL', 'getFile', 'provider returned an invalid file response')); }
  }
}

/** Read bounded UTF-8 text or a supported Workspace export. */
export class ReadFileTextOperation implements Query<ReadFileTextInput, ReadFileTextOutput> {
  constructor(readonly client: DriveClient) { if (!client) throw new TypeError('Drive client is required'); }
  /** Return the manifest definition. */ definition() { return ReadFileTextDefinition; }
  /** Fetch metadata before choosing export or download. */
  async invoke(call: Call, input: ReadFileTextInput): Promise<QueryAttempt<ReadFileTextOutput>> {
    if (!isDriveId(input.fileId)) return defect('readFileText', emptyRead(), 'VALIDATION', 'fileId must be a Drive file ID');
    let credentials: Credentials;
    try { credentials = await this.client.resolve(call); } catch { return defect('readFileText', emptyRead(), 'AUTHENTICATION', 'connection credentials are unavailable'); }
    const metadata = await this.client.read(call, credentials, this.client.fileMetadataURL(input.fileId, 'id,name,mimeType,size'), this.client.maxResponseBytes, 'notFound', 'invalidResponse', 'providerRejected', 'defect', 'readFileText');
    if (metadata.outcome) return queryOutcome(metadata.outcome, emptyRead());
    const metadataReceipt = this.client.receipt(call, metadata.result!.requestId, input.fileId);
    let file: File;
    try { file = convertFile(jsonObject(metadata.result!.response.body)); }
    catch { return newQueryBranch('invalidResponse', emptyRead(), metadataReceipt, failure('PROTOCOL', 'readFileText', 'provider returned an invalid file response')); }
    const output: ReadFileTextOutput = { fileId: file.id, name: file.name, mimeType: file.mimeType, byteCount: 0 };
    let contentURL: string;
    const exportMime = workspaceExports[file.mimeType];
    if (exportMime) {
      output.textMimeType = exportMime;
      contentURL = `${this.client.endpoint}/drive/v3/files/${goPathEscape(file.id)}/export?${this.client.query({ mimeType: exportMime })}`;
    } else if (file.mimeType.startsWith('text/') || driveTextTypes.has(file.mimeType)) {
      if ((file.sizeBytes ?? 0) > this.client.maxTextBytes) return newQueryBranch('tooLarge', output, metadataReceipt, failure('RESPONSE_TOO_LARGE', 'readFileText', 'file size exceeds the configured text limit'));
      output.textMimeType = file.mimeType;
      contentURL = `${this.client.endpoint}/drive/v3/files/${goPathEscape(file.id)}?${this.client.query({ alt: 'media', supportsAllDrives: 'true' })}`;
    } else {
      const message = file.mimeType === 'application/vnd.google-apps.folder' ? 'file is a folder' : file.mimeType === 'application/vnd.google-apps.shortcut' ? 'file is a shortcut; read its target file ID from getFile' : file.mimeType.startsWith('application/vnd.google-apps.') ? 'Google Workspace file type has no text export' : 'file type is not text';
      return newQueryBranch('unsupportedContent', output, metadataReceipt, failure('VALIDATION', 'readFileText', message));
    }
    const content = await this.client.read(call, credentials, contentURL, this.client.maxTextBytes, 'notFound', 'tooLarge', 'providerRejected', 'defect', 'readFileText');
    if (content.outcome) {
      if (!content.outcome.branch) return newQueryRetry(content.outcome.failure, content.outcome.retryAfterMs);
      delete output.textMimeType;
      return newQueryBranch(content.outcome.branch, output, content.outcome.receipt, content.outcome.failure);
    }
    const receipt = this.client.receipt(call, content.result!.requestId, file.id);
    const bytes = content.result!.response.body;
    let text: string;
    try { text = new TextDecoder('utf-8', { fatal: true }).decode(bytes); }
    catch { delete output.textMimeType; return newQueryBranch('unsupportedContent', output, receipt, failure('VALIDATION', 'readFileText', 'file content is not valid UTF-8 text')); }
    if (text.startsWith('\uFEFF')) text = text.slice(1);
    if (text.includes('\0')) { delete output.textMimeType; return newQueryBranch('unsupportedContent', output, receipt, failure('VALIDATION', 'readFileText', 'file content is not valid UTF-8 text')); }
    if (text) output.text = text;
    output.byteCount = Buffer.byteLength(text);
    return newQueryBranch('read', output, receipt);
  }
}

/** Create one file with a single-dispatch fence and duplicate read-back. */
export class UploadFileOperation implements Mutation<UploadFileInput, UploadFileOutput> {
  constructor(readonly client: DriveClient) { if (!client) throw new TypeError('Drive client is required'); }
  /** Return the manifest definition. */ definition() { return UploadFileDefinition; }
  /** Derive the stable provider key from the call ID. */ idempotencyKey(callId: string): string { return callId; }
  /** Reconcile retries before allowing a new upload. */
  async invoke(call: Call, input: UploadFileInput): Promise<MutationAttempt<UploadFileOutput>> {
    let fence: Fence;
    try { fence = newFence(call, UploadFileDefinition.stepDefaults.executeMethodTimeoutMs ?? 30_000); }
    catch { return newMutationBranch('defect', emptyUpload(), zeroReceipt(), failure('LOCAL_DEFECT', 'uploadFile', 'single-dispatch fence could not be created')); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return this.uncertain(call, 'upload checkpoint cannot be trusted');
    if (decision.kind === 'readBackRequired' && decision.marker?.callId && decision.marker.callId !== call.id) return this.uncertain(call, 'upload checkpoint belongs to another call');
    let content: Buffer;
    try { content = uploadContent(input, this.client.maxUploadBytes); }
    catch (error) { return newMutationBranch('defect', emptyUpload(), zeroReceipt(), failure('VALIDATION', 'uploadFile', error instanceof Error ? error.message : 'invalid upload input')); }
    const key = call.idempotencyKey ?? '';
    if (!key || Buffer.byteLength(key) > 100) return newMutationBranch('defect', emptyUpload(), zeroReceipt(), failure('LOCAL_DEFECT', 'uploadFile', 'idempotency key is missing or too long'));
    let credentials: Credentials;
    try { credentials = await this.client.resolve(call); }
    catch {
      const unavailable = failure('AUTHENTICATION', 'uploadFile', 'connection credentials are unavailable');
      return decision.kind === 'sendAllowed' ? this.retryBeforeSend(fence, unavailable, 0) : newMutationRetry(unavailable);
    }
    if (decision.kind === 'readBackRequired' && decision.marker?.providerId) {
      const byID = await this.client.read(call, credentials, this.client.fileMetadataURL(decision.marker.providerId, `${fullFileFields},appProperties`), this.client.maxResponseBytes, 'providerRejected', 'invalidResponse', 'providerRejected', 'defect', 'uploadFile');
      if (byID.outcome) return byID.outcome.branch ? this.uncertain(call, 'upload ID read-back could not confirm the outcome') : newMutationRetry(byID.outcome.failure, byID.outcome.retryAfterMs);
      try {
        const resource = jsonObject(byID.result!.response.body);
        if (resource.id !== decision.marker.providerId || !resource.appProperties || typeof resource.appProperties !== 'object' || (resource.appProperties as Record<string, unknown>).dexIdempotencyKey !== key) return this.uncertain(call, 'upload ID read-back found no matching file');
        const file = convertFile(resource);
        return newMutationBranch('uploaded', { file, isFromEarlierAttempt: true }, this.client.receipt(call, byID.result!.requestId, file.id));
      } catch { return this.uncertain(call, 'upload ID read-back could not confirm the outcome'); }
    }
    const lookup = await this.lookup(call, credentials, key);
    if (lookup.outcome) {
      if (decision.kind === 'readBackRequired') return lookup.outcome.branch ? this.uncertain(call, 'upload read-back could not confirm the outcome') : newMutationRetry(lookup.outcome.failure, lookup.outcome.retryAfterMs);
      return lookup.outcome.branch ? newMutationBranch(lookup.outcome.branch, emptyUpload(), lookup.outcome.receipt, lookup.outcome.failure)
        : this.retryBeforeSend(fence, lookup.outcome.failure, lookup.outcome.retryAfterMs);
    }
    if (lookup.file) return newMutationBranch('uploaded', { file: lookup.file, isFromEarlierAttempt: true }, this.client.receipt(call, lookup.result!.requestId, lookup.file.id));
    if (lookup.incomplete) {
      const pending = failure('AVAILABILITY', 'uploadFile', 'provider did not complete the duplicate lookup');
      return decision.kind === 'sendAllowed' ? this.retryBeforeSend(fence, pending, 0) : newMutationRetry(pending);
    }
    if (decision.kind === 'readBackRequired') return this.uncertain(call, 'prior upload cannot be confirmed by read-back');
    const { body, contentType } = multipartUpload(input, content, key);
    let sent: DriveResponse | undefined;
    let sendError: unknown;
    try {
      sent = await fence.dispatch(signal => this.client.request(call, credentials, 'POST', `${this.client.endpoint}/upload/drive/v3/files?${this.client.query({ uploadType: 'multipart', fields: fullFileFields, supportsAllDrives: 'true' })}`, this.client.maxResponseBytes, body, contentType, signal));
    } catch (error) { sendError = error; }
    if (sendError instanceof CheckpointError || sendError instanceof InsufficientTimeError) return newMutationRetry(failure('AVAILABILITY', 'uploadFile', 'upload was not sent before the attempt deadline'));
    const response = sent?.response;
    const requestID = sent?.requestId ?? '';
    const receipt = this.client.receipt(call, requestID);
    if (sendError && isProvablyUndispatched(sendError)) return this.retryAfterRejection(fence, 'TRANSPORT', 'upload failed before a connection was obtained', 0);
    if (response?.status === 429 || response?.status === 503) return this.retryAfterRejection(fence, statusFailureKind(response.status, []), 'provider returned a retryable upload status', this.client.retryDelay(response.headers));
    if (sendError || !response || response.status >= 500 || response.status === 409) return this.reconcile(call, credentials, key, requestID);
    const reasons = readErrorTokens(response.body, ['/error/errors/0/reason', '/error/status']);
    if (response.status === 401) {
      if (await this.client.refreshAfterRejection(call)) return this.retryAfterRejection(fence, 'AUTHENTICATION', 'provider rejected the upload credential', 0);
      return newMutationBranch('providerRejected', emptyUpload(), receipt, failure('AUTHENTICATION', 'uploadFile', 'provider rejected the upload credential'));
    }
    if (isRateLimited(response.status, reasons)) return this.retryAfterRejection(fence, 'RATE_LIMIT', 'provider returned a retryable upload status', this.client.retryDelay(response.headers));
    if (response.status >= 200 && response.status < 300) {
      try {
        const file = convertFile(jsonObject(response.body));
        try { await fence.recordProviderId(file.id); } catch { /* The accepted provider identity remains authoritative. */ }
        return newMutationBranch('uploaded', { file }, this.client.receipt(call, requestID, file.id));
      } catch { return this.reconcile(call, credentials, key, requestID); }
    }
    return newMutationBranch('providerRejected', emptyUpload(), receipt, failure(statusFailureKind(response.status, reasons), 'uploadFile', 'provider rejected the upload'));
  }

  async lookup(call: Call, credentials: Credentials, key: string): Promise<{ file?: File; incomplete?: boolean; result?: DriveResponse; outcome?: ReadOutcome }> {
    const query = `appProperties has { key=${quoteDriveQuery('dexIdempotencyKey')} and value=${quoteDriveQuery(key)} }`;
    const target = this.client.filesListURL({ q: query, fields: `incompleteSearch,files(${fullFileFields})`, pageSize: '10', spaces: 'drive' });
    const answer = await this.client.read(call, credentials, target, this.client.maxResponseBytes, 'providerRejected', 'invalidResponse', 'providerRejected', 'defect', 'uploadFile');
    if (answer.outcome) return { outcome: answer.outcome };
    try {
      const document = jsonObject(answer.result!.response.body);
      if (document.files != null && !Array.isArray(document.files)) throw new TypeError('invalid duplicate list');
      if (document.incompleteSearch != null && typeof document.incompleteSearch !== 'boolean') throw new TypeError('invalid incomplete flag');
      const files = ((document.files ?? []) as unknown[]).map(convertFile);
      const file = files.sort((left, right) => (left.createdTime ?? '').localeCompare(right.createdTime ?? ''))[0];
      return { file, incomplete: document.incompleteSearch === true, result: answer.result };
    } catch { return { outcome: { branch: 'invalidResponse', failure: failure('PROTOCOL', 'uploadFile', 'provider returned an invalid duplicate-lookup response'), receipt: this.client.receipt(call, answer.result!.requestId), retryAfterMs: 0 } }; }
  }

  async reconcile(call: Call, credentials: Credentials, key: string, requestID: string): Promise<MutationAttempt<UploadFileOutput>> {
    const lookup = await this.lookup(call, credentials, key);
    if (lookup.outcome) return lookup.outcome.branch ? this.uncertain(call, 'upload read-back could not confirm the outcome') : newMutationRetry(lookup.outcome.failure, lookup.outcome.retryAfterMs);
    if (lookup.file) return newMutationBranch('uploaded', { file: lookup.file }, this.client.receipt(call, lookup.result!.requestId, lookup.file.id));
    if (lookup.incomplete) return newMutationRetry(failure('AVAILABILITY', 'uploadFile', 'provider did not complete upload read-back'));
    return newMutationUncertain(emptyUpload(), failure('AVAILABILITY', 'uploadFile', 'upload was sent but read-back found no matching file'), this.client.receipt(call, requestID));
  }

  async retryBeforeSend(fence: Fence, reason: Failure, delay: number): Promise<MutationAttempt<UploadFileOutput>> {
    try { await fence.recordNotSent(); }
    catch { return newMutationRetry(failure('AVAILABILITY', 'uploadFile', 'upload was not sent, but its checkpoint failed'), delay); }
    return newMutationRetry(reason, delay);
  }

  async retryAfterRejection(fence: Fence, kind: Failure['kind'], message: string, delay: number): Promise<MutationAttempt<UploadFileOutput>> {
    try { await fence.recordCanResend(); }
    catch { return newMutationRetry(failure('AVAILABILITY', 'uploadFile', 'provider returned a retryable status, but its retry checkpoint failed'), delay); }
    return newMutationRetry(failure(kind, 'uploadFile', message), delay);
  }

  uncertain(call: Call, message: string): MutationAttempt<UploadFileOutput> {
    return newMutationUncertain(emptyUpload(), failure('AVAILABILITY', 'uploadFile', message), this.client.receipt(call));
  }
}

function uploadContent(input: UploadFileInput, limit: number): Buffer {
  if (!input.name.trim()) throw new TypeError('name is required');
  if (input.parentFolderId && !isDriveId(input.parentFolderId)) throw new TypeError('parentFolderId must be a Drive folder ID or root');
  if (!isPlainMediaType(input.mimeType)) throw new TypeError('mimeType must be one lowercase type/subtype without parameters');
  if (input.mimeType.startsWith('application/vnd.google-apps.')) throw new TypeError('mimeType cannot be a Google Workspace type');
  if (input.textContent && input.byteContent) throw new TypeError('set textContent or byteContent, not both');
  let content = Buffer.alloc(0);
  if (input.textContent) content = Buffer.from(input.textContent);
  else if (input.byteContent) {
    const encoded = input.byteContent.replace(/[\r\n]/g, '');
    if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded)) throw new TypeError('byteContent is not valid base64');
    content = Buffer.from(encoded, 'base64');
    if (content.toString('base64') !== encoded) throw new TypeError('byteContent is not valid base64');
  }
  if (content.length > limit) throw new TypeError(`content exceeds the configured upload limit of ${limit} bytes`);
  return content;
}
function multipartUpload(input: UploadFileInput, content: Buffer, key: string): { body: Buffer; contentType: string } {
  const boundary = `dex-${crypto.randomUUID()}`;
  const metadata = { appProperties: { dexIdempotencyKey: key }, mimeType: input.mimeType, name: input.name, ...(input.parentFolderId ? { parents: [input.parentFolderId] } : {}) };
  const body = Buffer.concat([
    Buffer.from(`--${boundary}\r\nContent-Type: application/json; charset=UTF-8\r\n\r\n${JSON.stringify(metadata)}\r\n--${boundary}\r\nContent-Type: ${input.mimeType}\r\n\r\n`),
    content, Buffer.from(`\r\n--${boundary}--\r\n`),
  ]);
  return { body, contentType: `multipart/related; boundary=${boundary}` };
}
function isProvablyUndispatched(error: unknown): boolean {
  return error instanceof Error && 'isProvablyNotSent' in error && error.isProvablyNotSent === true;
}
