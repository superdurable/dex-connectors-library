// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { GetOrderOperation, ListTransactionsOperation } from './operations.js';

/** Injectable bounded HTTP sender for provider tests. */
export type PayPalRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional network and clock dependencies. */
export interface PayPalClientOptions {
  /** Provider request sender; production uses the SDK transport. */
  send?: PayPalRequestSender;
  /** Clock used for token expiry and Retry-After. */
  now?: () => Date;
}
/** Classified exchange outcome. */
export interface PayPalFailure {
  /** Retry or selected terminal branch. */
  branch: 'retry' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect';
  /** Safe failure without provider text. */
  failure: Failure;
  /** Requested retry delay in milliseconds. */
  retryAfterMs: number;
}
/** Provider response or classified failure. */
export type PayPalExchange = { response: ProviderResponse } | { outcome: PayPalFailure };

/** Validated PayPal client with an expiring credential-scoped access token. */
export class PayPalClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: PayPalRequestSender;
  private readonly now: () => Date;
  private token: { credentialHash: string; value: string; expiresAt: number } | undefined;

  /** Construct without contacting PayPal. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: PayPalClientOptions = {}) {
    if (!credentials) throw new TypeError('PayPal credential source is required');
    this.baseUrl = validateBaseURL(config.baseUrl || 'https://api-m.paypal.com');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) {
      throw new TypeError('timeout and maxResponseBytes must be positive');
    }
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the getOrder query. */ getOrder(): GetOrderOperation { return new GetOrderOperation(this); }
  /** Bind the listTransactions query. */ listTransactions(): ListTransactionsOperation { return new ListTransactionsOperation(this); }

  /** Exchange a token, read one resource, and classify the response. */
  async read(call: Call, operation: string, path: string): Promise<PayPalExchange> {
    let credentials: Credentials;
    try {
      credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!credentials.clientId || !isHeaderSafeCredential(credentials.clientId) || !(credentials.clientSecret instanceof SecretString) ||
          !isHeaderSafeCredential(credentials.clientSecret.reveal())) throw new TypeError('invalid credentials');
    } catch { return { outcome: this.failure(operation, 'defect', 'AUTHENTICATION', 'PayPal credentials are unavailable') }; }
    const credentialHash = createHash('sha256').update(credentials.clientId + '\0' + credentials.clientSecret.reveal()).digest('hex');
    let accessToken = this.token?.credentialHash === credentialHash && this.now().getTime() < this.token.expiresAt ? this.token.value : undefined;
    if (!accessToken) {
      let tokenResponse: ProviderResponse;
      let isTokenBodyTooLarge = false;
      try {
        tokenResponse = await this.request('/v1/oauth2/token', 'POST', {
          Authorization: `Basic ${Buffer.from(`${credentials.clientId}:${credentials.clientSecret.reveal()}`).toString('base64')}`,
          'Content-Type': 'application/x-www-form-urlencoded',
        }, 65_536, Buffer.from('grant_type=client_credentials'), call);
      } catch (error) {
        if (error instanceof BodyTooLargeError && error.status !== undefined) {
          tokenResponse = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
          isTokenBodyTooLarge = true;
        } else {
          return { outcome: error instanceof BodyTooLargeError
            ? this.failure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'PayPal token response exceeded the limit')
            : this.failure(operation, 'retry', 'AVAILABILITY', 'PayPal token exchange failed') };
        }
      }
      if (tokenResponse.status === 429 || tokenResponse.status === 408 || tokenResponse.status >= 500) {
        return { outcome: this.failure(operation, 'retry', 'AVAILABILITY', 'PayPal token exchange unavailable', this.retryAfter(tokenResponse)) };
      }
      if (tokenResponse.status !== 200) return { outcome: this.failure(operation, 'providerRejected', 'AUTHENTICATION', 'PayPal rejected client credentials') };
      if (isTokenBodyTooLarge) return { outcome: this.failure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'PayPal token response exceeded the limit') };
      let token: unknown;
      try { token = JSON.parse(Buffer.from(tokenResponse.body).toString('utf8')); } catch { /* Invalid JSON is classified below. */ }
      if (!isToken(token)) return { outcome: this.failure(operation, 'invalidResponse', 'PROTOCOL', 'PayPal returned an invalid token response') };
      accessToken = token.access_token;
      if (typeof token.expires_in === 'number' && token.expires_in > 30) {
        this.token = { credentialHash, value: accessToken, expiresAt: this.now().getTime() + Math.min(token.expires_in - 30, 86_400) * 1000 };
      }
    }
    let response: ProviderResponse;
    let isReadBodyTooLarge = false;
    try { response = await this.request(path, 'GET', { Accept: 'application/json', Authorization: `Bearer ${accessToken}` }, this.maxResponseBytes, undefined, call); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        response = { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() };
        isReadBodyTooLarge = true;
      } else {
        return { outcome: error instanceof BodyTooLargeError
          ? this.failure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit')
          : this.failure(operation, 'retry', 'AVAILABILITY', 'provider read failed') };
      }
    }
    if (response.status === 429) return { outcome: this.failure(operation, 'retry', 'RATE_LIMIT', 'provider rate limited the read', this.retryAfter(response)) };
    if (response.status === 408 || response.status >= 500) return { outcome: this.failure(operation, 'retry', 'AVAILABILITY', 'provider read is temporarily unavailable', this.retryAfter(response)) };
    if (response.status === 404) return { outcome: this.failure(operation, 'notFound', 'PROVIDER_REJECTION', 'resource was not found') };
    if (response.status === 401) { this.token = undefined; return { outcome: this.failure(operation, 'providerRejected', 'AUTHENTICATION', 'provider rejected credentials') }; }
    if (response.status === 403) return { outcome: this.failure(operation, 'providerRejected', 'AUTHORIZATION', 'provider denied access') };
    if (response.status !== 200) return { outcome: this.failure(operation, 'providerRejected', 'PROVIDER_REJECTION', `provider rejected read with HTTP ${response.status}`) };
    if (isReadBodyTooLarge) return { outcome: this.failure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit') };
    return { response };
  }

  /** Construct a safe operation failure. */
  failure(operation: string, branch: PayPalFailure['branch'], kind: Failure['kind'], message: string, retryAfterMs = 0): PayPalFailure {
    return { branch, failure: { kind, provider: 'paypal', operation, message }, retryAfterMs };
  }

  private retryAfter(response: ProviderResponse): number {
    const value = response.headers['retry-after'];
    return parseRetryAfter(Array.isArray(value) ? value[0] ?? '' : value ?? '', this.now());
  }

  private request(path: string, method: string, headers: Record<string, string>, maxResponseBytes: number, body: Uint8Array | undefined, call: Call): Promise<ProviderResponse> {
    return this.send({ url: this.baseUrl + path, method, headers, ...(body ? { body } : {}), maxResponseBytes,
      addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP',
      timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal });
  }
}

function isToken(value: unknown): value is { access_token: string; token_type: string; expires_in?: number } {
  return value !== null && typeof value === 'object' &&
    'access_token' in value && typeof value.access_token === 'string' && isHeaderSafeCredential(value.access_token) &&
    'token_type' in value && typeof value.token_type === 'string' && value.token_type.toLowerCase() === 'bearer';
}
