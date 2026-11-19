// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListFormsPageDefinition } from './generated/definitions.js';
import type { Config, Credentials, ListFormsPageInput, ListFormsPageOutput, FormMetadata } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface JinshujuClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }

/** Resolve Access Token credentials for each form-list query. */
export class JinshujuClient {
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate local settings without contacting Jinshuju. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: JinshujuClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('Jinshuju configuration and credentials are required');
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > (16 << 20)) throw new TypeError('Jinshuju request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }

  /** Bind the form-list Query. */ listFormsPage(): ListFormsPageOperation { return new ListFormsPageOperation(this); }

  /** Send one bounded form-list request. */
  async getForms(call: Call, cursor: string): Promise<{ response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let token = '';
    try { const credential = await this.credentials.resolve(call, call.context.cancellationSignal); token = credential.accessToken instanceof SecretString ? credential.accessToken.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!token || token.length > 1024 || /[^\x21-\x7e]/.test(token)) return { error: 'credential', retryAfterMs: 0 };
    const request: ProviderRequest = {
      url: 'https://jinshuju.net/api/v1/forms' + (cursor ? '?next=' + encodeURIComponent(cursor) : ''),
      method: 'GET', headers: { Authorization: 'Bearer ' + token, Accept: 'application/json' }, timeoutMs: this.timeoutMs,
      maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal,
    };
    try {
      const response = await this.send(request);
      return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
    } catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
      return { error: 'transport', retryAfterMs: 0 };
    }
  }
}

/** Read one page of form names and identifiers without submissions. */
export class ListFormsPageOperation implements Query<ListFormsPageInput, ListFormsPageOutput> {
  /** Bind the provider client. */ constructor(private readonly client: JinshujuClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListFormsPageDefinition; }
  /** Classify one provider response using the Go connector contract. */
  async invoke(call: Call, input: ListFormsPageInput): Promise<QueryAttempt<ListFormsPageOutput>> {
    if (!input || typeof input.next !== 'string' || !validCursor(input.next)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.getForms(call, input.next);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const data = wire as { total?: unknown; count?: unknown; data?: unknown; next?: unknown };
    if (!Number.isSafeInteger(data.total) || !Number.isSafeInteger(data.count) || (data.total as number) < 0 || (data.count as number) < 0 || (data.count as number) > 50 || (data.total as number) < (data.count as number) || !Array.isArray(data.data) || data.data.length !== data.count || (data.next != null && typeof data.next !== 'string') || !validCursor((data.next ?? '') as string)) return branch('invalidResponse', 'PROTOCOL');
    const forms: FormMetadata[] = [];
    for (const rawForm of data.data) {
      if (!rawForm || typeof rawForm !== 'object' || Array.isArray(rawForm)) return branch('invalidResponse', 'PROTOCOL');
      const form = rawForm as { name?: unknown; token?: unknown };
      if (typeof form.name !== 'string' || !form.name || typeof form.token !== 'string' || !form.token) return branch('invalidResponse', 'PROTOCOL');
      forms.push({ name: form.name, token: form.token });
    }
    return newQueryBranch('listed', { total: data.total as number, count: data.count as number, forms, next: (data.next ?? '') as string });
  }
}

function validCursor(value: string): boolean { return value.length <= 256 && !/[^\x21-\x7e]/.test(value); }
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'jinshuju', operation: 'listFormsPage', kind, message: 'Jinshuju form-list query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<ListFormsPageOutput> { return newQueryBranch(name, { total: 0, count: 0, forms: [], next: '' }, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<ListFormsPageOutput> { return newQueryRetry(failure(kind), delay); }
