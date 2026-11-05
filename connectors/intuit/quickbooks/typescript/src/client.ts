// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { isIP } from 'node:net';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { ReauthorizationRequiredError, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { CreateCustomerOperation, CreateInvoiceOperation, FindCustomerOperation, GetInvoiceOperation, ListInvoicesOperation, RecordPaymentOperation, SendInvoiceOperation } from './operations.js';

const productionEndpoint = 'https://quickbooks.api.intuit.com';
const sandboxEndpoint = 'https://sandbox-quickbooks.api.intuit.com';
const realmPattern = /^[1-9][0-9]{0,24}$/;
const transactionPattern = /^[A-Za-z0-9-]{1,64}$/;

/** Injectable sender and clock for deterministic provider verification. */
export interface QuickBooksClientOptions {
  /** Caller-owned transport; production uses the SDK provider transport. */
  send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** UTC receipt and Retry-After clock. */
  now?: () => Date;
  /** Loopback HTTP endpoint for the connector's runnable example. */
  localProviderURL?: string;
}

/** One classified provider exchange. */
export interface QuickBooksExchange {
  /** Classified provider outcome. */
  disposition: 'succeeded' | 'notFound' | 'retry' | 'interrupted' | 'rejected' | 'nameConflict' | 'invalid' | 'defect';
  /** Successful body, when available. */
  body?: Uint8Array;
  /** Failure, when unsuccessful. */
  failure?: Failure;
  /** Delay for a retry in milliseconds. */
  retryAfterMs: number;
  /** Safe correlation receipt. */
  receipt: ReceiptDraft;
  /** HTTP status, when the provider answered. */
  status: number;
}

/** One QuickBooks request below the connected company's API path. */
export interface QuickBooksRequest {
  /** HTTP method. */ method: 'GET' | 'POST';
  /** Path below the company. */ path: string;
  /** Additional query values. */ query?: Record<string, string>;
  /** Encoded JSON payload supplied by an operation. */ body?: Uint8Array;
  /** Whether the operation uses QuickBooks requestid. */ mutation?: boolean;
}

interface AttemptBudget { deadline: number; minimumWriteTime: number; }

/** QuickBooks client with manifest-driven OAuth renewal and company identity validation. */
export class QuickBooksClient {
  private readonly endpoint: string;
  private readonly environment: 'production' | 'sandbox';
  private readonly configuredRealmID: string;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Intuit', auth: Authorization, credentialFields: CredentialFields });

  /** Validate immutable connection configuration without contacting Intuit. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: QuickBooksClientOptions = {}) {
    if (!credentials) throw new TypeError('QuickBooks credential provider is required');
    this.environment = config.environment === 'sandbox' ? 'sandbox' : 'production';
    if (config.environment && config.environment !== 'production' && config.environment !== 'sandbox') throw new TypeError('QuickBooks environment is invalid');
    this.configuredRealmID = (config.realmId ?? '').trim();
    if (this.configuredRealmID && !realmPattern.test(this.configuredRealmID)) throw new TypeError('QuickBooks realmId must be a company ID');
    this.maxResponseBytes = config.maxResponseBytes || 4_194_304;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('QuickBooks response limit must be positive');
    this.endpoint = options.localProviderURL ?? (this.environment === 'sandbox' ? sandboxEndpoint : productionEndpoint);
    if (options.localProviderURL) {
      const target = new URL(options.localProviderURL);
      const hostname = target.hostname.replace(/^\[|\]$/g, '').toLowerCase();
      const isLoopback = hostname === 'localhost' || (isIP(hostname) === 4 && hostname.startsWith('127.')) || hostname === '::1' || (isIP(hostname) === 6 && /^::ffff:7f[0-9a-f]{2}:[0-9a-f]{1,4}$/.test(hostname));
      if (!isLoopback || target.pathname !== '/' || target.search || target.hash || target.username || target.password) throw new TypeError('QuickBooks local provider URL must be a loopback origin');
    }
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the customer lookup. */ findCustomer() { return new FindCustomerOperation(this); }
  /** Bind customer creation. */ createCustomer() { return new CreateCustomerOperation(this); }
  /** Bind invoice creation. */ createInvoice() { return new CreateInvoiceOperation(this); }
  /** Bind invoice lookup. */ getInvoice() { return new GetInvoiceOperation(this); }
  /** Bind invoice paging. */ listInvoices() { return new ListInvoicesOperation(this); }
  /** Bind invoice delivery. */ sendInvoice() { return new SendInvoiceOperation(this); }
  /** Bind payment recording. */ recordPayment() { return new RecordPaymentOperation(this); }

  /** Return a secret-safe failure. */
  failure(kind: Failure['kind'], operation: string, message: string): Failure { return { kind, provider: 'quickbooks', operation, message }; }

  /** Send and classify one request, preserving the original requestid after a credential refresh. */
  async exchange(call: Call, operation: string, request: QuickBooksRequest, objectID = ''): Promise<QuickBooksExchange> {
    let credentials: Credentials;
    try { credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal); }
    catch (error) {
      const isReauthorization = error instanceof ReauthorizationRequiredError;
      const isMalformed = error instanceof TypeError;
      return this.localFailure(call, isReauthorization ? 'rejected' : isMalformed ? 'defect' : 'retry',
        isReauthorization || isMalformed ? 'AUTHENTICATION' : 'AVAILABILITY', operation,
        isReauthorization ? 'QuickBooks authorization must be renewed: connect the company again' : isMalformed ? 'QuickBooks connection credentials are unavailable' : 'Intuit access token refresh is temporarily unavailable', objectID);
    }
    const token = credentials.accessToken.reveal();
    if (!isHeaderSafeCredential(token)) return this.localFailure(call, 'defect', 'AUTHENTICATION', operation, 'QuickBooks connection credentials are unavailable', objectID);
    let realmID: string;
    try { realmID = resolveRealmID(credentials, this.configuredRealmID); }
    catch (error) { return this.localFailure(call, 'defect', 'AUTHENTICATION', operation, error instanceof Error ? error.message : 'QuickBooks company ID is invalid', objectID); }
    if (request.mutation && (!call.idempotencyKey || Buffer.byteLength(call.idempotencyKey, 'utf8') > 50)) return this.localFailure(call, 'defect', 'LOCAL_DEFECT', operation, 'QuickBooks mutation has no usable requestid', objectID);
    const startedAt = this.now().getTime();
    const available = Math.max(0, Math.min(30_000, (call.context.deadline?.getTime() ?? startedAt + 30_000) - startedAt));
    const budget = { deadline: startedAt + available, minimumWriteTime: available / 10 };
    let result = await this.sendRequest(call, operation, request, objectID, realmID, token, budget);
    if (result.status === 401 && 'resolveAfterRejection' in this.credentials) {
      try {
        const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
        if (isHeaderSafeCredential(replacement.accessToken.reveal()) && resolveRealmID(replacement, this.configuredRealmID) === realmID) result = await this.sendRequest(call, operation, request, objectID, realmID, replacement.accessToken.reveal(), budget);
      } catch { /* The original 401 remains the classified response. */ }
    }
    result.receipt.metadata = { ...(result.receipt.metadata ?? {}), realmId: realmID, environment: this.environment };
    return result;
  }

  private localFailure(call: Call, disposition: QuickBooksExchange['disposition'], kind: Failure['kind'], operation: string, message: string, objectID = ''): QuickBooksExchange {
    return { disposition, failure: this.failure(kind, operation, message), retryAfterMs: 0, status: 0, receipt: this.receipt(call, objectID) };
  }

  /** Create a receipt after a provider attempt. */
  receipt(call: Call, objectID: string): ReceiptDraft {
    return { callId: call.id, provider: 'quickbooks', observedAt: this.now().toISOString(), ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), ...(objectID ? { providerObjectId: objectID } : {}) };
  }

  private async sendRequest(call: Call, operation: string, request: QuickBooksRequest, objectID: string, realmID: string, token: string, budget: AttemptBudget): Promise<QuickBooksExchange> {
    const receipt = this.receipt(call, objectID);
    const now = this.now().getTime();
    if (request.mutation && (budget.minimumWriteTime <= 0 || budget.deadline - now < budget.minimumWriteTime)) return this.localFailure(call, 'retry', 'TRANSPORT', operation, 'QuickBooks write was not sent because too little attempt time remains', objectID);
    if (call.context.cancellationSignal.aborted) return this.localFailure(call, 'retry', 'TRANSPORT', operation, 'QuickBooks attempt has no time left for a request; no request was sent', objectID);
    const deadline = Math.min(call.context.deadline?.getTime() ?? Infinity, request.mutation ? budget.deadline : Infinity);
    if (deadline !== Infinity && deadline - now <= 500) return this.localFailure(call, 'retry', 'TRANSPORT', operation, 'QuickBooks attempt has no time left for a request; no request was sent', objectID);
    const timeoutMs = Math.min(20_000, deadline === Infinity ? 20_000 : deadline - now - 500);
    const query = new URLSearchParams({ ...(request.query ?? {}), minorversion: '75', ...(request.mutation ? { requestid: call.idempotencyKey! } : {}) });
    query.sort();
    const url = `${this.endpoint}/v3/company/${realmID}${request.path}?${query.toString()}`;
    const headers = { Authorization: `Bearer ${token}`, Accept: 'application/json', ...(request.body ? { 'Content-Type': 'application/json' } : request.method === 'POST' ? { 'Content-Type': 'application/octet-stream' } : {}) };
    let response: ProviderResponse;
    try {
      response = await this.send({ url, method: request.method, headers, ...(request.body ? { body: request.body } : {}), maxResponseBytes: this.maxResponseBytes, maxErrorResponseBytes: 65_536, ignoreErrorBodyReadFailure: true, addressPolicy: this.endpoint.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', timeoutMs, signal: call.context.cancellationSignal });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        const transaction = error.headers?.intuit_tid;
        const transactionID = Array.isArray(transaction) ? transaction[0] : transaction;
        if (transactionID && transactionPattern.test(transactionID)) receipt.providerRequestId = transactionID;
        return { disposition: 'invalid', status: error.status ?? 0, failure: this.failure('RESPONSE_TOO_LARGE', operation, 'QuickBooks response exceeds the configured maxResponseBytes limit'), receipt, retryAfterMs: 0 };
      }
      return { disposition: 'retry', status: 0, failure: this.failure('TRANSPORT', operation, 'QuickBooks request failed before a response arrived'), receipt, retryAfterMs: 0 };
    }
    const transactionID = response.headers.intuit_tid;
    const reportedID = Array.isArray(transactionID) ? transactionID[0] : transactionID;
    if (reportedID && transactionPattern.test(reportedID)) receipt.providerRequestId = reportedID;
    if (response.status >= 200 && response.status < 300) {
      if (Buffer.from(response.body).includes(token)) return { disposition: 'invalid', status: response.status, failure: this.failure('PROTOCOL', operation, 'QuickBooks response reflected the connection credential'), receipt, retryAfterMs: 0 };
      const fault = summarizeFault(response.body);
      if (fault.type || fault.count) return this.classifyStatus(operation, response, receipt, fault);
      return { disposition: 'succeeded', status: response.status, body: response.body, receipt, retryAfterMs: 0 };
    }
    return this.classifyStatus(operation, response, receipt, summarizeFault(response.body));
  }

  private classifyStatus(operation: string, response: ProviderResponse, receipt: ReceiptDraft, fault: FaultSummary): QuickBooksExchange {
    const status = response.status;
    if (fault.codes.length) receipt.metadata = { faultCode: fault.codes[0]! };
    const suffix = `${fault.type ? fault.type : ''}${fault.codes.length ? `${fault.type ? '; ' : ''}code ${fault.codes.join(', ')}` : ''}${fault.count > 1 ? `${fault.type || fault.codes.length ? '; ' : ''}${fault.count} errors` : ''}`;
    const classified = (disposition: QuickBooksExchange['disposition'], kind: Failure['kind'], message: string, retryAfterMs = 0): QuickBooksExchange => ({ disposition, status, failure: this.failure(kind, operation, `${message} (HTTP ${status})${suffix ? ` [${suffix}]` : ''}`), receipt, retryAfterMs });
    if (status === 429) return classified('retry', 'RATE_LIMIT', 'QuickBooks throttled the request', parseRetryAfter(String(response.headers['retry-after'] ?? ''), this.now()) || 60_000);
    if (status === 501) return classified('rejected', 'PROVIDER_REJECTION', 'QuickBooks does not implement the request');
    if (status === 408 || status >= 500) return classified('retry', 'AVAILABILITY', 'QuickBooks is temporarily unavailable');
    if (status === 401) return classified('rejected', 'AUTHENTICATION', 'QuickBooks rejected the access token');
    if (status === 403) return fault.codes.includes('3100') ? classified('rejected', 'AUTHORIZATION', 'QuickBooks token lacks the com.intuit.quickbooks.accounting scope') : classified('rejected', 'AUTHORIZATION', 'QuickBooks denied access to the company or the request');
    if (status === 404) return classified('notFound', 'NOT_FOUND', 'QuickBooks found no such resource');
    if (status === 409) return classified('retry', 'CONFLICT', 'QuickBooks returned a conflict; retrying the same requestid');
    if (fault.codes.includes('600')) return classified('retry', 'CONFLICT', 'QuickBooks is still answering an earlier attempt with this requestid');
    if (fault.codes.includes('610')) return classified('notFound', 'NOT_FOUND', 'QuickBooks found no such object');
    if (fault.codes.includes('6240')) return classified('nameConflict', 'CONFLICT', 'QuickBooks already has a name list entry with that display name');
    if (fault.codes.includes('6140')) return classified('rejected', 'CONFLICT', 'QuickBooks already has a transaction with that document number');
    if (fault.type.toLowerCase() === 'authenticationfault') return classified('rejected', 'AUTHENTICATION', 'QuickBooks rejected the authentication');
    if (fault.type.toLowerCase() === 'authorizationfault') return classified('rejected', 'AUTHORIZATION', 'QuickBooks denied the request');
    if (fault.type.toLowerCase() === 'systemfault') return classified('retry', 'AVAILABILITY', 'QuickBooks failed internally');
    if (status === 400) return classified('rejected', 'VALIDATION', 'QuickBooks rejected the request');
    if (status >= 200 && status < 300) return classified('rejected', 'VALIDATION', 'QuickBooks rejected the request');
    if (status >= 300 && status < 400) return classified('rejected', 'PROTOCOL', 'QuickBooks redirected the request');
    return classified('rejected', 'PROVIDER_REJECTION', 'QuickBooks rejected the request');
  }
}

interface FaultSummary { type: string; codes: string[]; count: number; }
function summarizeFault(body: Uint8Array): FaultSummary {
  try {
    const parseWithSource = JSON.parse as (text: string, reviver: (key: string, value: unknown, context: { source?: string }) => unknown) => unknown;
    const parsed = parseWithSource(Buffer.from(body).toString('utf8'), (_key, value, context) => typeof value === 'number' ? context.source ?? String(value) : value);
    if (!parsed || typeof parsed !== 'object' || !('Fault' in parsed) || !parsed.Fault || typeof parsed.Fault !== 'object') return { type: '', codes: [], count: 0 };
    const fault = parsed.Fault as Record<string, unknown>;
    if (fault.type != null && typeof fault.type !== 'string') return { type: '', codes: [], count: 0 };
    if (fault.Error != null && (!Array.isArray(fault.Error) || fault.Error.some(item => !item || typeof item !== 'object' || Array.isArray(item)))) return { type: '', codes: [], count: 0 };
    const errors = Array.isArray(fault.Error) ? fault.Error.slice(0, 1000) : [];
    const codes = errors.flatMap(error => { const code = error && typeof error === 'object' ? (error as Record<string, unknown>).code : undefined; const text = typeof code === 'number' || typeof code === 'string' ? String(code) : ''; return /^[0-9]{1,10}$/.test(text) ? [text] : []; }).slice(0, 5);
    return { type: typeof fault.type === 'string' && /^[A-Za-z]{1,64}$/.test(fault.type) ? fault.type : '', codes, count: errors.length };
  } catch { return { type: '', codes: [], count: 0 }; }
}

function resolveRealmID(credentials: Credentials, configured: string): string {
  const token = credentials.idToken?.reveal() ?? '';
  if (!token) { if (configured) return configured; throw new TypeError('QuickBooks company ID is unknown: authorize again while signed in to the company, or enter its company ID in realmId'); }
  if (Buffer.byteLength(token, 'utf8') > 16_384) throw new TypeError('QuickBooks ID token is too large');
  const parts = token.split('.');
  if (parts.length !== 3) throw new TypeError('QuickBooks ID token is not a JWT');
  const payloadSegment = parts[1]!.replace(/=+$/, '');
  if (!/^[A-Za-z0-9_-]+$/.test(payloadSegment) || payloadSegment.length % 4 === 1) throw new TypeError('QuickBooks ID token payload is not base64url');
  let claims: Record<string, unknown>;
  try {
    const decoded: unknown = JSON.parse(Buffer.from(payloadSegment, 'base64url').toString('utf8'));
    if (!decoded || typeof decoded !== 'object' || Array.isArray(decoded)) throw new TypeError('invalid object');
    claims = decoded as Record<string, unknown>;
  } catch { throw new TypeError('QuickBooks ID token payload is not a JSON object'); }
  if (claims.iss !== 'https://oauth.platform.intuit.com/op/v1') throw new TypeError('QuickBooks ID token was not issued by Intuit');
  const audience = claims.aud;
  if (!credentials.clientId || !(audience === credentials.clientId || (Array.isArray(audience) && audience.every(value => typeof value === 'string') && audience.includes(credentials.clientId)))) throw new TypeError('QuickBooks ID token was issued to another app');
  const realm = claims.realmid;
  if (!realm) { if (configured) return configured; throw new TypeError('QuickBooks company ID is unknown: authorize again while signed in to the company, or enter its company ID in realmId'); }
  if (typeof realm !== 'string' || !realmPattern.test(realm)) throw new TypeError('QuickBooks ID token realmid is not a company ID');
  if (configured && configured !== realm) throw new TypeError('the realmId setting names another company than the one Intuit authorized: clear it or authorize that company');
  return realm;
}
