// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { resolveCredential, resolveCredentialAfterRejection, type Call, type CredentialRefreshDriver, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, MaxErrorBodyBytes, isHeaderSafeCredential, parseRetryAfter, readErrorTokens, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { GetUserOperation, ListUsersOperation, CreateUserOperation, DisableUserOperation, EnableUserOperation, RevokeSignInSessionsOperation, AddUserToGroupOperation, RemoveUserFromGroupOperation } from './operations.js';

const graphURL = 'https://graph.microsoft.com/v1.0';
const reportableCodes = new Set(['Request_BadRequest', 'Request_ResourceNotFound', 'Request_UnsupportedQuery', 'Authorization_RequestDenied', 'Authorization_IdentityNotFound', 'InvalidAuthenticationToken', 'Directory_QuotaExceeded', 'Directory_ConcurrencyViolation', 'Directory_ResultSizeLimitExceeded', 'accessDenied', 'badRequest', 'invalidRequest', 'itemNotFound', 'unauthenticated', 'serviceNotAvailable', 'generalException', 'activityLimitReached', 'TooManyRequests']);

/** A single HTTP request sender for production and controlled provider tests. */
export type GraphRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Constructor dependencies for a Microsoft Graph client. */
export interface EntraClientOptions {
  /** A compatible fake endpoint for deterministic verification. */
  graphBaseURL?: string;
  /** One bounded HTTP sender. */
  send?: GraphRequestSender;
  /** Observation clock. */
  now?: () => Date;
  /** Manifest-backed credential renewal driver. */
  refreshDriver?: CredentialRefreshDriver<Credentials>;
  /** Source for Go-compatible initial password generation. */
  randomInt?: (upperBound: number) => number;
}
/** Provider response classification shared by all eight operations. */
export type GraphExchange = { outcome: 'succeeded' | 'retry' | 'notFound' | 'badRequest' | 'rejected' | 'invalidResponse' | 'defect'; response?: ProviderResponse; failure?: Failure; retryAfterMs: number };
/** One Graph request, including a previously validated opaque nextLink. */
export interface GraphRequest { method: string; path?: string; nextPageURL?: string; query?: Record<string, string>; payload?: unknown; eventual?: boolean }

/** Authenticated Microsoft Graph client with operation-specific adapters. */
export class EntraClient {
  readonly maxResponseBytes: number;
  readonly listUsersPageSize: number;
  readonly creationKeyAttribute: string;
  readonly randomInt?: (upperBound: number) => number;
  readonly #credentials: CredentialSource;
  readonly #refreshDriver: CredentialRefreshDriver<Credentials>;
  readonly #send: GraphRequestSender;
  readonly #baseURL: string;
  readonly #now: () => Date;
  readonly #renewedCredentials = new WeakMap<Call, Credentials>();

  /** Validate settings and bind the host-owned credential source. */
  constructor(config: Config, credentials: CredentialSource, options: EntraClientOptions = {}) {
    if (!credentials) throw new TypeError('credential provider is required');
    this.maxResponseBytes = config.maxResponseBytes || 4_194_304;
    this.listUsersPageSize = config.listUsersPageSize || 100;
    this.creationKeyAttribute = config.creationKeyAttribute || 'extensionAttribute15';
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Microsoft Entra ID response limit must be positive');
    if (!Number.isSafeInteger(this.listUsersPageSize) || this.listUsersPageSize < 1 || this.listUsersPageSize > 999) throw new TypeError('Microsoft Entra ID listUsers page size must be from 1 to 999');
    if (!/^extensionAttribute(?:[1-9]|1[0-5])$/.test(this.creationKeyAttribute)) throw new TypeError('Microsoft Entra ID creation key attribute is invalid');
    this.#credentials = credentials;
    this.#refreshDriver = options.refreshDriver ?? new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Microsoft', auth: Authorization, credentialFields: CredentialFields });
    this.#send = options.send ?? sendProviderRequest;
    this.#baseURL = options.graphBaseURL ?? graphURL;
    this.#now = options.now ?? (() => new Date());
    this.randomInt = options.randomInt;
  }

  /** Bind the getUser Query. */ getUser(): GetUserOperation { return new GetUserOperation(this); }
  /** Bind the listUsers Query. */ listUsers(): ListUsersOperation { return new ListUsersOperation(this); }
  /** Bind the createUser Mutation. */ createUser(): CreateUserOperation { return new CreateUserOperation(this); }
  /** Bind the disableUser Mutation. */ disableUser(): DisableUserOperation { return new DisableUserOperation(this); }
  /** Bind the enableUser Mutation. */ enableUser(): EnableUserOperation { return new EnableUserOperation(this); }
  /** Bind the revokeSignInSessions Mutation. */ revokeSignInSessions(): RevokeSignInSessionsOperation { return new RevokeSignInSessionsOperation(this); }
  /** Bind the addUserToGroup Mutation. */ addUserToGroup(): AddUserToGroupOperation { return new AddUserToGroupOperation(this); }
  /** Bind the removeUserFromGroup Mutation. */ removeUserFromGroup(): RemoveUserFromGroupOperation { return new RemoveUserFromGroupOperation(this); }

  /** Use generated grant rules for initial resolution and one rejected-token renewal. */
  async resolve(call: Call): Promise<Credentials | undefined> {
    try { const credentials = await resolveCredential(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal); return validCredentials(credentials) ? credentials : undefined; }
    catch { return undefined; }
  }

  /** Read a bounded user projection. */
  async readUser(call: Call, credentials: Credentials, operation: string, userKey: string, select: string): Promise<GraphExchange> {
    return this.exchange(call, credentials, operation, { method: 'GET', path: userPath(userKey), query: { '$select': select } });
  }

  /** Read one group identity after a membership 404. */
  async readGroup(call: Call, credentials: Credentials, operation: string, groupId: string): Promise<GraphExchange> {
    return this.exchange(call, credentials, operation, { method: 'GET', path: `/groups/${groupId}`, query: { '$select': 'id' } });
  }

  /** Send a Graph request and classify only safe provider metadata. */
  async exchange(call: Call, credentials: Credentials, operation: string, request: GraphRequest): Promise<GraphExchange> {
    const target = request.nextPageURL ?? this.#baseURL + (request.path ?? '') + (request.query ? `?${encodeGraphQuery(request.query)}` : '');
    let body: Uint8Array | undefined;
    try { if (request.payload !== undefined) body = Buffer.from(JSON.stringify(request.payload)); }
    catch { return this.failed('defect', operation, 'LOCAL_DEFECT', 'provider request could not be encoded'); }
    let current = this.#renewedCredentials.get(call) ?? credentials;
    for (let attempt = 0; attempt < 2; attempt++) {
      let status = 0;
      let response: ProviderResponse;
      try {
        response = await this.#send({ url: target, method: request.method, headers: { Authorization: `Bearer ${current.accessToken?.reveal() ?? ''}`, Accept: 'application/json', ...(body || request.method === 'POST' ? { 'Content-Type': 'application/json' } : {}), ...(request.eventual ? { ConsistencyLevel: 'eventual' } : {}) }, ...(body ? { body } : {}), maxResponseBytes: Math.max(this.maxResponseBytes, MaxErrorBodyBytes), addressPolicy: this.#baseURL === graphURL ? 'publicHTTPS' : 'privateNetworkHTTP', timeoutMs: 25_000, signal: call.context.cancellationSignal, onResponseChunk: (_chunk, code) => { status = code; } });
      } catch (error) {
        if (error instanceof BodyTooLargeError) status = error.status ?? status;
        if (error instanceof BodyTooLargeError && status > 0) {
          const boundedResponse: ProviderResponse = { status, headers: error.headers ?? {}, body: new Uint8Array() };
          if (status >= 200 && status < 300) return this.failed('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds the configured limit', boundedResponse);
          return this.classify(operation, boundedResponse);
        }
        return this.failed('retry', operation, 'TRANSPORT', 'provider is unavailable');
      }
      if (response.status >= 200 && response.status < 300 && response.body.byteLength > this.maxResponseBytes) return this.failed('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds the configured limit', response);
      if (response.status === 401 && attempt === 0 && 'resolveAfterRejection' in this.#credentials) {
        try {
          const renewed = await resolveCredentialAfterRejection(this.#credentials, call, this.#refreshDriver, call.context.cancellationSignal);
          if (validCredentials(renewed)) { this.#renewedCredentials.set(call, renewed); current = renewed; continue; }
        } catch { /* The original 401 remains authoritative. */ }
      }
      return this.classify(operation, response);
    }
    return this.failed('defect', operation, 'LOCAL_DEFECT', 'authenticated request retry was exhausted');
  }

  /** Make a receipt only after a provider answered. */
  receipt(call: Call, response?: ProviderResponse, objectId = ''): ReceiptDraft {
    if (!response) return {};
    return { callId: call.id, ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), provider: 'microsoft-entra-id', providerObjectId: objectId, providerRequestId: header(response, 'request-id') || header(response, 'client-request-id'), observedAt: this.#now().toISOString() };
  }

  /** Check Entra's one-minute replication window. */
  withinPropagationWindow(call: Call): boolean {
    const started = call.context.firstAttemptAt;
    return started instanceof Date && this.#now().getTime() - started.getTime() < 60_000;
  }

  private classify(operation: string, response: ProviderResponse): GraphExchange {
    if (response.status >= 200 && response.status < 300) return { outcome: 'succeeded', response, retryAfterMs: 0 };
    const code = readErrorTokens(response.body, ['/error/code', '/error/innerError/code', '/error/innererror/code'])[0] ?? '';
    const describe = (message: string) => message + (reportableCodes.has(code) ? ` (${code})` : '');
    if (response.status === 429) return this.failed('retry', operation, 'RATE_LIMIT', describe('provider throttled the request'), response, parseRetryAfter(header(response, 'retry-after'), this.#now()));
    if (response.status === 408 || response.status >= 500 && response.status !== 501 || response.status === 409 && code === 'Directory_ConcurrencyViolation') return this.failed('retry', operation, 'AVAILABILITY', describe(response.status === 409 ? 'provider reported a concurrent directory change' : 'provider is temporarily unavailable'), response, parseRetryAfter(header(response, 'retry-after'), this.#now()));
    if (response.status === 404) return this.failed('notFound', operation, 'NOT_FOUND', describe('resource was not found'), response);
    if (response.status === 400) return this.failed('badRequest', operation, 'VALIDATION', describe('provider rejected the request with HTTP 400'), response);
    const kind: Failure['kind'] = response.status === 401 ? 'AUTHENTICATION' : response.status === 403 ? 'AUTHORIZATION' : response.status === 409 || response.status === 412 ? 'CONFLICT' : 'PROVIDER_REJECTION';
    return this.failed('rejected', operation, kind, describe(`provider rejected the request with HTTP ${response.status}`), response);
  }

  private failed(outcome: GraphExchange['outcome'], operation: string, kind: Failure['kind'], message: string, response?: ProviderResponse, retryAfterMs = 0): GraphExchange {
    return { outcome, failure: { kind, provider: 'microsoft-entra-id', operation, message }, ...(response ? { response } : {}), retryAfterMs };
  }
}

/** Go-compatible OData query: literal keys, sorted by UTF-8 bytes, values query-escaped with spaces as %20. */
export function encodeGraphQuery(query: Record<string, string>): string {
  return Object.keys(query).sort((left, right) => Buffer.compare(Buffer.from(left), Buffer.from(right))).map(key => `${key}=${encodeURIComponent(replaceUnpairedSurrogates(query[key]!)).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`)}`).join('&');
}

/** Escape one Go URL path segment, including literal tildes. */
export function pathEscape(value: string): string { return encodeURIComponent(replaceUnpairedSurrogates(value)).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%24/g, '$').replace(/%26/g, '&').replace(/%2B/g, '+').replace(/%3D/g, '=').replace(/%40/g, '@').replace(/%3A/g, ':'); }

function replaceUnpairedSurrogates(value: string): string { return Array.from(value, character => { const code = character.codePointAt(0)!; return code >= 0xD800 && code <= 0xDFFF ? '\uFFFD' : character; }).join(''); }

/** Keep '$'-prefixed user names in Graph's quoted path form. */
export function userPath(key: string): string { return key.startsWith('$') ? `/users('${pathEscape(key.replaceAll("'", "''"))}')` : `/users/${pathEscape(key)}`; }

function validCredentials(credentials: Credentials): boolean {
  return !!credentials.accessToken && isHeaderSafeCredential(credentials.accessToken.reveal()) && (!credentials.authMethodId || credentials.authMethodId === 'entra-app-only' || credentials.authMethodId === 'microsoft-oauth');
}

function header(response: ProviderResponse, name: string): string {
  const value = response.headers[name];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
