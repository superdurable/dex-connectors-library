// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials } from './generated/models.js';
import { GetOrderOperation, ListOrdersOperation } from './operations.js';

/** Injectable HTTP sender for provider replay. */
export type ShopifyRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Transport dependency for deterministic tests. */
export interface ShopifyClientOptions {
  /** Override the default bounded HTTP sender. */
  send?: ShopifyRequestSender;
}

/** One classified provider exchange. */
export interface ShopifyExchangeFailure {
  /** Retry or a manifest branch. */
  branch: 'retry' | 'notFound' | 'providerRejected' | 'invalidResponse' | 'defect';
  /** Secret-safe failure. */
  failure: Failure;
  /** Provider delay in milliseconds. */
  retryAfterMs: number;
}

/** Bounded successful body or classified failure. */
export type ShopifyExchangeResult = { body: Uint8Array } | { outcome: ShopifyExchangeFailure };

/** Validated Shopify order client with call-time credential resolution. */
export class ShopifyClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: ShopifyRequestSender;

  /** Validate one shop connection without contacting Shopify. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: ShopifyClientOptions = {}) {
    if (!credentials) throw new TypeError('Shopify credential source is required');
    if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(config.shopSubdomain)) throw new TypeError('shopSubdomain must contain lowercase letters, digits, or internal hyphens');
    this.baseURL = `https://${config.shopSubdomain}.myshopify.com/admin/api/2026-10`;
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('timeout must be positive');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind one order lookup. */
  getOrder(): GetOrderOperation { return new GetOrderOperation(this); }
  /** Bind one order page lookup. */
  listOrders(): ListOrdersOperation { return new ListOrdersOperation(this); }

  /** Send one order read and apply Go's status classification. */
  async read(call: Call, operation: string, path: string, query?: URLSearchParams): Promise<ShopifyExchangeResult> {
    let token: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.accessToken instanceof SecretString)) throw new TypeError('invalid token');
      token = credentials.accessToken.reveal();
      if (!token || !isHeaderSafeCredential(token)) throw new TypeError('invalid token');
    } catch {
      return { outcome: this.failure('defect', operation, 'AUTHENTICATION', 'Shopify credentials are unavailable') };
    }
    let response: ProviderResponse;
    try {
      response = await this.send({
        url: this.baseURL + path + (query?.size ? `?${query.toString()}` : ''), method: 'GET',
        headers: { Accept: 'application/json', 'X-Shopify-Access-Token': token },
        maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS',
        timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
      });
    } catch (error) {
      if (error instanceof BodyTooLargeError) {
        const statusFailure = error.status === undefined ? null : this.classifyStatus(operation, error.status, error.headers ?? {});
        return { outcome: statusFailure ?? this.failure('invalidResponse', operation, 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit') };
      }
      return { outcome: this.failure('retry', operation, 'AVAILABILITY', 'provider read failed') };
    }
    const statusFailure = this.classifyStatus(operation, response.status, response.headers);
    return statusFailure ? { outcome: statusFailure } : { body: response.body };
  }

  private classifyStatus(operation: string, status: number, headers: ProviderResponse['headers']): ShopifyExchangeFailure | null {
    const retryAfter = headers['retry-after'];
    const delay = parseRetryAfter(Array.isArray(retryAfter) ? retryAfter[0] ?? '' : retryAfter ?? '');
    if (status === 429) return this.failure('retry', operation, 'RATE_LIMIT', 'provider rate limited the read', delay);
    if (status === 408 || status >= 500) return this.failure('retry', operation, 'AVAILABILITY', 'provider read is temporarily unavailable', delay);
    if (status === 404) return this.failure('notFound', operation, 'PROVIDER_REJECTION', 'resource was not found');
    if (status === 401) return this.failure('providerRejected', operation, 'AUTHENTICATION', 'provider rejected credentials');
    if (status === 403) return this.failure('providerRejected', operation, 'AUTHORIZATION', 'provider denied access');
    if (status !== 200) return this.failure('providerRejected', operation, 'PROVIDER_REJECTION', `provider rejected read with HTTP ${status}`);
    return null;
  }

  private failure(branch: ShopifyExchangeFailure['branch'], operation: string, kind: Failure['kind'], message: string, retryAfterMs = 0): ShopifyExchangeFailure {
    return { branch, failure: { kind, provider: 'shopify', operation, message }, retryAfterMs };
  }
}
