// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, ReauthorizationRequiredError, type Call, type CredentialProvider, type Failure, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, readErrorTokens, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ConnectionNotSelectedError, CredentialBrokerRejectedError, ObjectNotFoundError } from '@superdurable/connectors-sdk/projectconfig';
import type { Config, Credentials } from './generated/models.js';
import { SearchTicketsOperation, GetTicketOperation, CreateTicketOperation, UpdateTicketOperation, AddNoteOperation } from './operations.js';

const domainPattern = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;
const requestIDPattern = /^[A-Za-z0-9._:-]{1,128}$/;
const errorFieldPattern = /^[a-z][a-z0-9_]{0,63}$/;

/** Injected transport and clock for deterministic provider replay. */
export interface FreshdeskClientOptions {
  /** Local Freshdesk-compatible endpoint; production derives it from domain. */ apiBaseURL?: string;
  /** Bounded request sender; production uses the SDK transport. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Receipt and Retry-After clock. */ now?: () => Date;
}

/** Classified result of one Freshdesk HTTP exchange. */
export type Exchange =
  | { outcome: 'succeeded'; response: ProviderResponse }
  | { outcome: 'rateLimited' | 'notSent' | 'serviceUnavailable' | 'unavailable' | 'notFound' | 'invalid' | 'defect' | 'rejected'; failure: Failure; response?: ProviderResponse; retryAfterMs: number };

/** Validated, concurrent Freshdesk API client. */
export class FreshdeskClient {
  private readonly apiBaseURL: string;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;

  /** Bind immutable configuration and a live credential source. */
  constructor(config: Config, readonly credentials: CredentialProvider<Credentials>, options: FreshdeskClientOptions = {}) {
    if (!domainPattern.test(config.domain)) throw new TypeError('Freshdesk domain is invalid');
    if (!credentials) throw new TypeError('Freshdesk credential provider is required');
    this.maxResponseBytes = config.maxResponseBytes ?? 4_194_304;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Freshdesk response limit must be positive');
    this.apiBaseURL = validateBaseURL(options.apiBaseURL ?? `https://${config.domain}.freshdesk.com/api/v2`);
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the search query. */ searchTickets(): SearchTicketsOperation { return new SearchTicketsOperation(this); }
  /** Bind the ticket read query. */ getTicket(): GetTicketOperation { return new GetTicketOperation(this); }
  /** Bind the ticket creation mutation. */ createTicket(): CreateTicketOperation { return new CreateTicketOperation(this); }
  /** Bind the ticket update mutation. */ updateTicket(): UpdateTicketOperation { return new UpdateTicketOperation(this); }
  /** Bind the note mutation. */ addNote(): AddNoteOperation { return new AddNoteOperation(this); }

  /** Resolve the current API key, classifying terminal credential states. */
  async resolveCredential(call: Call, operation: string): Promise<{ key: string } | { failure: Failure; transient: boolean }> {
    let credential: Credentials;
    try { credential = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch (error) {
      const terminal = error instanceof ObjectNotFoundError || error instanceof ReauthorizationRequiredError || error instanceof ConnectionNotSelectedError || error instanceof CredentialBrokerRejectedError;
      return { failure: this.failure(operation, terminal ? 'AUTHENTICATION' : 'AVAILABILITY', terminal ? 'connection credentials are unavailable' : 'connection credentials could not be read; no request was sent'), transient: !terminal };
    }
    if (!(credential.apiKey instanceof SecretString) || !isHeaderSafeCredential(credential.apiKey.reveal()) || credential.apiKey.reveal().includes(':')) {
      return { failure: this.failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable'), transient: false };
    }
    return { key: credential.apiKey.reveal() };
  }

  /** Send one bounded request and classify its observable outcome. */
  async exchange(call: Call, operation: string, key: string, method: string, path: string, query?: URLSearchParams, payload?: unknown, signal?: AbortSignal): Promise<Exchange> {
    const url = `${this.apiBaseURL}${path}${query ? `?${query.toString()}` : ''}`;
    const headers: Record<string, string> = { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${key}:X`).toString('base64')}` };
    let body: Uint8Array | undefined;
    if (payload !== undefined) { try { body = Buffer.from(JSON.stringify(payload)); headers['Content-Type'] = 'application/json'; } catch { return { outcome: 'defect', failure: this.failure(operation, 'LOCAL_DEFECT', 'Freshdesk request could not be built'), retryAfterMs: 0 }; } }
    try {
      const response = await this.send({ url, method, headers, ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes, addressPolicy: this.apiBaseURL.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP', timeoutMs: 12_000, signal: signal ?? call.context.cancellationSignal });
      if (response.status >= 200 && response.status < 300) {
        if (Buffer.from(response.body).includes(Buffer.from(key))) return { outcome: 'invalid', response, failure: this.failure(operation, 'PROTOCOL', 'Freshdesk response reflected the connection credential'), retryAfterMs: 0 };
        return { outcome: 'succeeded', response };
      }
      return this.classifyStatus(operation, key, response);
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && (error.status < 200 || error.status >= 300)) {
          return this.classifyStatus(operation, key, { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() });
        }
        return { outcome: 'invalid', failure: this.failure(operation, 'RESPONSE_TOO_LARGE', 'Freshdesk response exceeds the configured maxResponseBytes limit'), retryAfterMs: 0 };
      }
      if (error instanceof AddressRefusal) return { outcome: 'defect', failure: this.failure(operation, 'LOCAL_DEFECT', 'Freshdesk request could not be built'), retryAfterMs: 0 };
      if (error instanceof ProviderTransportError && error.isProvablyNotSent) return { outcome: 'notSent', failure: this.failure(operation, 'TRANSPORT', 'Freshdesk could not be reached; no request was sent'), retryAfterMs: 0 };
      return { outcome: 'unavailable', failure: this.failure(operation, 'TRANSPORT', 'Freshdesk request failed before a response arrived'), retryAfterMs: 0 };
    }
  }

  private classifyStatus(operation: string, key: string, response: ProviderResponse): Exchange {
      const status = response.status;
      const retryAfterMs = parseRetryAfter(String(response.headers['retry-after'] ?? ''), this.now());
      let outcome: Exclude<Exchange['outcome'], 'succeeded'>;
      let kind: Failure['kind'];
      let message: string;
      if (status === 429) { outcome = 'rateLimited'; kind = 'RATE_LIMIT'; message = 'Freshdesk rate limited the request (HTTP 429)'; }
      else if (status === 503) { outcome = 'serviceUnavailable'; kind = 'AVAILABILITY'; message = 'Freshdesk is unavailable (HTTP 503)'; }
      else if (status === 408 || status >= 500) { outcome = 'unavailable'; kind = 'AVAILABILITY'; message = `Freshdesk could not complete the request (HTTP ${status})`; }
      else if (status === 404) { outcome = 'notFound'; kind = 'NOT_FOUND'; message = 'Freshdesk found no such resource (HTTP 404)'; }
      else if (status >= 300 && status < 400) { outcome = 'rejected'; kind = 'PROTOCOL'; message = `Freshdesk redirected the request (HTTP ${status}); check that the domain is the freshdesk.com helpdesk domain`; }
      else { outcome = 'rejected'; kind = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 409 ? 'CONFLICT' : status === 400 ? 'VALIDATION' : 'PROVIDER_REJECTION'; message = `Freshdesk rejected the request (HTTP ${status})`; }
      if (status < 300 || status >= 400) message += safeErrorSummary(response.body, key);
      return { outcome, response, failure: this.failure(operation, kind, message), retryAfterMs };
  }

  /** Build secret-free failure metadata. */ failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'freshdesk', operation, message }; }

  /** Build a stable receipt after provider observation. */
  receipt(call: Call, response?: ProviderResponse, objectID = 0): ReceiptDraft {
    const requestID = response?.headers['x-request-id'];
    const safeID = typeof requestID === 'string' && requestIDPattern.test(requestID) ? requestID : '';
    return { callId: call.id, provider: 'freshdesk', observedAt: this.now().toISOString(), ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), ...(safeID ? { providerRequestId: safeID } : {}), ...(objectID > 0 ? { providerObjectId: String(objectID) } : {}) };
  }
}

function safeErrorSummary(body: Uint8Array, secret: string): string {
  const code = readErrorTokens(body, ['/code'])[0];
  let document: unknown;
  try { document = JSON.parse(Buffer.from(body).toString('utf8')) as unknown; } catch { document = null; }
  const errors = document && typeof document === 'object' && 'errors' in document && Array.isArray(document.errors) ? document.errors : [];
  const fields: string[] = [];
  errors.forEach((_, index) => {
    const field = readErrorTokens(body, [`/errors/${index}/field`])[0];
    const fieldCode = readErrorTokens(body, [`/errors/${index}/code`])[0];
    if (field && errorFieldPattern.test(field) && !field.includes(secret)) fields.push(field + (fieldCode && !fieldCode.includes(secret) ? `=${fieldCode}` : ''));
  });
  const details = [...(code && !code.includes(secret) ? [code] : []), ...(fields.length ? [`errors: ${fields.sort().slice(0, 5).join(', ')}`] : [])];
  return details.length ? ` [${details.join('; ')}]` : '';
}
