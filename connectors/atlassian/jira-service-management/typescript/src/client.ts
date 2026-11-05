// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { AddressRefusal, BodyTooLargeError, isHeaderSafeCredential, MaxErrorBodyBytes, parseRetryAfter, ProviderTransportError, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { AddCommentOperation, CreateTicketOperation, FindCustomerByEmailOperation, GetTicketOperation, SearchTicketsOperation, TransitionTicketOperation, UpdateTicketOperation } from './operations.js';

const defaultEndpoint = 'https://api.atlassian.com';
const cloudIDPattern = /^[a-fA-F0-9]{8}-(?:[a-fA-F0-9]{4}-){3}[a-fA-F0-9]{12}$/;
const siteScopeMarkers = new Set(['read:servicedesk-request', 'write:servicedesk-request']);

type API = 'platform' | 'desk' | 'gateway';
export interface JiraRequest { method: string; api: API; path: string; query?: Record<string, string>; payload?: unknown; experimental?: boolean; }
export interface JiraResponse { status: number; headers: ProviderResponse['headers']; body: Uint8Array; isTooLarge: boolean; reflectedSecret: boolean; }
export interface JiraResult { response?: JiraResponse; error?: unknown; isProvablyNotSent?: boolean; }
export type Classification = { kind: 'success' | 'retry' | 'notFound' | 'rejected' | 'invalidResponse' | 'defect' | 'uncertain'; failure?: Failure; retryAfterMs: number; receipt: ReceiptDraft; };
export interface JiraClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; now?: () => Date; }

/** OAuth-backed Atlassian client shared by all seven ticket operations. */
export class JiraServiceManagementClient {
  private readonly endpoint: string;
  private readonly configuredCloudID: string;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Atlassian', auth: Authorization, credentialFields: CredentialFields });
  private resolvedCloudID = '';

  /** Validate settings and bind host-owned credentials. */
  constructor(config: Config, readonly credentials: CredentialSource, options: JiraClientOptions = {}) {
    if (!credentials) throw new TypeError('credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.configuredCloudID = (config.cloudId || '').trim().toLowerCase();
    if (this.configuredCloudID && !cloudIDPattern.test(this.configuredCloudID)) throw new TypeError('cloudId must be a site UUID');
    this.maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind customer lookup. */ findCustomerByEmail() { return new FindCustomerByEmailOperation(this); }
  /** Bind ticket search. */ searchTickets() { return new SearchTicketsOperation(this); }
  /** Bind ticket details. */ getTicket() { return new GetTicketOperation(this); }
  /** Bind ticket creation. */ createTicket() { return new CreateTicketOperation(this); }
  /** Bind label and priority changes. */ updateTicket() { return new UpdateTicketOperation(this); }
  /** Bind workflow transition. */ transitionTicket() { return new TransitionTicketOperation(this); }
  /** Bind a public or internal comment. */ addComment() { return new AddCommentOperation(this); }

  /** Resolve a credential and select one accessible site. */
  async start(call: Call, operation: string): Promise<{ token: string; cloudID: string } | Classification> {
    let token: string;
    try {
      const credential = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      token = credential.accessToken.reveal();
      if (!isHeaderSafeCredential(token)) throw new TypeError('invalid credential');
    } catch {
      return this.classification('defect', operation, 'AUTHENTICATION', 'Jira Service Management connection credentials are unavailable; reconnect the connection', {});
    }
    if (this.configuredCloudID || this.resolvedCloudID) return { token, cloudID: this.configuredCloudID || this.resolvedCloudID };
    const result = await this.exchange(call, { token, cloudID: '' }, { method: 'GET', api: 'gateway', path: '/oauth/token/accessible-resources' });
    if (result.error) return this.classification('retry', operation, 'TRANSPORT', 'Atlassian accessible resources are temporarily unavailable', {});
    const response = result.response!;
    if (response.status === 429) return this.classification('retry', operation, 'RATE_LIMIT', 'Atlassian rate limited the accessible resources lookup', {}, this.retryAfter(response));
    if (response.status === 408 || response.status >= 500) return this.classification('retry', operation, 'AVAILABILITY', 'Atlassian accessible resources are temporarily unavailable', {});
    if (response.status < 200 || response.status >= 300) return this.classification('defect', operation, statusFailureKind(response.status), `Atlassian rejected the accessible resources lookup with HTTP ${response.status}; reconnect the connection or set cloudId`, {});
    if (response.isTooLarge || response.reflectedSecret) return this.classification('defect', operation, 'PROTOCOL', 'Atlassian returned an unusable accessible resources list; set cloudId', {});
    let resources: unknown;
    try { resources = JSON.parse(Buffer.from(response.body).toString('utf8')); } catch { resources = null; }
    if (resources === null) resources = [];
    if (!Array.isArray(resources) || resources.some(resource => !isRecord(resource) || resource.id !== undefined && resource.id !== null && typeof resource.id !== 'string' || resource.scopes !== undefined && (resource.scopes !== null && (!Array.isArray(resource.scopes) || resource.scopes.some((scope: unknown) => scope !== null && typeof scope !== 'string'))))) return this.classification('defect', operation, 'PROTOCOL', 'Atlassian returned an invalid accessible resources list; set cloudId', {});
    const ids = [...new Set(resources.flatMap((resource: unknown) => {
      if (!isRecord(resource) || typeof resource.id !== 'string' || !cloudIDPattern.test(resource.id) || !Array.isArray(resource.scopes) || !resource.scopes.some(scope => siteScopeMarkers.has(scope))) return [];
      return [resource.id.toLowerCase()];
    }))].sort();
    if (ids.length !== 1) return this.classification('defect', operation, ids.length ? 'VALIDATION' : 'AUTHORIZATION', ids.length ? `the authorization grants ${ids.length} Jira Service Management sites; choose one with the site picker or set cloudId` : 'the authorization grants no Jira Service Management site; reconnect and pick the site on the consent screen', {});
    this.resolvedCloudID = ids[0]!;
    return { token, cloudID: this.resolvedCloudID };
  }

  /** Send a provider request, refreshing once after a 401 when the host supports it. */
  async exchange(call: Call, session: { token: string; cloudID: string }, request: JiraRequest): Promise<JiraResult> {
    let result = await this.sendOnce(call, session.token, session.cloudID, request);
    if (result.response?.status !== 401 || !('resolveAfterRejection' in this.credentials)) return result;
    try {
      const credential = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      session.token = credential.accessToken.reveal();
      if (!session.token) return result;
      result = await this.sendOnce(call, session.token, session.cloudID, request);
    } catch { /* An unavailable replacement leaves the original 401 for classification. */ }
    return result;
  }

  private async sendOnce(call: Call, token: string, cloudID: string, request: JiraRequest): Promise<JiraResult> {
    const prefix = request.api === 'gateway' ? '' : `/ex/jira/${pathEscape(cloudID)}${request.api === 'desk' ? '/rest/servicedeskapi' : '/rest/api/3'}`;
    const query = request.query ? '?' + Object.entries(request.query).sort(([a], [b]) => a.localeCompare(b)).map(([key, value]) => `${queryEscape(key)}=${queryEscape(value)}`).join('&') : '';
    let body: Uint8Array | undefined;
    try { if (request.payload !== undefined) body = Buffer.from(JSON.stringify(request.payload), 'utf8'); }
    catch (error) { return { error, isProvablyNotSent: true }; }
    const providerRequest: ProviderRequest = {
      url: this.endpoint + prefix + request.path + query, method: request.method,
      headers: { Authorization: `Bearer ${token}`, Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}), ...(request.experimental ? { 'X-ExperimentalApi': 'opt-in' } : {}) },
      ...(body ? { body } : {}), maxResponseBytes: Math.max(this.maxResponseBytes, MaxErrorBodyBytes),
      timeoutMs: 20_000, addressPolicy: this.endpoint === defaultEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP', signal: call.context.cancellationSignal,
    };
    try {
      const response = await this.send(providerRequest);
      const isSuccess = response.status >= 200 && response.status < 300;
      return { response: { ...response, body: isSuccess && response.body.byteLength > this.maxResponseBytes ? new Uint8Array() : response.body, isTooLarge: isSuccess && response.body.byteLength > this.maxResponseBytes, reflectedSecret: Buffer.from(response.body).includes(Buffer.from(token)) } };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array(), isTooLarge: true, reflectedSecret: false } };
      return { error, isProvablyNotSent: error instanceof ProviderTransportError ? error.isProvablyNotSent : error instanceof AddressRefusal };
    }
  }

  /** Classify read response without choosing an operation-specific branch. */
  read(call: Call, operation: string, subject: string, result: JiraResult, objectID = ''): Classification {
    const receipt = this.receipt(call, result.response, objectID);
    if (result.error) return this.classification('retry', operation, 'TRANSPORT', `Jira Service Management ${subject} is temporarily unavailable`, receipt);
    const response = result.response!;
    if (response.status === 404) return this.classification('notFound', operation, 'NOT_FOUND', `Jira Service Management ${subject} was not found or is not visible to the connection`, receipt);
    if (response.status === 429) return this.classification('retry', operation, 'RATE_LIMIT', `Jira Service Management rate limited the ${subject}`, receipt, this.retryAfter(response));
    if (response.status === 408 || response.status >= 500) return this.classification('retry', operation, 'AVAILABILITY', `Jira Service Management ${subject} is temporarily unavailable`, receipt, this.retryAfter(response));
    if (response.status >= 400 && response.status < 500) return this.classification('rejected', operation, statusFailureKind(response.status), `Jira Service Management rejected the ${subject} with HTTP ${response.status}`, receipt);
    if (response.status < 200 || response.status >= 300 || response.reflectedSecret) return this.classification('invalidResponse', operation, 'PROTOCOL', `Jira Service Management returned an invalid ${subject} response`, receipt);
    if (response.isTooLarge) return this.classification('invalidResponse', operation, 'RESPONSE_TOO_LARGE', `Jira Service Management ${subject} response exceeds the configured size limit`, receipt);
    return { kind: 'success', retryAfterMs: 0, receipt };
  }

  /** Classify an unkeyed write, distinguishing a proven pre-send failure. */
  write(call: Call, operation: string, subject: string, result: JiraResult, objectID = ''): Classification {
    const receipt = this.receipt(call, result.response, objectID);
    if (result.error) return this.classification(result.isProvablyNotSent ? 'retry' : 'uncertain', operation, 'TRANSPORT', `Jira Service Management ${subject} outcome is unknown`, receipt);
    const response = result.response!;
    if (response.status >= 200 && response.status < 300) {
      if (response.isTooLarge || response.reflectedSecret) return this.classification('uncertain', operation, response.isTooLarge ? 'RESPONSE_TOO_LARGE' : 'PROTOCOL', `Jira Service Management accepted the ${subject} but its response is unusable`, receipt);
      return { kind: 'success', retryAfterMs: 0, receipt };
    }
    if (response.status === 429 || response.status === 503) return this.classification('retry', operation, response.status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', `Jira Service Management did not write the ${subject}`, receipt, this.retryAfter(response));
    if (response.status === 409) return this.classification('uncertain', operation, 'CONFLICT', `Jira Service Management ${subject} outcome requires read-back after HTTP 409`, receipt);
    if (response.status === 404) return this.classification('notFound', operation, 'NOT_FOUND', `Jira Service Management ${subject} was not found`, receipt);
    if (response.status >= 400 && response.status < 500 && response.status !== 408) return this.classification('rejected', operation, statusFailureKind(response.status), `Jira Service Management rejected the ${subject} with HTTP ${response.status}`, receipt);
    return this.classification('uncertain', operation, 'AVAILABILITY', `Jira Service Management ${subject} outcome is unknown after HTTP ${response.status}`, receipt);
  }

  /** Construct one secret-safe result receipt. */
  receipt(call: Call, response?: JiraResponse, objectID = ''): ReceiptDraft {
    const headers = response?.headers ?? {};
    const requestID = [headers['x-arequestid'], headers['atl-traceid']].map(value => Array.isArray(value) ? value.join(',') : String(value ?? '')).find(value => value.length <= 128 && /^[!-~]+$/.test(value));
    return { callId: call.id, provider: 'jira-service-management', observedAt: this.now().toISOString(), ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), ...(objectID ? { providerObjectId: objectID } : {}), ...(requestID ? { providerRequestId: requestID } : {}) };
  }

  /** Create one failure result with no provider-controlled message content. */
  classification(kind: Classification['kind'], operation: string, failureKind: Failure['kind'], message: string, receipt: ReceiptDraft, retryAfterMs = 0): Classification {
    return { kind, failure: { kind: failureKind, provider: 'jira-service-management', operation, message }, retryAfterMs, receipt };
  }

  private retryAfter(response: JiraResponse): number { return parseRetryAfter(String(response.headers['retry-after'] ?? ''), this.now()); }
}

export function isRecord(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }
export function pathEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%24|%26|%2B|%3D|%3A|%40/gi, code => String.fromCharCode(parseInt(code.slice(1), 16))); }
export function queryEscape(value: string): string { return encodeURIComponent(value).replace(/%20/g, '+').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`); }
function statusFailureKind(status: number): Failure['kind'] { return status === 400 || status === 422 ? 'VALIDATION' : status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 404 ? 'NOT_FOUND' : status === 429 ? 'RATE_LIMIT' : status === 409 ? 'CONFLICT' : 'PROVIDER_REJECTION'; }
