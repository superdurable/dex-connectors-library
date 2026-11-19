// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ReauthorizationRequiredError, SecretString, newQueryBranch, newQueryRetry, resolveCredential, resolveCredentialAfterRejection, type Call, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { ListCustomerFieldsDefinition } from './generated/definitions.js';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Config, CredentialSource, Credentials, CustomerFields, ListCustomerFieldsInput } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface OkkiClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
const empty: CustomerFields = { fields: [] };
interface Exchange { response?: ProviderResponse; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number; }
/** Resolve renewable OKKI credentials for each customer-field query. */
export class OkkiClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;
  private readonly refreshDriver: ManifestCredentialRefreshDriver<Credentials>;
  /** Validate settings without contacting OKKI. */
  constructor(config: Config, private readonly credentials: CredentialSource, options: OkkiClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('OKKI settings and credentials are required');
    const url = new URL(config.baseUrl || 'https://api-sandbox.xiaoman.cn');
    this.timeoutMs = config.timeoutMs || 15_000;
    this.maxResponseBytes = config.maxResponseBytes || 1_048_576;
    if (url.protocol !== 'https:' || (url.pathname !== '/' && url.pathname !== '') || url.search || url.hash || !Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0 || this.maxResponseBytes > 16 * 1024 * 1024) throw new TypeError('OKKI settings are invalid');
    this.baseUrl = url.origin;
    this.send = options.send ?? sendProviderRequest;
    this.refreshDriver = new ManifestCredentialRefreshDriver<Credentials>({ providerName: 'OKKI', auth: Authorization, credentialFields: CredentialFields });
  }
  /** Bind the customer-field Query. */ listCustomerFields(): ListCustomerFieldsOperation { return new ListCustomerFieldsOperation(this); }
  /** Send one bounded request, renewing a rejected token once when supported. */
  async request(call: Call): Promise<Exchange> {
    let resolved: Credentials;
    try { resolved = await resolveCredential(this.credentials, call, this.refreshDriver, call.context.cancellationSignal); }
    catch (error) { return { error: error instanceof ReauthorizationRequiredError ? 'credential' : 'transport', retryAfterMs: 0 }; }
    for (let attempt = 0; attempt < 2; attempt++) {
      const token = resolved.accessToken instanceof SecretString ? resolved.accessToken.reveal() : '';
      if (!token || token.length > 8192 || /[^\x21-\x7e]/.test(token)) return { error: 'credential', retryAfterMs: 0 };
      const request: ProviderRequest = { url: `${this.baseUrl}/v1/company/fields?type=company`, method: 'GET', headers: { Accept: 'application/json', Authorization: token }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
      let response: ProviderResponse;
      try { response = await this.send(request); }
      catch (error) {
        if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: 0 };
        return { error: 'transport', retryAfterMs: 0 };
      }
      if (response.status === 401 && attempt === 0) {
        try { resolved = await resolveCredentialAfterRejection(this.credentials, call, this.refreshDriver, call.context.cancellationSignal); continue; }
        catch (error) { if (!(error instanceof ReauthorizationRequiredError)) return { error: 'transport', retryAfterMs: 0 }; }
      }
      return { response, error: response.body.length > this.maxResponseBytes ? 'tooLarge' : undefined, retryAfterMs: parseRetryAfter(header(response.headers, 'retry-after')) };
    }
    return { error: 'transport', retryAfterMs: 0 };
  }
}
/** List bounded customer field definitions. */
export class ListCustomerFieldsOperation implements Query<ListCustomerFieldsInput, CustomerFields> {
  /** Bind the provider client. */ constructor(private readonly client: OkkiClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return ListCustomerFieldsDefinition; }
  /** Classify the provider response using the Go contract. */
  async invoke(call: Call, _input: ListCustomerFieldsInput): Promise<QueryAttempt<CustomerFields>> {
    void _input;
    const exchange = await this.client.request(call);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('TRANSPORT');
    const { status, body } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    let wire: unknown;
    try { wire = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(body)) as unknown; } catch { return branch('invalidResponse', 'PROTOCOL'); }
    if (!wire || typeof wire !== 'object' || Array.isArray(wire)) return branch('invalidResponse', 'PROTOCOL');
    const record = wire as Record<string, unknown>;
    if (!Number.isInteger(record.code)) return branch('invalidResponse', 'PROTOCOL');
    if (Number(record.code) >= 500) return retry('AVAILABILITY');
    if (record.code !== 200) return branch('providerRejected', 'PROVIDER_REJECTION');
    if (!Array.isArray(record.data) || record.data.length > 500) return branch('invalidResponse', 'PROTOCOL');
    const fields: NonNullable<CustomerFields['fields']> = [];
    for (const item of record.data) {
      if (!item || typeof item !== 'object' || Array.isArray(item)) return branch('invalidResponse', 'PROTOCOL');
      const value = item as Record<string, unknown>;
      const id = typeof value.id === 'string' ? value.id : Number.isSafeInteger(value.id) ? String(value.id) : undefined;
      if (id === undefined) return branch('invalidResponse', 'PROTOCOL');
      if (!id) continue;
      if (id.length > 128 || typeof value.name !== 'string' || !value.name || value.name.length > 512 || !Number.isInteger(value.field_type) || Number(value.field_type) <= 0) return branch('invalidResponse', 'PROTOCOL');
      fields.push({ id, name: value.name, fieldType: Number(value.field_type) });
    }
    return newQueryBranch('listed', { fields });
  }
}
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'okki', operation: 'listCustomerFields', kind, message: 'OKKI customer-field query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<CustomerFields> { return newQueryBranch(name, empty, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<CustomerFields> { return newQueryRetry(failure(kind), delay); }
