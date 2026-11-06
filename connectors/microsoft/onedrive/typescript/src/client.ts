// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { resolveCredential, resolveCredentialAfterRejection, type Call, type CredentialProvider, type CredentialRefreshDriver, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, isHeaderSafeCredential, MaxErrorBodyBytes, parseRetryAfter, ProviderTransportError, readErrorTokens, sendProviderRequest, validateBaseURL, type ProviderHTTPClient, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { Authorization, CredentialFields } from './generated/refresh.js';
import { DriveItemSchema, type Config, type Credentials, type DriveItem, type ItemIdentity } from './generated/models.js';
import { CreateFolderOperation, GetFileOperation, ReadFileTextOperation, SearchFilesOperation, UploadFileOperation } from './operations.js';

/** Graph item metadata projection; download URLs never enter the result. */
export const itemFields = 'id,name,size,webUrl,eTag,cTag,createdDateTime,lastModifiedDateTime,parentReference,file,folder,package,createdBy,lastModifiedBy';
/** Graph search projection. */
export const searchFields = 'id,name,size,webUrl,lastModifiedDateTime,parentReference,file,folder,package';
/** Graph text-read metadata projection. */
export const textFields = 'id,name,size,file,folder,package';
const idPattern = /^[A-Za-z0-9!_.-]{1,256}$/;
const retryableCodes = new Set(['activityLimitReached', 'throttledRequest', 'TooManyRequests', 'serviceNotAvailable']);
const errorPointers = ['/error/code', '/error/innerError/code', '/error/innererror/code'];

/** One exchange with response status retained when a body read fails. */
export interface Exchange {
  /** HTTP status, if a response arrived. */ status: number;
  /** Response headers. */ headers: ProviderResponse['headers'];
  /** Bounded provider body. */ body: Uint8Array;
  /** Sanitized transport or local failure. */ error?: 'defect' | 'transport' | 'read' | 'tooLarge';
  /** Current credential after an optional 401 refresh. */ credentials?: Credentials;
}

/** Injectable transport and clock; application code uses the generated refresh driver. */
export interface OneDriveClientOptions {
  /** HTTP transport, including the provider fake used by conformance. */ transport?: ProviderHTTPClient;
  /** Generated OAuth and client-credentials grant executor. */ refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Observation and Retry-After clock. */ now?: () => Date;
}

/** Microsoft Graph client bound to one renewable connection. */
export class OneDriveClient {
  /** API base URL ending in /v1.0. */ readonly apiBaseURL: string;
  /** Configured Graph origin. */ readonly origin: URL;
  /** Complete item and page body limit in bytes. */ readonly maxResponseBytes: number;
  /** Download content limit in bytes. */ readonly maxTextBytes: number;
  /** Single PUT content limit in bytes. */ readonly maxUploadBytes: number;
  /** Default search page size. */ readonly searchPageSize: number;
  /** Clock for receipts and provider delays. */ readonly now: () => Date;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly refreshDriver: CredentialRefreshDriver<Credentials>;
  private readonly transport: ProviderHTTPClient;

  /** Validate settings and inject a credential source before provider access. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: OneDriveClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Microsoft OneDrive configuration and credentials are required');
    const endpoint = validateBaseURL(config.endpoint || 'https://graph.microsoft.com');
    const maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    const maxTextBytes = config.maxTextBytes ?? 1_048_576;
    const maxUploadBytes = config.maxUploadBytes ?? 5_242_880;
    const searchPageSize = config.searchPageSize ?? 50;
    if (!Number.isSafeInteger(maxResponseBytes) || maxResponseBytes < 1 || !Number.isSafeInteger(maxTextBytes) || maxTextBytes < 1) throw new TypeError('Microsoft OneDrive response and text limits must be positive');
    if (!Number.isSafeInteger(maxUploadBytes) || maxUploadBytes < 1 || maxUploadBytes > 5_242_880) throw new TypeError('Microsoft OneDrive upload limit must be from 1 to 5242880 bytes');
    if (!Number.isSafeInteger(searchPageSize) || searchPageSize < 1 || searchPageSize > 200) throw new TypeError('Microsoft OneDrive search page size must be from 1 to 200');
    this.origin = new URL(endpoint);
    this.apiBaseURL = endpoint + '/v1.0';
    this.maxResponseBytes = maxResponseBytes;
    this.maxTextBytes = maxTextBytes;
    this.maxUploadBytes = maxUploadBytes;
    this.searchPageSize = searchPageSize;
    this.credentials = credentials;
    this.transport = options.transport ?? { send: sendProviderRequest };
    this.now = options.now ?? (() => new Date());
    this.refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Microsoft', auth: Authorization, credentialFields: CredentialFields });
  }

  /** Return the bound searchFiles Query. */ searchFiles(): SearchFilesOperation { return new SearchFilesOperation(this); }
  /** Return the bound getFile Query. */ getFile(): GetFileOperation { return new GetFileOperation(this); }
  /** Return the bound readFileText Query. */ readFileText(): ReadFileTextOperation { return new ReadFileTextOperation(this); }
  /** Return the bound uploadFile Mutation. */ uploadFile(): UploadFileOperation { return new UploadFileOperation(this); }
  /** Return the bound createFolder Mutation. */ createFolder(): CreateFolderOperation { return new CreateFolderOperation(this); }

  /** Resolve generated token grants and enforce app-only drive IDs. */
  async resolveCredentials(call: Call, operation: string, driveID: string): Promise<{ credentials?: Credentials; failure?: Failure }> {
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      if (!isHeaderSafeCredential(credentials.accessToken?.reveal() ?? '') || !['', 'microsoft-oauth', 'microsoft-app-only'].includes(credentials.authMethodId ?? '')) throw new TypeError('invalid token');
      if (!driveID && credentials.authMethodId === 'microsoft-app-only') return { failure: failure(operation, 'VALIDATION', 'driveId is required with app-only authorization, which has no signed-in user') };
      return { credentials };
    } catch { return { failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable') }; }
  }

  /** Address a selected drive or the signed-in user's drive. */
  driveURL(driveID: string): string { return this.apiBaseURL + (driveID ? '/drives/' + pathEscape(driveID) : '/me/drive'); }
  /** Address one item, treating blank and root as the drive root. */
  itemURL(driveID: string, itemID: string): string { return this.driveURL(driveID) + (itemID && itemID !== 'root' ? '/items/' + pathEscape(itemID) : '/root'); }
  /** Address a child by parent folder and escaped name. */
  childURL(driveID: string, parentID: string, name: string): string { return this.itemURL(driveID, parentID) + ':/' + pathEscape(name); }
  /** Accept only one same-origin Graph v1.0 next-page URL. */
  validNextLink(link: string): boolean {
    try { const parsed = new URL(link); const originPath = this.origin.pathname === '/' ? '' : this.origin.pathname; return parsed.protocol === this.origin.protocol && parsed.host === this.origin.host && !parsed.username && !parsed.password && !parsed.hash && parsed.pathname.startsWith(originPath + '/v1.0/'); }
    catch { return false; }
  }

  /** Send a bounded request, repeating one 401 only after coordinated refresh. */
  async send(call: Call, credentials: Credentials, method: string, target: string, options: { body?: Uint8Array; contentType?: string; limit?: number; raw?: boolean; preauthenticated?: boolean } = {}): Promise<Exchange> {
    for (let attempt = 0; attempt < 2; attempt++) {
      let status = 0;
      let headers: ProviderResponse['headers'] = {};
      const request: ProviderRequest = {
        url: target, method,
        headers: { ...(!options.preauthenticated ? { Authorization: 'Bearer ' + (credentials.accessToken?.reveal() ?? '') } : {}), ...(!options.raw ? { Accept: 'application/json' } : {}), ...(options.contentType ? { 'Content-Type': options.contentType } : {}) },
        maxResponseBytes: Math.max(options.limit ?? this.maxResponseBytes, MaxErrorBodyBytes),
        addressPolicy: this.origin.protocol === 'http:' ? 'privateNetworkHTTP' : 'publicHTTPS',
        timeoutMs: 25_000, signal: call.context.cancellationSignal,
        onResponseChunk: (_chunk, observedStatus, observedHeaders) => { status = observedStatus; headers = observedHeaders; },
        ...(options.body !== undefined ? { body: options.body } : {}),
      };
      let response: ProviderResponse;
      try { response = await this.transport.send(request); }
      catch (error) {
        if (error instanceof BodyTooLargeError) {
          status = error.status ?? status; headers = error.headers ?? headers;
          if (status >= 200 && status < 300) return { status, headers, body: new Uint8Array(), error: 'tooLarge' };
          response = { status, headers, body: new Uint8Array() };
        } else if (error instanceof ProviderTransportError || error instanceof Error) {
          return { status, headers, body: new Uint8Array(), error: status ? 'read' : 'transport' };
        } else return { status: 0, headers: {}, body: new Uint8Array(), error: 'transport' };
      }
      status = response.status; headers = response.headers;
      if (status >= 200 && status < 300 && response.body.length > (options.limit ?? this.maxResponseBytes)) return { status, headers, body: new Uint8Array(), error: 'tooLarge' };
      if (status !== 401 || attempt || options.preauthenticated || !('resolveAfterRejection' in this.credentials)) return { status, headers, body: response.body, credentials };
      try { credentials = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal); }
      catch { return { status, headers, body: response.body, credentials }; }
    }
    return { status: 0, headers: {}, body: new Uint8Array(), error: 'defect' };
  }

  /** Classify a Graph read before an operation selects its branch. */
  failedRead(operation: string, exchange: Exchange, branches: { notFound: string; providerRejected: string; invalidResponse: string; defect: string }): { branch: string; failure: Failure; retryAfterMs: number; receipt: ReceiptDraft } | undefined {
    const received = exchange.status ? this.receipt(exchange) : {};
    if (exchange.error === 'defect') return { branch: branches.defect, failure: failure(operation, 'LOCAL_DEFECT', 'provider request could not be built'), retryAfterMs: 0, receipt: {} };
    if (exchange.error === 'tooLarge') return { branch: branches.invalidResponse, failure: failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds the configured limit'), retryAfterMs: 0, receipt: received };
    if (exchange.error) return { branch: '', failure: failure(operation, 'TRANSPORT', 'provider is unavailable'), retryAfterMs: 0, receipt: {} };
    if (exchange.status >= 200 && exchange.status < 300) return undefined;
    const codes = readErrorTokens(exchange.body, errorPointers);
    const kind = statusFailureKind(exchange.status, codes);
    if (retryableStatus(exchange.status, codes)) return { branch: '', failure: failure(operation, kind, 'provider temporarily rejected the request'), retryAfterMs: parseRetryAfter(header(exchange.headers, 'retry-after'), this.now()), receipt: {} };
    if (exchange.status === 404) return { branch: branches.notFound, failure: failure(operation, 'NOT_FOUND', 'item was not found or is not visible to the connection'), retryAfterMs: 0, receipt: received };
    return { branch: branches.providerRejected, failure: failure(operation, kind, 'provider rejected the request'), retryAfterMs: 0, receipt: received };
  }

  /** Safe provider correlation receipt, completed by the SDK runner. */
  receipt(exchange: Exchange, objectID = ''): ReceiptDraft { return { provider: 'microsoft-graph', observedAt: this.now().toISOString(), ...(objectID ? { providerObjectId: objectID } : {}), ...(header(exchange.headers, 'request-id') ? { providerRequestId: header(exchange.headers, 'request-id') } : {}) }; }
}

/** Graph failure with no provider response body. */
export function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'microsoft-graph', operation, message }; }
/** Read a header independent of Node's incoming-header case. */
export function header(headers: ProviderResponse['headers'], name: string): string { const value = headers[name] ?? headers[name.toLowerCase()] ?? headers[name.toUpperCase()]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
/** Go url.PathEscape for one segment. */
export function pathEscape(value: string): string { return escapeBytes(value, false); }
/** Go url.QueryEscape for one value. */
export function queryEscape(value: string): string { return escapeBytes(value, true); }
function escapeBytes(value: string, query: boolean): string {
  let encoded = '';
  for (const byte of Buffer.from(value, 'utf8')) {
    const character = String.fromCharCode(byte);
    if (/[A-Za-z0-9_.~-]/.test(character) || !query && /[$&+:=@]/.test(character)) encoded += character;
    else if (query && byte === 32) encoded += '+';
    else encoded += '%' + byte.toString(16).toUpperCase().padStart(2, '0');
  }
  return encoded;
}
/** Keep fixed OData names literal and encode their values exactly as Go. */
export function odataQuery(...pairs: readonly [string, string][]): string { return pairs.map(([key, value]) => key + '=' + queryEscape(value)).join('&'); }
/** Validate Graph drive and folder IDs before any request. */
export function validateIDs(driveID: string, itemID: string): void { if (driveID && !idPattern.test(driveID)) throw new TypeError('driveId must be a Microsoft Graph drive ID or blank'); if (itemID && !idPattern.test(itemID)) throw new TypeError('folder ID must be a Microsoft Graph item ID, root, or blank'); }
/** Validate OneDrive and SharePoint item names. */
export function validateName(name: string, folder: boolean): void {
  if (!name.trim()) throw new TypeError('name is required');
  if (Buffer.byteLength(name) > 1024) throw new TypeError('name is too long or not valid UTF-8');
  if (name.trim() !== name) throw new TypeError('name cannot start or end with whitespace');
  if (/[/\\*<>?:|#%"]/.test(name)) throw new TypeError('name cannot contain / \\ * < > ? : | # % or "');
  if (name.startsWith('~')) throw new TypeError('name cannot start with ~');
  if (folder && name.endsWith('.')) throw new TypeError('folder name cannot end with a period');
  // eslint-disable-next-line no-control-regex
  if (/[\x00-\x1f\x7f]/.test(name)) throw new TypeError('name cannot contain control characters');
}
/** Graph retry policy excludes unimplemented and full-storage statuses. */
export function retryableStatus(status: number, codes: readonly string[]): boolean { return status !== 501 && status !== 507 && (status === 408 || status === 429 || status >= 500 || codes.some(code => retryableCodes.has(code))); }
/** Go provider status failure-kind precedence. */
export function statusFailureKind(status: number, codes: readonly string[]): Failure['kind'] {
  if (status === 401) return 'AUTHENTICATION';
  if (status === 429 || codes.some(code => retryableCodes.has(code))) return 'RATE_LIMIT';
  if (status === 403) return 'AUTHORIZATION';
  if (status === 404) return 'NOT_FOUND';
  if ([409, 423, 412].includes(status)) return 'CONFLICT';
  if (status === 507) return 'QUOTA_EXHAUSTED';
  if (status >= 500) return 'AVAILABILITY';
  return 'PROVIDER_REJECTION';
}
/** Read Graph error codes from the SDK's bounded parser. */
export function errorCodes(body: Uint8Array): string[] { return readErrorTokens(body, errorPointers); }

interface JSONContext { source: string }
function rawNumberReviver(_key: string, value: unknown, context: JSONContext): unknown {
  if (typeof value === 'number' && !Number.isFinite(value)) throw new TypeError('nonfinite JSON number');
  if ((_key === 'size' || _key === 'childCount') && value !== null && typeof value === 'number') {
    if (!/^-?(0|[1-9][0-9]*)$/.test(context.source) || !Number.isSafeInteger(value)) throw new TypeError(_key + ' cannot be decoded exactly as a Go int64');
  }
  return value;
}
/** Decode exact modeled integers; reject any value JavaScript cannot retain. */
export function decodeJSON(body: Uint8Array): unknown {
  const jsonWithSource = JSON as typeof JSON & { parse(text: string, reviver: (key: string, value: unknown, context: JSONContext) => unknown): unknown };
  return jsonWithSource.parse(Buffer.from(body).toString('utf8'), rawNumberReviver);
}
function object(value: unknown, field: string): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError(field + ' must be an object'); return value as Record<string, unknown>; }
function stringValue(value: unknown, field: string): string { if (value == null) return ''; if (typeof value !== 'string') throw new TypeError(field + ' must be a string'); return value; }
function integerValue(value: unknown, field: string): number { if (value == null) return 0; if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new TypeError(field + ' cannot be decoded exactly as a Go int64'); return value; }
function identity(value: unknown): ItemIdentity | undefined {
  if (value == null) return undefined;
  const identities = object(value, 'identity');
  if (identities.user != null) { const user = object(identities.user, 'user'); return { ...(user.displayName ? { displayName: stringValue(user.displayName, 'displayName') } : {}), ...(user.email ? { email: stringValue(user.email, 'email') } : {}) }; }
  if (identities.application != null) { const application = object(identities.application, 'application'); return { ...(application.displayName ? { displayName: stringValue(application.displayName, 'displayName') } : {}), isApplication: true }; }
  return undefined;
}
/** Strictly decode one Graph DriveItem and project only Go-modeled fields. */
export function decodeItem(raw: unknown): DriveItem {
  const value = object(raw, 'driveItem');
  const id = stringValue(value.id, 'id'), name = stringValue(value.name, 'name');
  if (!idPattern.test(id) || !name) throw new TypeError('item resource lacks a valid ID or name');
  const size = integerValue(value.size, 'size');
  if (size < 0) throw new TypeError('item size is invalid');
  const parent = value.parentReference == null ? {} : object(value.parentReference, 'parentReference');
  const file = value.file == null ? {} : object(value.file, 'file');
  const folder = value.folder == null ? {} : object(value.folder, 'folder');
  const hashes = file.hashes == null ? {} : object(file.hashes, 'hashes');
  const createdBy = identity(value.createdBy), lastModifiedBy = identity(value.lastModifiedBy);
  return DriveItemSchema.parse({
    id, name, isFolder: value.folder != null || value.root != null, sizeBytes: size,
    ...(parent.driveId ? { driveId: stringValue(parent.driveId, 'driveId') } : {}),
    ...(parent.driveType ? { driveType: stringValue(parent.driveType, 'driveType') } : {}),
    ...(parent.siteId ? { siteId: stringValue(parent.siteId, 'siteId') } : {}),
    ...(parent.id ? { parentFolderId: stringValue(parent.id, 'parentReference.id') } : {}),
    ...(parent.path ? { parentPath: stringValue(parent.path, 'path') } : {}),
    ...(value.package != null ? { isPackage: true } : {}),
    ...(folder.childCount != null && integerValue(folder.childCount, 'childCount') !== 0 ? { childCount: integerValue(folder.childCount, 'childCount') } : {}),
    ...(file.mimeType ? { mimeType: stringValue(file.mimeType, 'mimeType') } : {}),
    ...(hashes.quickXorHash ? { quickXorHash: stringValue(hashes.quickXorHash, 'quickXorHash') } : {}),
    ...(value.eTag ? { eTag: stringValue(value.eTag, 'eTag') } : {}),
    ...(value.cTag ? { cTag: stringValue(value.cTag, 'cTag') } : {}),
    ...(value.createdDateTime ? { createdDateTime: stringValue(value.createdDateTime, 'createdDateTime') } : {}),
    ...(value.lastModifiedDateTime ? { lastModifiedDateTime: stringValue(value.lastModifiedDateTime, 'lastModifiedDateTime') } : {}),
    ...(value.webUrl ? { webUrl: stringValue(value.webUrl, 'webUrl') } : {}),
    ...(createdBy ? { createdBy } : {}), ...(lastModifiedBy ? { lastModifiedBy } : {}),
  });
}
