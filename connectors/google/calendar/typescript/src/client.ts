// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { resolveCredential, resolveCredentialAfterRejection, type Call, type Receipt } from '@superdurable/connectors-sdk';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials } from './generated/models.js';
import { ListEventsOperation, GetEventOperation, CreateEventOperation, UpdateEventOperation, QueryFreeBusyOperation } from './operations.js';

const defaultEndpoint = 'https://www.googleapis.com/calendar/v3';
const defaultMaxResponseBytes = 4_194_304;

/** A provider response with the status retained when its body is oversized. */
export interface CalendarResponse { status: number; headers: ProviderResponse['headers']; body: Uint8Array; isTooLarge: boolean; }

/** Injectable transport and receipt clock for deterministic tests. */
export interface CalendarClientOptions {
  /** Replace the production HTTP sender. */ send?: (request: ProviderRequest) => Promise<ProviderResponse>;
  /** Set the clock used by receipts. */ now?: () => Date;
}

/** Google Calendar client bound to host-owned renewable credentials. */
export class CalendarClient {
  private readonly endpoint: string;
  private readonly maxResponseBytes: number;
  private readonly timeoutMs: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly now: () => Date;
  private readonly refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'Google', auth: Authorization, credentialFields: CredentialFields });

  /** Validate configuration and bind the credential source. */
  constructor(config: Config & { timeout?: number }, private readonly credentials: CredentialSource, options: CalendarClientOptions = {}) {
    if (!credentials) throw new TypeError('credential provider is required');
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    const parsedEndpoint = new URL(this.endpoint);
    if (parsedEndpoint.protocol === 'http:' && !['localhost', '127.0.0.1'].includes(parsedEndpoint.hostname)) throw new TypeError('Google Calendar endpoint must use HTTPS');
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    this.timeoutMs = config.timeout ?? 25_000;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Google Calendar response limit must be positive');
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 1) throw new TypeError('Google Calendar timeout must be positive');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the event listing query. */ listEvents(): ListEventsOperation { return new ListEventsOperation(this); }
  /** Bind the event read query. */ getEvent(): GetEventOperation { return new GetEventOperation(this); }
  /** Bind the event creation mutation. */ createEvent(): CreateEventOperation { return new CreateEventOperation(this); }
  /** Bind the event update mutation. */ updateEvent(): UpdateEventOperation { return new UpdateEventOperation(this); }
  /** Bind the free/busy query. */ queryFreeBusy(): QueryFreeBusyOperation { return new QueryFreeBusyOperation(this); }

  /** Resolve a generated OAuth or delegated access credential without exposing refresh errors. */
  async resolveAccessToken(call: Call): Promise<string | undefined> {
    try {
      const credentials = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
      return credentials.accessToken?.reveal() || undefined;
    } catch { return undefined; }
  }

  /** Send a Calendar request, refreshing once after a rejected access token. */
  async request(call: Call, accessToken: string, method: string, path: string, query?: Record<string, string>, payload?: unknown, ifMatch?: string): Promise<CalendarResponse> {
    const search = new URLSearchParams();
    for (const key of Object.keys(query ?? {}).sort()) search.set(key, query![key]!);
    const encodedQuery = search.toString().replace(/~/g, '%7E');
    const url = `${this.endpoint}${path}${encodedQuery ? `?${encodedQuery}` : ''}`;
    const body = payload === undefined ? undefined : Buffer.from(JSON.stringify(payload), 'utf8');
    for (let attempt = 0; attempt < 2; attempt++) {
      const headers: Record<string, string> = { Authorization: `Bearer ${accessToken}`, Accept: 'application/json' };
      if (body) headers['Content-Type'] = 'application/json';
      if (ifMatch) headers['If-Match'] = ifMatch;
      const request: ProviderRequest = {
        url, method, headers, ...(body ? { body } : {}),
        maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: this.endpoint === defaultEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      };
      let response: CalendarResponse;
      try { response = { ...await this.send(request), isTooLarge: false }; }
      catch (error) {
        if (!(error instanceof BodyTooLargeError) || error.status === undefined || error.headers === undefined) throw error;
        response = { status: error.status, headers: error.headers, body: new Uint8Array(), isTooLarge: true };
      }
      if (response.status !== 401 || attempt > 0 || !('resolveAfterRejection' in this.credentials)) return response;
      try {
        const replacement = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal);
        accessToken = replacement.accessToken?.reveal() ?? '';
        if (!accessToken) return response;
      } catch { return response; }
    }
    throw new Error('authenticated request retry was exhausted');
  }

  /** Record a secret-free provider receipt. */
  receipt(call: Call, response: CalendarResponse | undefined, objectId = ''): Receipt {
    const requestId = response && (header(response, 'x-goog-request-id') || header(response, 'x-request-id'));
    return {
      callId: call.id, provider: 'google-calendar', observedAt: this.now().toISOString(),
      ...(call.idempotencyKey ? { idempotencyKey: call.idempotencyKey } : {}),
      ...(objectId ? { providerObjectId: objectId } : {}),
      ...(requestId ? { providerRequestId: requestId } : {}),
    };
  }
}

/** Read a response header case-insensitively. */
export function header(response: CalendarResponse, name: string): string {
  const entry = Object.entries(response.headers).find(([key]) => key.toLowerCase() === name.toLowerCase());
  const value = entry?.[1];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}

/** Escape a path segment with Go url.PathEscape's safe character set. */
export function pathEscape(value: string): string {
  return Array.from(Buffer.from(value, 'utf8')).map(byte => {
    const character = String.fromCharCode(byte);
    return /[A-Za-z0-9._~\-$&+=:@]/.test(character) ? character : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`;
  }).join('');
}
