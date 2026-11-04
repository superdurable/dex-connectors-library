// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, readErrorTokens, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type FailureKind, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { CreateEnvelopeFromTemplateOperation, DownloadCombinedDocumentOperation, GetEnvelopeOperation, ListEnvelopeRecipientsOperation, VoidEnvelopeOperation } from './operations.js';

const guidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const productionHostPattern = /^(?!demo\.)[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.docusign\.net$/;
const maximumErrorBodyBytes = 65_536;
const defaultResponseBytes = 1_048_576;
const defaultDocumentBytes = 67_108_864;
const rateLimitCodes = new Set([
  'HOURLY_APIINVOCATION_LIMIT_EXCEEDED', 'BURST_APIINVOCATION_LIMIT_EXCEEDED',
  'HOURLY_ENVELOPE_POLLING_LIMIT_EXCEEDED', 'BURST_ENVELOPE_POLLING_LIMIT_EXCEEDED',
  'HOURLY_APIINVOCATION_ENVELOPE_LIMIT_EXCEEDED', 'BURST_APIINVOCATION_ENVELOPE_LIMIT_EXCEEDED',
]);

/** Reference handed to an application-owned combined PDF store. */
export interface CombinedDocumentReference {
  /** Account that owns the PDF. */ accountId: string;
  /** Envelope whose documents were combined. */ envelopeId: string;
  /** Whether the certificate of completion is included. */ includesCertificate: boolean;
}
/** Store a complete PDF outside Dex payloads and return a safe, stable location. */
export interface CombinedDocumentStore {
  /** Consume the whole stream atomically and return a repeat-safe location. */
  storeCombinedDocument(reference: CombinedDocumentReference, content: AsyncIterable<Uint8Array>): Promise<string>;
}
/** Replace the transport, clock, or document store for an application or provider test. */
export interface DocuSignClientOptions {
  /** Override the bounded JSON request transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Override the clock used for receipts and rate limits. */
  now?: () => Date;
  /** Persist a combined PDF outside Dex payloads. */
  documentStore?: CombinedDocumentStore;
}
/** One provider answer; oversized applies only to successful JSON bodies. */
export interface DocuSignResponse {
  /** HTTP status. */ status: number;
  /** Response headers. */ headers: ProviderResponse['headers'];
  /** Bounded response bytes. */ body: Uint8Array;
  /** Whether a successful body exceeded its limit. */ isTooLarge: boolean;
}
/** Open successful PDF stream or a bounded non-success answer. */
export type DocumentResponse = { stream: AsyncIterable<Uint8Array>; headers: ProviderResponse['headers']; close(): Promise<void> } | { rejected: DocuSignResponse };
/** Session credentials and the account selected by DocuSign userinfo. */
export interface DocuSignSession {
  /** Credentials for this call. */ credentials: Credentials;
  /** Selected API account ID. */ accountId: string;
  /** Validated regional API base URI. */ baseUri: string;
}
/** Sanitized classification of a provider refusal. */
export interface DocuSignOutcome {
  /** Whether Dex can safely retry. */ retry: boolean;
  /** Whether the provider found no matching object. */ notFound: boolean;
  /** Provider requested retry delay in milliseconds. */ retryAfterMs: number;
  /** Safe provider error code. */ errorCode: string;
  /** Persistable failure without provider message text. */ failure: Failure;
}
/** Session establishment failure before an eSignature request. */
export class SessionError extends Error {
  /** Keep the route and sanitized failure for an operation. */
  constructor(readonly route: 'retry' | 'defect' | 'rejected' | 'invalidResponse', readonly failure: Failure, readonly retryAfterMs = 0) { super(failure.message); }
}
/** Successful JSON response exceeded maxResponseBytes. */
export class ResponseTooLargeError extends Error {}

/** Return whether a provider identifier is a DocuSign GUID. */
export function isGuid(value: string): boolean { return guidPattern.test(value); }
/** Escape a path segment with Go url.PathEscape semantics. */
export function escapePath(value: string): string {
  return Array.from(Buffer.from(value)).map(byte => {
    const character = String.fromCharCode(byte);
    return /^[A-Za-z0-9\-._~$&+=:@]$/.test(character) ? character : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
  }).join('');
}
/** Return a secret-safe failure for one operation. */
export function failure(operation: string, kind: FailureKind, message: string): Failure { return { kind, provider: 'docusign', operation, message }; }

/** Authenticated DocuSign eSignature client bound to one Dex connection. */
export class DocuSignClient {
  readonly maxResponseBytes: number;
  readonly maxDocumentBytes: number;
  readonly documentStore: CombinedDocumentStore | undefined;
  private readonly accountId: string;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly hasCustomSend: boolean;
  private readonly now: () => Date;
  private readonly accountCache = new Map<string, { accountId: string; baseUri: string; at: number }>();
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'DocuSign', auth: Authorization, credentialFields: CredentialFields });

  /** Validate connection settings and bind host-owned renewable credentials. */
  constructor(config: Config, readonly credentials: CredentialSource, options: DocuSignClientOptions = {}) {
    if (!credentials) throw new TypeError('DocuSign credential provider is required');
    this.accountId = config.accountId ?? '';
    if (this.accountId && !isGuid(this.accountId)) throw new TypeError('configuration accountId must be a DocuSign API account ID GUID, such as 00000000-0000-0000-0000-000000000000');
    this.maxResponseBytes = config.maxResponseBytes || defaultResponseBytes;
    this.maxDocumentBytes = config.maxDocumentBytes || defaultDocumentBytes;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('maxResponseBytes must be positive');
    if (!Number.isSafeInteger(this.maxDocumentBytes) || this.maxDocumentBytes <= 0) throw new TypeError('maxDocumentBytes must be positive');
    if (config.connectMaxBodyBytes !== undefined && (!Number.isSafeInteger(config.connectMaxBodyBytes) || config.connectMaxBodyBytes < 0)) throw new TypeError('connectMaxBodyBytes must be non-negative');
    this.send = options.send ?? sendProviderRequest;
    this.hasCustomSend = options.send !== undefined;
    this.now = options.now ?? (() => new Date());
    this.documentStore = options.documentStore;
  }

  /** Bind the template creation Mutation. */
  createEnvelopeFromTemplate(): CreateEnvelopeFromTemplateOperation { return new CreateEnvelopeFromTemplateOperation(this); }
  /** Bind the envelope Query. */
  getEnvelope(): GetEnvelopeOperation { return new GetEnvelopeOperation(this); }
  /** Bind the recipients Query. */
  listEnvelopeRecipients(): ListEnvelopeRecipientsOperation { return new ListEnvelopeRecipientsOperation(this); }
  /** Bind the void Mutation. */
  voidEnvelope(): VoidEnvelopeOperation { return new VoidEnvelopeOperation(this); }
  /** Bind the combined PDF Query. */
  downloadCombinedDocument(): DownloadCombinedDocumentOperation { return new DownloadCombinedDocumentOperation(this); }

  /** Resolve renewable credentials and DocuSign's own account host. */
  async openSession(call: Call, operation: string): Promise<DocuSignSession> {
    let credentials: Credentials;
    try { credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal); }
    catch (error) {
      const isTerminal = error instanceof Error && error.name === 'ReauthorizationRequiredError';
      throw new SessionError(isTerminal ? 'defect' : 'retry', failure(operation, isTerminal ? 'AUTHENTICATION' : 'AVAILABILITY', isTerminal
        ? 'the DocuSign connection needs reauthorization; authorize it again in Dex Web Connections'
        : 'the DocuSign access token could not be loaded or refreshed yet'));
    }
    if (!isHeaderSafeCredential(credentials.accessToken.reveal())) throw new SessionError('defect', failure(operation, 'AUTHENTICATION', 'the DocuSign access token is blank or contains characters a header cannot carry'));
    const method = credentials.authMethodId ?? 'docusign-oauth';
    if (method !== 'docusign-oauth' && method !== 'docusign-developer-oauth') throw new SessionError('defect', failure(operation, 'LOCAL_DEFECT', 'the DocuSign connection names no known environment; authorize it again in Dex Web Connections'));
    const key = `${call.connection.provider}/${call.connection.name}/${createHash('sha256').update(credentials.accessToken.reveal()).digest('hex')}`;
    const cached = this.accountCache.get(key);
    if (cached && this.now().getTime() - cached.at < 3_600_000) return { credentials, accountId: cached.accountId, baseUri: cached.baseUri };
    const accountServer = method === 'docusign-developer-oauth' ? 'https://account-d.docusign.com' : 'https://account.docusign.com';
    let response: DocuSignResponse;
    try { response = await this.request(call, credentials, 'GET', `${accountServer}/oauth/userinfo`, undefined, 'application/json', this.maxResponseBytes); }
    catch (error) {
      if (error instanceof ResponseTooLargeError) throw new SessionError('invalidResponse', failure(operation, 'RESPONSE_TOO_LARGE', 'DocuSign account information exceeds the configured maxResponseBytes'));
      throw new SessionError('retry', failure(operation, 'TRANSPORT', 'DocuSign account information could not be read'));
    }
    if (response.status === 429) throw new SessionError('retry', failure(operation, 'RATE_LIMIT', 'DocuSign limited the hourly account information requests for this user or integration key'), nextHour(this.now()));
    if (response.status >= 500 || response.status === 408) throw new SessionError('retry', failure(operation, 'AVAILABILITY', `DocuSign account information answered HTTP ${response.status}`));
    if (response.status === 401) throw new SessionError('rejected', failure(operation, 'AUTHENTICATION', 'DocuSign rejected the access token; authorize the connection again'));
    if (response.status !== 200) throw new SessionError('rejected', failure(operation, 'PROVIDER_REJECTION', `DocuSign account information answered HTTP ${response.status}`));
    let accounts: unknown;
    try { accounts = (JSON.parse(Buffer.from(response.body).toString('utf8')) as Record<string, unknown>).accounts; }
    catch { throw new SessionError('invalidResponse', failure(operation, 'PROTOCOL', 'DocuSign returned a malformed response: account information lists no accounts')); }
    if (!Array.isArray(accounts) || accounts.length === 0) throw new SessionError('invalidResponse', failure(operation, 'PROTOCOL', 'DocuSign returned a malformed response: account information lists no accounts'));
    for (const entry of accounts) {
      if (entry === null) continue;
      if (typeof entry !== 'object' || Array.isArray(entry)) throw new SessionError('invalidResponse', failure(operation, 'PROTOCOL', 'DocuSign returned a malformed response: account information lists no accounts'));
      const account = entry as Record<string, unknown>;
      if ([account.account_id, account.base_uri].some(value => value !== undefined && value !== null && typeof value !== 'string')) {
        throw new SessionError('invalidResponse', failure(operation, 'PROTOCOL', 'DocuSign returned a malformed response: account information lists no accounts'));
      }
    }
    const selected = accounts.find((entry: unknown) => {
      if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return false;
      const account = entry as Record<string, unknown>;
      return this.accountId ? typeof account.account_id === 'string' && account.account_id.toLowerCase() === this.accountId.toLowerCase() : account.is_default === true || account.is_default === 'true';
    }) as Record<string, unknown> | undefined;
    if (!selected) throw new SessionError('defect', failure(operation, 'VALIDATION', this.accountId
      ? 'the authorizing DocuSign user does not belong to the configured accountId; correct accountId or authorize as a member of that account'
      : "DocuSign reports no default account for the authorizing user; set the connection's accountId"));
    if (typeof selected.account_id !== 'string' || !isGuid(selected.account_id) || typeof selected.base_uri !== 'string') throw new SessionError('invalidResponse', failure(operation, 'PROTOCOL', 'DocuSign returned a malformed response: the account ID is not a GUID'));
    const baseUriMatch = /^https:\/\/([a-z0-9.-]+)\/?$/.exec(selected.base_uri);
    const host = baseUriMatch?.[1];
    if (!host || !(method === 'docusign-developer-oauth' ? host === 'demo.docusign.net' : productionHostPattern.test(host))) {
      throw new SessionError('invalidResponse', failure(operation, 'PROTOCOL', 'DocuSign returned a malformed response: the account base URI is not a DocuSign eSignature host of this environment'));
    }
    const baseUri = `https://${host}`;
    const accountId = selected.account_id.toLowerCase();
    this.accountCache.set(key, { accountId, baseUri, at: this.now().getTime() });
    return { credentials, accountId, baseUri };
  }

  /** Send one eSignature request after account selection. */
  requestAPI(call: Call, session: DocuSignSession, method: string, path: string, query?: Record<string, string>, body?: unknown, accept = 'application/json', limit = this.maxResponseBytes, timeoutMs = 25_000): Promise<DocuSignResponse> {
    const suffix = query ? `?${Object.entries(query).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0).map(([key, value]) => `${goQueryEscape(key)}=${goQueryEscape(value)}`).join('&')}` : '';
    return this.request(call, session.credentials, method, `${session.baseUri}/restapi/v2.1/accounts/${escapePath(session.accountId)}${path}${suffix}`, body, accept, limit, timeoutMs);
  }

  /** Open the combined PDF without buffering a successful response in Dex memory. */
  async openDocument(call: Call, session: DocuSignSession, envelopeId: string, includeCertificate: boolean): Promise<DocumentResponse> {
    const path = `/restapi/v2.1/accounts/${escapePath(session.accountId)}/envelopes/${escapePath(envelopeId)}/documents/combined?certificate=${includeCertificate}`;
    if (this.hasCustomSend) {
      const response = await this.requestAPI(call, session, 'GET', `/envelopes/${escapePath(envelopeId)}/documents/combined`, { certificate: String(includeCertificate) }, undefined, 'application/pdf', this.maxDocumentBytes, 110_000);
      if (response.status < 200 || response.status >= 300) return { rejected: response };
      return { headers: response.headers, stream: (async function* () { yield response.body; })(), async close() {} };
    }
    let credentials = session.credentials;
    for (let attempt = 0; attempt < 2; attempt++) {
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), 110_000);
      const onCancel = () => controller.abort(call.context.cancellationSignal.reason);
      call.context.cancellationSignal.addEventListener('abort', onCancel, { once: true });
      let response: Response;
      try {
        response = await fetch(`${session.baseUri}${path}`, {
          method: 'GET', headers: { Authorization: `Bearer ${credentials.accessToken.reveal()}`, Accept: 'application/pdf' },
          redirect: 'manual', signal: controller.signal,
        });
      } catch (error) {
        clearTimeout(timeout);
        call.context.cancellationSignal.removeEventListener('abort', onCancel);
        throw error;
      }
      const headers: Record<string, string> = {};
      response.headers.forEach((value, name) => { headers[name] = value; });
      const rejectedBody = response.status < 200 || response.status >= 300 ? await readErrorBody(response, maximumErrorBodyBytes) : undefined;
      if (response.status === 401 && attempt === 0 && 'resolveAfterRejection' in this.credentials) {
        clearTimeout(timeout);
        call.context.cancellationSignal.removeEventListener('abort', onCancel);
        try { credentials = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal); }
        catch { return { rejected: { status: 401, headers, body: rejectedBody ?? new Uint8Array(), isTooLarge: false } }; }
        if (!isHeaderSafeCredential(credentials.accessToken.reveal())) return { rejected: { status: 401, headers, body: rejectedBody ?? new Uint8Array(), isTooLarge: false } };
        continue;
      }
      if (response.status < 200 || response.status >= 300) {
        clearTimeout(timeout);
        call.context.cancellationSignal.removeEventListener('abort', onCancel);
        return { rejected: { status: response.status, headers, body: rejectedBody ?? new Uint8Array(), isTooLarge: false } };
      }
      const reader = response.body?.getReader();
      if (!reader) {
        clearTimeout(timeout);
        call.context.cancellationSignal.removeEventListener('abort', onCancel);
        throw new Error('DocuSign PDF body was unreadable');
      }
      const stream = (async function* () {
        while (true) {
          const chunk = await reader.read();
          if (chunk.done) return;
          yield chunk.value;
        }
      })();
      return { headers, stream, async close() {
        await reader.cancel().catch(() => { /* Close errors cannot change a completed response classification. */ });
        clearTimeout(timeout);
        call.context.cancellationSignal.removeEventListener('abort', onCancel);
      } };
    }
    throw new Error('DocuSign PDF authentication retry exhausted');
  }

  /** Classify one conclusive refusal without exposing provider messages. */
  classify(operation: string, response: DocuSignResponse): DocuSignOutcome {
    const errorCode = readErrorTokens(response.body, ['/errorCode', '/error'])[0] ?? '';
    const code = errorCode.toUpperCase();
    const status = response.status;
    let retry = false; let notFound = false; let retryAfterMs = 0; let kind: FailureKind; let message: string;
    if (rateLimitCodes.has(code) || status === 429) {
      retry = true; kind = 'RATE_LIMIT'; message = 'DocuSign rate limited the request';
      const reset = Number(response.headers['x-ratelimit-reset']);
      const resetDelay = Number.isFinite(reset) && reset > 0 ? Math.max(0, reset * 1000 - this.now().getTime()) : 0;
      retryAfterMs = code.startsWith('BURST_') ? 30_000 : Math.min(resetDelay || parseRetryAfter(String(response.headers['retry-after'] ?? ''), this.now()) || (code.startsWith('HOURLY_') ? nextHour(this.now()) : 0), 3_600_000);
    } else if (status >= 500 || status === 408) { retry = true; kind = 'AVAILABILITY'; message = `DocuSign answered HTTP ${status}`; }
    else if (code === 'ENVELOPE_ALLOWANCE_EXCEEDED') { kind = 'QUOTA_EXHAUSTED'; message = 'the DocuSign account has no envelope allowance left'; }
    else if (status === 401 || ['USER_AUTHENTICATION_FAILED', 'AUTHORIZATION_INVALID_TOKEN', 'PARTNER_AUTHENTICATION_FAILED'].includes(code)) { kind = 'AUTHENTICATION'; message = 'DocuSign rejected the access token; authorize the connection again'; }
    else if (status === 403 || ['ACCOUNT_LACKS_PERMISSIONS', 'USER_LACKS_PERMISSIONS', 'USER_DOES_NOT_BELONG_TO_SPECIFIED_ACCOUNT', 'ACCOUNT_NOT_AUTHORIZED_FOR_ENVELOPE', 'USER_NOT_ENVELOPE_SENDER_OR_RECIPIENT', 'USER_LACKS_MEMBERSHIP', 'USER_NOT_ACCOUNT_ADMIN'].includes(code)) { kind = 'AUTHORIZATION'; message = 'DocuSign denied permission for this account, user, or envelope'; }
    else if (status === 404 || ['ENVELOPE_DOES_NOT_EXIST', 'RESOURCE_NOT_FOUND', 'DOCUMENT_DOES_NOT_EXIST'].includes(code)) { notFound = true; kind = 'NOT_FOUND'; message = 'the DocuSign envelope or document was not found'; }
    else { kind = 'PROVIDER_REJECTION'; message = `DocuSign rejected the request with HTTP ${status}`; }
    return { retry, notFound, retryAfterMs, errorCode, failure: failure(operation, kind, errorCode ? `${message} (${errorCode})` : message) };
  }

  /** Build a provider receipt with only safe envelope identity and error code. */
  receipt(envelopeId: string, errorCode = ''): ReceiptDraft {
    return { provider: 'docusign', providerObjectId: envelopeId, observedAt: this.now().toISOString(), ...(errorCode ? { metadata: { errorCode } } : {}) };
  }

  /** Return the injected clock for recovery windows and receipts. */
  currentTime(): Date { return this.now(); }

  private async request(call: Call, credentials: Credentials, method: string, url: string, body: unknown, accept: string, limit: number, timeoutMs = 25_000): Promise<DocuSignResponse> {
    const encoded = body === undefined ? undefined : Buffer.from(JSON.stringify(body));
    let current = credentials;
    for (let attempt = 0; attempt < 2; attempt++) {
      const request: ProviderRequest = { url, method, headers: { Authorization: `Bearer ${current.accessToken.reveal()}`, Accept: accept, ...(encoded ? { 'Content-Type': 'application/json' } : {}) },
        ...(encoded ? { body: encoded } : {}), maxResponseBytes: Math.max(limit, maximumErrorBodyBytes), addressPolicy: 'publicHTTPS', timeoutMs, signal: call.context.cancellationSignal };
      let response: ProviderResponse;
      try { response = await this.send(request); }
      catch (error) {
        if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers !== undefined && (error.status < 200 || error.status >= 300)) return { status: error.status, headers: error.headers, body: new Uint8Array(), isTooLarge: false };
        if (error instanceof BodyTooLargeError) throw new ResponseTooLargeError();
        throw error;
      }
      const answer = { ...response, isTooLarge: response.status >= 200 && response.status < 300 && response.body.length > limit };
      if (answer.isTooLarge) throw new ResponseTooLargeError();
      if (response.status !== 401 || attempt !== 0 || !('resolveAfterRejection' in this.credentials)) return answer;
      try { current = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal); }
      catch { return answer; }
      if (!isHeaderSafeCredential(current.accessToken.reveal())) return answer;
    }
    throw new Error('DocuSign authenticated request retry was exhausted');
  }
}

function nextHour(now: Date): number { return 3_600_000 - (now.getTime() % 3_600_000); }
function goQueryEscape(value: string): string { return encodeURIComponent(value).replace(/%20/g, '+').replace(/~/g, '%7E').replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`); }
async function readErrorBody(response: Response, maximumBytes: number): Promise<Uint8Array> {
  const reader = response.body?.getReader();
  if (!reader) return new Uint8Array();
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    while (length < maximumBytes) {
      const chunk = await reader.read();
      if (chunk.done) break;
      const bounded = chunk.value.subarray(0, maximumBytes - length);
      chunks.push(bounded); length += bounded.length;
    }
  } catch { /* Go keeps the HTTP status even when an error body is interrupted. */ }
  finally { await reader.cancel().catch(() => { /* The response is already classified by status. */ }); }
  return Buffer.concat(chunks, length);
}
