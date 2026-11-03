// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { SecretString, type Call, type CredentialProvider, type Failure } from '@superdurable/connectors-sdk';
import type { Config, Credentials } from './generated/models.js';
import { GetOrderOperation, ListOrdersOperation } from './operations.js';

/** Injectable HTTP sender for provider tests. */
export type WooCommerceRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Optional transport and clock dependencies. */
export interface WooCommerceClientOptions {
  /** Omit for the Connector SDK's redirect-safe transport. */ send?: WooCommerceRequestSender;
  /** Clock for Retry-After dates. */ now?: () => Date;
}
/** Classified result of a provider read. */
export type WooCommerceExchange =
  | { response: ProviderResponse }
  | { branch: 'defect' | 'notFound' | 'providerRejected' | 'invalidResponse'; failure: Failure }
  | { retry: true; failure: Failure; retryAfterMs: number };

/** WooCommerce client that resolves credentials for every read. */
export class WooCommerceClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly credentials: CredentialProvider<Credentials>;
  private readonly send: WooCommerceRequestSender;
  private readonly now: () => Date;

  /** Validate connection settings without contacting the store. */
  constructor(config: Config, credentials: CredentialProvider<Credentials>, options: WooCommerceClientOptions = {}) {
    if (!credentials) throw new TypeError('WooCommerce credential source is required');
    const storeURL = validateBaseURL(config.storeUrl);
    if (storeURL.toLowerCase().endsWith('/wp-json/wc/v3')) throw new TypeError('storeUrl must be the WordPress Site Address without /wp-json/wc/v3');
    this.baseURL = `${storeURL}/wp-json/wc/v3`;
    this.timeoutMs = config.timeoutMs ?? 15_000;
    this.maxResponseBytes = config.maxResponseBytes ?? 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('timeout and maxResponseBytes must be positive');
    this.credentials = credentials;
    this.send = options.send ?? sendProviderRequest;
    this.now = options.now ?? (() => new Date());
  }

  /** Bind the listOrders query. */ listOrders(): ListOrdersOperation { return new ListOrdersOperation(this); }
  /** Bind the getOrder query. */ getOrder(): GetOrderOperation { return new GetOrderOperation(this); }

  /** Send one authenticated GET and classify its status before reading the body. */
  async exchange(call: Call, operation: string, path: string, query = ''): Promise<WooCommerceExchange> {
    let consumerKey: string;
    let consumerSecret: string;
    try {
      const credentials = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(credentials.consumerKey instanceof SecretString) || !(credentials.consumerSecret instanceof SecretString)) return this.branchFailure(operation, 'defect', 'AUTHENTICATION', 'WooCommerce credentials are unavailable');
      consumerKey = credentials.consumerKey.reveal();
      consumerSecret = credentials.consumerSecret.reveal();
    } catch { return this.branchFailure(operation, 'defect', 'AUTHENTICATION', 'WooCommerce credentials are unavailable'); }
    if (!isHeaderSafeCredential(consumerKey) || !isHeaderSafeCredential(consumerSecret)) return this.branchFailure(operation, 'defect', 'AUTHENTICATION', 'WooCommerce credentials are unavailable');
    const request: ProviderRequest = {
      url: `${this.baseURL}${path}${query ? `?${query}` : ''}`, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${consumerKey}:${consumerSecret}`, 'utf8').toString('base64')}` },
      timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes,
      addressPolicy: 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    try {
      const response = await this.send(request);
      return this.classifyStatus(operation, response.status, response.headers) ?? { response };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        const classified = this.classifyStatus(operation, error.status, error.headers ?? {});
        if (classified) return classified;
        return this.branchFailure(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit');
      }
      return { retry: true, failure: this.failure(operation, 'AVAILABILITY', 'provider read failed'), retryAfterMs: 0 };
    }
  }

  private classifyStatus(operation: string, status: number, headers: ProviderResponse['headers']): WooCommerceExchange | undefined {
    if (status === 429 || status === 408 || status >= 500) {
      const header = headers['retry-after'];
      return { retry: true, failure: this.failure(operation, status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', status === 429 ? 'provider rate limited the read' : 'provider read is temporarily unavailable'), retryAfterMs: parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '', this.now()) };
    }
    if (status === 404) return this.branchFailure(operation, 'notFound', 'PROVIDER_REJECTION', 'resource was not found');
    if (status === 401) return this.branchFailure(operation, 'providerRejected', 'AUTHENTICATION', 'provider rejected credentials');
    if (status === 403) return this.branchFailure(operation, 'providerRejected', 'AUTHORIZATION', 'provider denied access');
    if (status !== 200) return this.branchFailure(operation, 'providerRejected', 'PROVIDER_REJECTION', `provider rejected read with HTTP ${status}`);
    return undefined;
  }

  private branchFailure(operation: string, branch: 'defect' | 'notFound' | 'providerRejected' | 'invalidResponse', kind: Failure['kind'], message: string): WooCommerceExchange {
    return { branch, failure: this.failure(operation, kind, message) };
  }
  private failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'woocommerce', operation, message }; }
}
