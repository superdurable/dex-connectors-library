// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { TrackEventOperation, ExportEventsOperation } from './operations.js';

const defaultBaseUrl = 'https://api.mixpanel.com';
const defaultExportBaseUrl = 'https://data.mixpanel.com/api/2.0';
const defaultTimeoutMs = 15_000;
const defaultMaxResponseBytes = 2_097_152;

/** Injectable sender for provider tests; production uses the SDK transport. */
export type MixpanelRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Transport dependency for deterministic provider tests. */
export interface MixpanelClientOptions {
  /** Provider HTTP sender. */ send?: MixpanelRequestSender;
  /** Clock for Retry-After dates. */ now?: () => Date;
}

/** Classified result of one provider exchange. */
export type MixpanelExchange =
  | { status: number; body: Uint8Array }
  | { failure: Failure; disposition: 'defect' | 'retry' | 'invalidResponse'; retryAfterMs: number };

/** Authenticated client for Mixpanel event import and raw export. */
export class MixpanelClient {
  readonly baseUrl: string;
  readonly exportBaseUrl: string;
  readonly projectId: number;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: MixpanelRequestSender;
  private readonly now: () => Date;

  /** Validate configuration and retain immutable provider dependencies. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: MixpanelClientOptions = {}) {
    if (!credentials) throw new TypeError('Mixpanel credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || defaultBaseUrl);
    this.exportBaseUrl = validateBaseURL(config.exportBaseUrl || defaultExportBaseUrl);
    if (!Number.isSafeInteger(config.projectId) || (config.projectId ?? 0) <= 0) throw new TypeError('Mixpanel project ID must be positive');
    this.projectId = config.projectId!;
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) throw new TypeError('Mixpanel timeout and maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the stable event import mutation. */
  trackEvent(): TrackEventOperation { return new TrackEventOperation(this); }
  /** Bind the bounded raw export query. */
  exportEvents(): ExportEventsOperation { return new ExportEventsOperation(this); }

  /** Resolve current credentials and send one bounded provider request. */
  async exchange(call: Call, operation: string, method: 'GET' | 'POST', address: string, payload?: Uint8Array): Promise<MixpanelExchange> {
    let username: string;
    let secret: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      username = credentials.serviceAccountUsername.reveal();
      secret = credentials.serviceAccountSecret.reveal();
      if (!isHeaderSafeCredential(username) || !isHeaderSafeCredential(secret)) throw new TypeError('invalid credentials');
    } catch {
      return this.failed(operation, 'AUTHENTICATION', 'Mixpanel credentials are unavailable or invalid', 'defect');
    }
    const headers: Record<string, string> = {
      Authorization: `Basic ${Buffer.from(`${username}:${secret}`, 'utf8').toString('base64')}`,
      Accept: method === 'GET' ? 'text/plain' : 'application/json',
      ...(method === 'POST' ? { 'Content-Type': 'application/json' } : {}),
    };
    const request: ProviderRequest = {
      url: address, method, headers, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: address.startsWith(defaultBaseUrl) || address.startsWith(defaultExportBaseUrl) ? 'publicHTTPS' : 'privateNetworkHTTP',
      timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
      ...(payload ? { body: payload } : {}),
    };
    try {
      const response = await this.send(request);
      return this.classifyResponse(operation, response.status, response.body, response.headers);
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        return this.classifyResponse(operation, error.status, new Uint8Array(), error.headers ?? {}, true);
      }
      return this.failed(operation, 'TRANSPORT', 'Mixpanel response is unavailable', 'retry');
    }
  }

  private classifyResponse(operation: string, status: number, body: Uint8Array, headers: ProviderResponse['headers'], isTooLarge = false): MixpanelExchange {
    if (status === 408 || status === 409 || status === 429 || status >= 500) {
      const value = headers['retry-after'];
      const retryAfterMs = parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '', this.now());
      return this.failed(operation, 'AVAILABILITY', 'Mixpanel is temporarily unavailable', 'retry', retryAfterMs);
    }
    if (status !== 200) return { status, body: new Uint8Array() };
    if (isTooLarge) return this.failed(operation, 'RESPONSE_TOO_LARGE', 'Mixpanel response exceeded the limit', 'invalidResponse');
    return { status, body };
  }

  private failed(operation: string, kind: Failure['kind'], message: string, disposition: 'defect' | 'retry' | 'invalidResponse', retryAfterMs = 0): MixpanelExchange {
    return { failure: { kind, provider: 'mixpanel', operation, message }, disposition, retryAfterMs };
  }
}
