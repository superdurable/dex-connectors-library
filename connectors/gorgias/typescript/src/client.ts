// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, ProviderStalledError, ProviderTransportError, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { defaultTriggerLogger, ReauthorizationRequiredError, type Call, type CredentialProvider, type Failure, type Receipt, type TriggerLogger } from '@superdurable/connectors-sdk';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import { isCredentialMaterialError, type Config, type Credentials } from './generated/models.js';
import { SearchTicketsOperation, GetTicketOperation, FindCustomerByEmailOperation, CreateTicketOperation, UpdateTicketOperation, AddNoteOperation } from './operations.js';

const domainPattern = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;
const requestIDPattern = /^[A-Za-z0-9._:-]{1,128}$/;
const apiCallLimitPattern = /^[0-9]{1,6}\/[0-9]{1,6}$/;
const errorCodePattern = /^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$/;
const errorFieldPattern = /^[a-z][a-z0-9_]{0,63}$/;

/** One classified Gorgias exchange, with provider response data only on success. */
export type Exchange = {
  outcome: 'succeeded' | 'notFound' | 'rateLimited' | 'notSent' | 'unavailable' | 'rejected' | 'invalid' | 'defect';
  /** Present whenever a status arrived, also when the body then failed; a 2xx proves acceptance. */
  response?: ProviderResponse;
  failure?: Failure;
  retryAfterMs: number;
  /** The body exceeded maxResponseBytes and was not read, so it cannot prove a refusal. */
  bodyTooLarge?: boolean;
  /** The body contains the API key, so it cannot prove a refusal. */
  bodyReflectsCredential?: boolean;
  /** The top-level identity a readable body names; a write answer that carries an ID was accepted. */
  object?: ObjectMarker;
};

/** Top-level id, external_id, and ticket_id of a response body; zeros and empty when absent. */
export type ObjectMarker = { id: number; externalId: string; ticketId: number };

/** Credential resolution; isTerminal marks revoked, unselected, or unusable credentials. */
export type CredentialResolution = { credentials?: Credentials; failure?: Failure; isTerminal?: boolean };

/** Injectable request sender for credential safe tests. */
export type RequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Client dependencies for tests and local provider stand-ins. */
export interface GorgiasClientOptions {
  /** Alternate loopback API base URL. */ apiBaseURL?: string;
  /** Alternate bounded HTTP sender. */ send?: RequestSender;
  /** Receipt, Retry-After, and ticketChanged baseline clock. */ now?: () => Date;
  /**
   * Receives the ticketChanged runner's records: seeds, reseeds after an undecodable cursor, failed polls,
   * rate-limit pauses, and the durable inboxes newProjectTicketTriggerRunner opens. Records carry binding
   * identity and never event contents or credentials. Omitted uses defaultTriggerLogger. Operation Steps do not log.
   */
  logger?: TriggerLogger;
}

// Keeps a ticketChanged poll loop from spending the account's 40 requests per 20 seconds on empty polls.
const minimumPollIntervalMs = 10_000;

/** Authenticated Gorgias API client shared by connector operations. */
export class GorgiasClient {
  private readonly endpoint: string;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly maxResponseBytes: number;
  private readonly send: RequestSender;
  private readonly now: () => Date;
  /** Delay between ticketChanged polls in milliseconds, at least 10 seconds; one minute by default. */ readonly pollIntervalMs: number;
  /** Receives the ticketChanged runner's and durable inboxes' records. */ readonly triggerLogger: TriggerLogger;

  /** Validate configuration and bind the credential source. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: GorgiasClientOptions = {}) {
    if (!domainPattern.test(config.domain)) throw new TypeError('Gorgias domain must be lowercase letters, digits, and hyphens');
    if (!credentials) throw new TypeError('Gorgias credential provider is required');
    const maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    if (!Number.isSafeInteger(maxResponseBytes) || maxResponseBytes < 1) throw new TypeError('Gorgias response limit must be positive');
    this.endpoint = validateBaseURL(options.apiBaseURL ?? `https://${config.domain}.gorgias.com/api`);
    this.credentials = credentials;
    this.maxResponseBytes = maxResponseBytes;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
    // A zero pollInterval keeps the one-minute default, as Go's withConfigDefaults does.
    this.pollIntervalMs = config.pollIntervalMs === undefined || config.pollIntervalMs === 0 ? 60_000 : config.pollIntervalMs;
    if (!Number.isSafeInteger(this.pollIntervalMs) || this.pollIntervalMs < minimumPollIntervalMs) throw new TypeError('Gorgias pollInterval must be at least 10s');
    this.triggerLogger = options.logger ?? defaultTriggerLogger;
  }

  /** Bind searchTickets. */ searchTickets(): SearchTicketsOperation { return new SearchTicketsOperation(this); }
  /** Bind getTicket. */ getTicket(): GetTicketOperation { return new GetTicketOperation(this); }
  /** Bind findCustomerByEmail. */ findCustomerByEmail(): FindCustomerByEmailOperation { return new FindCustomerByEmailOperation(this); }
  /** Bind createTicket. */ createTicket(): CreateTicketOperation { return new CreateTicketOperation(this); }
  /** Bind updateTicket. */ updateTicket(): UpdateTicketOperation { return new UpdateTicketOperation(this); }
  /** Bind addNote. */ addNote(): AddNoteOperation { return new AddNoteOperation(this); }

  /** Return the current time of the injected clock. */ currentTime(): Date { return this.now(); }

  /**
   * Resolve current credentials for a Query without retaining credential bearing errors. A terminal failure selects
   * defect; any other one is retried, because nothing reached Gorgias.
   */
  async resolveCredentials(call: Call, operation: string): Promise<CredentialResolution> {
    return this.resolveWriteCredentials(call, operation);
  }

  /**
   * Resolve current credentials without retaining credential bearing errors. A failure is terminal for
   * revoked, unselected, or unusable credentials; any other provider error may succeed on a later attempt.
   */
  async resolveWriteCredentials(call: Call, operation: string): Promise<CredentialResolution> {
    const unusable = { failure: gorgiasFailure('AUTHENTICATION', operation, 'connection credentials are unavailable'), isTerminal: true };
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch (error) {
      // Credential provider errors may contain secrets, so only their class is kept.
      if (error instanceof ReauthorizationRequiredError || error instanceof ObjectNotFoundError || error instanceof ConnectionNotSelectedError || error instanceof CredentialBrokerRejectedError || isCredentialMaterialError(error)) return unusable;
      return { failure: gorgiasFailure('AVAILABILITY', operation, 'connection credentials could not be read'), isTerminal: false };
    }
    const secret = credentials?.apiKey?.reveal?.();
    if (typeof credentials?.email === 'string' && isBareEmail(credentials.email) && !credentials.email.includes(':') && secret && isHeaderSafeCredential(secret)) return { credentials };
    return unusable;
  }

  /** Send one request and classify its result before operation policy is applied. */
  async exchange(call: Call, credentials: Credentials, operation: string, method: string, path: string, query?: URLSearchParams, payload?: unknown, signal?: AbortSignal): Promise<Exchange> {
    let url: string;
    try {
      const encodedQuery = query ? new URLSearchParams([...query.entries()].sort(([left], [right]) => left.localeCompare(right))).toString() : '';
      url = `${this.endpoint}${path}${encodedQuery ? `?${encodedQuery}` : ''}`;
      new URL(url);
    } catch { return { outcome: 'defect', failure: gorgiasFailure('LOCAL_DEFECT', operation, 'Gorgias request could not be built'), retryAfterMs: 0 }; }
    const headers: Record<string, string> = {
      Authorization: `Basic ${Buffer.from(`${credentials.email}:${credentials.apiKey.reveal()}`, 'utf8').toString('base64')}`,
      Accept: 'application/json',
    };
    if (payload !== undefined) headers['Content-Type'] = 'application/json';
    const request: ProviderRequest = {
      url, method, headers, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.endpoint.startsWith('https://') && !this.endpoint.includes('127.0.0.1') ? 'publicHTTPS' : 'privateNetworkHTTP',
      timeoutMs: 8_000, signal: signal ?? call.context.cancellationSignal,
      ...(payload !== undefined ? { body: Buffer.from(JSON.stringify(payload), 'utf8') } : {}),
    };
    let response: ProviderResponse;
    let bodyTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined && error.headers) {
        if (error.status >= 200 && error.status < 300) return { outcome: 'invalid', response: { status: error.status, headers: error.headers, body: new Uint8Array() }, failure: gorgiasFailure('RESPONSE_TOO_LARGE', operation, 'Gorgias response exceeds the configured maxResponseBytes limit'), retryAfterMs: 0 };
        response = { status: error.status, headers: error.headers, body: new Uint8Array() };
        bodyTooLarge = true;
      } else {
        const isNotSent = error instanceof ProviderTransportError && error.isProvablyNotSent;
        // A status that arrived before the body failed is kept, so a 2xx still proves acceptance.
        const arrived = (error instanceof ProviderTransportError || error instanceof ProviderStalledError) && error.status !== undefined && error.headers
          ? { response: { status: error.status, headers: error.headers, body: new Uint8Array() } } : {};
        const message = arrived.response ? 'Gorgias response could not be read' : isNotSent ? 'Gorgias could not be reached; no request was sent' : 'Gorgias request failed before a response arrived';
        return { outcome: isNotSent ? 'notSent' : 'unavailable', ...arrived, failure: gorgiasFailure('TRANSPORT', operation, message), retryAfterMs: 0 };
      }
    }
    const object = objectMarker(response.body);
    if (response.status >= 200 && response.status < 300) {
      if (Buffer.from(response.body).includes(Buffer.from(credentials.apiKey.reveal(), 'utf8'))) return { outcome: 'invalid', response: { ...response, body: new Uint8Array() }, failure: gorgiasFailure('PROTOCOL', operation, 'Gorgias response reflected the connection credential'), retryAfterMs: 0, object };
      return { outcome: 'succeeded', response, retryAfterMs: 0, object };
    }
    const apiKey = credentials.apiKey.reveal();
    const bodyReflectsCredential = apiKey !== '' && Buffer.from(response.body).includes(Buffer.from(apiKey, 'utf8'));
    return { ...this.classifyFailureStatus(response, credentials, operation), bodyTooLarge, bodyReflectsCredential, object };
  }

  /** Map a non-2xx response; Gorgias documents 429 as a request it did not perform. */
  private classifyFailureStatus(response: ProviderResponse, credentials: Credentials, operation: string): Exchange {
    const retryAfterMs = parseRetryAfter(header(response, 'retry-after'), this.now());
    const status = response.status;
    const summary = status === 401 || status === 403 ? '' : errorSummary(response.body, credentials.apiKey.reveal());
    const suffix = summary ? ` [${summary}]` : '';
    if (status === 429) return { outcome: 'rateLimited', response: safeResponse(response), failure: gorgiasFailure('RATE_LIMIT', operation, `Gorgias rate limited the request (HTTP 429)${suffix}`), retryAfterMs };
    if (status === 408 || status >= 500) return { outcome: 'unavailable', response: safeResponse(response), failure: gorgiasFailure('AVAILABILITY', operation, `Gorgias could not complete the request (HTTP ${status})${suffix}`), retryAfterMs };
    if (status === 404) return { outcome: 'notFound', response: safeResponse(response), failure: gorgiasFailure('NOT_FOUND', operation, `Gorgias found no such resource (HTTP 404)${suffix}`), retryAfterMs: 0 };
    if (status >= 300 && status < 400) return { outcome: 'rejected', response: safeResponse(response), failure: gorgiasFailure('PROTOCOL', operation, `Gorgias redirected the request (HTTP ${status}); the ticket or customer may have been merged into another`), retryAfterMs: 0 };
    const kind = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 409 ? 'CONFLICT' : status === 400 ? 'VALIDATION' : 'PROVIDER_REJECTION';
    return { outcome: 'rejected', response: safeResponse(response), failure: gorgiasFailure(kind, operation, `Gorgias rejected the request (HTTP ${status})${suffix}`), retryAfterMs: 0 };
  }

  /** Make a provider safe receipt from one exchange. */
  receipt(call: Call, exchange?: Exchange, objectID = 0): Receipt {
    const response = exchange?.response;
    const requestID = response ? header(response, 'x-request-id') : '';
    const apiCallLimit = response ? header(response, 'x-gorgias-account-api-call-limit') : '';
    return {
      callId: call.id, provider: 'gorgias', observedAt: this.now().toISOString().replace('.000Z', 'Z'),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectID > 0 ? { providerObjectId: String(objectID) } : {}),
      ...(requestIDPattern.test(requestID) ? { providerRequestId: requestID } : {}),
      ...(apiCallLimitPattern.test(apiCallLimit) ? { metadata: { apiCallLimit } } : {}),
    };
  }
}

/** Build one credential safe failure. */
export function gorgiasFailure(kind: Failure['kind'], operation: string, message: string): Failure { return { kind, provider: 'gorgias', operation, message }; }

/** Match Go's bare address restrictions before using it as a Basic user name. */
export function isBareEmail(value: string): boolean {
  if (!value || /[\s"'()<>,;]/.test(value) || value.includes('..')) return false;
  const parts = value.split('@');
  if (parts.length !== 2 || !parts[0] || !parts[1]) return false;
  return /^[A-Za-z0-9!#$%&*+\-/=?^_`{|}~]+(?:\.[A-Za-z0-9!#$%&*+\-/=?^_`{|}~]+)*$/.test(parts[0]) &&
    parts[1].split('.').every(label => /^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$/.test(label));
}

function safeResponse(response: ProviderResponse): ProviderResponse { return { ...response, body: new Uint8Array() }; }
/** Read the top-level identity of a body as Go decodes it; any ill-typed field names no object. */
function objectMarker(body: Uint8Array): ObjectMarker {
  const none = { id: 0, externalId: '', ticketId: 0 };
  let parsed: unknown;
  try { parsed = JSON.parse(Buffer.from(body).toString('utf8')); } catch { return none; }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return none;
  const { id, external_id: externalId, ticket_id: ticketId } = parsed as Record<string, unknown>;
  const isOptionalInteger = (value: unknown) => value === undefined || value === null || Number.isSafeInteger(value);
  if (!isOptionalInteger(id) || !isOptionalInteger(ticketId) || !(externalId === undefined || externalId === null || typeof externalId === 'string')) return none;
  return { id: (id as number | null | undefined) ?? 0, externalId: (externalId as string | null | undefined) ?? '', ticketId: (ticketId as number | null | undefined) ?? 0 };
}
function header(response: ProviderResponse, name: string): string {
  const value = response.headers[name.toLowerCase()];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
function errorSummary(body: Uint8Array, secret: string): string {
  try {
    const parsed = JSON.parse(Buffer.from(body).toString('utf8')) as { error?: { msg?: unknown; data?: Record<string, unknown> } };
    const parts: string[] = [];
    if (typeof parsed.error?.msg === 'string' && errorCodePattern.test(parsed.error.msg) && !parsed.error.msg.includes(secret)) parts.push(parsed.error.msg);
    const fields = Object.keys(parsed.error?.data ?? {}).filter(field => errorFieldPattern.test(field) && !field.includes(secret)).sort().slice(0, 5);
    if (fields.length) parts.push(`fields: ${fields.join(', ')}`);
    return parts.join('; ');
  } catch { return ''; }
}
