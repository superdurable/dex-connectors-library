// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Call, CredentialRefreshDriver, FailureKind } from '@superdurable/connectors-sdk';
import { resolveCredential, resolveCredentialAfterRejection } from '@superdurable/connectors-sdk';
import {
  BodyTooLargeError, isHeaderSafeCredential, sendProviderRequest, validateBaseURL,
  type AddressPolicy, type ProviderRequest, type ProviderResponse,
} from '@superdurable/connectors-sdk/providerhttp';
import type { Config, CredentialSource, Credentials } from './models.js';
import { GoogleOAuthAuthMethodID, WorkspaceDomainDelegationAuthMethodID } from './models.js';
import { parseMailbox } from './message.js';

/** Testable provider transport; production uses the SDK's pinned, bounded HTTP sender. */
export type GmailTransport = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Public Gmail client settings after applying manifest defaults. */
export interface GmailClientSettings {
  /** Validated API base URL. */ readonly endpoint: string;
  /** Maximum complete response bytes. */ readonly maxResponseBytes: number;
  /** Maximum encoded MIME message bytes. */ readonly maxMessageBytes: number;
  /** Address policy for the configured endpoint. */ readonly addressPolicy: AddressPolicy;
}

/** Apply and validate the Gmail manifest's transport defaults. */
export function gmailClientSettings(config: Config = {}): GmailClientSettings {
  const endpoint = validateBaseURL(config.endpoint ?? 'https://gmail.googleapis.com/gmail/v1');
  const maxResponseBytes = config.maxResponseBytes ?? 1_048_576;
  const maxMessageBytes = config.maxMessageBytes ?? 10_485_760;
  if (!Number.isSafeInteger(maxResponseBytes) || maxResponseBytes < 1 || !Number.isSafeInteger(maxMessageBytes) || maxMessageBytes < 1) {
    throw new TypeError('Gmail response and message limits must be positive integers');
  }
  if (config.pollIntervalMs !== undefined && (!Number.isSafeInteger(config.pollIntervalMs) || config.pollIntervalMs < 1000)) throw new TypeError('Gmail poll interval is invalid');
  if (config.pollPageSize !== undefined && (!Number.isSafeInteger(config.pollPageSize) || config.pollPageSize < 1 || config.pollPageSize > 100)) throw new TypeError('Gmail poll page size is invalid');
  return Object.freeze({ endpoint, maxResponseBytes, maxMessageBytes, addressPolicy: endpoint.startsWith('http://') ? 'privateNetworkHTTP' : 'publicHTTPS' });
}

/** Failure after credential lookup or credential validation. */
export class GmailCredentialError extends Error {
  constructor() { super('connection credentials are unavailable'); this.name = 'GmailCredentialError'; }
}

/** Credential-safe HTTP client shared by all three operations. */
export class GmailHTTPClient {
  readonly #settings: GmailClientSettings;
  readonly #credentials: CredentialSource;
  readonly #refreshDriver: CredentialRefreshDriver<Credentials>;
  readonly #transport: GmailTransport;

  /** Inject settings, host-managed credential store, refresh driver, and optional test transport. */
  constructor(config: Config, credentials: CredentialSource, refreshDriver: CredentialRefreshDriver<Credentials>, transport: GmailTransport = sendProviderRequest) {
    if (!credentials || !refreshDriver || !transport) throw new TypeError('Gmail credential provider, refresh driver, and transport are required');
    this.#settings = gmailClientSettings(config);
    this.#credentials = credentials;
    this.#refreshDriver = refreshDriver;
    this.#transport = transport;
  }

  /** Return validated request and message bounds. */
  settings(): GmailClientSettings { return this.#settings; }

  /** Resolve a private token at call time; never put it in a receipt or Flow value. */
  async resolveCredentials(call: Call, signal?: AbortSignal): Promise<Credentials> {
    try {
      const credentials = await resolveCredential(this.#credentials, call, this.#refreshDriver, signal);
      validateCredentials(credentials);
      return credentials;
    } catch { throw new GmailCredentialError(); }
  }

  /** Send one authenticated request, refreshing at most once after a 401. */
  async request(call: Call, credentials: Credentials, method: string, pathAndQuery: string, body?: Uint8Array, signal?: AbortSignal): Promise<{ response: ProviderResponse; credentials: Credentials }> {
    let currentCredentials = credentials;
    for (let attempt = 0; attempt < 2; attempt++) {
      const token = currentCredentials.accessToken.reveal();
      if (!isHeaderSafeCredential(token)) throw new GmailCredentialError();
      const request: ProviderRequest = {
        url: `${this.#settings.endpoint}${pathAndQuery}`, method,
        headers: { Authorization: `Bearer ${token}`, ...(body ? { 'Content-Type': 'application/json' } : {}) },
        maxResponseBytes: this.#settings.maxResponseBytes, addressPolicy: this.#settings.addressPolicy,
        timeoutMs: 25_000, ...(body ? { body } : {}), ...(signal ? { signal } : {}),
      };
      let response: ProviderResponse;
      try { response = await this.#transport(request); }
      catch (error) {
        if (!(error instanceof BodyTooLargeError) || error.status !== 401) throw error;
        response = { status: 401, headers: error.headers ?? {}, body: new Uint8Array() };
      }
      if (response.status !== 401 || attempt !== 0 || !('resolveAfterRejection' in this.#credentials)) return { response, credentials: currentCredentials };
      try {
        const refreshed = await resolveCredentialAfterRejection(this.#credentials, call, this.#refreshDriver, signal);
        validateCredentials(refreshed);
        currentCredentials = refreshed;
      } catch { return { response, credentials: currentCredentials }; }
    }
    throw new GmailCredentialError();
  }
}

/** Map conclusive Gmail HTTP refusals to the Go connector's FailureKind. */
export function statusFailureKind(status: number): FailureKind {
  switch (status) {
    case 401: return 'AUTHENTICATION';
    case 403: return 'AUTHORIZATION';
    case 404: return 'NOT_FOUND';
    case 409: return 'CONFLICT';
    default: return 'PROVIDER_REJECTION';
  }
}

/** Parse Gmail's integer Retry-After seconds, rejecting malformed values. */
export function gmailRetryAfterMs(headers: Record<string, string | string[] | undefined>): number {
  const value = headers['retry-after'] ?? headers['Retry-After'];
  if (value === undefined) return 0;
  const text = Array.isArray(value) ? value[0] : value;
  if (!text || !/^\d+$/.test(text)) throw new TypeError('Gmail returned an invalid Retry-After header');
  const seconds = Number(text);
  if (!Number.isSafeInteger(seconds)) throw new TypeError('Gmail returned an invalid Retry-After header');
  return seconds * 1000;
}

/** Read the safe Gmail request correlation ID. */
export function gmailRequestID(headers: Record<string, string | string[] | undefined>): string {
  const value = headers['x-goog-request-id'] ?? headers['X-Goog-Request-Id'] ?? headers['x-request-id'] ?? headers['X-Request-Id'];
  return (Array.isArray(value) ? value[0] : value) ?? '';
}

function validateCredentials(credentials: Credentials): void {
  const token = credentials.accessToken.reveal();
  if (!token || !isHeaderSafeCredential(token)) throw new GmailCredentialError();
  const mailbox = parseMailbox(credentials.primaryEmail);
  if (!mailbox || mailbox.address.toLowerCase() !== credentials.primaryEmail.trim().toLowerCase()) throw new GmailCredentialError();
  if (credentials.authMethodId && ![GoogleOAuthAuthMethodID, WorkspaceDomainDelegationAuthMethodID].includes(credentials.authMethodId)) throw new GmailCredentialError();
}
