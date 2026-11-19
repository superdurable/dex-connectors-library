// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type CredentialProvider, type Failure, type MutationAttempt, type QueryAttempt } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, isHeaderSafeCredential, parseRetryAfter, sendProviderRequest, validateBaseURL, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import type { Config, Credentials, SelectRowsOutput, UpsertRowsOutput } from './generated/models.js';
import { SelectRowsOperation, UpsertRowsOperation } from './operations.js';

const emptyOutput = { data: null };

/** Injectable provider transport for deterministic tests. */
export type SupabaseRequestSender = (request: ProviderRequest) => Promise<ProviderResponse>;
/** Dependencies used by one provider client. */
export interface SupabaseClientOptions {
  /** Sender; production uses the SDK transport. */ send?: SupabaseRequestSender;
}

type Exchange = { body: Uint8Array } | { branch?: string; failure: Failure; retryAfterMs?: number };

/** Supabase REST client with immutable configuration and credentials resolved per call. */
export class SupabaseClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: SupabaseRequestSender;
  /** Validate settings without contacting Supabase. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: SupabaseClientOptions = {}) {
    if (!credentials) throw new TypeError('Supabase credentials are required');
    this.baseUrl = validateBaseURL(config.baseUrl);
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 2_097_152;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0) throw new TypeError('Supabase timeout must be positive');
    if (!Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('Supabase maxResponseBytes must be positive');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the select query. */ selectRows(): SelectRowsOperation { return new SelectRowsOperation(this); }
  /** Bind the repeat-safe upsert mutation. */ upsertRows(): UpsertRowsOperation { return new UpsertRowsOperation(this); }

  /** Make one exchange, applying the Go provider status and body rules. */
  async exchange(call: Call, operation: string, method: 'GET' | 'POST', path: string, query: URLSearchParams, body?: Uint8Array): Promise<Exchange> {
    let token: string;
    try { token = (await this.credentials.resolve(call, call.context.cancellationSignal)).apiKey.reveal(); }
    catch { return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credentials are unavailable') }; }
    if (!token || !isHeaderSafeCredential(token)) return { branch: 'defect', failure: failure(operation, 'AUTHENTICATION', 'connection credential is invalid') };
    const sortedQuery = [...query.entries()].sort(([left], [right]) => Buffer.compare(Buffer.from(left), Buffer.from(right)));
    const url = `${this.baseUrl}${path}?${sortedQuery.map(([key, value]) => `${goQueryEscape(key)}=${goQueryEscape(value)}`).join('&')}`;
    let response: ProviderResponse;
    try {
      response = await this.send({ url, method,
        headers: { Accept: 'application/json', 'User-Agent': 'superdurable-supabase/0.1', apikey: token,
          ...(method === 'POST' ? { Prefer: 'resolution=merge-duplicates,return=representation' } : {}),
          ...(body ? { 'Content-Type': 'application/json' } : {}) },
        ...(body ? { body } : {}), maxResponseBytes: this.maxResponseBytes,
        timeoutMs: this.timeoutMs, signal: call.context.cancellationSignal,
        addressPolicy: this.baseUrl.startsWith('https://') ? 'publicHTTPS' : 'privateNetworkHTTP' });
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) {
        const statusOutcome = classifyStatus(operation, error.status, error.headers?.['retry-after']);
        if (statusOutcome) return statusOutcome;
        return { branch: 'invalidResponse', failure: failure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeded the configured limit') };
      }
      return { failure: failure(operation, 'AVAILABILITY', 'provider request failed') };
    }
    const statusOutcome = classifyStatus(operation, response.status, response.headers['retry-after']);
    if (statusOutcome) return statusOutcome;
    let parsed: unknown;
    try { parsed = parseProviderJSON(response.body); }
    catch { return { branch: 'invalidResponse', failure: failure(operation, 'PROTOCOL', 'provider returned malformed JSON') }; }
    if (!Array.isArray(parsed)) return { branch: 'invalidResponse', failure: failure(operation, 'PROTOCOL', 'provider returned an incomplete response') };
    return { body: response.body };
  }
}

function classifyStatus(operation: string, status: number, retryHeader: string | string[] | undefined): Exchange | undefined {
  if (status === 429 || status === 408 || status >= 500) return { failure: failure(operation, status === 429 ? 'RATE_LIMIT' : 'AVAILABILITY', 'provider is temporarily unavailable'), retryAfterMs: parseRetryAfter(Array.isArray(retryHeader) ? retryHeader[0] ?? '' : retryHeader ?? '') };
  if (status === (operation === 'selectRows' ? 200 : 201) || (operation === 'upsertRows' && status === 200)) return undefined;
  const kind = status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : status === 400 || status === 422 ? 'VALIDATION' : 'PROVIDER_REJECTION';
  return { branch: 'providerRejected', failure: failure(operation, kind, `provider rejected the request with HTTP ${status}`) };
}

/** Build a secret-safe connector failure. */
export function failure(operation: string, kind: Failure['kind'], message: string): Failure { return { kind, provider: 'supabase', operation, message }; }
/** Convert one classified exchange to the query attempt. */
export function queryExchange(exchange: Exchange): QueryAttempt<SelectRowsOutput> {
  if ('body' in exchange) return newQueryBranch('selected', { data: parseProviderJSON(exchange.body) }, zeroReceipt());
  return exchange.branch ? newQueryBranch(exchange.branch, emptyOutput, zeroReceipt(), exchange.failure) : newQueryRetry(exchange.failure, exchange.retryAfterMs);
}
/** Convert one classified exchange to the mutation attempt. */
export function mutationExchange(exchange: Exchange): MutationAttempt<UpsertRowsOutput> {
  if ('body' in exchange) return newMutationBranch('upserted', { data: parseProviderJSON(exchange.body) }, zeroReceipt());
  return exchange.branch ? newMutationBranch(exchange.branch, emptyOutput, zeroReceipt(), exchange.failure) : newMutationRetry(exchange.failure, exchange.retryAfterMs);
}

function parseProviderJSON(body: Uint8Array): unknown {
  const rawJSON = (JSON as typeof JSON & { rawJSON(text: string): unknown }).rawJSON;
  return JSON.parse(Buffer.from(body).toString('utf8'), (_key, parsedValue: unknown, context?: { source?: string }) =>
    typeof parsedValue === 'number' && context?.source ? rawJSON(context.source) : parsedValue) as unknown;
}

function goQueryEscape(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%20/g, '+');
}
