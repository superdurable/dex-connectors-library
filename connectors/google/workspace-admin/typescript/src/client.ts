// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { AddUserToGroupOperation, CreateUserOperation, GetUserOperation, ListUsersOperation, RemoveUserFromGroupOperation, SuspendUserOperation, UnsuspendUserOperation } from './operations.js';

const defaultEndpoint = 'https://admin.googleapis.com';
const defaultResponseBytes = 4_194_304;
const defaultPageSize = 100;
const requestTimeoutMs = 25_000;
const provider = 'google-workspace-admin';

/** Injectable request sender and clock for deterministic provider tests. */
export interface WorkspaceAdminClientOptions {
  /** Provider HTTP sender. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Receipt and Retry-After clock. */
  now?: () => Date;
  /** Generated initial password source; production uses cryptographic randomness. */
  randomBytes?: (length: number) => Uint8Array;
}

/** Safe classification of one Directory API exchange. */
export interface DirectoryOutcome {
  /** Exchange result. */
  kind: 'retry' | 'notFound' | 'conflict' | 'rejected' | 'invalidResponse' | 'defect';
  /** Secret-safe failure. */
  failure: Failure;
  /** Provider retry delay. */
  retryAfterMs: number;
  /** Provider receipt, when a response was received. */
  receipt: ReceiptDraft;
}

/** A response or classified failure. */
export type DirectoryExchange = { response: ProviderResponse; receipt: ReceiptDraft } | { outcome: DirectoryOutcome };

/** Google Workspace Admin Directory API client. */
export class WorkspaceAdminClient {
  private readonly endpoint: string;
  readonly maxResponseBytes: number;
  readonly listUsersPageSize: number;
  readonly now: () => Date;
  readonly randomBytes: (length: number) => Uint8Array;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Google', auth: Authorization, credentialFields: CredentialFields });

  /** Validate configuration and bind the host-owned credential source. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: WorkspaceAdminClientOptions = {}) {
    if (!credentials) throw new TypeError('Google Workspace Admin credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint) + '/admin/directory/v1';
    this.maxResponseBytes = config.maxResponseBytes || defaultResponseBytes;
    this.listUsersPageSize = config.listUsersPageSize || defaultPageSize;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Google Workspace Admin response limit must be positive');
    if (!Number.isSafeInteger(this.listUsersPageSize) || this.listUsersPageSize < 1 || this.listUsersPageSize > 500) throw new TypeError('Google Workspace Admin listUsers page size must be from 1 to 500');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    this.randomBytes = options.randomBytes ?? (length => crypto.getRandomValues(new Uint8Array(length)));
  }

  /** Bind getUser. */ getUser() { return new GetUserOperation(this); }
  /** Bind listUsers. */ listUsers() { return new ListUsersOperation(this); }
  /** Bind createUser. */ createUser() { return new CreateUserOperation(this); }
  /** Bind suspendUser. */ suspendUser() { return new SuspendUserOperation(this); }
  /** Bind unsuspendUser. */ unsuspendUser() { return new UnsuspendUserOperation(this); }
  /** Bind addUserToGroup. */ addUserToGroup() { return new AddUserToGroupOperation(this); }
  /** Bind removeUserFromGroup. */ removeUserFromGroup() { return new RemoveUserFromGroupOperation(this); }

  /** Create a receipt using only provider identifiers and correlation metadata. */
  receipt(call: Call, response?: ProviderResponse, objectId = ''): ReceiptDraft {
    return {
      callId: call.id, idempotencyKey: call.idempotencyKey, provider,
      ...(objectId ? { providerObjectId: objectId } : {}),
      ...(response ? { providerRequestId: responseHeader(response, 'x-goog-request-id') || responseHeader(response, 'x-request-id') } : {}),
      observedAt: this.now().toISOString(),
    };
  }

  /** Resolve a token through the generated refresh declaration. */
  async resolveAccessToken(call: Call): Promise<string | undefined> {
    try {
      const credential = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      if (credential.authMethodId && !['workspace-domain-delegation', 'google-oauth'].includes(credential.authMethodId)) return undefined;
      const token = credential.accessToken?.reveal() ?? '';
      return isHeaderSafeToken(token) ? token : undefined;
    } catch { return undefined; }
  }

  /** Send one Directory request, including a single refresh after HTTP 401. */
  async exchange(call: Call, operation: string, method: string, path: string, query?: Record<string, string>, payload?: unknown): Promise<DirectoryExchange> {
    let token = await this.resolveAccessToken(call);
    if (!token) return { outcome: this.outcome('defect', operation, 'AUTHENTICATION', 'connection credentials are unavailable') };
    let body: Uint8Array | undefined;
    try { if (payload !== undefined) body = Buffer.from(JSON.stringify(payload)); }
    catch { return { outcome: this.outcome('defect', operation, 'LOCAL_DEFECT', 'provider request could not be encoded') }; }
    const url = this.endpoint + path + (query && Object.keys(query).length ? '?' + goQueryEncode(query) : '');
    for (let attempt = 0; attempt < 2; attempt++) {
      let response: ProviderResponse;
      try {
        response = await this.send({ url, method, headers: { Authorization: `Bearer ${token}`, Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) },
          ...(body ? { body } : {}), maxResponseBytes: Math.max(this.maxResponseBytes, 65_536), timeoutMs: requestTimeoutMs,
          addressPolicy: this.endpoint.startsWith(defaultEndpoint) ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal });
      } catch (error) {
        if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers !== undefined) {
          response = { status: error.status, headers: error.headers, body: new Uint8Array() };
          if (response.status >= 200 && response.status < 300) return { outcome: this.outcome('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds the configured limit', this.receipt(call, response)) };
        } else {
          return { outcome: this.outcome('retry', operation, 'TRANSPORT', 'provider is unavailable') };
        }
      }
      const receipt = this.receipt(call, response);
      if (response.status === 401 && attempt === 0 && 'resolveAfterRejection' in this.credentials) {
        try {
          const refreshed = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
          const refreshedToken = refreshed.accessToken?.reveal() ?? '';
          if (isHeaderSafeToken(refreshedToken)) { token = refreshedToken; continue; }
        } catch { /* Google 401 remains the classified provider response. */ }
      }
      if (response.status >= 200 && response.status < 300) {
        if (response.body.length > this.maxResponseBytes) return { outcome: this.outcome('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds the configured limit', receipt) };
        return { response, receipt };
      }
      const reason = googleReason(response.body);
      const suffix = reason && reportableReasons.has(reason) ? ` (${reason})` : '';
      if (hasCreationIncompleteMessage(response.body)) return { outcome: this.outcome('retry', operation, 'AVAILABILITY', 'Google is still creating the account', receipt, retryDelay(response, this.now())) };
      if (response.status === 429 || (response.status === 403 && retryableForbiddenReasons.has(reason))) return { outcome: this.outcome('retry', operation, 'RATE_LIMIT', 'provider rate limited the request' + suffix, receipt, retryDelay(response, this.now())) };
      if (response.status === 408 || response.status >= 500) return { outcome: this.outcome('retry', operation, 'AVAILABILITY', 'provider is temporarily unavailable' + suffix, receipt, retryDelay(response, this.now())) };
      if (response.status === 404) return { outcome: this.outcome('notFound', operation, 'NOT_FOUND', 'resource was not found', receipt) };
      if (response.status === 409) return { outcome: this.outcome('conflict', operation, 'CONFLICT', 'provider reported a conflicting resource' + suffix, receipt) };
      const kind: Failure['kind'] = response.status === 400 ? 'VALIDATION' : response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : response.status === 412 ? 'CONFLICT' : 'PROVIDER_REJECTION';
      return { outcome: this.outcome('rejected', operation, kind, `provider rejected the request with HTTP ${response.status}${suffix}`, receipt) };
    }
    return { outcome: this.outcome('defect', operation, 'LOCAL_DEFECT', 'authenticated request retry was exhausted') };
  }

  private outcome(kind: DirectoryOutcome['kind'], operation: string, failureKind: Failure['kind'], message: string, receipt: ReceiptDraft = {}, retryAfterMs = 0): DirectoryOutcome {
    return { kind, failure: { kind: failureKind, provider, operation, message }, receipt, retryAfterMs };
  }
}

const retryableForbiddenReasons = new Set(['userRateLimitExceeded', 'rateLimitExceeded', 'quotaExceeded']);
const reportableReasons = new Set(['badRequest', 'invalid', 'required', 'notFound', 'duplicate', 'conflict', 'conditionNotMet', 'failedPrecondition', 'forbidden', 'insufficientPermissions', 'authError', 'limitExceeded', 'backendError', 'userRateLimitExceeded', 'rateLimitExceeded', 'quotaExceeded']);

function isHeaderSafeToken(token: string): boolean { return token !== '' && ![...token].some(character => character.charCodeAt(0) < 33 || character.charCodeAt(0) > 126); }
function responseHeader(response: ProviderResponse, name: string): string { const value = Object.entries(response.headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function retryDelay(response: ProviderResponse, now: Date): number { return parseRetryAfter(responseHeader(response, 'retry-after'), now); }
function googleReason(body: Uint8Array): string {
  try { const parsed = JSON.parse(Buffer.from(body).toString()) as { error?: { errors?: Array<{ reason?: unknown }>; status?: unknown } }; const reason = parsed.error?.errors?.[0]?.reason; return typeof reason === 'string' ? reason : typeof parsed.error?.status === 'string' ? parsed.error.status : ''; } catch { return ''; }
}
function hasCreationIncompleteMessage(body: Uint8Array): boolean {
  try { const parsed = JSON.parse(Buffer.from(body).toString()) as { error?: { message?: unknown; errors?: Array<{ message?: unknown }> } }; return [parsed.error?.message, ...(parsed.error?.errors ?? []).map(item => item.message)].some(item => typeof item === 'string' && item.includes('User creation is not complete')); } catch { return false; }
}
/** Encode a path segment exactly as Go url.PathEscape. */
export function goPathEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => '%' + character.charCodeAt(0).toString(16).toUpperCase()).replace(/%(24|26|2B|3D|3A|40)/g, (_, code: string) => String.fromCharCode(parseInt(code, 16))); }
/** Encode a query exactly as Go url.Values.Encode. */
export function goQueryEncode(values: Readonly<Record<string, string | readonly string[]>>): string { return Object.keys(values).sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right))).flatMap(key => { const entry = values[key]!; return (typeof entry === 'string' ? [entry] : entry).map(value => `${goQueryEscape(key)}=${goQueryEscape(value)}`); }).join('&'); }
function goQueryEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%20/g, '+'); }
