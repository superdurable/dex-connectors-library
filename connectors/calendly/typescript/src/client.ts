// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type FailureKind, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { ListScheduledEventsOperation, GetScheduledEventOperation, ListEventInviteesOperation, CancelScheduledEventOperation, CreateSchedulingLinkOperation, CreateWebhookSubscriptionOperation } from './operations.js';

const apiBaseURL = 'https://api.calendly.com';

/** Optional transport and clock for deterministic provider tests. */
export interface CalendlyClientOptions {
  /** Replace the HTTP sender without changing the provider URL. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Receipt and rate limit clock. */ now?: () => Date;
}

/** One response or secret-safe local failure. */
export type CalendlyExchange =
  | { kind: 'response'; response: ProviderResponse }
  | { kind: 'tooLarge' | 'malformed' | 'transport' | 'notSent' | 'defect'; response?: ProviderResponse };

/** Credential resolution result. */
export type CalendlyCredentialResult =
  | { kind: 'resolved'; credentials: Credentials }
  | { kind: 'terminal'; message: string }
  | { kind: 'transient'; message: string };

/** Calendly API v2 client with generated OAuth refresh declarations. */
export class CalendlyClient {
  readonly maxResponseBytes: number;
  readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Calendly', auth: Authorization, credentialFields: CredentialFields });

  /** Validate settings and bind a credential source. */
  constructor(config: Config, readonly credentials: CredentialSource, options: CalendlyClientOptions = {}) {
    if (!credentials) throw new TypeError('Calendly credential provider is required');
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Calendly response limit is invalid');
    if (config.webhookMaxBodyBytes !== undefined && (!Number.isSafeInteger(config.webhookMaxBodyBytes) || config.webhookMaxBodyBytes < 0)) throw new TypeError('Calendly webhook body limit is invalid');
    if (config.webhookSignatureToleranceMs !== undefined && (!Number.isSafeInteger(config.webhookSignatureToleranceMs) || config.webhookSignatureToleranceMs < 0)) throw new TypeError('Calendly webhook signature tolerance is invalid');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the scheduled event listing Query. */ listScheduledEvents() { return new ListScheduledEventsOperation(this); }
  /** Bind the scheduled event read Query. */ getScheduledEvent() { return new GetScheduledEventOperation(this); }
  /** Bind the invitee listing Query. */ listEventInvitees() { return new ListEventInviteesOperation(this); }
  /** Bind the event cancellation Mutation. */ cancelScheduledEvent() { return new CancelScheduledEventOperation(this); }
  /** Bind the single-use link Mutation. */ createSchedulingLink() { return new CreateSchedulingLinkOperation(this); }
  /** Bind the webhook subscription Mutation. */ createWebhookSubscription() { return new CreateWebhookSubscriptionOperation(this); }

  /** Capture only provider-safe receipt fields. */
  receipt(call: Call, objectURI = ''): ReceiptDraft {
    return { callId: call.id, provider: 'calendly', observedAt: this.now().toISOString(), ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}), ...(objectURI ? { providerObjectId: objectURI } : {}) };
  }

  /** Resolve private or shared credentials through the manifest refresh driver. */
  async resolveCredentials(call: Call): Promise<CalendlyCredentialResult> {
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      if (!isHeaderSafeCredential(credentials.accessToken.reveal())) return { kind: 'terminal', message: 'the Calendly access token is blank or contains characters a header cannot carry' };
      return { kind: 'resolved', credentials };
    } catch (error) {
      const terminal = error instanceof ReauthorizationRequiredError;
      return terminal
        ? { kind: 'terminal', message: 'the Calendly connection needs reauthorization; authorize it again in Dex Web Connectors' }
        : { kind: 'transient', message: 'the Calendly access token could not be loaded or refreshed yet' };
    }
  }

  /** Send a Calendly request and retry a rejected OAuth token once. */
  async exchange(call: Call, credentials: Credentials, method: string, path: string, query?: Record<string, string>, payload?: unknown, signal?: AbortSignal, allowRefresh = true): Promise<CalendlyExchange> {
    const url = apiBaseURL + path + (query && Object.keys(query).length ? '?' + encodeGoQuery(query) : '');
    let body: Uint8Array | undefined;
    try { if (payload !== undefined) body = Buffer.from(JSON.stringify(payload)); }
    catch { return { kind: 'defect' }; }
    for (let attempt = 0; attempt < 2; attempt++) {
      const token = credentials.accessToken.reveal();
      const request: ProviderRequest = { url, method, headers: { Authorization: `Bearer ${token}`, Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) }, ...(body ? { body } : {}), addressPolicy: 'publicHTTPS', maxResponseBytes: this.maxResponseBytes, timeoutMs: 25_000, signal: signal ?? call.context.cancellationSignal };
      let response: ProviderResponse;
      try { response = await this.send(request); }
      catch (error) {
        if (error instanceof BodyTooLargeError) {
          if (error.status !== undefined && (error.status < 200 || error.status >= 300)) return { kind: 'response', response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() } };
          return { kind: 'tooLarge' };
        }
        return { kind: error instanceof ProviderTransportError && error.isProvablyNotSent ? 'notSent' : 'transport' };
      }
      if (response.status !== 401 || attempt > 0 || !allowRefresh || credentials.authMethodId === 'personal-access-token' || !('resolveAfterRejection' in this.credentials)) return { kind: 'response', response };
      try {
        const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
        if (!isHeaderSafeCredential(replacement.accessToken.reveal())) return { kind: 'response', response };
        credentials.accessToken = replacement.accessToken;
        if (replacement.refreshToken) credentials.refreshToken = replacement.refreshToken;
      } catch { return { kind: 'response', response }; }
    }
    return { kind: 'defect' };
  }

  /** Classify an HTTP error without persisting the provider body. */
  failure(operation: string, response: ProviderResponse): { failure: Failure; retryAfterMs: number; isRetry: boolean } {
    const status = response.status;
    if (status === 429) {
      const retryAfter = headerValue(response.headers, 'retry-after');
      const resetSeconds = Number(headerValue(response.headers, 'x-ratelimit-reset').trim());
      return { failure: failure(operation, 'RATE_LIMIT', 'Calendly rate limited the request'), retryAfterMs: parseRetryAfter(retryAfter, this.now()) || (Number.isSafeInteger(resetSeconds) && resetSeconds > 0 ? Math.min(resetSeconds * 1000, 3_600_000) : 0), isRetry: true };
    }
    if (status >= 500) return { failure: failure(operation, 'AVAILABILITY', `Calendly answered HTTP ${status}`), retryAfterMs: 0, isRetry: true };
    if (status === 401) return { failure: failure(operation, 'AUTHENTICATION', 'Calendly rejected the access token; generate a new personal access token or authorize the connection again'), retryAfterMs: 0, isRetry: false };
    if (status === 403) {
      let insufficientScope = false;
      try { insufficientScope = (JSON.parse(Buffer.from(response.body).toString('utf8')) as { title?: unknown }).title === 'Insufficient scope'; } catch { /* An unreadable error body retains its HTTP classification. */ }
      return { failure: failure(operation, 'AUTHORIZATION', insufficientScope ? 'the Calendly token lacks a scope this operation needs; grant the scopes listed in the connector guide' : 'Calendly denied permission for this user, organization, or resource'), retryAfterMs: 0, isRetry: false };
    }
    if (status === 404) return { failure: failure(operation, 'NOT_FOUND', 'the Calendly resource was not found'), retryAfterMs: 0, isRetry: false };
    if (status === 409) return { failure: failure(operation, 'CONFLICT', 'Calendly reports that the resource already exists'), retryAfterMs: 0, isRetry: false };
    return { failure: failure(operation, 'PROVIDER_REJECTION', `Calendly rejected the request with HTTP ${status}`), retryAfterMs: 0, isRetry: false };
  }
}

/** Construct a failure with the stable connector identity. */
export function failure(operation: string, kind: FailureKind, message: string): Failure { return { kind, provider: 'calendly', operation, message }; }

/** Encode keys and values as Go url.Values.Encode does. */
export function encodeGoQuery(values: Record<string, string>): string {
  const escape = (value: string) => Array.from(Buffer.from(value)).map(byte => {
    const char = String.fromCharCode(byte);
    if (/[A-Za-z0-9_.~-]/.test(char)) return char === '~' ? '%7E' : char;
    return char === ' ' ? '+' : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
  }).join('');
  return Object.keys(values).sort().map(key => `${escape(key)}=${escape(values[key]!)}`).join('&');
}

function headerValue(headers: ProviderResponse['headers'], name: string): string {
  const value = headers[name];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}
