// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import emailAddresses from 'email-addresses';
import type { Config, Credentials } from './generated/models.js';
import { CreateOrUpdateContactOperation, GetContactOperation } from './operations.js';

const publicEndpoint = 'https://api.brevo.com/v3';
const defaultTimeoutMs = 15_000;
const defaultMaxResponseBytes = 2_097_152;

/** Provider sender injected for deterministic tests. */
export type BrevoRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional transport and clock dependencies. */
export interface BrevoClientOptions {
  /** Bounded provider sender; the SDK transport is used by default. */
  send?: BrevoRequestSender;
  /** Clock for Retry-After HTTP dates. */
  now?: () => Date;
}

/** One bounded provider exchange or a classified local or transport error. */
export type BrevoExchange =
  | { response: ProviderResponse }
  | { failure: Failure; classification: 'defect' | 'invalidResponse' | 'retry'; retryAfterMs: number };

/** Brevo HTTP client with immutable settings and per-call credential resolution. */
export class BrevoClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: BrevoRequestSender;
  private readonly now: () => Date;

  /** Validate connection settings before any provider call. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: BrevoClientOptions = {}) {
    if (!credentials) throw new TypeError('Brevo credential provider is required');
    this.baseUrl = validateBaseURL(config.baseUrl || publicEndpoint);
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Brevo timeout must be positive milliseconds');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Brevo maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the repeat-safe contact upsert. */
  createOrUpdateContact(): CreateOrUpdateContactOperation { return new CreateOrUpdateContactOperation(this); }

  /** Bind the contact lookup. */
  getContact(): GetContactOperation { return new GetContactOperation(this); }

  /** Send one authenticated request and classify transport failures. */
  async exchange(call: Call, operation: string, method: string, path: string, body?: Uint8Array): Promise<BrevoExchange> {
    let apiKey: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.apiKey instanceof SecretString)) throw new TypeError('missing key');
      apiKey = credentials.apiKey.reveal();
      if (!isHeaderSafeCredential(apiKey)) throw new TypeError('invalid key');
    } catch {
      return { failure: brevoFailure(operation, 'AUTHENTICATION', 'Brevo connection credentials are unavailable or invalid'), classification: 'defect', retryAfterMs: 0 };
    }
    try {
      const response = await this.send({
        url: this.baseUrl + path, method,
        headers: { 'api-key': apiKey, Accept: 'application/json', 'Content-Type': 'application/json' },
        ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes,
        timeoutMs: this.timeoutMs,
        addressPolicy: this.baseUrl === publicEndpoint ? 'publicHTTPS' : 'privateNetworkHTTP',
        signal: call.context.cancellationSignal,
      });
      return { response };
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.status !== 200 && error.status !== 201) {
          return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() } };
        }
        return { failure: brevoFailure(operation, 'RESPONSE_TOO_LARGE', 'Brevo contact response exceeded the limit'), classification: 'invalidResponse', retryAfterMs: 0 };
      }
      return { failure: brevoFailure(operation, 'TRANSPORT', 'Brevo contact response is unavailable'), classification: 'retry', retryAfterMs: 0 };
    }
  }

  /** Parse a provider-directed retry delay. */
  retryAfter(response: ProviderResponse): number {
    const value = response.headers['retry-after'];
    return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '', this.now());
  }
}

/** Produce a secret-safe operation failure. */
export function brevoFailure(operation: string, kind: Failure['kind'], message: string): Failure {
  return { kind, provider: 'brevo', operation, message };
}

/** Accept only a plain address, never a display name. */
export function isPlainEmail(value: string): boolean {
  const address = emailAddresses.parseOneAddress({ input: value, rfc6532: true });
  return address?.type === 'mailbox' && address.address === value && value.includes('@');
}

/** Statuses safe to retry for both reads and the repeat-safe upsert. */
export function isRetryableStatus(status: number): boolean {
  return status === 408 || status === 409 || status === 429 || status >= 500;
}

/** Map definitive provider rejection to the Go connector's failure kinds. */
export function rejectionFailureKind(status: number): Failure['kind'] {
  if (status === 401) return 'AUTHENTICATION';
  if (status === 403) return 'AUTHORIZATION';
  return 'PROVIDER_REJECTION';
}
