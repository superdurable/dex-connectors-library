// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider } from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, ProviderTransportError, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials } from './generated/models.js';
import { SearchOperation, SaveObjectOperation } from './operations.js';

const publicHostTemplate = 'https://algolia.net/1';

/** One provider result from a bounded Algolia request. */
export type AlgoliaResponse =
  | { kind: 'response'; status: number; body: Uint8Array }
  | { kind: 'retry'; retryAfterMs: number; failureKind: 'TRANSPORT' | 'AVAILABILITY'; message: string }
  | { kind: 'failure'; failureKind: 'AUTHENTICATION' | 'LOCAL_DEFECT' | 'RESPONSE_TOO_LARGE'; message: string };

/** Validate Algolia settings and resolve credentials for each call. */
export class Client {
  /** Validated Algolia API base URL. */
  readonly baseUrl: string;
  /** Per-request timeout in milliseconds. */
  readonly timeoutMs: number;
  /** Largest accepted successful response in bytes. */
  readonly maxResponseBytes: number;
  /** Bound search operation. */
  readonly search: () => SearchOperation;
  /** Bound replacement operation. */
  readonly saveObject: () => SaveObjectOperation;

  /** Construct without contacting Algolia; the optional sender supports deterministic provider tests. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, private readonly sender: (request: ProviderRequest) => Promise<ProviderResponse> = sendProviderRequest) {
    if (!credentials) throw new TypeError('Algolia credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || publicHostTemplate);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Algolia timeout must be positive milliseconds');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Algolia response limit must be positive bytes');
    const searchOperation = new SearchOperation(this);
    const saveObjectOperation = new SaveObjectOperation(this);
    this.search = () => searchOperation;
    this.saveObject = () => saveObjectOperation;
  }

  /** Send one request, classifying network and response safety failures. */
  async send(call: Call, method: 'POST' | 'PUT', path: string, body: Uint8Array): Promise<AlgoliaResponse> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { return { kind: 'failure', failureKind: 'AUTHENTICATION', message: 'Algolia credentials are unavailable or invalid' }; }
    if (!/^[A-Z0-9]+$/.test(credentials.applicationId) || !(credentials.apiKey instanceof SecretString) || !isHeaderSafeCredential(credentials.apiKey.reveal())) {
      return { kind: 'failure', failureKind: 'AUTHENTICATION', message: 'Algolia credentials are unavailable or invalid' };
    }
    const baseUrl = this.baseUrl === publicHostTemplate ? `https://${credentials.applicationId}.algolia.net/1` : this.baseUrl;
    try {
      const response = await this.sender({
        url: `${baseUrl}${path}`, method, body,
        headers: { 'x-algolia-api-key': credentials.apiKey.reveal(), 'x-algolia-application-id': credentials.applicationId, Accept: 'application/json', 'Content-Type': 'application/json' },
        maxResponseBytes: this.maxResponseBytes, timeoutMs: this.timeoutMs,
        addressPolicy: baseUrl.startsWith('http://') ? 'privateNetworkHTTP' : 'publicHTTPS', signal: call.context.cancellationSignal,
      });
      if (response.status === 408 || response.status === 409 || response.status === 429 || response.status >= 500) {
        return { kind: 'retry', failureKind: 'AVAILABILITY', message: 'Algolia is temporarily unavailable', retryAfterMs: parseRetryAfter(String(response.headers['retry-after'] ?? '')) };
      }
      return { kind: 'response', status: response.status, body: response.body };
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        const status = error.status ?? 0;
        if (status === 408 || status === 409 || status === 429 || status >= 500) {
          return { kind: 'retry', failureKind: 'AVAILABILITY', message: 'Algolia is temporarily unavailable', retryAfterMs: parseRetryAfter(String(error.headers?.['retry-after'] ?? '')) };
        }
        if (status !== 200 && status !== 0) return { kind: 'response', status, body: new Uint8Array() };
        return { kind: 'failure', failureKind: 'RESPONSE_TOO_LARGE', message: 'Algolia response exceeded the limit' };
      }
      if (error instanceof AddressRefusal) return { kind: 'failure', failureKind: 'LOCAL_DEFECT', message: 'Algolia request could not be built' };
      if (error instanceof ProviderTransportError) return { kind: 'retry', failureKind: 'TRANSPORT', message: 'Algolia response is unavailable', retryAfterMs: 0 };
      return { kind: 'retry', failureKind: 'TRANSPORT', message: 'Algolia response is unavailable', retryAfterMs: 0 };
    }
  }
}
