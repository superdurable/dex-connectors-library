// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { AddressRefusal, BodyTooLargeError, MaxErrorBodyBytes, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ReauthorizationRequiredError, SecretString, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { AddCommentOperation, CreateIssueOperation, GetIssueOperation, SearchIssuesOperation, TransitionIssueOperation } from './operations.js';

const defaultEndpoint = 'https://api.atlassian.com';
const defaultMaxResponseBytes = 4_194_304;
const requestTimeoutMs = 20_000;
const cloudIdPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const safeRequestIdPattern = /^[!-~]{1,128}$/;

/** Replace Jira network requests in provider tests. */
export interface JiraClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; now?: () => Date; }
/** One response or sanitized request error. */
export type JiraExchange = { response: ProviderResponse; isTooLarge?: boolean; reflectsSecret?: boolean } | { error: Error; isProvablyNotSent: boolean };
/** Resolved credential and Jira Cloud site for one operation. */
export interface JiraSession { token: string; cloudId: string; call: Call; signal: AbortSignal; }
/** Session failure is a terminal defect or a safe retry. */
export class JiraSessionFailure extends Error {
  /** Classify setup without exposing credential material. */
  constructor(readonly failure: Failure, readonly retry: boolean, readonly retryAfterMs = 0) { super(failure.message); }
}

/** Jira Cloud REST client with manifest-driven OAuth renewal. */
export class JiraClient {
  readonly endpoint: string;
  readonly maxResponseBytes: number;
  private readonly configuredCloudId: string;
  private resolvedCloudId = '';
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Jira', auth: Authorization, credentialFields: CredentialFields });

  /** Validate connection configuration and bind host-owned credentials. */
  constructor(config: Config, readonly credentials: CredentialSource, options: JiraClientOptions = {}) {
    if (!credentials) throw new TypeError('Jira credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.configuredCloudId = (config.cloudId ?? '').trim().toLowerCase();
    if (this.configuredCloudId && !cloudIdPattern.test(this.configuredCloudId)) throw new TypeError('Jira cloudId must be a site UUID');
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Jira maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind searchIssues. */ searchIssues(): SearchIssuesOperation { return new SearchIssuesOperation(this); }
  /** Bind getIssue. */ getIssue(): GetIssueOperation { return new GetIssueOperation(this); }
  /** Bind createIssue. */ createIssue(): CreateIssueOperation { return new CreateIssueOperation(this); }
  /** Bind transitionIssue. */ transitionIssue(): TransitionIssueOperation { return new TransitionIssueOperation(this); }
  /** Bind addComment. */ addComment(): AddCommentOperation { return new AddCommentOperation(this); }

  /** Start one attempt, resolving an access token and the selected Jira site. */
  async startSession(call: Call, operation: string): Promise<JiraSession> {
    const remainingMs = call.context.deadline ? call.context.deadline.getTime() - Date.now() - 1_000 : 25_000;
    const availableMs = Math.min(25_000, remainingMs);
    if (availableMs <= 0) throw new JiraSessionFailure(jiraFailure(operation, 'AVAILABILITY', 'Jira Execute deadline is too near for a request'), true);
    const signal = AbortSignal.any([call.context.cancellationSignal, AbortSignal.timeout(availableMs)]);
    let credentials: Credentials;
    try {
      credentials = await resolveCredential(this.credentials, call, this.refreshDriver, signal);
    } catch (error) {
      if (signal.aborted) throw new JiraSessionFailure(jiraFailure(operation, 'AVAILABILITY', 'Jira credential lookup did not finish within the Execute budget'), true);
      if (error instanceof ReauthorizationRequiredError) throw new JiraSessionFailure(jiraFailure(operation, 'AUTHENTICATION', 'Jira connection credentials are unavailable; reconnect the connection'), false);
      throw new JiraSessionFailure(jiraFailure(operation, 'AVAILABILITY', 'Jira credential lookup is temporarily unavailable'), true);
    }
    const token = credentials.accessToken instanceof SecretString ? credentials.accessToken.reveal() : '';
    if (!isHeaderSafeCredential(token)) throw new JiraSessionFailure(jiraFailure(operation, 'AUTHENTICATION', 'Jira connection credentials are unavailable; reconnect the connection'), false);
    const session = { token, cloudId: this.configuredCloudId || this.resolvedCloudId, call, signal };
    if (session.cloudId) return session;
    const exchange = await this.exchangeOnce(session, 'GET', '/oauth/token/accessible-resources', undefined, undefined, true);
    if ('error' in exchange) throw new JiraSessionFailure(jiraFailure(operation, 'TRANSPORT', 'Atlassian accessible resources are temporarily unavailable'), true);
    const response = exchange.response;
    if (response.status === 429) throw new JiraSessionFailure(jiraFailure(operation, 'RATE_LIMIT', 'Atlassian rate limited the accessible resources lookup'), true, retryAfter(response));
    if (response.status === 408 || response.status >= 500) throw new JiraSessionFailure(jiraFailure(operation, 'AVAILABILITY', 'Atlassian accessible resources are temporarily unavailable'), true);
    if (response.status < 200 || response.status >= 300) throw new JiraSessionFailure(jiraFailure(operation, statusFailureKind(response.status), 'Atlassian rejected the accessible resources lookup; reconnect or set cloudId'), false);
    if (exchange.isTooLarge || exchange.reflectsSecret) throw new JiraSessionFailure(jiraFailure(operation, 'PROTOCOL', 'Atlassian returned an unusable accessible resources list; set cloudId'), false);
    let resources: unknown;
    try { resources = JSON.parse(Buffer.from(response.body).toString('utf8')); }
    catch { throw new JiraSessionFailure(jiraFailure(operation, 'PROTOCOL', 'Atlassian returned an invalid accessible resources list; set cloudId'), false); }
    if (!Array.isArray(resources)) throw new JiraSessionFailure(jiraFailure(operation, 'PROTOCOL', 'Atlassian returned an invalid accessible resources list; set cloudId'), false);
    const granted: string[] = [];
    for (const resource of resources) {
      if (!isObject(resource)) throw new JiraSessionFailure(jiraFailure(operation, 'PROTOCOL', 'Atlassian returned an invalid accessible resources list; set cloudId'), false);
      const id = resource.id ?? '', scopes = resource.scopes ?? [];
      if (typeof id !== 'string' || !Array.isArray(scopes) || !scopes.every(scope => typeof scope === 'string')) throw new JiraSessionFailure(jiraFailure(operation, 'PROTOCOL', 'Atlassian returned an invalid accessible resources list; set cloudId'), false);
      if (cloudIdPattern.test(id) && scopes.some(scope => scope === 'read:jira-work' || scope === 'write:jira-work')) granted.push(id.toLowerCase());
    }
    const sites = [...new Set(granted)].sort();
    if (!sites.length) throw new JiraSessionFailure(jiraFailure(operation, 'AUTHORIZATION', 'the authorization grants no Jira site; reconnect and pick a Jira site'), false);
    if (sites.length > 1) throw new JiraSessionFailure(jiraFailure(operation, 'VALIDATION', `the authorization grants ${sites.length} Jira sites; set cloudId`), false);
    this.resolvedCloudId = sites[0]!;
    return { ...session, cloudId: this.resolvedCloudId };
  }

  /** Send one authenticated request, refreshing once after a rejected token. */
  async exchange(session: JiraSession, method: string, path: string, query?: Record<string, string>, payload?: unknown, signal?: AbortSignal): Promise<JiraExchange> {
    for (let attempt = 0; attempt < 2; attempt++) {
      const result = await this.exchangeOnce(session, method, path, query, payload, false, signal);
      if ('error' in result || result.response.status !== 401 || attempt === 1 || !('resolveAfterRejection' in this.credentials)) return result;
      try {
        const replacement = await resolveCredentialAfterRejection(this.credentials, session.call, this.refreshDriver, signal ?? session.signal);
        const token = replacement.accessToken instanceof SecretString ? replacement.accessToken.reveal() : '';
        if (!isHeaderSafeCredential(token)) return result;
        session.token = token;
      } catch { return result; }
    }
    throw new Error('Jira credential retry exhausted');
  }

  /** Send one non-replayable write behind a Dex dispatch fence. */
  async exchangeFencedWrite(session: JiraSession, method: string, path: string, payload: unknown, signal: AbortSignal): Promise<JiraExchange> {
    return this.exchangeOnce(session, method, path, undefined, payload, false, signal);
  }

  /** Ask the generated refresh path whether a rejected token can be replaced. */
  async refreshRejectedCredential(session: JiraSession): Promise<boolean> {
    if (!('resolveAfterRejection' in this.credentials)) return false;
    try {
      const replacement = await resolveCredentialAfterRejection(this.credentials, session.call, this.refreshDriver, session.signal);
      return replacement.accessToken instanceof SecretString && isHeaderSafeCredential(replacement.accessToken.reveal());
    } catch { return false; }
  }

  /** Build a secret-safe receipt with exact provider identity. */
  receipt(session: JiraSession, response?: ProviderResponse, objectId = ''): ReceiptDraft {
    const headers = response?.headers;
    const requestId = firstHeader(headers, 'x-arequestid') || firstHeader(headers, 'atl-traceid');
    return { callId: session.call.id, provider: 'jira', observedAt: this.now().toISOString(), ...(session.call.idempotencyKey ? { idempotencyKey: session.call.idempotencyKey } : {}), ...(objectId ? { providerObjectId: objectId } : {}), ...(requestId && safeRequestIdPattern.test(requestId) ? { providerRequestId: requestId } : {}) };
  }

  private async exchangeOnce(session: JiraSession, method: string, path: string, query?: Record<string, string>, payload?: unknown, isGatewayPath = false, signal?: AbortSignal): Promise<JiraExchange> {
    let body: Uint8Array | undefined;
    try { if (payload !== undefined) body = Buffer.from(JSON.stringify(payload), 'utf8'); }
    catch { return { error: new TypeError('Jira request could not be built'), isProvablyNotSent: true }; }
    const target = this.endpoint + (isGatewayPath ? '' : `/ex/jira/${pathEscape(session.cloudId)}/rest/api/3`) + path + (query && Object.keys(query).length ? '?' + encodeQuery(query) : '');
    const request: ProviderRequest = { url: target, method, headers: { Authorization: `Bearer ${session.token}`, Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) }, ...(body ? { body } : {}), addressPolicy: this.endpoint === defaultEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP', maxResponseBytes: this.maxResponseBytes, maxErrorResponseBytes: MaxErrorBodyBytes, timeoutMs: requestTimeoutMs, signal: signal ? AbortSignal.any([signal, session.signal]) : session.signal };
    try {
      const response = await this.send(request);
      const isTooLarge = response.status >= 200 && response.status < 300 && response.body.length > this.maxResponseBytes;
      const reflectsSecret = !!session.token && Buffer.from(response.body).includes(Buffer.from(session.token));
      return { response: { ...response, body: isTooLarge || reflectsSecret ? new Uint8Array() : response.body }, isTooLarge, reflectsSecret };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, isTooLarge: error.status >= 200 && error.status < 300 };
      return { error: error instanceof Error ? error : new Error('Jira request failed'), isProvablyNotSent: error instanceof AddressRefusal || (error instanceof ProviderTransportError && error.isProvablyNotSent) };
    }
  }
}

/** Exact Go url.PathEscape safe characters. */
export function pathEscape(value: string): string { return encodeURIComponent(value).replace(/%24|%26|%2B|%3D|%3A|%40/gi, escape => String.fromCharCode(Number.parseInt(escape.slice(1), 16))).replace(/[!'()*]/g, character => '%' + character.charCodeAt(0).toString(16).toUpperCase()); }
/** Go url.Values.Encode ordering and QueryEscape. */
export function encodeQuery(query: Record<string, string>): string { return Object.keys(query).sort().map(key => `${queryEscape(key)}=${queryEscape(query[key]!)}`).join('&'); }
function queryEscape(value: string): string { return encodeURIComponent(value).replace(/%20/g, '+').replace(/[!'()*~]/g, character => '%' + character.charCodeAt(0).toString(16).toUpperCase()); }
/** Secret-safe provider failure. */
export function jiraFailure(operation: string, kind: Failure['kind'], message: string): Failure { return { provider: 'jira', operation, kind, message }; }
/** Jira status classification shared by setup and operation requests. */
export function statusFailureKind(status: number): Failure['kind'] { return status === 400 ? 'VALIDATION' : status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 404 ? 'NOT_FOUND' : status === 409 ? 'CONFLICT' : status === 429 ? 'RATE_LIMIT' : 'PROVIDER_REJECTION'; }
/** Parse Retry-After without exposing response data. */
export function retryAfter(response: ProviderResponse): number { return parseRetryAfter(firstHeader(response.headers, 'retry-after')); }
function firstHeader(headers: ProviderResponse['headers'] | undefined, name: string): string { const key = Object.keys(headers ?? {}).find(candidate => candidate.toLowerCase() === name); const value = key ? headers?.[key] : undefined; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
