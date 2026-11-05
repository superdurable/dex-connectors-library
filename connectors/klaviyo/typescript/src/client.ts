// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isHeaderSafeCredential, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { CreateEventOperation, GetProfileOperation } from './operations.js';

const defaultBaseUrl = 'https://a.klaviyo.com/api';

/** Injectable sender for provider replay and local integration tests. */
export type KlaviyoRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional request transport and clock dependencies. */
export interface KlaviyoClientOptions {
  /** Replaces the production HTTP transport for tests. */
  send?: KlaviyoRequestSender;
  /** Supplies the clock for Retry-After parsing. */
  now?: () => Date;
}

/** One authenticated Klaviyo API client. */
export class KlaviyoClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: KlaviyoRequestSender;
  readonly now: () => Date;

  /** Validate configuration and retain a call-time credential source. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: KlaviyoClientOptions = {}) {
    if (!credentials) throw new TypeError('Klaviyo credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || defaultBaseUrl);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) throw new TypeError('Klaviyo limits must be positive integers');
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind a profile lookup. */
  getProfile(): GetProfileOperation { return new GetProfileOperation(this); }
  /** Bind an event submission. */
  createEvent(): CreateEventOperation { return new CreateEventOperation(this); }

  /** Send one authenticated request, preserving transport and body-limit failures for classification. */
  async request(call: Call, method: 'GET' | 'POST', path: string, body?: Uint8Array): Promise<ProviderResponse> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { throw new InvalidKlaviyoCredentialError(); }
    if (!(credentials.privateApiKey instanceof SecretString) || !isHeaderSafeCredential(credentials.privateApiKey.reveal())) throw new InvalidKlaviyoCredentialError();
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}`, method,
      headers: { Authorization: `Klaviyo-API-Key ${credentials.privateApiKey.reveal()}`, Accept: 'application/vnd.api+json', 'Content-Type': 'application/vnd.api+json', revision: '2026-10-15' },
      maxResponseBytes: this.maxResponseBytes,
      timeoutMs: this.timeoutMs,
      addressPolicy: this.baseUrl === defaultBaseUrl ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
      ...(body ? { body } : {}),
    };
    return this.send(request);
  }
}

/** Local credential validation failed before a provider request. */
export class InvalidKlaviyoCredentialError extends Error {
  /** Construct a secret-safe credential error. */
  constructor() { super('Klaviyo credentials are unavailable or invalid'); }
}
