// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isHeaderSafeCredential, MaxErrorBodyBytes, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type ReceiptDraft } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { GenerateContentOperation } from './operations.js';

const defaultEndpoint = 'https://generativelanguage.googleapis.com/v1beta';
const modelPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

/** HTTP sender for deterministic provider tests. */
export type GeminiRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Injectable HTTP transport and clock. */
export interface GeminiClientOptions {
  /** Provider sender; defaults to the SDK's bounded no-redirect transport. */
  send?: GeminiRequestSender;
  /** Clock used for receipts and Retry-After dates. */
  now?: () => Date;
}

/** Terminal failure to resolve a usable Gemini credential. */
export class GeminiCredentialError extends Error {}

/** Validated Gemini API client that resolves credentials for every call. */
export class GeminiClient {
  readonly endpoint: string;
  readonly model: string;
  readonly maxResponseBytes: number;
  readonly credentials: CredentialProvider<Credentials>;
  readonly send: GeminiRequestSender;
  readonly now: () => Date;

  /** Capture connection settings without contacting Gemini. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: GeminiClientOptions = {}) {
    if (!credentials) throw new TypeError('Gemini credential provider is required');
    const model = (config.model || 'gemini-3.5-flash-lite').trim().replace(/^models\//, '');
    if (!modelPattern.test(model)) throw new TypeError('Gemini model must be a model ID');
    this.model = model;
    this.endpoint = validateBaseURL(config.endpoint || defaultEndpoint);
    this.maxResponseBytes = config.maxResponseBytes || 8_388_608;
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 1) throw new TypeError('Gemini maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the generateContent Query. */
  generateContent(): GenerateContentOperation { return new GenerateContentOperation(this); }

  /** Resolve the secret and send one bounded request. */
  async exchange(call: Call, model: string, body: Uint8Array): Promise<ProviderResponse> {
    let credentials: Credentials;
    try { credentials = await this.credentials.resolve(call, call.context.cancellationSignal); }
    catch { throw new GeminiCredentialError('Gemini credentials are unavailable'); }
    if (!(credentials.apiKey instanceof SecretString) || !isHeaderSafeCredential(credentials.apiKey.reveal())) throw new GeminiCredentialError('Gemini credentials are invalid');
    const request: ProviderRequest = {
      url: `${this.endpoint}/models/${encodeURIComponent(model)}:generateContent`, method: 'POST',
      headers: { 'x-goog-api-key': credentials.apiKey.reveal(), 'Content-Type': 'application/json', Accept: 'application/json' },
      body, maxResponseBytes: Math.max(this.maxResponseBytes, MaxErrorBodyBytes), timeoutMs: 270_000,
      addressPolicy: this.endpoint.startsWith('http:') ? 'privateNetworkHTTP' : 'publicHTTPS',
      signal: call.context.cancellationSignal,
    };
    return this.send(request);
  }

  /** Secret-safe correlation receipt for a provider response. */
  receipt(call: Call, response: ProviderResponse, objectId = ''): ReceiptDraft {
    const requestId = header(response, 'x-goog-request-id') || header(response, 'x-request-id');
    return { callId: call.id, provider: 'gemini', observedAt: this.now().toISOString(),
      ...(requestId ? { providerRequestId: requestId } : {}), ...(objectId ? { providerObjectId: objectId } : {}) };
  }
}

/** Read a case-insensitive provider header. */
export function header(response: ProviderResponse, name: string): string {
  const value = response.headers[name] ?? response.headers[name.toLowerCase()];
  return Array.isArray(value) ? value[0] ?? '' : value ?? '';
}

/** Provider delay with Google RetryInfo taking precedence over Retry-After. */
export function retryDelay(response: ProviderResponse, now: Date): number {
  try {
    const parsed: unknown = JSON.parse(Buffer.from(response.body).toString('utf8'));
    const error = Array.isArray(parsed) ? parsed[0] : parsed;
    if (error && typeof error === 'object' && 'error' in error) {
      const details = (error as { error?: { details?: Array<{ '@type'?: string; retryDelay?: string }> } }).error?.details;
      for (const detail of details ?? []) {
        if (detail['@type'] === 'type.googleapis.com/google.rpc.RetryInfo' && typeof detail.retryDelay === 'string' && /^[0-9]{1,10}(\.[0-9]{1,9})?s$/.test(detail.retryDelay)) {
          const milliseconds = Number.parseFloat(detail.retryDelay) * 1000;
          if (milliseconds > 0) return Math.min(milliseconds, 3_600_000);
        }
      }
    }
  } catch { /* Malformed error details leave Retry-After as the fallback. */ }
  return parseRetryAfter(header(response, 'retry-after'), now);
}
