// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type FailureKind, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { SearchPagesOperation, GetPageOperation, CreatePageOperation, UpdatePageOperation, AddCommentOperation } from './operations.js';

const defaultEndpoint = 'https://api.atlassian.com';
const cloudIdPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const confluenceScopes = /confluence/;
const encoder = new TextEncoder();

/** Injectable provider transport and clock for deterministic connector tests. */
export interface ConfluenceClientOptions {
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  now?: () => Date;
}

/** One Confluence or gateway exchange with secret-safe transport evidence. */
export interface ConfluenceExchange {
  status: number;
  headers: ProviderResponse['headers'];
  body: Uint8Array;
  isTooLarge: boolean;
  reflectsSecret?: boolean;
  isDispatched: boolean;
  error?: 'transport' | 'request';
}

/** One authenticated invocation's site and access token. */
export interface ConfluenceSession { call: Call; cloudId: string; token: string; }

/** Secret-safe session failure. */
export interface ConfluenceSessionFailure { failure: Failure; isRetryable: boolean; retryAfterMs: number; }

/** Result of resolving the OAuth credential and Confluence site. */
export type ConfluenceSessionResult = { session: ConfluenceSession } | { error: ConfluenceSessionFailure };

/** Transport and status classification shared by the five operations. */
export interface ConfluenceClassification {
  outcome: 'accepted' | 'notApplied' | 'notFound' | 'rejected' | 'ambiguous' | 'invalid' | 'defect' | 'retry';
  failure?: Failure;
  retryAfterMs: number;
}

/** Atlassian Cloud REST client with host-managed rotating OAuth credentials. */
export class ConfluenceClient {
  readonly endpoint: string;
  readonly configuredCloudId: string;
  readonly maxResponseBytes: number;
  readonly now: () => Date;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Atlassian', auth: Authorization, credentialFields: CredentialFields });
  private resolvedCloudId = '';

  /** Validate connection settings and bind the host credential source. */
  constructor(config: Config, readonly credentials: CredentialSource, options: ConfluenceClientOptions = {}) {
    if (!credentials) throw new TypeError('Confluence credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.configuredCloudId = (config.cloudId || '').trim().toLowerCase();
    if (this.configuredCloudId && !cloudIdPattern.test(this.configuredCloudId)) throw new TypeError('Confluence cloudId must be a site UUID');
    this.maxResponseBytes = config.maxResponseBytes || 8_388_608;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Confluence maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the page search Query. */
  searchPages(): SearchPagesOperation { return new SearchPagesOperation(this); }
  /** Bind the page read Query. */
  getPage(): GetPageOperation { return new GetPageOperation(this); }
  /** Bind the page create Mutation. */
  createPage(): CreatePageOperation { return new CreatePageOperation(this); }
  /** Bind the page update Mutation. */
  updatePage(): UpdatePageOperation { return new UpdatePageOperation(this); }
  /** Bind the footer comment Mutation. */
  addComment(): AddCommentOperation { return new AddCommentOperation(this); }

  /** Resolve the OAuth token and the single selected Confluence Cloud site. */
  async startSession(call: Call, operation: string): Promise<ConfluenceSessionResult> {
    let credentials: Credentials;
    try {
      credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      const token = credentials.accessToken.reveal();
      if (!isHeaderSafeCredential(token)) throw new TypeError('invalid access token');
    } catch {
      return { error: { failure: confluenceFailure(operation, 'AUTHENTICATION', 'Confluence connection credentials are unavailable; reconnect the connection'), isRetryable: false, retryAfterMs: 0 } };
    }
    const session: ConfluenceSession = { call, cloudId: this.configuredCloudId || this.resolvedCloudId, token: credentials.accessToken.reveal() };
    if (session.cloudId) return { session };
    const response = await this.exchange(session, 'GET', 'gateway', '/oauth/token/accessible-resources');
    if (response.error) return { error: { failure: confluenceFailure(operation, 'TRANSPORT', 'Atlassian accessible resources are temporarily unavailable'), isRetryable: true, retryAfterMs: 0 } };
    if (response.status === 429) return { error: { failure: confluenceFailure(operation, 'RATE_LIMIT', 'Atlassian rate limited the accessible resources lookup'), isRetryable: true, retryAfterMs: this.retryAfter(response) } };
    if (response.status === 408 || response.status >= 500) return { error: { failure: confluenceFailure(operation, 'AVAILABILITY', 'Atlassian accessible resources are temporarily unavailable'), isRetryable: true, retryAfterMs: 0 } };
    if (response.status < 200 || response.status >= 300) return { error: { failure: confluenceFailure(operation, statusFailureKind(response.status), `Atlassian rejected the accessible resources lookup with HTTP ${response.status}; reconnect the connection or set cloudId`), isRetryable: false, retryAfterMs: 0 } };
    let sites: string[];
    try {
      const resources: unknown = JSON.parse(new TextDecoder().decode(response.body));
      if (!Array.isArray(resources)) throw new TypeError();
      sites = [...new Set(resources.filter((resource): resource is { id: string; scopes: string[] } =>
        resource && typeof resource === 'object' && typeof resource.id === 'string' && Array.isArray(resource.scopes) && resource.scopes.some((scope: unknown) => typeof scope === 'string' && confluenceScopes.test(scope)))
        .map(resource => resource.id.toLowerCase()).filter(id => cloudIdPattern.test(id)))].sort();
    } catch {
      return { error: { failure: confluenceFailure(operation, 'PROTOCOL', 'Atlassian returned an invalid accessible resources list; set cloudId'), isRetryable: false, retryAfterMs: 0 } };
    }
    if (!sites.length) return { error: { failure: confluenceFailure(operation, 'AUTHORIZATION', 'the authorization grants no Confluence site; reconnect and pick a Confluence site on the consent screen'), isRetryable: false, retryAfterMs: 0 } };
    if (sites.length !== 1) return { error: { failure: confluenceFailure(operation, 'VALIDATION', `the authorization grants ${sites.length} Confluence sites; choose one with the Confluence site picker or set cloudId`), isRetryable: false, retryAfterMs: 0 } };
    session.cloudId = sites[0]!;
    this.resolvedCloudId = session.cloudId;
    return { session };
  }

  /** Exchange one request and refresh a rejected credential once. */
  async exchange(session: ConfluenceSession, method: string, api: 'content' | 'search' | 'gateway', path: string, query?: Record<string, string>, payload?: unknown): Promise<ConfluenceExchange> {
    const prefix = api === 'gateway' ? '' : `/ex/confluence/${session.cloudId}/wiki/${api === 'content' ? 'api/v2' : 'rest/api'}`;
    const search = query ? '?' + Object.entries(query).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0).map(([key, value]) => `${queryEscape(key)}=${queryEscape(value)}`).join('&') : '';
    const url = this.endpoint + prefix + path + search;
    let body: Uint8Array | undefined;
    try { if (payload !== undefined) body = encoder.encode(JSON.stringify(payload)); }
    catch { return { status: 0, headers: {}, body: new Uint8Array(), isTooLarge: false, isDispatched: false, error: 'request' }; }
    for (let attempt = 0; attempt < 2; attempt++) {
      let response: ConfluenceExchange;
      const request: ProviderRequest = {
        url, method, headers: { Authorization: `Bearer ${session.token}`, Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) },
        ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes, timeoutMs: 20_000,
        addressPolicy: this.endpoint === defaultEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP', signal: session.call.context.cancellationSignal,
      };
      try {
        const received = await this.send(request);
        response = { ...received, isTooLarge: false, isDispatched: true,
          reflectsSecret: Buffer.from(received.body).includes(Buffer.from(session.token)) };
      } catch (error) {
        if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers !== undefined) response = { status: error.status, headers: error.headers, body: new Uint8Array(), isTooLarge: true, isDispatched: true };
        else return { status: 0, headers: {}, body: new Uint8Array(), isTooLarge: false, isDispatched: !(error instanceof ProviderTransportError && error.isProvablyNotSent), error: 'transport' };
      }
      if (response.status !== 401 || attempt === 1 || !('resolveAfterRejection' in this.credentials)) return response;
      try {
        const replacement = await resolveCredentialAfterRejection(this.credentials, session.call, this.refreshDriver, session.call.context.cancellationSignal);
        session.token = replacement.accessToken.reveal();
        if (!isHeaderSafeCredential(session.token)) return response;
      } catch { return response; }
    }
    throw new Error('Confluence credential rejection retry exhausted');
  }

  /** Classify a read without inspecting an error response body. */
  classifyRead(operation: string, subject: string, response: ConfluenceExchange): ConfluenceClassification {
    if (response.error === 'request') return classified('defect', confluenceFailure(operation, 'LOCAL_DEFECT', 'Confluence request could not be built'));
    if (response.error) return classified('retry', confluenceFailure(operation, 'TRANSPORT', `Confluence ${subject} is temporarily unavailable`));
    if (response.status === 404) return classified('notFound', confluenceFailure(operation, 'NOT_FOUND', `Confluence ${subject} was not found or is not visible to the connection`));
    if (response.status === 429) return classified('retry', confluenceFailure(operation, 'RATE_LIMIT', `Confluence rate limited the ${subject}`), this.retryAfter(response));
    if (response.status === 408 || response.status >= 500) return classified('retry', confluenceFailure(operation, 'AVAILABILITY', `Confluence ${subject} is temporarily unavailable`), this.retryAfter(response));
    if (response.status >= 400 && response.status < 500) return classified('rejected', confluenceFailure(operation, statusFailureKind(response.status), `Confluence rejected the ${subject} with HTTP ${response.status}`));
    if (response.status < 200 || response.status >= 300) return classified('invalid', confluenceFailure(operation, 'PROTOCOL', `Confluence returned unexpected HTTP ${response.status} for the ${subject}`));
    if (response.isTooLarge) return classified('invalid', confluenceFailure(operation, 'RESPONSE_TOO_LARGE', `Confluence ${subject} response exceeds the configured size limit`));
    if (response.reflectsSecret) return classified('invalid', confluenceFailure(operation, 'PROTOCOL', `Confluence ${subject} response contains credential material`));
    return classified('accepted');
  }

  /** Classify a write before any read-back or business reconciliation. */
  classifyWrite(operation: string, subject: string, response: ConfluenceExchange): ConfluenceClassification {
    if (response.error === 'request') return classified('defect', confluenceFailure(operation, 'LOCAL_DEFECT', 'Confluence request could not be built'));
    if (response.error && !response.isDispatched) return classified('notApplied', confluenceFailure(operation, 'TRANSPORT', `Confluence could not be reached, so no ${subject} was written`));
    if (response.error) return classified('ambiguous', confluenceFailure(operation, 'TRANSPORT', `Confluence ${subject} outcome is unknown`));
    if (response.status >= 200 && response.status < 300) {
      if (response.isTooLarge) return classified('ambiguous', confluenceFailure(operation, 'RESPONSE_TOO_LARGE', `Confluence accepted the ${subject} but its response exceeds the configured size limit`));
      if (response.reflectsSecret) return classified('ambiguous', confluenceFailure(operation, 'PROTOCOL', `Confluence accepted the ${subject} but its response contains credential material`));
      return classified('accepted');
    }
    if (response.status === 429) return classified('notApplied', confluenceFailure(operation, 'RATE_LIMIT', `Confluence rate limited the ${subject} before writing it`), this.retryAfter(response));
    if (response.status === 404) return classified('notFound', confluenceFailure(operation, 'NOT_FOUND', `Confluence rejected the ${subject} with HTTP 404`));
    if (response.status >= 400 && response.status < 500 && response.status !== 408) return classified('rejected', confluenceFailure(operation, statusFailureKind(response.status), `Confluence rejected the ${subject} with HTTP ${response.status}`));
    return classified('ambiguous', confluenceFailure(operation, 'AVAILABILITY', `Confluence ${subject} outcome is unknown after HTTP ${response.status}`), this.retryAfter(response));
  }

  /** Produce a safe request receipt with the exact provider object identifier. */
  receipt(session: ConfluenceSession, response: ConfluenceExchange, objectId = ''): ReceiptDraft {
    const traceId = response.headers['atl-traceid'];
    const safeTrace = typeof traceId === 'string' && /^[\x21-\x7e]{1,128}$/.test(traceId) ? traceId : '';
    return { callId: session.call.id, provider: 'confluence', observedAt: this.now().toISOString(),
      ...(session.call.idempotencyKey ? { idempotencyKey: session.call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}), ...(safeTrace ? { providerRequestId: safeTrace } : {}) };
  }

  /** Parse Retry-After against the injected clock. */
  retryAfter(response: ConfluenceExchange): number {
    const header = response.headers['retry-after'];
    return parseRetryAfter(typeof header === 'string' ? header : '', this.now());
  }
}

function classified(outcome: ConfluenceClassification['outcome'], failure?: Failure, retryAfterMs = 0): ConfluenceClassification { return { outcome, ...(failure ? { failure } : {}), retryAfterMs }; }
/** Build a connector failure without reflecting provider payloads or credentials. */
export function confluenceFailure(operation: string, kind: FailureKind, message: string): Failure { return { provider: 'confluence', operation, kind, message }; }
function statusFailureKind(status: number): FailureKind { return status === 400 ? 'VALIDATION' : status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 404 ? 'NOT_FOUND' : status === 409 ? 'CONFLICT' : status === 429 ? 'RATE_LIMIT' : 'PROVIDER_REJECTION'; }
function queryEscape(value: string): string { return encodeURIComponent(value).replace(/%20/g, '+').replace(/[!'()*~]/g, character => '%' + character.charCodeAt(0).toString(16).toUpperCase()); }
/** Decode a bounded JSON object, rejecting array or primitive responses. */
export function decodeObject(response: ConfluenceExchange): Record<string, unknown> {
  const decoded: unknown = JSON.parse(new TextDecoder().decode(response.body));
  if (!decoded || typeof decoded !== 'object' || Array.isArray(decoded)) throw new TypeError('invalid response object');
  return decoded as Record<string, unknown>;
}
/** Escape one URL path segment like Go url.PathEscape. */
export function pathEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => '%' + character.charCodeAt(0).toString(16).toUpperCase()).replace(/%24/g, '$').replace(/%26/g, '&').replace(/%2B/g, '+').replace(/%3D/g, '=').replace(/%3A/g, ':').replace(/%40/g, '@'); }
