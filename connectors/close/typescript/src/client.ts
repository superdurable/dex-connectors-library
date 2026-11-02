// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, type Call, type CredentialProvider, type Failure, type QueryAttempt } from '@superdurable/connectors-sdk';
import { newQueryBranch, newQueryRetry } from '@superdurable/connectors-sdk';
import { AddressRefusal, BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, ProviderTransportError, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials, Lead, LeadPage } from './generated/models.js';
import { GetLeadOperation, ListLeadsOperation } from './operations.js';

const defaultBaseURL = 'https://api.close.com/api/v1';
const defaultTimeoutMs = 30_000;
const defaultMaxResponseBytes = 4_194_304;

/** HTTP sender used by Close requests, injectable for provider tests. */
export type CloseRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;

/** Optional transport dependency for deterministic tests. */
export interface CloseClientOptions {
  /** Sender; omitted uses the SDK's bounded, pinned DNS transport. */
  send?: CloseRequestSender;
}

/** Immutable Close settings and per-call credential resolution. */
export class CloseClient {
  private readonly baseURL: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: CloseRequestSender;

  /** Validate configuration without contacting Close. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: CloseClientOptions = {}) {
    if (!credentials) throw new TypeError('Close credential source is required');
    this.baseURL = validateBaseURL(config.baseUrl || defaultBaseURL);
    this.timeoutMs = config.timeoutMs || defaultTimeoutMs;
    this.maxResponseBytes = config.maxResponseBytes || defaultMaxResponseBytes;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Close timeout must be positive');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Close maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the listLeads Query. */ listLeads(): ListLeadsOperation { return new ListLeadsOperation(this); }
  /** Bind the getLead Query. */ getLead(): GetLeadOperation { return new GetLeadOperation(this); }

  /** Send one read and classify its provider outcome. */
  async get<Output>(call: Call, operation: string, path: string, successBranch: string, notFoundBranch: string, decode: (body: Uint8Array) => Output, zero: Output): Promise<QueryAttempt<Output>> {
    let apiKey: string;
    try {
      const resolved = await this.credentials.resolve(call, call.context.cancellationSignal);
      if (!(resolved.apiKey instanceof SecretString)) throw new TypeError('Close API key is invalid');
      apiKey = resolved.apiKey.reveal();
      if (!apiKey || !isHeaderSafeCredential(apiKey)) throw new TypeError('Close API key is invalid');
    } catch { return this.failed(operation, 'defect', 'AUTHENTICATION', 'connection credentials are unavailable', zero); }
    const request: ProviderRequest = {
      url: this.baseURL + path, method: 'GET',
      headers: { Accept: 'application/json', Authorization: `Basic ${Buffer.from(`${apiKey}:`).toString('base64')}` },
      maxResponseBytes: Math.max(this.maxResponseBytes, 65_536),
      timeoutMs: this.timeoutMs, addressPolicy: this.baseURL === defaultBaseURL ? 'publicHTTPS' : 'privateNetworkHTTP',
      signal: call.context.cancellationSignal,
    };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError) {
        if (error.status !== undefined && error.status !== 200) return this.classify(operation, error.status, error.headers?.['retry-after'], notFoundBranch, zero);
        return this.failed(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded configured limit', zero);
      }
      if (error instanceof AddressRefusal) return this.failed(operation, 'defect', 'LOCAL_DEFECT', 'request cannot be built', zero);
      if (error instanceof ProviderTransportError) return this.retry(operation, 'AVAILABILITY', 'provider request failed');
      return this.retry(operation, 'AVAILABILITY', 'provider request failed');
    }
    if (response.status !== 200) return this.classify(operation, response.status, response.headers['retry-after'], notFoundBranch, zero);
    if (response.body.length > this.maxResponseBytes) return this.failed(operation, 'invalidResponse', 'RESPONSE_TOO_LARGE', 'provider response exceeded configured limit', zero);
    try { return newQueryBranch(successBranch, decode(response.body), {}); }
    catch { return this.failed(operation, 'invalidResponse', 'PROTOCOL', 'provider returned unusable JSON', zero); }
  }

  private classify<Output>(operation: string, status: number, header: string | string[] | undefined, notFoundBranch: string, zero: Output): QueryAttempt<Output> {
    const retryAfterMs = parseRetryAfter(Array.isArray(header) ? header[0] ?? '' : header ?? '');
    if (status === 429) return this.retry(operation, 'RATE_LIMIT', 'provider rate limit', retryAfterMs);
    if (status === 408 || status === 409 || status >= 500) return this.retry(operation, 'AVAILABILITY', 'provider temporarily unavailable', retryAfterMs);
    if (status === 404 && notFoundBranch !== 'providerRejected') return this.failed(operation, notFoundBranch, 'NOT_FOUND', 'provider resource was not found', zero);
    if (status === 401) return this.failed(operation, 'providerRejected', 'AUTHENTICATION', 'provider rejected the API key', zero);
    if (status === 403) return this.failed(operation, 'providerRejected', 'AUTHORIZATION', 'provider denied the request', zero);
    return this.failed(operation, 'providerRejected', 'PROVIDER_REJECTION', `provider rejected HTTP ${status}`, zero);
  }

  private failed<Output>(operation: string, branch: string, kind: Failure['kind'], message: string, zero: Output): QueryAttempt<Output> {
    return newQueryBranch(branch, zero, {}, { kind, provider: 'close', operation, message });
  }

  private retry<Output>(operation: string, kind: Failure['kind'], message: string, after = 0): QueryAttempt<Output> {
    return newQueryRetry({ kind, provider: 'close', operation, message }, after);
  }
}

/** Empty result used for failed list branches. */
export const emptyLeadPage: LeadPage = { leads: [], hasMore: false };
/** Empty result used for failed get branches. */
export const emptyLead: Lead = { id: '', name: '' };
