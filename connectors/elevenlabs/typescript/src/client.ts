// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure, type QueryAttempt, newQueryBranch, newQueryRetry } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { CreateSpeechOperation, ListVoicesOperation } from './operations.js';

/** HTTP sender used by ElevenLabs, replaceable by a deterministic provider fixture. */
export type ElevenLabsRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Injectable dependencies for provider tests. */
export interface ElevenLabsClientOptions {
  /** HTTP sender; production uses the SDK's bounded, no-redirect transport. */
  send?: ElevenLabsRequestSender;
  /** Clock used to interpret HTTP Retry-After dates. */
  now?: () => Date;
}

/** Immutable provider settings with credentials resolved for every invocation. */
export class ElevenLabsClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: ElevenLabsRequestSender;
  private readonly now: () => Date;

  /** Validate settings without contacting the provider. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: ElevenLabsClientOptions = {}) {
    if (!credentials) throw new TypeError('ElevenLabs credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api.elevenlabs.io');
    this.timeoutMs = config.timeoutMs || 120_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs < 0) throw new TypeError('ElevenLabs timeout must be nonnegative');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes < 0) throw new TypeError('ElevenLabs response limit must be nonnegative');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the voice-list query. */
  listVoices(): ListVoicesOperation { return new ListVoicesOperation(this); }
  /** Bind the speech-generation query. */
  createSpeech(): CreateSpeechOperation { return new CreateSpeechOperation(this); }

  /** Send one request and classify its status before decoding its body. */
  async invokeQuery<Output>(call: Call, operation: string, success: string, method: string, path: string, query: URLSearchParams, body: Uint8Array | undefined, empty: Output, decode: (body: Uint8Array) => Output): Promise<QueryAttempt<Output>> {
    let key: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(resolved.apiKey instanceof SecretString)) throw new TypeError('API key is invalid');
      key = resolved.apiKey.reveal();
      if (!isHeaderSafeCredential(key)) throw new TypeError('API key is invalid');
    } catch { return failed(operation, 'defect', empty, 'AUTHENTICATION', 'connection credentials are unavailable or invalid'); }
    const encodedQuery = query.toString();
    const request: ProviderRequest = {
      url: `${this.baseUrl}${path}${encodedQuery ? `?${encodedQuery}` : ''}`,
      method, headers: { 'xi-api-key': key, Accept: path.startsWith('/v1/text-to-speech/') ? 'audio/mpeg' : 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) },
      ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes,
      addressPolicy: this.baseUrl.startsWith('http://') ? 'privateNetworkHTTP' : 'publicHTTPS',
      timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    let isTooLarge = false;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        isTooLarge = true;
      } else return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider request failed'));
    }
    const status = response.status;
    const retryAfter = parseRetryAfter(header(response, 'retry-after'), this.now());
    if (status === 429) return newQueryRetry(failure(operation, 'RATE_LIMIT', 'provider rate limited the request'), retryAfter);
    if (status === 408 || status >= 500) return newQueryRetry(failure(operation, 'AVAILABILITY', 'provider is temporarily unavailable'), retryAfter);
    if (status !== 200) return failed(operation, 'providerRejected', empty, 'PROVIDER_REJECTION', `provider rejected the request with HTTP ${status}`);
    if (isTooLarge) return failed(operation, 'invalidResponse', empty, 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit');
    try { return newQueryBranch(success, decode(response.body), {}); }
    catch { return failed(operation, 'invalidResponse', empty, 'PROTOCOL', 'provider returned an invalid response'); }
  }
}

function header(response: ProviderResponse, name: string): string {
  const value = response.headers[name];
  return Array.isArray(value) ? (value[0] ?? '') : (value ?? '');
}

function failure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'elevenlabs', operation, message };
}

function failed<Output>(operation: string, branch: string, empty: Output, kind: Failure['kind'], message: string): QueryAttempt<Output> {
  return newQueryBranch(branch, empty, {}, failure(operation, kind, message));
}
