// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { resolveCredential, resolveCredentialAfterRejection, type Call, type CredentialRefreshDriver, type Failure, type FailureKind, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, MaxErrorBodyBytes, parseRetryAfter, readErrorTokens, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { isRefreshRequired, ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials, File, FileSummary } from './generated/models.js';
import { GetFileOperation, ReadFileTextOperation, SearchFilesOperation, UploadFileOperation } from './operations.js';

/** Provider transport, clock, and generated credential refresh collaborators. */
export interface DriveClientOptions {
  /** Override the HTTP transport for provider tests or a controlled gateway. */
  transport?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Override the clock for deterministic receipts and Retry-After. */
  now?: () => Date;
  /** Override the generated refresh driver for controlled credential tests. */
  refreshDriver?: CredentialRefreshDriver<Credentials>;
}

/** A classified Drive read failure with its safe receipt. */
export type ReadOutcome = { branch?: string; failure: Failure; retryAfterMs: number; receipt: ReceiptDraft };
/** Raw Drive response and the credential used for it. */
export type DriveResponse = { response: ProviderResponse; requestId: string; credentials: Credentials };

/** Shared provider client for all Drive operations. */
export class DriveClient {
  readonly endpoint: string;
  readonly maxResponseBytes: number;
  readonly maxTextBytes: number;
  readonly maxUploadBytes: number;
  readonly searchPageSize: number;
  readonly #credentials: CredentialSource;
  readonly #transport: (request: ProviderRequest) => Promise<ProviderResponse>;
  readonly #refreshDriver: CredentialRefreshDriver<Credentials>;
  readonly #now: () => Date;

  /** Validate the manifest settings and bind a host-owned credential source. */
  constructor(config: Config, credentials: CredentialSource, options: DriveClientOptions = {}) {
    if (!credentials) throw new TypeError('Google Drive credential source is required');
    this.endpoint = validateBaseURL(config.endpoint ?? 'https://www.googleapis.com');
    this.maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
    this.maxTextBytes = config.maxTextBytes ?? 1_048_576;
    this.maxUploadBytes = config.maxUploadBytes ?? 5_242_880;
    this.searchPageSize = config.searchPageSize ?? 25;
    if (![this.maxResponseBytes, this.maxTextBytes, this.maxUploadBytes, this.searchPageSize].every(value => Number.isSafeInteger(value) && value > 0)
      || this.maxUploadBytes > 5_242_880 || this.searchPageSize > 100) throw new TypeError('Google Drive connection limits are invalid');
    this.#credentials = credentials;
    this.#transport = options.transport ?? sendProviderRequest;
    this.#now = options.now ?? (() => new Date());
    this.#refreshDriver = options.refreshDriver ?? {
      refreshRequired: state => isRefreshRequired(Boolean(state.credentials.accessToken?.reveal()), state.expiresAt, state.now, 'refresh'),
      refresh: (state, signal) => new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Google', auth: Authorization, credentialFields: CredentialFields }).refresh(state, signal),
    };
  }

  /** Bind searchFiles to this connection. */
  searchFiles(): SearchFilesOperation { return new SearchFilesOperation(this); }
  /** Bind getFile to this connection. */
  getFile(): GetFileOperation { return new GetFileOperation(this); }
  /** Bind readFileText to this connection. */
  readFileText(): ReadFileTextOperation { return new ReadFileTextOperation(this); }
  /** Bind uploadFile to this connection. */
  uploadFile(): UploadFileOperation { return new UploadFileOperation(this); }

  /** Resolve a credential outside Flow state. */
  async resolve(call: Call): Promise<Credentials> {
    const credentials = await resolveCredential(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal);
    if (!credentials.accessToken || !isHeaderSafeCredential(credentials.accessToken.reveal()) ||
      !['', 'google-oauth', 'workspace-domain-delegation'].includes(credentials.authMethodId ?? '')) throw new CredentialError();
    return credentials;
  }

  /** Send one request and allow one coordinated refresh after a 401. */
  async request(call: Call, credentials: Credentials, method: string, target: string, limit: number, body?: Uint8Array, contentType?: string, signal?: AbortSignal): Promise<DriveResponse> {
    let active = credentials;
    for (let attempt = 0; attempt < 2; attempt++) {
      const token = active.accessToken?.reveal() ?? '';
      if (!isHeaderSafeCredential(token)) throw new CredentialError();
      const request: ProviderRequest = {
        url: target, method, headers: { Authorization: `Bearer ${token}`, ...(contentType ? { 'Content-Type': contentType } : {}) },
        maxResponseBytes: limit, maxErrorResponseBytes: Math.max(limit, MaxErrorBodyBytes), ignoreErrorBodyReadFailure: true,
        addressPolicy: 'unrestricted', timeoutMs: 25_000,
        ...(body ? { body } : {}), ...(signal ? { signal } : {}),
      };
      let response: ProviderResponse;
      try { response = await this.#transport(request); }
      catch (error) {
        if (error instanceof BodyTooLargeError && error.status && (error.status < 200 || error.status >= 300))
          response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        else throw error;
      }
      const requestId = header(response.headers, 'x-goog-request-id') || header(response.headers, 'x-request-id');
      if (response.status !== 401 || method === 'POST' || attempt > 0 || !('resolveAfterRejection' in this.#credentials)) return { response, requestId, credentials: active };
      try {
        active = await resolveCredentialAfterRejection(this.#credentials, call, this.#refreshDriver, signal ?? call.context.cancellationSignal);
        if (!active.accessToken || !isHeaderSafeCredential(active.accessToken.reveal())) return { response, requestId, credentials };
      } catch { return { response, requestId, credentials }; }
    }
    throw new CredentialError();
  }

  /** Force one host-coordinated refresh after an upload credential rejection. */
  async refreshAfterRejection(call: Call): Promise<boolean> {
    if (!('resolveAfterRejection' in this.#credentials)) return false;
    try {
      const credentials = await resolveCredentialAfterRejection(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal);
      return Boolean(credentials.accessToken && isHeaderSafeCredential(credentials.accessToken.reveal()));
    } catch { return false; }
  }

  /** Classify an idempotent read and retain its response on success. */
  async read(call: Call, credentials: Credentials, target: string, limit: number, notFound: string, tooLarge: string, rejected: string, defect: string, operation: string): Promise<{ result?: DriveResponse; outcome?: ReadOutcome }> {
    let result: DriveResponse;
    try { result = await this.request(call, credentials, 'GET', target, limit); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status && error.status >= 200 && error.status < 300)
        return { outcome: { branch: tooLarge, failure: failure('RESPONSE_TOO_LARGE', operation, 'provider response exceeds the configured limit'), retryAfterMs: 0, receipt: this.receipt(call, header(error.headers ?? {}, 'x-goog-request-id')) } };
      if (error instanceof CredentialError) return { outcome: { branch: defect, failure: failure('LOCAL_DEFECT', operation, 'provider request could not be built'), retryAfterMs: 0, receipt: {} } };
      return { outcome: { failure: failure('TRANSPORT', operation, 'provider is unavailable'), retryAfterMs: 0, receipt: {} } };
    }
    const { response, requestId } = result;
    const receipt = this.receipt(call, requestId);
    if (response.status >= 200 && response.status < 300 && response.body.length > limit)
      return { outcome: { branch: tooLarge, failure: failure('RESPONSE_TOO_LARGE', operation, 'provider response exceeds the configured limit'), retryAfterMs: 0, receipt } };
    if (response.status >= 200 && response.status < 300) return { result };
    const reasons = readErrorTokens(response.body, ['/error/errors/0/reason', '/error/status']);
    const kind = statusFailureKind(response.status, reasons);
    if (response.status >= 500 || isRateLimited(response.status, reasons)) return { outcome: { failure: failure(kind, operation, 'provider temporarily rejected the request'), retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after'), this.#now()), receipt: {} } };
    if (response.status === 404) return { outcome: { branch: notFound, failure: failure('NOT_FOUND', operation, 'file was not found or is not visible to the connection'), retryAfterMs: 0, receipt } };
    if (reasons[0] === 'exportSizeLimitExceeded') return { outcome: { branch: tooLarge, failure: failure('RESPONSE_TOO_LARGE', operation, 'Google export size limit was exceeded'), retryAfterMs: 0, receipt } };
    return { outcome: { branch: rejected, failure: failure(kind, operation, 'provider rejected the request'), retryAfterMs: 0, receipt } };
  }

  /** Build the same sorted query encoding as Go url.Values.Encode. */
  query(values: Record<string, string>): string {
    return Object.keys(values).sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right))).map(key => `${goQueryEscape(key)}=${goQueryEscape(values[key]!)}`).join('&');
  }

  /** Build a Drive files.list URL across visible drives. */
  filesListURL(query: Record<string, string>): string {
    return `${this.endpoint}/drive/v3/files?${this.query({ ...query, corpora: 'allDrives', supportsAllDrives: 'true', includeItemsFromAllDrives: 'true' })}`;
  }

  /** Build a file metadata URL. */
  fileMetadataURL(fileId: string, fields: string): string {
    return `${this.endpoint}/drive/v3/files/${goPathEscape(fileId)}?${this.query({ fields, supportsAllDrives: 'true' })}`;
  }

  /** Build a safe correlation receipt without credential material. */
  receipt(call: Call, requestId = '', objectId = ''): ReceiptDraft {
    return { callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'google-drive',
      ...(objectId ? { providerObjectId: objectId } : {}), ...(requestId ? { providerRequestId: requestId } : {}), observedAt: this.#now().toISOString() };
  }

  /** Parse Retry-After against the injected clock. */
  retryDelay(headers: Record<string, unknown>): number { return parseRetryAfter(header(headers, 'retry-after'), this.#now()); }
}

/** An unusable access credential. */
export class CredentialError extends Error { constructor() { super('connection credentials are unavailable'); } }
/** Construct a sanitized provider failure. */
export function failure(kind: FailureKind, operation: string, message: string): Failure { return { kind, provider: 'google-drive', operation, message }; }
/** Read a response header case insensitively. */
export function header(headers: Record<string, unknown>, name: string): string {
  const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1];
  return Array.isArray(value) ? String(value[0] ?? '') : typeof value === 'string' ? value : '';
}
/** Match Go's PathEscape safe characters, including reserved subdelimiters. */
export function goPathEscape(value: string): string { return encodeURIComponent(value).replace(/%24/g, '$').replace(/%26/g, '&').replace(/%2B/g, '+').replace(/%3D/g, '=').replace(/%3A/g, ':').replace(/%40/g, '@').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`); }
/** Match Go's QueryEscape safe set. */
export function goQueryEscape(value: string): string { return encodeURIComponent(value).replace(/%20/g, '+').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`); }
/** Validate a Drive ID or root alias. */
export function isDriveId(value: string): boolean { return /^[A-Za-z0-9_-]{1,256}$/.test(value); }
/** Validate a lowercase media type without parameters. */
export function isPlainMediaType(value: string): boolean { return /^[a-z0-9!#$&^_.+-]+\/[a-z0-9!#$&^_.+-]+$/.test(value); }
/** Escape a Drive search literal. */
export function quoteDriveQuery(value: string): string { return `'${value.replaceAll('\\', '\\\\').replaceAll("'", "\\'")}'`; }
/** Classify a Google status and machine readable reason. */
export function statusFailureKind(status: number, reasons: string[]): FailureKind {
  if (status === 401) return 'AUTHENTICATION';
  if (isRateLimited(status, reasons)) return 'RATE_LIMIT';
  if (status === 403) return 'AUTHORIZATION';
  if (status === 404) return 'NOT_FOUND';
  if (status === 409) return 'CONFLICT';
  if (status >= 500) return 'AVAILABILITY';
  return 'PROVIDER_REJECTION';
}
/** Classify Google rate limiting. */
export function isRateLimited(status: number, reasons: string[]): boolean { return status === 429 || (status === 403 && ['rateLimitExceeded', 'userRateLimitExceeded'].includes(reasons[0] ?? '')); }
/** Convert a provider resource without coercing wrong-typed fields. */
export function convertFile(value: unknown): File {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid file');
  const resource = value as Record<string, unknown>;
  if (typeof resource.id !== 'string' || !isDriveId(resource.id) || typeof resource.name !== 'string' || !resource.name || typeof resource.mimeType !== 'string' || !resource.mimeType) throw new TypeError('invalid file');
  if (resource.appProperties != null && (typeof resource.appProperties !== 'object' || Array.isArray(resource.appProperties) || Object.values(resource.appProperties).some(property => typeof property !== 'string'))) throw new TypeError('invalid app properties');
  const result: File = { id: resource.id, name: resource.name, mimeType: resource.mimeType, isTrashed: resource.trashed === true };
  for (const field of ['description', 'driveId', 'md5Checksum', 'webViewLink'] as const) {
    if (resource[field] !== undefined && resource[field] !== null && typeof resource[field] !== 'string') throw new TypeError('invalid file');
    if (resource[field]) result[field] = resource[field] as string;
  }
  for (const field of ['createdTime', 'modifiedTime'] as const) {
    if (resource[field] !== undefined && resource[field] !== null) {
      if (typeof resource[field] !== 'string') throw new TypeError('invalid file time');
      result[field] = goRFC3339(resource[field] as string);
    }
  }
  if (resource.parents !== undefined && resource.parents !== null) {
    if (!Array.isArray(resource.parents) || resource.parents.some(parent => typeof parent !== 'string')) throw new TypeError('invalid parents');
    if (resource.parents.length) result.parents = resource.parents as string[];
  }
  for (const [source, target] of [['size', 'sizeBytes'], ['version', 'version']] as const) {
    if (resource[source] !== undefined && resource[source] !== null) {
      if (typeof resource[source] !== 'string' || !/^[+-]?[0-9]+$/.test(resource[source] as string)) throw new TypeError('invalid integer');
      const parsed = Number(resource[source]); if (!Number.isSafeInteger(parsed)) throw new TypeError('invalid integer');
      if (parsed < 0) throw new TypeError('invalid integer');
      if (parsed) result[target] = parsed;
    }
  }
  if (resource.trashed !== undefined && resource.trashed !== null && typeof resource.trashed !== 'boolean') throw new TypeError('invalid trashed');
  for (const [source, target] of [['owners', 'owners'], ['lastModifyingUser', 'lastModifyingUser']] as const) {
    if (resource[source] === undefined || resource[source] === null) continue;
    const users = target === 'owners' ? resource[source] : [resource[source]];
    if (!Array.isArray(users) || users.some(user => !user || typeof user !== 'object' || Array.isArray(user) ||
      Object.entries(user).some(([key, field]) => !['displayName', 'emailAddress'].includes(key) || typeof field !== 'string'))) throw new TypeError('invalid user');
    if (target === 'owners' && users.length) result.owners = users as File['owners'];
    if (target === 'lastModifyingUser') result.lastModifyingUser = users[0] as File['lastModifyingUser'];
  }
  if (resource.shortcutDetails !== undefined && resource.shortcutDetails !== null) {
    const shortcut = resource.shortcutDetails;
    if (!shortcut || typeof shortcut !== 'object' || Array.isArray(shortcut)) throw new TypeError('invalid shortcut');
    const fields = shortcut as Record<string, unknown>;
    if (typeof fields.targetId !== 'string' || (fields.targetMimeType !== undefined && typeof fields.targetMimeType !== 'string')) throw new TypeError('invalid shortcut');
    result.shortcut = { targetFileId: fields.targetId, ...(fields.targetMimeType ? { targetMimeType: fields.targetMimeType as string } : {}) };
  }
  return result;
}
/** Select the partial file fields returned by search. */
export function fileSummary(file: File): FileSummary {
  return { id: file.id, name: file.name, mimeType: file.mimeType,
    ...(file.parents ? { parents: file.parents } : {}), ...(file.modifiedTime ? { modifiedTime: file.modifiedTime } : {}), ...(file.webViewLink ? { webViewLink: file.webViewLink } : {}) };
}
/** Decode one successful JSON response without leaking provider content. */
export function jsonObject(body: Uint8Array): Record<string, unknown> {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid provider object');
  return value as Record<string, unknown>;
}

/** Parse Go's RFC 3339 time without JavaScript Date rollover or offset limits. */
export function goRFC3339(value: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})([.,]\d+)?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match) throw new TypeError('invalid RFC 3339 time');
  const [, yearText, monthText, dayText, hourText, minuteText, secondText, fraction, zone] = match;
  const year = Number(yearText), month = Number(monthText), day = Number(dayText);
  const isLeapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const daysInMonth = [31, isLeapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (month < 1 || month > 12 || day < 1 || day > daysInMonth[month - 1]! ||
    Number(hourText) > 23 || Number(minuteText) > 59 || Number(secondText) > 59) throw new TypeError('invalid RFC 3339 time');
  if (zone !== 'Z' && (Number(zone!.slice(1, 3)) > 99 || Number(zone!.slice(4)) > 59)) throw new TypeError('invalid RFC 3339 offset');
  const decimal = fraction ? `.${fraction.slice(1, 10).padEnd(9, '0').replace(/0+$/, '')}` : '';
  const encodedZone = zone === '+00:00' || zone === '-00:00' ? 'Z' : zone;
  return `${yearText}-${monthText}-${dayText}T${hourText}:${minuteText}:${secondText}${decimal === '.' ? '' : decimal}${encodedZone}`;
}
